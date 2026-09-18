from __future__ import annotations

import json
import os
import time
import urllib.error
import urllib.request
from typing import Protocol

from ..diffutils import bounded_diff
from ..digests import canonical_digest
from ..models import GraderResult, GraderSpec, GraderStatus
from .base import GradeContext

FINAL_TEXT_LIMIT = 20_000
DIFF_LIMIT = 20_000
# Bump whenever the anchor bands / dimensions / system prompt wording changes:
# the judge identity must stay traceable after model snapshots drift.
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
    def complete(self, messages: list[dict[str, str]]) -> str: ...


class DeepSeekJudgeClient:
    """Synchronous JSON-mode judge call; no new dependency beyond urllib."""

    def __init__(self, *, api_key: str | None = None, base_url: str | None = None,
                 model: str | None = None, timeout_seconds: int = 180, retries: int = 2) -> None:
        self.api_key = api_key if api_key is not None else os.environ.get("DS_API_KEY", "")
        self.base_url = (base_url or os.environ.get("DS_BASE_URL", "https://api.deepseek.com")).rstrip("/")
        self.model = model or os.environ.get("DS_MODEL", "deepseek-chat")
        self.timeout_seconds = timeout_seconds
        self.retries = retries

    def complete(self, messages: list[dict[str, str]]) -> str:
        body = json.dumps(
            {
                "model": self.model,
                "messages": messages,
                "temperature": 0,
                "response_format": {"type": "json_object"},
            },
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
                content = payload["choices"][0]["message"]["content"].strip()
                if content.startswith("```"):
                    content = content.split("\n", 1)[1].rsplit("```", 1)[0]
                return content
            except urllib.error.HTTPError as exc:
                if exc.code not in {408, 429}:
                    # 4xx failures (e.g. 400 context-length exceeded) are
                    # deterministic; retrying only delays the same outcome.
                    raise RuntimeError(f"judge request rejected: HTTP {exc.code}") from exc
                last_error = exc
                time.sleep(2)
            except (urllib.error.URLError, TimeoutError, KeyError, IndexError, ValueError) as exc:
                last_error = exc
                time.sleep(2)
        raise RuntimeError(f"judge request failed after retries: {last_error}")


class LLMQualityGrader:
    name = "llm_quality"
    version = "1.0.0"

    def __init__(self, spec: GraderSpec, client: JudgeClient | None = None) -> None:
        self.spec = spec
        self.client = client if client is not None else DeepSeekJudgeClient()

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
            )
        rubric = self._rubric(context)
        evidence = self._evidence(context)
        messages = [
            {"role": "system", "content": _system_prompt(case.category, rubric)},
            {"role": "user", "content": evidence},
        ]
        try:
            content = self.client.complete(messages)
            grade = json.loads(content)
            overall = _clamp(float(grade["overall"]))
            dimensions = []
            for item in grade.get("dimensions", []):
                dimensions.append(
                    {"name": str(item.get("name", ""))[:80], "score": _clamp(float(item["score"])),
                     "reason": str(item.get("reason", ""))[:500]}
                )
            summary = str(grade.get("reason", ""))[:4000]
            identity = judge_identity(self.client)
            header = f"[judge={identity['judge_model']} prompt={identity['judge_prompt_version']}] "
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
                        "prompt_excerpt": evidence[:2000],
                        "grade": {"overall": overall, "dimensions": dimensions},
                    }
                ),
                summary=(header + summary)[:4000] if summary else f"{header}overall={overall}",
            )
        except Exception as exc:  # noqa: BLE001 - judge failure must not break grading
            identity = judge_identity(self.client)
            return GraderResult(
                grader=self.name,
                version=self.version,
                status=GraderStatus.ERROR,
                score=None,
                duration_ms=int((time.monotonic() - started) * 1000),
                evidence_digest=canonical_digest(
                    {
                        "judge_model": identity["judge_model"],
                        "judge_prompt_version": identity["judge_prompt_version"],
                        "trial_id": context.trial_id,
                        "error": str(exc)[:500],
                    }
                ),
                summary=f"[judge={identity['judge_model']}] judge error: {str(exc)[:3800]}",
            )

    def _rubric(self, context: GradeContext) -> str:
        if self.spec.asset_id and context.hidden_assets is not None:
            rubric = context.hidden_assets / self.spec.asset_id / "rubric.md"
            if rubric.is_file():
                return rubric.read_text(encoding="utf-8")[:20_000]
        return "（无内嵌 rubric：按类别锚点评级）"

    def _evidence(self, context: GradeContext) -> str:
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
        parts = [
            f"用户需求：\n{context.case.prompt}",
            f"类别：{context.case.category}",
        ]
        if final_text:
            parts.append(f"Agent 最终回复：\n{final_text}")
        if transcript:
            parts.append(f"Agent 过程记录（截断）：\n{transcript}")
        if diff.strip():
            parts.append(f"Git Diff（截断）：\n{diff}")
        if not final_text and not transcript and not diff.strip():
            parts.append("（Agent 未产出可评估文本与变更）")
        return "\n\n".join(parts)


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
