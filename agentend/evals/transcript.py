"""Deterministic structural compression of run events into a judge transcript.

Text reasoning and conclusions stay intact; tool activity is reduced to call
headers plus bounded result excerpts. This is pure code with no LLM involved:
the same event stream always compresses to byte-identical output, so the judge
sees strictly more process signal without any added scoring randomness.
"""

from __future__ import annotations

import json
from collections.abc import Iterable
from typing import Any

TOOL_ARGS_LIMIT = 300
TOOL_RESULT_LIMIT = 500
TEXT_LIMIT = 4_000
ERROR_LIMIT = 1_000
TRANSCRIPT_LIMIT = 60_000


def compress_event(event: dict[str, Any]) -> str | None:
    """Render one event as a bounded transcript line; None when not evidence."""
    event_type = event.get("type")
    content = event.get("content") if isinstance(event.get("content"), dict) else {}
    if event_type == "text":
        text = content.get("text")
        if not isinstance(text, str) or not text.strip():
            return None
        return _cap(text, TEXT_LIMIT)
    if event_type == "tool_call":
        tool = str(content.get("tool") or "?")
        call_id = str(content.get("tool_call_id") or "?")
        args = content.get("args", content.get("input"))
        return f"→ {tool}#{_short(call_id)} {_cap(_compact(args), TOOL_ARGS_LIMIT)}"
    if event_type == "tool_result":
        tool = str(content.get("tool") or "?")
        call_id = str(content.get("tool_call_id") or "?")
        status = content.get("status") or ("error" if content.get("is_error") else "ok")
        exit_code = content.get("exit_code")
        result = content.get("result", content.get("output"))
        suffix = f" exit={exit_code}" if isinstance(exit_code, int) and not isinstance(exit_code, bool) else ""
        return f"← {tool}#{_short(call_id)} [{status}{suffix}] {_cap(_compact(result), TOOL_RESULT_LIMIT)}"
    if event_type == "error":
        raw = content.get("message", content.get("raw", content))
        return f"! error {_cap(_compact(raw), ERROR_LIMIT)}"
    return None


def build_transcript(events: Iterable[dict[str, Any]], *, limit: int = TRANSCRIPT_LIMIT) -> str | None:
    """Compress events in order; keep the tail when the budget is exceeded."""
    parts = [part for part in (compress_event(event) for event in events) if part]
    joined = "\n".join(parts)
    if len(joined) > limit:
        marker = f"…[earlier transcript truncated, showing the last {limit:,} characters]…\n"
        joined = marker + joined[-limit:]
    return joined or None


def _cap(text: str, limit: int) -> str:
    if len(text) <= limit:
        return text
    return text[:limit] + f" …[+{len(text) - limit} chars]"


def _compact(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    try:
        return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    except (TypeError, ValueError):
        return str(value)


def _short(call_id: str) -> str:
    return call_id[:12]
