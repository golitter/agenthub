from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from evals.graders.base import GradeContext
from evals.graders.llm_quality import DeepSeekJudgeClient, LLMQualityGrader
from evals.models import CaseManifest, GraderSpec, GraderStatus

DIGEST = "sha256:" + "0" * 64


class FakeJudgeClient:
    def __init__(self, content: str) -> None:
        self.content = content
        self.messages: list[list[dict[str, str]]] = []

    def complete(self, messages: list[dict[str, str]]) -> str:
        self.messages.append(messages)
        return self.content


def _git(repository: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repository), *args], check=True, capture_output=True, text=True
    ).stdout.strip()


@pytest.fixture()
def context(tmp_path: Path) -> GradeContext:
    repository = tmp_path / "repo"
    repository.mkdir()
    _git(repository, "init", "-q", "-b", "main")
    _git(repository, "config", "user.email", "e@x")
    _git(repository, "config", "user.name", "t")
    (repository / "solution.py").write_text("VALUE = 1\n", encoding="utf-8")
    _git(repository, "add", ".")
    _git(repository, "commit", "-q", "-m", "base")
    base = _git(repository, "rev-parse", "HEAD")

    hidden = tmp_path / "hidden" / "quality-v1"
    hidden.mkdir(parents=True)
    (hidden / "rubric.md").write_text("# 要点\n- 必须提到 Alpha 项目启动于 2023 年\n", encoding="utf-8")

    case = CaseManifest(
        schema_version=1,
        case_id="chat-001",
        category="chat",
        difficulty="easy",
        fixture={"source": "fixtures/a.bundle", "sha256": DIGEST, "base_ref": "main"},
        prompt="介绍你自己",
        scope={"allowed_paths": ["**"], "forbidden_paths": []},
        graders=[{"type": "llm_quality", "asset_id": "quality-v1"}],
        expected={"require_change": False},
    )
    return GradeContext(
        trial_id="trial-1",
        case=case,
        repository=repository,
        base_revision=base,
        final_revision=base,
        hidden_assets=tmp_path / "hidden",
        run_facts={"final_text": "我是 Nova，2023 年启动 Alpha 项目的助手。", "transcript_text": None},
    )


def test_judge_normal_parse_scores_overall(context: GradeContext) -> None:
    payload = {
        "dimensions": [{"name": "任务完成", "score": 85, "reason": "覆盖要点"}],
        "overall": 85,
        "reason": "总体不错",
    }
    client = FakeJudgeClient(json.dumps(payload, ensure_ascii=False))
    graded = LLMQualityGrader(GraderSpec(type="llm_quality", asset_id="quality-v1"), client=client).grade(context)
    assert graded.status == GraderStatus.PASSED
    assert graded.score == pytest.approx(0.85)
    # FakeJudgeClient exposes no .model -> identity falls back to "injected-stub".
    assert graded.summary.startswith("[judge=injected-stub prompt=1.1.0] 总体不错")


def test_judge_identity_names_model_and_prompt_version() -> None:
    from evals.graders.llm_quality import PROMPT_VERSION, judge_identity

    class NamedClient(FakeJudgeClient):
        model = "deepseek-chat"

    assert judge_identity(NamedClient("{}")) == {
        "judge_model": "deepseek-chat",
        "judge_prompt_version": PROMPT_VERSION,
    }
    assert judge_identity(FakeJudgeClient("{}"))["judge_model"] == "injected-stub"


def test_judge_summary_header_carries_identity(context: GradeContext) -> None:
    class NamedClient(FakeJudgeClient):
        model = "deepseek-chat"

    payload = {"dimensions": [], "overall": 70, "reason": "ok"}
    graded = LLMQualityGrader(
        GraderSpec(type="llm_quality"), client=NamedClient(json.dumps(payload))
    ).grade(context)
    assert graded.summary.startswith("[judge=deepseek-chat prompt=")
    # error path keeps at least the model identity
    errored = LLMQualityGrader(
        GraderSpec(type="llm_quality"), client=NamedClient("not-json")
    ).grade(context)
    assert errored.status == GraderStatus.ERROR
    assert errored.summary.startswith("[judge=deepseek-chat]")


def test_judge_clamps_out_of_range_scores(context: GradeContext) -> None:
    for overall, expected in ((150, 1.0), (-5, 0.0)):
        payload = {"dimensions": [], "overall": overall, "reason": "x"}
        graded = LLMQualityGrader(
            GraderSpec(type="llm_quality"), client=FakeJudgeClient(json.dumps(payload))
        ).grade(context)
        assert graded.status == GraderStatus.PASSED
        assert graded.score == expected


def test_judge_malformed_json_becomes_error_not_zero(context: GradeContext) -> None:
    graded = LLMQualityGrader(
        GraderSpec(type="llm_quality"), client=FakeJudgeClient("这不是 JSON")
    ).grade(context)
    assert graded.status == GraderStatus.ERROR
    assert graded.score is None
    assert "judge error" in graded.summary


