from __future__ import annotations

import json
import sys
import urllib.request
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from evals.context_budgeter import JudgeEvidenceOverflow, build_budgeted_messages
from evals.evidence import EvidenceKind, chunks_from_facts, make_chunk
from evals.jev_client import TypeSafeJevClient
from evals.token_budget import ContextBudget, estimate_messages
from evals.transcript import compress_event


def test_weighted_estimator_is_conservative_for_mixed_text() -> None:
    estimate = estimate_messages([{"role": "user", "content": "abcd 12 中文{}"}], safety_factor=1.15)
    assert estimate.raw_tokens >= 256
    assert estimate.tokens >= estimate.raw_tokens
    assert estimate.estimator_version == "weighted_chars_v1"


def test_weighted_estimator_counts_each_short_ascii_word() -> None:
    estimate = estimate_messages([{"role": "user", "content": "a " * 1000}], safety_factor=1.0)
    assert estimate.raw_tokens >= 1000 + 256


def test_context_budget_rejects_invalid_ratios() -> None:
    with pytest.raises(ValueError):
        ContextBudget(prune_target_ratio=0.8, prune_trigger_ratio=0.7)
    with pytest.raises(ValueError):
        ContextBudget(context_window_tokens=1000, output_reserve_tokens=900)
    with pytest.raises(ValueError):
        ContextBudget(estimator_safety_factor=0.9)
    with pytest.raises(ValueError):
        ContextBudget(max_overflow_retries=2)


def test_budgeter_keeps_protected_evidence_and_drops_progress() -> None:
    budget = ContextBudget(
        context_window_tokens=4096,
        output_reserve_tokens=256,
        min_safety_margin_tokens=256,
        prune_trigger_ratio=0.5,
        prune_target_ratio=0.3,
        jev_enabled=False,
    )
    chunks = [
        make_chunk("prompt", EvidenceKind.CASE_PROMPT, "用户需求"),
        make_chunk("error", EvidenceKind.ERROR, "error: failed run"),
        make_chunk("progress", EvidenceKind.PROGRESS, "progress " * 20_000),
    ]
    messages, decision = build_budgeted_messages("judge system", chunks, budget)
    assert decision.estimate.tokens <= decision.target_tokens
    assert "failed run" in messages[1]["content"]
    assert "progress" not in messages[1]["content"]


def test_protected_evidence_overflow_is_explicit() -> None:
    budget = ContextBudget(
        context_window_tokens=4096,
        output_reserve_tokens=256,
        min_safety_margin_tokens=256,
        prune_trigger_ratio=0.5,
        prune_target_ratio=0.3,
        jev_enabled=False,
    )
    chunks = [make_chunk("error", EvidenceKind.ERROR, "failure " * 20_000)]
    with pytest.raises(JudgeEvidenceOverflow):
        build_budgeted_messages("judge system", chunks, budget)


def test_jev_only_receives_unprotected_chunks_and_scores_are_a_signal() -> None:
    class StubJev:
        def __init__(self) -> None:
            self.received = []

        def rank(self, chunks, *, prompt):  # noqa: ANN001
            self.received = chunks
            return {chunk.id: 0.0 for chunk in chunks}

    budget = ContextBudget(
        context_window_tokens=4096,
        output_reserve_tokens=256,
        min_safety_margin_tokens=256,
        prune_trigger_ratio=0.5,
        prune_target_ratio=0.3,
    )
    jev = StubJev()
    chunks = [
        make_chunk("error", EvidenceKind.ERROR, "error"),
        make_chunk("progress", EvidenceKind.PROGRESS, "progress " * 1000),
    ]
    messages, _ = build_budgeted_messages("judge system", chunks, budget, jev=jev)
    assert [chunk.id for chunk in jev.received] == ["progress"]
    assert "error" in messages[1]["content"]


def test_canonical_transcript_pairs_and_protects_noop_dispatch() -> None:
    chunks = chunks_from_facts(
        prompt="不要派发子 Agent",
        category="no_op",
        rubric="",
        final_text="",
        transcript=(
            '→ dispatch_agent#abc123 {"task":"x"}\n'
            "← dispatch_agent#abc123 [ok] completed"
        ),
        diff="",
        facts={},
    )
    tool_chunks = [chunk for chunk in chunks if chunk.tool_call_id == "abc123"]
    assert [chunk.kind for chunk in tool_chunks] == [
        EvidenceKind.AGENT_DISPATCH,
        EvidenceKind.AGENT_RESULT,
    ]
    assert all(chunk.protected for chunk in tool_chunks)
    assert len({chunk.atomic_group for chunk in tool_chunks}) == 1


