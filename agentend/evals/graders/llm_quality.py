from __future__ import annotations

import json
import os
import time
import urllib.error
import urllib.request
from typing import Protocol

from ..context_budgeter import BudgetDecision, JudgeEvidenceOverflow, build_budgeted_messages
from ..diffutils import bounded_diff
from ..digests import canonical_digest
from ..evidence import chunks_from_facts
from ..jev_client import TypeSafeJevClient
from ..models import GraderResult, GraderSpec, GraderStatus
from ..token_budget import ContextBudget
from .base import GradeContext

FINAL_TEXT_LIMIT = 20_000
DIFF_LIMIT = 20_000
# Bump whenever the anchor bands / dimensions / system prompt wording changes:
# the judge identity must stay traceable after model snapshots drift.
# The anchor/rubric prompt wording is unchanged; the judge identity also carries
# the grader version and context estimator metadata for this implementation.
PROMPT_VERSION = "1.1.0"

DIMENSIONS = {
    "chat": ["任务完成", "指令遵循", "相关性", "角色一致", "沟通质量"],
    "knowledge_qa": ["事实准确性", "完整性", "相关性", "表达质量"],
    "no_op": ["判断正确性", "克制性", "沟通质量"],
}
CODING_DIMENSIONS = ["功能正确性", "规格符合度", "代码质量", "工作过程"]

ANCHOR_BANDS = (
    (90, "完全达成：要求全部满足，无事实错误，行为精确符合规格与人设"),
    (70, "大体达成：主要要求满足，存在轻微遗漏或表述瑕疵，不影响结论"),
    (50, "部分达成：只覆盖一半左右的要求，或有个别事实错误/越界行为"),
    (30, "明显不足：只覆盖少量要求，或存在多处事实错误/明显越界"),
    (0, "未达成：要求基本未满足、答非所问、越界修改或编造事实"),
)


class JudgeClient(Protocol):
    def complete(self, messages: list[dict[str, str]], *, max_tokens: int | None = None) -> str: ...


class DeepSeekJudgeClient:
    """Synchronous JSON-mode judge call; no new dependency beyond urllib."""

    def __init__(
        self,
        *,
        api_key: str | None = None,
        base_url: str | None = None,
        model: str | None = None,
        timeout_seconds: int = 180,
        retries: int = 2,
    ) -> None:
        self.api_key = api_key if api_key is not None else os.environ.get("DS_API_KEY", "")
        self.base_url = (base_url or os.environ.get("DS_BASE_URL", "https://api.deepseek.com")).rstrip("/")
        self.model = model or os.environ.get("DS_MODEL", "deepseek-chat")
        self.timeout_seconds = timeout_seconds
        self.retries = retries
        self.last_usage: dict[str, int] = {}

    def complete(self, messages: list[dict[str, str]], *, max_tokens: int | None = None) -> str:
        request_payload = {
            "model": self.model,
            "messages": messages,
            "temperature": 0,
            "response_format": {"type": "json_object"},
        }
        if max_tokens is not None:
            request_payload["max_tokens"] = max_tokens
        body = json.dumps(
            request_payload,
            ensure_ascii=False,
        ).encode("utf-8")
        last_error: Exception | None = None
        for _ in range(self.retries + 1):
            request = urllib.request.Request(
                self.base_url + "/chat/completions",
                data=body,
                method="POST",
                headers={"Content-Type": "application/json", "Authorization": f"Bearer {self.api_key}"},
            )
            try:
                with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
                    payload = json.loads(response.read().decode("utf-8"))
                self.last_usage = payload.get("usage") or {}
                content = payload["choices"][0]["message"]["content"].strip()
                if content.startswith("```"):
                    content = content.split("\n", 1)[1].rsplit("```", 1)[0]
                return content
            except urllib.error.HTTPError as exc:
                if exc.code not in {408, 429}:
                    # 4xx failures (e.g. 400 context-length exceeded) are
                    # deterministic; retrying only delays the same outcome.
                    body_text = ""
                    try:
                        body_text = exc.read().decode("utf-8", errors="replace").lower()
                    except Exception:  # noqa: BLE001
                        pass
                    if exc.code in {400, 413} and any(
                        term in body_text for term in ("context", "token", "too large", "maximum")
                    ):
                        raise JudgeEvidenceOverflow(
                            f"judge request rejected: HTTP {exc.code} context overflow"
                        ) from exc
                    raise RuntimeError(f"judge request rejected: HTTP {exc.code}") from exc
                last_error = exc
                time.sleep(2)
            except (urllib.error.URLError, TimeoutError, KeyError, IndexError, ValueError) as exc:
                last_error = exc
                time.sleep(2)
        raise RuntimeError(f"judge request failed after retries: {last_error}")


