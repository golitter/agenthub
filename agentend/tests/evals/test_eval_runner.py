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


@pytest.mark.asyncio
async def test_eval_runner_collects_final_and_transcript_text(tmp_path: Path) -> None:
    repository = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repository)

    def factory(_spec):
        async def runner(emit):
            await emit({"type": "text", "content": {"text": "第一步：阅读仓库"}})
            await emit({"type": "text", "content": {"text": ""}})  # blank text is dropped
            await emit({"type": "tool_call", "content": {"tool": "edit"}})
            await emit({"type": "text", "content": {"text": "第二步：修改完成"}})
            await emit({"type": "done", "content": {"final_text": "已按要求修复 solution.py"}})

        return runner, None

    eval_runner = AgentHubEvalRunner(supervisor, repository, factory)
    record, facts = await eval_runner.execute(
        run_id="eval-run-b",
        task_id="eval-task-b",
        session_id="eval-session-b",
        workspace_id="eval-workspace-b",
        agent_type="fake",
        budget=AgentRunBudget(wall_time_seconds=5),
        timeout_seconds=5,
    )
    assert facts["final_text"] == "已按要求修复 solution.py"  # done.final_text wins over last text
    # Structural compression interleaves reasoning text with tool headers.
    assert facts["transcript_text"] == "第一步：阅读仓库\n→ edit#? \n第二步：修改完成"
    await supervisor.shutdown()
    await repository.close()


@pytest.mark.asyncio
async def test_eval_runner_final_text_falls_back_to_last_text(tmp_path: Path) -> None:
    repository = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repository)

    def factory(_spec):
        async def runner(emit):
            await emit({"type": "text", "content": {"text": "回答内容"}})
            await emit({"type": "done", "content": {}})

        return runner, None

    eval_runner = AgentHubEvalRunner(supervisor, repository, factory)
    _record, facts = await eval_runner.execute(
        run_id="eval-run-c",
        task_id="eval-task-c",
        session_id="eval-session-c",
        workspace_id="eval-workspace-c",
        agent_type="fake",
        budget=AgentRunBudget(wall_time_seconds=5),
        timeout_seconds=5,
    )
    assert facts["final_text"] == "回答内容"
    assert facts["transcript_text"] == "回答内容"
    await supervisor.shutdown()
    await repository.close()


@pytest.mark.asyncio
async def test_eval_runner_caps_each_transcript_event(tmp_path: Path) -> None:
    repository = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repository)
    first, second, third = "甲" * 30_000, "乙" * 30_000, "丙" * 30_000

    def factory(_spec):
        async def runner(emit):
            await emit({"type": "text", "content": {"text": first}})
            await emit({"type": "text", "content": {"text": second}})
            await emit({"type": "text", "content": {"text": third}})
            await emit({"type": "done", "content": {}})

        return runner, None

    eval_runner = AgentHubEvalRunner(supervisor, repository, factory)
    _record, facts = await eval_runner.execute(
        run_id="eval-run-d",
        task_id="eval-task-d",
        session_id="eval-session-d",
        workspace_id="eval-workspace-d",
        agent_type="fake",
        budget=AgentRunBudget(wall_time_seconds=30),
        timeout_seconds=30,
    )
    # Structural compression caps every event instead of letting one giant
    # reply consume the whole transcript budget.
    assert facts["transcript_text"].count("…[+26000 chars]") == 3
    assert "甲" * 4_000 in facts["transcript_text"]
    assert facts["final_text"] == third[:20_000] + "…[truncated]"
    await supervisor.shutdown()
    await repository.close()


@pytest.mark.asyncio
async def test_eval_runner_transcript_keeps_tool_headers_and_bounded_results(tmp_path: Path) -> None:
    repository = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repository)
    huge_output = "x" * 5_000

    def factory(_spec):
        async def runner(emit):
            call = {"tool_call_id": "call-1", "tool": "read", "args": {"path": "solution.py"}}
            result = {"tool_call_id": "call-1", "tool": "read", "result": huge_output, "status": "success"}
            await emit({"type": "tool_call", "content": call})
            await emit({"type": "tool_result", "content": result})
            await emit({"type": "text", "content": {"text": "已定位缺陷并修复"}})
            await emit({"type": "done", "content": {}})

        return runner, None

    eval_runner = AgentHubEvalRunner(supervisor, repository, factory)
    _record, facts = await eval_runner.execute(
        run_id="eval-run-f",
        task_id="eval-task-f",
        session_id="eval-session-f",
        workspace_id="eval-workspace-f",
        agent_type="fake",
        budget=AgentRunBudget(wall_time_seconds=30),
        timeout_seconds=30,
    )
    transcript = facts["transcript_text"]
    result_line = transcript.split("← read#call-1")[1].split("\n")[0]
    assert "→ read#call-1" in transcript and '"path":"solution.py"' in transcript  # call header + args
    assert "← read#call-1 [success]" in transcript  # result header keeps status
    assert "x" * 500 in result_line and "x" * 501 not in result_line  # excerpt capped
    assert "+4500 chars" in transcript  # dropped bulk is accounted for
    assert "已定位缺陷并修复" in transcript  # reasoning text stays complete
    await supervisor.shutdown()
    await repository.close()


@pytest.mark.asyncio
async def test_eval_runner_collects_done_event_beyond_5000_events(tmp_path: Path) -> None:
    repository = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repository)

    def factory(_spec):
        async def runner(emit):
            await emit({"type": "text", "content": {"text": "开头的回答"}})  # seq 1: head window
            for index in range(5001):
                await emit({"type": "tool_call", "content": {"tool_call_id": str(index), "tool": "step"}})
            done_event = {
                "type": "done",
                "content": {"final_text": "尾部结论", "usage": {"total_tokens": 99}, "trace_id": "trace-z"},
            }
            await emit(done_event)

        return runner, None

    eval_runner = AgentHubEvalRunner(supervisor, repository, factory)
    _record, facts = await eval_runner.execute(
        run_id="eval-run-e",
        task_id="eval-task-e",
        session_id="eval-session-e",
        workspace_id="eval-workspace-e",
        agent_type="fake",
        budget=AgentRunBudget(wall_time_seconds=120),
        timeout_seconds=120,
    )
    assert facts["final_text"] == "尾部结论"  # trailing done event survives the 5000-row cap
    assert facts["total_tokens"] == 99
    assert facts["trace_id"] == "trace-z"
    await supervisor.shutdown()
    await repository.close()
