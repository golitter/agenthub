from src.api.v1.agent import _artifact_process_env
from src.adapters.base import child_process_env
from src.schemas.request import AgentRequest
from src.schemas.events import EventType, StreamEvent
from src.transport.sanitizer import sanitize_stream_event


def test_large_tool_result_is_replaced_by_size_marker() -> None:
    event = StreamEvent.create(EventType.TOOL_RESULT, tool="render", result="x" * 20_000)
    sanitized = sanitize_stream_event(event)

    assert event.content["result"] == "x" * 20_000
    assert "result" not in sanitized.content
    assert sanitized.content["output_size"] == 20_000


def test_tool_metadata_is_preserved_but_payloads_are_removed() -> None:
    tool = StreamEvent.create(EventType.TOOL_CALL, tool="render", args={"command": "html-render"})
    tool_with_result = StreamEvent.create(EventType.TOOL_CALL, tool="opencode", args={}, result="output")

    sanitized = sanitize_stream_event(tool)
    assert sanitized.content["tool"] == "render"
    assert "args" not in sanitized.content
    assert sanitized.content["input_size"] > 0
    assert "result" not in sanitize_stream_event(tool_with_result).content


def test_structured_tool_payload_size_is_counted_without_forwarding_payload() -> None:
    event = StreamEvent.create(
        EventType.TOOL_RESULT,
        tool="taskctl",
        result={"stdout": "记忆" * 2000, "items": ["x", "y"]},
    )

    sanitized = sanitize_stream_event(event)

    assert "result" not in sanitized.content
    expected = ('{"stdout":"' + "记忆" * 2000 + '\",\"items\":["x","y"]}').encode("utf-8")
    assert sanitized.content["output_size"] == len(expected)


def test_done_text_is_removed_only_at_outward_boundary() -> None:
    event = StreamEvent.create(EventType.DONE, text="完成", usage={"input_tokens": 1})
    sanitized = sanitize_stream_event(event)

    assert event.content["text"] == "完成"
    assert "text" not in sanitized.content
    assert sanitized.content["text_omitted"] is True


def test_raw_adapter_fallback_never_crosses_outward_boundary() -> None:
    event = StreamEvent.create(EventType.TEXT, raw={"tool_result": "x" * 20_000})
    sanitized = sanitize_stream_event(event)

    assert "raw" not in sanitized.content
    assert event.content["raw"]["tool_result"] == "x" * 20_000


def test_error_transcript_is_bounded_at_outward_boundary() -> None:
    event = StreamEvent.create(EventType.ERROR, error="错误" * 10_000)
    sanitized = sanitize_stream_event(event)

    assert len(sanitized.content["error"].encode("utf-8")) <= 8 * 1024
    assert sanitized.content["error_truncated"] is True
    assert sanitized.content["error_bytes"] == len(("错误" * 10_000).encode("utf-8"))
    assert event.content["error"] == "错误" * 10_000


def test_child_process_context_does_not_forward_agentend_credentials(monkeypatch) -> None:
    monkeypatch.setenv("MYSQL_PASSWORD", "db-secret")
    monkeypatch.setenv("LANGFUSE_SECRET_KEY", "trace-secret")
    monkeypatch.setenv("PATH", "/bin")

    env = child_process_env({"AGENTHUB_ARTIFACT_TOKEN": "scoped-token", "UNTRUSTED": "ignored"})

    assert "MYSQL_PASSWORD" not in env
    assert "LANGFUSE_SECRET_KEY" not in env
    assert env["PATH"] == "/bin"
    assert env["AGENTHUB_ARTIFACT_TOKEN"] == "scoped-token"
    assert "UNTRUSTED" not in env


def test_child_process_context_clears_stale_artifact_values(monkeypatch) -> None:
    monkeypatch.setenv("AGENTHUB_ARTIFACT_TOKEN", "old-token")
    env = child_process_env()
    assert "AGENTHUB_ARTIFACT_TOKEN" not in env


def test_artifact_process_context_rejects_malformed_or_unbounded_input() -> None:
    base = {
        "task_id": "task",
        "session_id": "session",
        "message": "render",
        "message_id": "not-a-uuid",
        "artifact_upload_token": "token",
    }
    assert _artifact_process_env(AgentRequest(**base)) == {}

    base["message_id"] = "11111111-1111-4111-8111-111111111111"
    base["artifact_upload_token"] = "x" * 4097
    assert _artifact_process_env(AgentRequest(**base)) == {}
