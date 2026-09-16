from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from src.adapters.base import ToolCallTracker
from src.schemas.events import EventType, StreamEvent
from src.transport.sanitizer import sanitize_stream_event


def test_generated_tool_identity_pairs_same_named_concurrent_calls_in_order() -> None:
    tracker = ToolCallTracker("run-1")
    first = tracker.normalize(StreamEvent.create(EventType.TOOL_CALL, tool="bash", args={"cmd": "one"}))
    second = tracker.normalize(StreamEvent.create(EventType.TOOL_CALL, tool="bash", args={"cmd": "two"}))
    first_result = tracker.normalize(StreamEvent.create(EventType.TOOL_RESULT, tool="bash", result="one"))
    second_result = tracker.normalize(StreamEvent.create(EventType.TOOL_RESULT, tool="bash", result="two"))

    assert first.content["tool_call_id"] == "run-1:1"
    assert second.content["tool_call_id"] == "run-1:2"
    assert first_result.content["tool_call_id"] == first.content["tool_call_id"]
    assert second_result.content["tool_call_id"] == second.content["tool_call_id"]
    assert first_result.content["status"] == "success"


def test_provider_identity_is_preserved_and_unclosed_call_becomes_incomplete() -> None:
    tracker = ToolCallTracker("run-1")
    call = tracker.normalize(
        StreamEvent.create(EventType.TOOL_CALL, tool_call_id="provider-42", tool="read", args={})
    )
    assert call.content["tool_call_id"] == "provider-42"
    incomplete = tracker.incomplete_events()
    assert len(incomplete) == 1
    assert incomplete[0].content["tool_call_id"] == "provider-42"
    assert incomplete[0].content["status"] == "incomplete"


def test_transport_sanitizer_keeps_tool_lifecycle_fields() -> None:
    event = StreamEvent.create(
        EventType.TOOL_CALL,
        tool_call_id="run-1:1",
        tool="bash",
        status="started",
        started_at=1.0,
        args={"secret": "large payload"},
    )
    safe = sanitize_stream_event(event)
    assert safe.content["tool_call_id"] == "run-1:1"
    assert safe.content["status"] == "started"
    assert safe.content["started_at"] == 1.0
    assert "args" not in safe.content