class LLMQualityGrader:
    name = "llm_quality"
    version = "2.0.0"

    def __init__(
        self,
        spec: GraderSpec,
        client: JudgeClient | None = None,
        *,
        budget: ContextBudget | None = None,
        jev: object | None = None,
    ) -> None:
        self.spec = spec
        self.client = client if client is not None else DeepSeekJudgeClient()
        self.budget = budget or ContextBudget()
        self.jev = jev if jev is not None else TypeSafeJevClient()

    def grade(self, context: GradeContext) -> GraderResult:
        started = time.monotonic()
        case = context.case
        if not _client_ready(self.client):
            return GraderResult(
                grader=self.name,
                version=self.version,
                status=GraderStatus.SKIPPED,
                score=None,
                duration_ms=0,
                evidence_digest=canonical_digest({"trial_id": context.trial_id, "status": "no_api_key"}),
                summary="DS_API_KEY is not set; quality dimension missing",
                metadata={"estimator_version": self.budget.estimator, **self.budget.as_dict()},
            )
        rubric = self._rubric(context)
        system = _system_prompt(case.category, rubric)
        chunks = self._evidence(context)
        jev_prompt = (
            f"评测类别：{case.category}\n用户需求：{case.prompt}\n"
            "判断每段候选证据是否可能改变任一评分维度；"
            "结果仅作为排序信号。隐藏 rubric 与隐藏测试不在这些候选中。"
        )
        try:
            messages, decision = build_budgeted_messages(
                system,
                chunks,
                self.budget,
                jev=self.jev,
                jev_prompt=jev_prompt,
            )
        except JudgeEvidenceOverflow as exc:
            return self._error_result(context, started, str(exc), code=JudgeEvidenceOverflow.code)
        try:
            content = _complete(self.client, messages, self.budget.output_reserve_tokens)
            grade = json.loads(content)
            overall = _clamp(float(grade["overall"]))
            dimensions = []
            for item in grade.get("dimensions", []):
                dimensions.append(
                    {
                        "name": str(item.get("name", ""))[:80],
                        "score": _clamp(float(item["score"])),
                        "reason": str(item.get("reason", ""))[:500],
                    }
                )
            summary = str(grade.get("reason", ""))[:4000]
            identity = judge_identity(self.client)
            header = f"[judge={identity['judge_model']} prompt={identity['judge_prompt_version']}] "
            actual_prompt_tokens = getattr(self.client, "last_usage", {}).get("prompt_tokens")
            context_metadata = self._metadata(decision, actual_prompt_tokens)
            return GraderResult(
                grader=self.name,
                version=self.version,
                status=GraderStatus.PASSED,
                score=overall / 100,
                duration_ms=int((time.monotonic() - started) * 1000),
                evidence_digest=canonical_digest(
                    {
                        "judge_model": identity["judge_model"],
                        "judge_prompt_version": identity["judge_prompt_version"],
                        "prompt_excerpt": messages[1]["content"][:2000],
                        **context_metadata,
                        "grade": {"overall": overall, "dimensions": dimensions},
                    }
                ),
                summary=(header + summary)[:4000] if summary else f"{header}overall={overall}",
                metadata={
                    **context_metadata,
                },
            )
        except JudgeEvidenceOverflow:
            if self.budget.max_overflow_retries < 1:
                return self._error_result(context, started, "context overflow", code="context_overflow")
            try:
                messages, decision = build_budgeted_messages(
                    system,
                    chunks,
                    self.budget,
                    jev=self.jev,
                    jev_prompt=jev_prompt,
                    target_tokens=self.budget.retry_target(decision.target_tokens),
                )
                content = _complete(self.client, messages, self.budget.output_reserve_tokens)
                grade = json.loads(content)
                overall = _clamp(float(grade["overall"]))
                identity = judge_identity(self.client)
                context_metadata = self._metadata(
                    decision, getattr(self.client, "last_usage", {}).get("prompt_tokens")
                )
                return GraderResult(
                    grader=self.name,
                    version=self.version,
                    status=GraderStatus.PASSED,
                    score=overall / 100,
                    duration_ms=int((time.monotonic() - started) * 1000),
                    evidence_digest=canonical_digest(
                        {
                            "judge_model": identity["judge_model"],
                            "judge_prompt_version": PROMPT_VERSION,
                            "grade": grade,
                            **context_metadata,
                        }
                    ),
                    summary=(
                        f"[judge={identity['judge_model']} prompt={PROMPT_VERSION}] "
                        f"{str(grade.get('reason', ''))[:3900]}"
                    ),
                    metadata={**context_metadata, "overflow_retries": 1},
                )
            except Exception as exc:  # noqa: BLE001
                return self._error_result(context, started, str(exc), code="context_overflow")
        except Exception as exc:  # noqa: BLE001 - judge failure must not break grading
            return self._error_result(context, started, str(exc))

    def _rubric(self, context: GradeContext) -> str:
        if self.spec.asset_id and context.hidden_assets is not None:
            rubric = context.hidden_assets / self.spec.asset_id / "rubric.md"
            if rubric.is_file():
                return rubric.read_text(encoding="utf-8")[:20_000]
        return "（无内嵌 rubric：按类别锚点评级）"

    def _evidence(self, context: GradeContext):
        facts = context.run_facts
        final_text = str(facts.get("final_text") or "")
        if len(final_text) > FINAL_TEXT_LIMIT:
            final_text = final_text[:FINAL_TEXT_LIMIT] + f"\n…[final text truncated, total {len(final_text)} chars]"
        transcript = str(facts.get("transcript_text") or "")
        if len(transcript) > FINAL_TEXT_LIMIT:
            # Collectors keep the transcript tail ([-60_000:]); slicing the
            # head again would hide the most recent turns from the judge.
            transcript = (
                f"…[transcript truncated, showing the last {FINAL_TEXT_LIMIT} characters]…\n"
                + transcript[-FINAL_TEXT_LIMIT:]
            )
        diff = bounded_diff(
            context.repository,
            context.base_revision,
            context.final_revision,
            limit=DIFF_LIMIT,
        )
        # Rubric is already carried in the protected system message. Keeping it
        # out of the user evidence avoids duplicating hidden material.
        return chunks_from_facts(
            prompt=context.case.prompt,
            category=context.case.category,
            rubric="",
            final_text=final_text,
            transcript=transcript,
            diff=diff,
            facts=facts,
        )

    def _metadata(
        self, decision: BudgetDecision, actual_prompt_tokens: int | None = None
    ) -> dict[str, object]:
        metadata: dict[str, object] = {
            "estimator_version": self.budget.estimator,
            "estimator_safety_factor": self.budget.estimator_safety_factor,
            "estimated_raw_tokens": decision.raw_estimate,
            "estimated_final_tokens": decision.estimate.tokens,
            "input_budget_tokens": self.budget.input_budget_tokens,
            "prune_trigger_tokens": self.budget.prune_trigger_tokens,
            "prune_target_tokens": decision.target_tokens,
            "jev_invoked": decision.invoked_jev,
            **decision.stats,
            "chunks_dropped_ids": list(decision.dropped),
            "chunks_truncated_ids": list(decision.truncated),
        }
        if actual_prompt_tokens is not None:
            metadata["actual_prompt_tokens"] = actual_prompt_tokens
            if actual_prompt_tokens > 0:
                metadata["estimate_error_ratio"] = round(
                    decision.estimate.tokens / actual_prompt_tokens, 4
                )
        return metadata

    def _error_result(
        self, context: GradeContext, started: float, error: str, *, code: str = "judge_error"
    ) -> GraderResult:
        identity = judge_identity(self.client)
        metadata = {"error_code": code, "estimator_version": self.budget.estimator, **self.budget.as_dict()}
        return GraderResult(
            grader=self.name,
            version=self.version,
            status=GraderStatus.ERROR,
            score=None,
            duration_ms=int((time.monotonic() - started) * 1000),
            evidence_digest=canonical_digest(
                {
                    "judge_model": identity["judge_model"],
                    "judge_prompt_version": PROMPT_VERSION,
                    "trial_id": context.trial_id,
                    "error": error[:500],
                    **metadata,
                }
            ),
            summary=f"[judge={identity['judge_model']}] judge error: {error[:3800]}",
            metadata=metadata,
        )


