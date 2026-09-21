"""Semantic evidence chunks and conservative protection rules for judges."""

from __future__ import annotations

import re
from dataclasses import dataclass, replace
from enum import Enum
from typing import Any


class EvidenceKind(str, Enum):
    CASE_PROMPT = "case_prompt"
    RUBRIC = "rubric"
    FINAL_REPLY = "final_reply"
    AGENT_DISPATCH = "agent_dispatch"
    AGENT_RESULT = "agent_result"
    ERROR = "error"
    TEST_SUMMARY = "test_summary"
    DIFF_HUNK = "diff_hunk"
    PROGRESS = "progress"
    DUPLICATE = "duplicate"
    RUN_FACT = "run_fact"


@dataclass(frozen=True)
class EvidenceChunk:
    id: str
    kind: EvidenceKind
    text: str
    protected: bool
    priority: int
    tool_call_id: str | None = None
    estimated_tokens: int = 0
    jev_keep_probability: float | None = None
    jev_drop_probability: float | None = None
    group_id: str | None = None

    @property
    def atomic_group(self) -> str:
        return self.group_id or self.tool_call_id or self.id


_PROTECTED = {EvidenceKind.CASE_PROMPT, EvidenceKind.RUBRIC, EvidenceKind.ERROR, EvidenceKind.TEST_SUMMARY}
_PRIORITY = {
    EvidenceKind.CASE_PROMPT: 100,
    EvidenceKind.RUBRIC: 100,
    EvidenceKind.FINAL_REPLY: 80,
    EvidenceKind.ERROR: 95,
    EvidenceKind.TEST_SUMMARY: 90,
    EvidenceKind.DIFF_HUNK: 80,
    EvidenceKind.AGENT_RESULT: 60,
    EvidenceKind.AGENT_DISPATCH: 40,
    EvidenceKind.RUN_FACT: 40,
    EvidenceKind.PROGRESS: 20,
    EvidenceKind.DUPLICATE: 0,
}

_TOOL_CALL = re.compile(r"^→\s+(?P<tool>[^#\s]+)#(?P<id>\S+)\s*(?P<body>.*)$")
_TOOL_RESULT = re.compile(r"^←\s+(?P<tool>[^#\s]+)#(?P<id>\S+)\s*(?P<body>.*)$")
_NON_SUCCESS_STATES = {"failed", "error", "cancelled", "timeout", "timed_out", "invalid"}
_FINAL_STATE_FACTS = {"run_state", "integration_status", "subtask_final_status"}
_DISPATCH_FACTS = {"plan_task_count", "implementer_count", "max_concurrent_implementers"}
_ERROR_FACTS = {
    "termination_reason",
    "retry_count",
    "replan_count",
    "conflict_count",
    "conflict_recovery_count",
    "integration_conflict_seen",
    "conflict_chains",
}


def make_chunk(
    chunk_id: str,
    kind: EvidenceKind,
    text: str,
    *,
    protected: bool | None = None,
    priority: int | None = None,
    tool_call_id: str | None = None,
    group_id: str | None = None,
) -> EvidenceChunk:
    return EvidenceChunk(
        id=chunk_id,
        kind=kind,
        text=text,
        protected=kind in _PROTECTED if protected is None else protected,
        priority=_PRIORITY.get(kind, 40) if priority is None else priority,
        tool_call_id=tool_call_id,
        group_id=group_id,
    )


def chunks_from_facts(
    *, prompt: str, category: str, rubric: str, final_text: str, transcript: str, diff: str, facts: dict[str, Any]
) -> list[EvidenceChunk]:
    chunks = [
        make_chunk("case-prompt", EvidenceKind.CASE_PROMPT, f"用户需求：\n{prompt}"),
        make_chunk("category", EvidenceKind.CASE_PROMPT, f"类别：{category}"),
    ]
    if rubric:
        chunks.append(make_chunk("rubric", EvidenceKind.RUBRIC, f"参考 rubric：\n{rubric}"))
    if final_text:
        chunks.append(make_chunk("final-reply", EvidenceKind.FINAL_REPLY, f"Agent 最终回复：\n{final_text}"))
    if transcript:
        chunks.extend(_transcript_chunks(transcript, category))
    if diff.strip():
        diff_lower = diff.lower()
        diff_protected = any(
            pattern.lower() in diff_lower
            for pattern in (
                "forbidden",
                "allowlist",
                "symlink",
                "submodule",
                ".git/",
            )
        )
        chunks.append(
            make_chunk(
                "diff",
                EvidenceKind.DIFF_HUNK,
                f"Git Diff：\n{diff}",
                protected=diff_protected,
                priority=95 if diff_protected else None,
            )
        )
    if not final_text and not transcript and not diff.strip():
        chunks.append(make_chunk("empty-output", EvidenceKind.ERROR, "（Agent 未产出可评估文本与变更）"))
    # Only explicitly allowlisted scoring facts may leave the process. Runtime
    # identifiers, trace URLs, cost and timing are intentionally excluded.
    for key, value in facts.items():
        if key in {"final_text", "transcript_text"}:
            continue
        if value in (None, "", 0, False, [], {}):
            continue
        fact = _fact_chunk(key, value, category)
        if fact is None:
            continue
        chunks.append(fact)
    return chunks


