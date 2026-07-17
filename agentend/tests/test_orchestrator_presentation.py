import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import src.adapters.orchestrator as orchestrator_module
from src.adapters.orchestrator import OrchestratorAdapter, _build_observability_config, _child_result_text
from src.orchestrator.models import TaskResult
from src.orchestrator.reporting.aggregator import build_final_summary_block
from src.schemas.events import EventType


def _extract_json_block(text: str) -> dict:
    line = next(line for line in text.splitlines() if line.startswith("json: "))
    return json.loads(line[len("json: ") :])


def test_child_result_text_uses_structured_failure_block() -> None:
    result = TaskResult(
        task_id="task-004",
        agent="执行者",
        success=False,
        content="已经完成部分改造",
        duration=12.5,
        error_type="timeout",
        error_message="Task task-004 exceeded 300.0s",
    )

    text = _child_result_text(result)

    assert "[Timeout]" not in text
    assert "[Error]" not in text
    assert "type: task_failure" in text
    payload = _extract_json_block(text)
    assert payload["task_id"] == "task-004"
    assert payload["failureType"] == "timeout"


def test_final_summary_block_is_summary_first() -> None:
    block = build_final_summary_block(
        [
            TaskResult(
                task_id="task-001",
                agent="执行者",
                success=True,
                content="完成权限页面",
                duration=5,
            ),
            TaskResult(
                task_id="task-002",
                agent="检查者",
                success=False,
                content="",
                duration=300,
                error_type="timeout",
                error_message="Task task-002 exceeded 300.0s",
            ),
        ]
    )

    payload = _extract_json_block(block)
    assert payload["status"] == "partial"
    assert payload["completed"] == 1
    assert payload["failed"] == 1
    assert payload["details"][1]["status"] == "failed"
    assert "[Timeout]" not in block


@pytest.mark.asyncio
async def test_orchestrator_reason_error_becomes_stream_error_event() -> None:
    adapter = OrchestratorAdapter()

    events = await adapter._handle_reason(
        {
            "output_type": "error",
            "text": "Orchestrator 推理失败：APIConnectionError: Connection error.",
        }
    )

    assert len(events) == 1
    assert events[0].type == EventType.ERROR.value
    assert events[0].content["error"] == "Orchestrator 推理失败：APIConnectionError: Connection error."


def test_orchestrator_observability_config_covers_each_replan_iteration(monkeypatch) -> None:
    callbacks = [object(), object()]
    monkeypatch.setattr(orchestrator_module, "create_orchestrator_callback", lambda: callbacks.pop(0))

    first, first_metadata = _build_observability_config("session-1", "task-1", 0)
    second, second_metadata = _build_observability_config("session-1", "task-1", 1)

    assert first["configurable"]["thread_id"] == "session-1"
    assert first["run_name"] == "orchestrator iteration=0"
    assert second["run_name"] == "orchestrator iteration=1"
    assert first["callbacks"][0] is not second["callbacks"][0]
    assert first_metadata == {
        "task_id": "task-1",
        "session_id": "session-1",
        "agent_type": "orchestrator",
        "iteration": 0,
    }
    assert second_metadata["iteration"] == 1


def test_orchestrator_observability_config_falls_back_without_callback(monkeypatch) -> None:
    monkeypatch.setattr(orchestrator_module, "create_orchestrator_callback", lambda: None)

    config, metadata = _build_observability_config("session-1", "task-1", 0)

    assert "callbacks" not in config
    assert config["metadata"] is metadata
    assert config["configurable"]["thread_id"] == "session-1"
