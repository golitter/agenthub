"""Bound the AgentEnd -> Backend SSE payload without changing Agent context.

The CLI adapters still receive and aggregate their complete tool output. This
module is used only at the outward SSE boundary, so builtin render/taskctl
payloads cannot make every later SSE frame grow with the full tool output.
"""

from __future__ import annotations

import json
from typing import Any

from src.schemas.events import EventType, StreamEvent

_MAX_ERROR_BYTES = 8 * 1024


def _encoded_size(value: Any) -> int:
    try:
        # Count encoder chunks directly instead of materializing one complete
        # JSON string and a second UTF-8 byte buffer for a large tool payload.
        # The payload is already held by the adapter; the transport boundary
        # must not add another O(n) aggregate allocation just to report size.
        encoder = json.JSONEncoder(ensure_ascii=False, separators=(",", ":"))
        return sum(len(chunk.encode("utf-8")) for chunk in encoder.iterencode(value))
    except (TypeError, ValueError, UnicodeError, OverflowError, RecursionError):
        return len(str(value).encode("utf-8", errors="replace"))


def _byte_size(value: Any) -> int:
    if isinstance(value, str):
        return len(value.encode("utf-8"))
    if isinstance(value, (bytes, bytearray, memoryview)):
        return len(value)
    return _encoded_size(value)


def _truncate_utf8(value: str, max_bytes: int) -> str:
    encoded = value.encode("utf-8")
    if len(encoded) <= max_bytes:
        return value
    # Decode with replacement disabled so the visible prefix never ends in a
    # split UTF-8 sequence. The suffix is deliberately small and constant.
    suffix = "…[truncated]"
    budget = max(0, max_bytes - len(suffix.encode("utf-8")))
    return encoded[:budget].decode("utf-8", errors="ignore") + suffix


def sanitize_stream_event(event: StreamEvent) -> StreamEvent:
    """Return a bounded copy suitable for the outward SSE stream.

    Small tool metadata remains available for the UI. Tool payloads and
    duplicated tool HTML are removed while their byte sizes are retained;
    user-visible text is preserved. The original event object is never mutated
    and therefore remains safe for `/execute` and tracing consumers.
    """

    # Only the top-level mapping is copied. We remove keys but never mutate
    # their nested values, so a deep copy would duplicate the very payload we
    # are trying to keep off the transport path.
    content = dict(event.content or {})
    event_type = event.type.value if isinstance(event.type, EventType) else str(event.type)
    # `raw` is an adapter fallback containing the complete CLI JSON event. It
    # has no stable UI meaning and must never bypass the payload boundary,
    # regardless of which event type happened to carry it.
    content.pop("raw", None)

    if event_type == EventType.TOOL_CALL.value:
        if "args" in content:
            original = content["args"]
            content["input_size"] = _byte_size(original)
            content.pop("args", None)
        if "result" in content:
            original = content["result"]
            content["output_size"] = _byte_size(original)
            content.pop("result", None)
    elif event_type == EventType.TOOL_RESULT.value:
        if "result" in content:
            original = content["result"]
            content["output_size"] = _byte_size(original)
            content.pop("result", None)
    elif event_type == EventType.DONE.value:
        if isinstance(content.get("text"), str):
            text = content["text"]
            content["text_omitted"] = True
            content["text_bytes"] = len(text.encode("utf-8"))
            content.pop("text", None)
    elif event_type == EventType.ERROR.value:
        # Error payloads are user-visible, but a CLI can put an entire failed
        # command transcript in stderr. Keep the useful prefix bounded so an
        # error cannot reintroduce the same SSE/Redis amplification path.
        for key in ("error", "message"):
            value = content.get(key)
            if not isinstance(value, str):
                continue
            size = len(value.encode("utf-8"))
            if size > _MAX_ERROR_BYTES:
                content[key] = _truncate_utf8(value, _MAX_ERROR_BYTES)
                content[f"{key}_truncated"] = True
                content[f"{key}_bytes"] = size
    return event.model_copy(update={"content": content})


__all__ = ["sanitize_stream_event"]
