import asyncio
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.adapters.orchestrator import OrchestratorAdapter
from src.orchestrator.execution.engine import ExecutionEngine
from src.orchestrator.models import DispatchResult


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