def _transcript_chunks(transcript: str, category: str) -> list[EvidenceChunk]:
    """Parse the canonical transcript arrows and preserve call/result groups."""

    lines = transcript.splitlines() or [transcript]
    chunks: list[EvidenceChunk] = []
    prose: list[str] = []
    prose_index = 0

    def flush_prose() -> None:
        nonlocal prose_index
        if not prose:
            return
        block = "\n".join(prose)
        lowered = block.lower()
        kind = (
            EvidenceKind.ERROR
            if any(word in lowered for word in ("error", "failed", "timeout", "cancelled", "non-zero"))
            else EvidenceKind.PROGRESS
        )
        if "test" in lowered or "pytest" in lowered:
            kind = EvidenceKind.TEST_SUMMARY
        chunks.append(
            make_chunk(
                f"transcript-{prose_index}",
                kind,
                f"Agent 过程记录：\n{block}",
            )
        )
        prose.clear()
        prose_index += 1

    for line_index, line in enumerate(lines):
        call = _TOOL_CALL.match(line)
        result = _TOOL_RESULT.match(line)
        if call or result:
            flush_prose()
            match = call or result
            assert match is not None
            call_id = match.group("id")[:128]
            group_id = f"tool:{call_id}"
            kind = EvidenceKind.AGENT_DISPATCH if call else EvidenceKind.AGENT_RESULT
            protected = category in {"no_op", "chat", "knowledge_qa"}
            if result and _result_failed(match.group("body")):
                kind = EvidenceKind.ERROR
                protected = True
            chunks.append(
                make_chunk(
                    f"tool-{line_index}",
                    kind,
                    line,
                    protected=protected,
                    priority=95 if protected else None,
                    tool_call_id=call_id,
                    group_id=group_id,
                )
            )
            continue
        prose.append(line)
        if len(prose) >= 80:
            flush_prose()
    flush_prose()

    # If one side of a call/result pair is protected (for example, a failed
    # result), protect the entire atomic group so the judge never sees an
    # orphan result or loses the action that produced an error.
    protected_groups = {chunk.atomic_group for chunk in chunks if chunk.protected}
    chunks = [
        replace(chunk, protected=True, priority=max(chunk.priority, 95))
        if chunk.atomic_group in protected_groups
        else chunk
        for chunk in chunks
    ]
    return chunks


def _result_failed(body: str) -> bool:
    lowered = body.lower()
    if any(word in lowered for word in ("[error", "[failed", "timeout", "cancelled", "non-zero")):
        return True
    match = re.search(r"\bexit=(-?\d+)\b", lowered)
    return bool(match and int(match.group(1)) != 0)


def _fact_chunk(key: str, value: Any, category: str) -> EvidenceChunk | None:
    if key in _DISPATCH_FACTS:
        protected = category in {"no_op", "chat", "knowledge_qa"}
        return make_chunk(
            f"fact-{key}",
            EvidenceKind.AGENT_DISPATCH,
            f"运行事实 {key}：{value}",
            protected=protected,
            priority=95 if protected else 60,
        )
    if key in _ERROR_FACTS:
        return make_chunk(
            f"fact-{key}",
            EvidenceKind.ERROR,
            f"运行事实 {key}：{value}",
            protected=True,
            priority=95,
        )
    if key in _FINAL_STATE_FACTS:
        failed = _contains_failure(value)
        return make_chunk(
            f"fact-{key}",
            EvidenceKind.ERROR if failed else EvidenceKind.RUN_FACT,
            f"运行事实 {key}：{value}",
            protected=failed,
            priority=95 if failed else 90,
        )
    return None


def _contains_failure(value: Any) -> bool:
    if isinstance(value, dict):
        return any(_contains_failure(item) for item in value.values())
    if isinstance(value, (list, tuple, set)):
        return any(_contains_failure(item) for item in value)
    return str(value).strip().lower() in _NON_SUCCESS_STATES
