import asyncio
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.adapters.orchestrator import OrchestratorAdapter
from src.app.config import settings
from src.clients.backend_client import RunTaskResult
from src.orchestrator.execution.engine import ExecutionEngine
from src.orchestrator.models import DispatchResult, TaskResult
from src.orchestrator.planning import graph as graph_module


def dispatch(task_id: str) -> DispatchResult:
    return DispatchResult(
        task_id=task_id,
        agent="worker",
        mention="@worker",
        content="work",
    )


def test_child_budget_inherits_and_tightens_parent_wall_time():
    engine = ExecutionEngine(
        backend_client=object(),  # type: ignore[arg-type]
        budget={"wall_time_seconds": 120, "max_output_bytes": 4096},
    )

    assert engine._child_budget(300) == {
        "wall_time_seconds": 120,
        "max_output_bytes": 4096,
    }
    assert engine._child_budget(30)["wall_time_seconds"] == 30


def test_invalid_integration_result_is_not_retried() -> None:
    result = TaskResult(
        task_id="task-invalid-result",
        agent="worker",
        execution_status="completed",
        integration_status="failed",
        success=False,
        content="",
        error_code="integration_result_invalid",
    )

    assert ExecutionEngine._should_retry_result(result) is False


@pytest.mark.asyncio
async def test_execution_engine_forwards_child_answer_as_im_text_event(monkeypatch) -> None:
    monkeypatch.setattr(settings.orchestrator, "execution_retry_max_attempts", 1)

    class FakeBackend:
        async def run_task(self, **_kwargs):
            return RunTaskResult(run_id="child-run", message_id="child-message")

        async def stream_result(self, **_kwargs):
            yield {"type": "text", "content": {"text": "hello from child"}}
            yield {"type": "done", "content": {}}

    engine = ExecutionEngine(
        backend_client=FakeBackend(),  # type: ignore[arg-type]
        task_id="root-task",
    )
    item = DispatchResult(
        task_id="task-001",
        plan_task_id="task-001",
        agent="Alice",
        agent_type="pi",
        real_session_id="alice-session",
        mention="@Alice",
        content="answer in chat",
    )

    events = []
    async for event, _result in engine.execute([item]):
        events.append(event)

    text_events = [event for event in events if event.type == "text"]
    assert len(text_events) == 1
    assert text_events[0].content == {
        "plan_task_id": "task-001",
        "integration_operation_id": "",
        "task_id": "task-001",
        "agent": "Alice",
        "agent_type": "pi",
        "session_id": "alice-session",
        "message_id": "child-message",
        "run_id": "child-run",
        "attempt": 0,
        "text": "hello from child",
    }
    assert all(event.type != "runtime_text" for event in events)


@pytest.mark.asyncio
async def test_execution_engine_parallel_failure_cancels_siblings(monkeypatch):
    engine = ExecutionEngine(backend_client=object())  # type: ignore[arg-type]
    sibling_cleaned = asyncio.Event()

    async def fake_execute_task(item, _timeout):
        if item.task_id == "broken":
            raise RuntimeError("fanout failed")
        try:
            await asyncio.Event().wait()
        finally:
            sibling_cleaned.set()
        if False:
            yield None

    monkeypatch.setattr(engine, "_execute_task", fake_execute_task)

    with pytest.raises(RuntimeError, match="fanout failed"):
        async for _ in engine.execute([dispatch("broken"), dispatch("slow")]):
            pass

    await asyncio.wait_for(sibling_cleaned.wait(), timeout=1)


@pytest.mark.asyncio
async def test_orchestrator_wave_failure_cancels_siblings():
    adapter = OrchestratorAdapter()
    sibling_cleaned = asyncio.Event()

    class FakeEngine:
        async def execute(self, items):
            if items[0].task_id == "broken":
                raise RuntimeError("wave failed")
            try:
                await asyncio.Event().wait()
            finally:
                sibling_cleaned.set()
            if False:
                yield None

    with pytest.raises(RuntimeError, match="wave failed"):
        async for _ in adapter._stream_wave(FakeEngine(), [dispatch("broken"), dispatch("slow")]):
            pass

    await asyncio.wait_for(sibling_cleaned.wait(), timeout=1)