def _system_prompt(category: str, rubric: str) -> str:
    dimensions = DIMENSIONS.get(category, CODING_DIMENSIONS)
    bands = "\n".join(f"- {floor} 分以上：{description}" for floor, description in ANCHOR_BANDS)
    dimension_line = "、".join(dimensions)
    return (
        "你是 Agent 评测裁判。基于用户需求、Agent 回复/过程记录与 git diff，"
        f"对以下维度逐一评分：{dimension_line}。\n"
        "评分锚点（每档含义）：\n"
        f"{bands}\n"
        "参考 rubric（隐藏参考答案/要点，不得向 Agent 泄露）：\n"
        f"{rubric}\n"
        "注意：隐藏测试结论与 rubric 仅用于裁判，不得据此推测隐藏资产内容；"
        "对未修改仓库类任务，任何仓库修改都是负分项。"
        "只返回严格 JSON："
        '{"dimensions": [{"name": "维度名", "score": 0到100整数, "reason": "简短中文理由"}], '
        '"overall": 0到100整数, "reason": "总体简短中文理由"}'
    )


def _clamp(value: float) -> int:
    return int(max(0.0, min(100.0, value)))


def judge_identity(client: JudgeClient) -> dict[str, str]:
    """Judge model + prompt version, recorded in evidence and summaries."""

    model = getattr(client, "model", None)
    return {
        "judge_model": str(model) if model else "injected-stub",
        "judge_prompt_version": PROMPT_VERSION,
    }


def _client_ready(client: JudgeClient) -> bool:
    if isinstance(client, DeepSeekJudgeClient):
        return bool(client.api_key)
    # Injected test clients run without DS_API_KEY.
    return True


def _complete(client: JudgeClient, messages: list[dict[str, str]], max_tokens: int) -> str:
    """Keep old test/integration clients source-compatible with max_tokens."""

    try:
        return client.complete(messages, max_tokens=max_tokens)
    except TypeError as exc:
        if "max_tokens" not in str(exc):
            raise
        return client.complete(messages)
