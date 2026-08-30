import sys
from pathlib import Path

import pytest
from sse_starlette.sse import EventSourceResponse

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.api.v1.agent import (
    _legacy_system_prompt_append,
    _request_fingerprint,
    _validate_active_pin_snapshot_budget,
    agent_stream,
)
from src.execution.models import RunSpec
from src.execution.repository import SQLiteRunRepository
from src.execution.supervisor import RunSupervisor
from src.generated.agent_run import AgentRunBudget
from src.orchestrator.planning.context_builder import build_active_pin_snapshot
from src.rules.builtin import PinRule, SafetyRule, SkillRule
from src.rules.engine import RuleEngine
from src.schemas.request import AgentRequest, AgentType


def test_rule_engine_emits_structured_channels_and_preserves_empty_allowlist() -> None:
    announcement = {
        "id": 7,
        "sender_id": "maintainer",
        "sender_name": "Maintainer",
        "content": "Do not change the schema",
        "created_at": "2026-08-28T00:00:00Z",
    }
    passed, result = RuleEngine([SafetyRule(), PinRule(), SkillRule()]).evaluate(
        {
            "allowed_tools": [],
            "pinned_announcements": [announcement],
        }
    )

    assert passed is True
    assert result["allowed_tools"] == []
    assert result["active_pins"] == [announcement]
    assert len(result["system_constraints"]) == 1
    assert len(result["capability_hints"]) == 1

    result["active_pin_snapshot"] = build_active_pin_snapshot("task", [announcement])
    legacy = _legacy_system_prompt_append(result)
    assert "announcement:7" in legacy
    assert "Do not change the schema" in legacy

    passed, error = RuleEngine([SafetyRule()]).evaluate({"allowed_tools": "read_file"})
    assert passed is False
    assert error["rule"] == "safety"


def test_request_fingerprint_excludes_dynamic_group_window() -> None:
    base = AgentRequest(
        task_id="task",
        session_id="session",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
        group_chat_messages=[{"content": "first"}],
    )
    changed_window = base.model_copy(update={"group_chat_messages": [{"content": "second"}]})
    changed_message = base.model_copy(update={"message": "different"})

    assert _request_fingerprint(base, "/workspace") == _request_fingerprint(
        changed_window, "/workspace"
    )
    assert _request_fingerprint(base, "/workspace") != _request_fingerprint(
        changed_message, "/workspace"
    )


def test_request_fingerprint_is_stable_when_server_allocates_run_id() -> None:
    initial = AgentRequest(
        task_id="task",
        session_id="session",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
    )
    reconnect = initial.model_copy(update={"run_id": "allocated-run"})

    assert _request_fingerprint(initial, "") == _request_fingerprint(reconnect, "")


def test_request_fingerprint_ignores_server_allocated_root_identity() -> None:
    initial = AgentRequest(
        task_id="task",
        session_id="session",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
    )
    reconnect = initial.model_copy(
        update={"run_id": "allocated-run", "root_run_id": "allocated-run"}
    )

    assert _request_fingerprint(initial, "") == _request_fingerprint(reconnect, "")


def test_request_fingerprint_keeps_explicit_invalid_root_identity_distinct() -> None:
    initial = AgentRequest(
        task_id="task",
        session_id="session",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
    )
    reconnect = initial.model_copy(update={"run_id": "allocated-run", "root_run_id": "other-root"})

    assert _request_fingerprint(initial, "") != _request_fingerprint(reconnect, "")


def test_request_fingerprint_normalizes_equivalent_workspace_paths(tmp_path: Path) -> None:
    request = AgentRequest(
        task_id="task",
        session_id="session",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
        workspace_path=str(tmp_path / "nested" / ".."),
        repo_path=str(tmp_path / "repo" / ".."),
    )
    reconnect = request.model_copy(
        update={
            "workspace_path": str(tmp_path),
            "repo_path": str(tmp_path),
        }
    )

    assert _request_fingerprint(request, str(tmp_path)) == _request_fingerprint(
        reconnect, str(tmp_path)
    )