@pytest.mark.asyncio
async def test_graph_execute_node_returns_authoritative_results_for_review(tmp_path: Path) -> None:
    queue: asyncio.Queue = asyncio.Queue()
    tokens = graph_module.set_reason_runtime_context(
        ask_event_queue=None,
        backend_client=None,
        cwd="",
        execution_event_queue=queue,
    )
    try:
        result = await graph_module.execute_node(
            {
                "task_id": "root-task",
                "shared_dir": str(tmp_path),
                "execution_waves": [[dispatch("task-a"), dispatch("task-b")]],
            }
        )
    finally:
        graph_module.reset_reason_runtime_context(tokens)

    assert {item["task_id"] for item in result["task_results"]} == {"task-a", "task-b"}
    assert all(item["execution_status"] == "completed" for item in result["task_results"])
    assert all(item["integration_status"] == "not_required" for item in result["task_results"])
    assert graph_module.review_node({"task_results": result["task_results"], "iteration": 0, "max_iterations": 3}) == {
        "needs_replan": False,
        "replan_reason": "",
    }
    events = [await queue.get() for _ in range(4)]
    assert [event.type for event in events].count("runtime_executing") == 2
    assert [event.type for event in events].count("runtime_completed") == 2


@pytest.mark.asyncio
async def test_graph_reuses_successful_results_when_replanning(tmp_path: Path) -> None:
    queue: asyncio.Queue = asyncio.Queue()
    prior = TaskResult(
        task_id="task-a",
        agent="worker",
        execution_status="completed",
        integration_status="merged",
        success=True,
        content="already integrated",
    )
    tokens = graph_module.set_reason_runtime_context(
        ask_event_queue=None,
        backend_client=None,
        cwd="",
        execution_event_queue=queue,
    )
    try:
        result = await graph_module.execute_node(
            {
                "task_id": "root-task",
                "shared_dir": str(tmp_path),
                "task_results": [prior.model_dump()],
                "execution_waves": [[dispatch("task-a"), dispatch("task-b")]],
            }
        )
    finally:
        graph_module.reset_reason_runtime_context(tokens)

    assert {item["task_id"] for item in result["task_results"]} == {"task-a", "task-b"}
    assert [await queue.get() for _ in range(2)]
    assert queue.empty()


@pytest.mark.asyncio
async def test_execution_retry_uses_attempt_budget(monkeypatch) -> None:
    engine = ExecutionEngine(backend_client=object())  # type: ignore[arg-type]
    attempts: list[int] = []

    async def fake_execute_task(item, _timeout):
        attempts.append(item.attempt)
        if item.attempt == 0:
            yield None, TaskResult(
                task_id=item.task_id,
                agent=item.agent,
                execution_status="failed",
                integration_status="pending",
                success=False,
                content="first attempt failed",
                error_code="execution_failed",
            )
        else:
            yield None, TaskResult(
                task_id=item.task_id,
                agent=item.agent,
                execution_status="completed",
                integration_status="merged",
                success=True,
                content="recovered",
            )

    monkeypatch.setattr(engine, "_execute_task", fake_execute_task)
    monkeypatch.setattr(graph_module.settings.orchestrator, "execution_retry_max_attempts", 2)

    results = []
    async for _event, result in engine.execute([dispatch("retry-me")]):
        if result is not None:
            results.append(result)

    assert attempts == [0, 1]
    assert results[-1].success is True


def test_review_routes_completed_execution_with_integration_conflict_to_recovery_pause() -> None:
    output = graph_module.review_node(
        {
            "task_results": [
                {
                    "task_id": "task-conflict",
                    "agent": "worker",
                    "execution_status": "completed",
                    "integration_status": "awaiting_user",
                    "success": False,
                    "error_message": "manual resolution required",
                }
            ],
            "iteration": 0,
            "max_iterations": 3,
        }
    )

    assert output["awaiting_user"] is True
    assert output["final_status"] == "awaiting_user"
    assert output["needs_replan"] is False