def test_missing_api_key_skips_quality_dimension(context: GradeContext, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("DS_API_KEY", raising=False)
    graded = LLMQualityGrader(GraderSpec(type="llm_quality")).grade(context)
    assert graded.status == GraderStatus.SKIPPED
    assert graded.score is None
    assert "DS_API_KEY" in graded.summary


def test_deepseek_client_ready_depends_on_key(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("DS_API_KEY", raising=False)
    assert DeepSeekJudgeClient().api_key == ""
    monkeypatch.setenv("DS_API_KEY", "sk-test")
    assert DeepSeekJudgeClient().api_key == "sk-test"


def test_prompt_carries_rubric_category_and_evidence(context: GradeContext) -> None:
    payload = {"dimensions": [], "overall": 50, "reason": "中"}
    client = FakeJudgeClient(json.dumps(payload, ensure_ascii=False))
    LLMQualityGrader(GraderSpec(type="llm_quality", asset_id="quality-v1"), client=client).grade(context)
    system, user = client.messages[0]
    assert "Alpha 项目启动于 2023 年" in system["content"]  # hidden rubric reached the judge
    assert "任务完成" in system["content"]  # chat dimensions selected
    assert "介绍你自己" in user["content"]
    assert "类别：chat" in user["content"]
    assert "Nova" in user["content"]
    assert "对未修改仓库类任务" in system["content"]


def test_no_output_evidence_is_still_graded(context: GradeContext) -> None:
    import dataclasses

    context = dataclasses.replace(context, run_facts={})
    payload = {"dimensions": [], "overall": 10, "reason": "无产出"}
    client = FakeJudgeClient(json.dumps(payload, ensure_ascii=False))
    graded = LLMQualityGrader(GraderSpec(type="llm_quality"), client=client).grade(context)
    assert graded.status == GraderStatus.PASSED
    assert "未产出可评估文本与变更" in client.messages[0][1]["content"]


def test_judge_evidence_keeps_transcript_tail_not_middle(context: GradeContext) -> None:
    import dataclasses

    long_transcript = "A" * 10_000 + "\n" + "B" * 20_000 + "\n最终结论：修复完成\n"
    context = dataclasses.replace(context, run_facts={"final_text": None, "transcript_text": long_transcript})
    payload = {"dimensions": [], "overall": 50, "reason": "x"}
    client = FakeJudgeClient(json.dumps(payload))
    LLMQualityGrader(GraderSpec(type="llm_quality"), client=client).grade(context)
    user = client.messages[0][1]["content"]
    assert "transcript truncated, showing the last" in user  # judge knows evidence was cut
    assert "最终结论：修复完成" in user  # the most recent turns stay visible
    assert "A" * 100 not in user  # the stale head is dropped, not the tail


def test_judge_client_does_not_retry_deterministic_4xx(monkeypatch: pytest.MonkeyPatch) -> None:
    import urllib.error

    monkeypatch.setenv("DS_API_KEY", "sk-test")
    calls = {"count": 0}

    class RejectingResponse:
        def __enter__(self):
            raise urllib.error.HTTPError(
                url="judge", code=400, msg="context length exceeded", hdrs=None, fp=None
            )

        def __exit__(self, *args):
            return False

    def fake_urlopen(request, timeout=None):  # noqa: ANN001
        calls["count"] += 1
        return RejectingResponse()

    monkeypatch.setattr(urllib.request, "urlopen", fake_urlopen)
    client = DeepSeekJudgeClient()
    with pytest.raises(RuntimeError, match="HTTP 400"):
        client.complete([{"role": "user", "content": "hi"}])
    assert calls["count"] == 1  # deterministic 4xx fails fast instead of sleeping through retries


def test_bounded_diff_samples_head_and_tail(tmp_path: Path) -> None:
    from evals.diffutils import bounded_diff

    repository = tmp_path / "repo"
    repository.mkdir()
    _git(repository, "init", "-q", "-b", "main")
    _git(repository, "config", "user.email", "e@x")
    _git(repository, "config", "user.name", "t")
    (repository / "file_a.py").write_text("\n".join(f"old-a-{i}" for i in range(1500)), encoding="utf-8")
    (repository / "file_b.py").write_text("\n".join(f"old-b-{i}" for i in range(1500)), encoding="utf-8")
    _git(repository, "add", ".")
    _git(repository, "commit", "-q", "-m", "base")
    base = _git(repository, "rev-parse", "HEAD")
    (repository / "file_a.py").write_text("\n".join(f"new-a-{i}" for i in range(1500)), encoding="utf-8")
    (repository / "file_b.py").write_text("\n".join(f"new-b-{i}" for i in range(1500)), encoding="utf-8")
    _git(repository, "add", ".")
    _git(repository, "commit", "-q", "-m", "final")
    final = _git(repository, "rev-parse", "HEAD")

    diff = bounded_diff(repository, base, final, limit=4000)
    assert "bytes omitted from the middle of the diff" in diff
    assert "-old-a-0" in diff  # head of the diff (first file's changes) stays visible
    assert "+new-b-1499" in diff  # tail (later files) stays visible too
    assert "+new-a-0" not in diff  # the middle is genuinely omitted