def test_request_fingerprint_ignores_transport_and_message_correlation() -> None:
    initial = AgentRequest(
        task_id="task",
        session_id="session",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
        message_id="11111111-1111-4111-8111-111111111111",
        stream=True,
    )
    reconnect = initial.model_copy(
        update={
            "message_id": "22222222-2222-4222-8222-222222222222",
            "stream": False,
        }
    )

    assert _request_fingerprint(initial, "") == _request_fingerprint(reconnect, "")


def test_request_fingerprint_ignores_live_orchestrator_agent_projection() -> None:
    base = AgentRequest(
        task_id="task",
        session_id="session",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
        config={"agents": [{"id": "worker-a"}], "soul_md": "identity"},
    )
    changed_projection = base.model_copy(
        update={"config": {"agents": [{"id": "worker-b"}], "soul_md": "identity"}}
    )

    assert _request_fingerprint(base, "") == _request_fingerprint(changed_projection, "")


def test_active_pin_snapshot_rejects_partial_backend_records() -> None:
    with pytest.raises(ValueError, match="created_at"):
        build_active_pin_snapshot(
            "task",
            [
                {
                    "id": 1,
                    "sender_id": "a",
                    "sender_name": "A",
                    "content": "constraint",
                }
            ],
        )

    with pytest.raises(ValueError, match="content"):
        build_active_pin_snapshot(
            "task",
            [
                {
                    "id": 1,
                    "sender_id": "a",
                    "sender_name": "A",
                    "content": {"untrusted": "object"},
                    "created_at": "2026-08-28T00:00:00Z",
                }
            ],
        )


def test_active_pin_budget_is_rejected_before_run_admission(monkeypatch) -> None:
    from fastapi import HTTPException
    from src.app.config import settings

    request = AgentRequest(
        task_id="task",
        session_id="session",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
    )
    snapshot = build_active_pin_snapshot(
        "task",
        [
            {
                "id": 1,
                "sender_id": "maintainer",
                "sender_name": "Maintainer",
                "content": "x" * 200,
                "created_at": "2026-08-28T00:00:00Z",
            }
        ],
    )
    monkeypatch.setattr(settings.orchestrator, "active_pin_max_tokens", 1)

    with pytest.raises(HTTPException) as error:
        _validate_active_pin_snapshot_budget(request, snapshot)
    assert error.value.status_code == 400
    assert "token budget" in str(error.value.detail)

    cli_request = request.model_copy(update={"agent_type": AgentType.CLAUDE_CODE})
    with pytest.raises(HTTPException) as cli_error:
        _validate_active_pin_snapshot_budget(cli_request, snapshot)
    assert cli_error.value.status_code == 400


class _UnexpectedPinClient:
    def __init__(self) -> None:
        self.calls = 0

    async def get_pinned_announcements(self, task_id: str):
        self.calls += 1
        raise AssertionError("existing Run reconnect must not query Pins")


@pytest.mark.asyncio
async def test_existing_run_stream_reconnect_skips_pin_query(tmp_path: Path) -> None:
    request = AgentRequest(
        task_id="task",
        session_id="session",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
        run_id="run-existing",
    )
    repository = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repository)
    await repository.create(
        RunSpec(
            run_id="run-existing",
            root_run_id="run-existing",
            task_id="task",
            session_id="session",
            workspace_id="orchestrator:task",
            agent_type="orchestrator",
            request_fingerprint=_request_fingerprint(request, ""),
            budget=AgentRunBudget(),
        ),
        runtime={"active_pin_snapshot": build_active_pin_snapshot("task", [])},
    )
    backend = _UnexpectedPinClient()

    response = await agent_stream(
        request=request,
        adapter_registry=None,
        rule_engine=None,
        session_mgr=None,
        session_store=None,
        workspace_mgr=None,
        backend_client=backend,
        integration_service=None,
        run_supervisor=supervisor,
        path_policy=None,
    )

    assert isinstance(response, EventSourceResponse)
    assert response.headers["x-agent-run-id"] == "run-existing"
    assert backend.calls == 0
    await repository.close()
