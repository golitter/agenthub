from __future__ import annotations

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from evals.runner import AgentHubEvalRunner
from src.execution.repository import SQLiteRunRepository
from src.execution.supervisor import RunSupervisor
from src.generated.agent_run import AgentRunBudget


@pytest.mark.asyncio
async def test_eval_runner_creates_requested_by_eval_and_collects_usage(tmp_path: Path) -> None:
    repository = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repository)

    def factory(_spec):
        async def runner(emit):
            await emit({"type": "tool_call", "content": {"tool_call_id": "one", "tool": "read"}})
            await emit({"type": "text", "content": {"text": "done"}})
            await emit({"type": "done", "content": {"usage": {"total_tokens": 12}, "trace_id": "trace-a"}})

        return runner, None

    eval_runner = AgentHubEvalRunner(supervisor, repository, factory)
    record, facts = await eval_runner.execute(
        run_id="eval-run-a",
        task_id="eval-task-a",
        session_id="eval-session-a",
        workspace_id="eval-workspace-a",
        agent_type="fake",
        budget=AgentRunBudget(wall_time_seconds=5),
        timeout_seconds=5,
    )
    assert record.spec.requested_by == "eval"
    assert facts["run_state"] == "completed"
    assert facts["total_tokens"] == 12
    assert facts["trace_id"] == "trace-a"
    assert facts["ttfa_seconds"] is not None
    await supervisor.shutdown()
    await repository.close()