def test_failed_tool_result_protects_its_call_in_coding_transcript() -> None:
    chunks = chunks_from_facts(
        prompt="修复代码",
        category="bugfix",
        rubric="",
        final_text="",
        transcript="→ Bash#call1 pytest\n← Bash#call1 [error exit=1] 1 failed",
        diff="",
        facts={},
    )
    tool_chunks = [chunk for chunk in chunks if chunk.tool_call_id == "call1"]
    assert len(tool_chunks) == 2
    assert all(chunk.protected for chunk in tool_chunks)


def test_multiline_tool_result_stays_one_parseable_transcript_line() -> None:
    rendered = compress_event(
        {
            "type": "tool_result",
            "content": {
                "tool_call_id": "call1",
                "tool": "Bash",
                "status": "error",
                "exit_code": 1,
                "result": "first line\nsecond line",
            },
        }
    )
    assert rendered is not None
    assert rendered.count("\n") == 0
    assert "first line\\nsecond line" in rendered
    chunks = chunks_from_facts(
        prompt="修复代码",
        category="bugfix",
        rubric="",
        final_text="",
        transcript="→ Bash#call1 pytest\n" + rendered,
        diff="",
        facts={},
    )
    tool_chunks = [chunk for chunk in chunks if chunk.tool_call_id == "call1"]
    assert len(tool_chunks) == 2
    assert all(chunk.protected for chunk in tool_chunks)


def test_run_fact_allowlist_omits_identifiers_and_keeps_final_state() -> None:
    chunks = chunks_from_facts(
        prompt="修复代码",
        category="bugfix",
        rubric="",
        final_text="完成",
        transcript="",
        diff="",
        facts={
            "root_run_id": "secret-run-id",
            "trace_url": "https://trace.invalid/private",
            "duration_seconds": 12.3,
            "run_state": "completed",
            "integration_status": "merged",
        },
    )
    rendered = "\n".join(chunk.text for chunk in chunks)
    assert "secret-run-id" not in rendered
    assert "trace.invalid" not in rendered
    assert "duration_seconds" not in rendered
    final_state = [chunk for chunk in chunks if chunk.id in {"fact-run_state", "fact-integration_status"}]
    assert all(chunk.priority == 90 for chunk in final_state)
    assert all(not chunk.protected for chunk in final_state)


def test_typesafe_client_uses_systemone_protocol(monkeypatch: pytest.MonkeyPatch) -> None:
    captured: dict[str, object] = {}

    class Response:
        def __enter__(self):
            return self

        def __exit__(self, *_args):
            return False

        def read(self) -> bytes:
            body = captured["body"]
            assert isinstance(body, dict)
            answers = {name: {"type": "noul", "noul": 0.75} for name in body["questions"]}
            return json.dumps({"answers": answers}).encode()

    def fake_urlopen(request, timeout=None):  # noqa: ANN001
        captured["url"] = request.full_url
        captured["body"] = json.loads(request.data.decode())
        captured["timeout"] = timeout
        return Response()

    monkeypatch.setattr(urllib.request, "urlopen", fake_urlopen)
    client = TypeSafeJevClient(api_key="typesafe-test")
    chunks = [make_chunk("candidate", EvidenceKind.PROGRESS, "ordinary progress")]
    scores = client.rank(chunks, prompt="用户需求：只修复错误")
    assert captured["url"] == "https://api.typesafe.ai/v1/systemone"
    body = captured["body"]
    assert isinstance(body, dict)
    assert body["model"] == "jev-latest"
    assert set(body) == {"model", "state", "questions"}
    assert "只修复错误" in body["state"]["goal"]
    assert scores == {"candidate": 0.75}


def test_budget_config_mapping_round_trips() -> None:
    budget = ContextBudget.from_mapping({"context_window_tokens": 8192, "output_reserve_tokens": 512})
    assert budget.context_window_tokens == 8192
    assert json.loads(json.dumps(budget.as_dict()))["estimator_version"] == "weighted_chars_v1"
