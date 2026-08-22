import pytest
from fastapi import HTTPException

from src.api.v1.agent import (
    _artifact_process_env,
    _require_phase2_integration_credentials,
    _run_process_env,
)
from src.app.config import settings
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


def test_ordinary_events_do_not_expose_git_audit_facts() -> None:
    event = StreamEvent.create(
        EventType.INTEGRATION_COMPLETED,
        plan_task_id="task-001",
        integration_operation_id="operation-1",
        source_branch="agent/session/task",
        target_branch="task/scope",
        source_commit="source-sha",
        target_commit="target-sha",
        merge_base="base-sha",
        workspace_path="/tmp/secret-worktree",
    )
    sanitized = sanitize_stream_event(event)
    assert sanitized.content["plan_task_id"] == "task-001"
    assert sanitized.content["integration_operation_id"] == "operation-1"
    for key in (
        "source_branch",
        "target_branch",
        "source_commit",
        "target_commit",
        "merge_base",
        "workspace_path",
    ):
        assert key not in sanitized.content


def test_nested_ordinary_events_do_not_expose_git_audit_facts() -> None:
    event = StreamEvent.create(
        EventType.PLANNING,
        node="dispatch",
        dispatch={
            "task_id": "task-001",
            "plan_task_id": "task-001",
            "workspace_path": "/tmp/secret-worktree",
            "workspace_handle": "opaque-workspace",
            "integration_scope_id": "scope-1",
            "source_branch": "agent/session/scope-1",
        },
    )

    sanitized = sanitize_stream_event(event)

    assert sanitized.content["dispatch"]["task_id"] == "task-001"
    for key in (
        "workspace_path",
        "workspace_handle",
        "integration_scope_id",
        "source_branch",
    ):
        assert key not in sanitized.content["dispatch"]


def test_ordinary_events_only_forward_normalized_conflict_paths() -> None:
    event = StreamEvent.create(
        EventType.INTEGRATION_CONFLICT,
        conflict_files=["src/main.py", "/tmp/secret", "../escape", "ok.txt", "a\\b"],
    )
    sanitized = sanitize_stream_event(event)
    assert sanitized.content["conflict_files"] == ["src/main.py", "ok.txt"]


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


def test_phase2_process_context_does_not_expose_plan_or_workspace_identity(monkeypatch) -> None:
    monkeypatch.setattr(settings.orchestrator, "integration_service_execute_enabled", True)
    request = AgentRequest(
        task_id="scope",
        session_id="session",
        message="merge",
        run_id="33333333-3333-4333-8333-333333333333",
        root_run_id="11111111-1111-4111-8111-111111111111",
        parent_run_id="22222222-2222-4222-8222-222222222222",
        current_run_id="22222222-2222-4222-8222-222222222222",
        plan_task_id="plan-task-1",
        integration_operation_id="44444444-4444-4444-8444-444444444444",
        workspace_id="workspace-identity",
        workspace_handle="opaque-workspace",
        integration_attempt=3,
        integration_capability="single-use-capability",
    )
    env = _run_process_env(request)
    assert env["AGENTHUB_RUN_ID"] == "33333333-3333-4333-8333-333333333333"
    assert env["AGENTHUB_INTEGRATION_OPERATION_ID"] == "44444444-4444-4444-8444-444444444444"
    assert env["AGENTHUB_INTEGRATION_CAPABILITY"] == "single-use-capability"
    for forbidden in (
        "AGENTHUB_ROOT_RUN_ID",
        "AGENTHUB_PARENT_RUN_ID",
        "AGENTHUB_CURRENT_RUN_ID",
        "AGENTHUB_PLAN_TASK_ID",
        "AGENTHUB_WORKSPACE_HANDLE",
        "AGENTHUB_INTEGRATION_ATTEMPT",
    ):
        assert forbidden not in env


def test_phase2_operation_without_capability_cannot_fall_back_to_v1(monkeypatch) -> None:
    monkeypatch.setattr(settings.orchestrator, "integration_service_execute_enabled", True)
    request = AgentRequest(
        task_id="scope",
        session_id="session",
        message="merge",
        run_id="33333333-3333-4333-8333-333333333333",
        root_run_id="11111111-1111-4111-8111-111111111111",
        parent_run_id="22222222-2222-4222-8222-222222222222",
        current_run_id="22222222-2222-4222-8222-222222222222",
        plan_task_id="plan-task-1",
        integration_operation_id="44444444-4444-4444-8444-444444444444",
        workspace_id="workspace-identity",
        workspace_handle="opaque-workspace",
    )
    with pytest.raises(HTTPException) as exc_info:
        _require_phase2_integration_credentials(request)
    assert exc_info.value.status_code == 401
    assert exc_info.value.detail["code"] == "integration_capability_invalid"
