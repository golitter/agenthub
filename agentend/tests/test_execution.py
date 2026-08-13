import asyncio
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.api.v1.agent import _validated_budget
from src.execution.models import RunSpec
from src.execution.repository import ParentRunClosedError, RunConflictError, SQLiteRunRepository
from src.execution.supervisor import RunSupervisor
from src.generated.agent_run import AgentRunBudget, AgentRunState, AgentRunTerminationReason


def spec(
    run_id: str,
    *,
    root: str | None = None,
    parent: str | None = None,
    task: str = "task-1",
    request_fingerprint: str = "request-a",
) -> RunSpec:
    return RunSpec(
        run_id=run_id,
        root_run_id=root or run_id,
        parent_run_id=parent,
        task_id=task,
        session_id=f"session-{run_id}",
        workspace_id=f"workspace-{run_id}",
        agent_type="codex",
        request_fingerprint=request_fingerprint,
        budget=AgentRunBudget(wall_time_seconds=5),
    )


@pytest.mark.asyncio
async def test_repository_idempotency_and_spec_conflict(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    first, created = await repo.create(spec("root"))
    assert created and first.state == AgentRunState.QUEUED
    same, created = await repo.create(spec("root"))
    assert not created and same.spec.task_id == "task-1"
    with pytest.raises(RunConflictError):
        await repo.create(spec("root", task="other-task"))
    with pytest.raises(RunConflictError):
        await repo.create(spec("root", request_fingerprint="request-b"))
    await repo.close()


@pytest.mark.asyncio
async def test_repository_rejects_two_active_runs_for_same_session(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    first = spec("first")
    second = spec("second")
    second = RunSpec(
        **{
            **second.__dict__,
            "session_id": first.session_id,
        }
    )
    await repo.create(first)
    with pytest.raises(RunConflictError, match="session already"):
        await repo.create(second)
    await repo.close()


@pytest.mark.asyncio
async def test_parent_admission_fence_rejects_late_child(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    await repo.create(spec("root"))
    await repo.close_admission("root")
    with pytest.raises(ParentRunClosedError):
        await repo.create(spec("child", root="root", parent="root"))
    await repo.close()


@pytest.mark.asyncio
async def test_root_run_must_reference_itself(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    with pytest.raises(ParentRunClosedError):
        await repo.create(spec("not-root", root="different-root"))
    await repo.close()


@pytest.mark.asyncio
async def test_child_must_share_parent_task_and_respect_child_budget(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    root = spec("root")
    root.budget.max_children = 1
    await repo.create(root)
    with pytest.raises(ParentRunClosedError, match="different task"):
        await repo.create(spec("wrong-task", root="root", parent="root", task="task-2"))
    child_1 = spec("child-1", root="root", parent="root")
    child_1.budget.max_children = root.budget.max_children
    await repo.create(child_1)
    child_2 = spec("child-2", root="root", parent="root")
    child_2.budget.max_children = root.budget.max_children
    with pytest.raises(ParentRunClosedError, match="child budget"):
        await repo.create(child_2)
    await repo.close()


@pytest.mark.asyncio
async def test_child_budget_cannot_expand_parent_limits(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    root = spec("root")
    root.budget.max_output_bytes = 1024
    await repo.create(root)
    child = spec("child", root="root", parent="root")
    child.budget.max_output_bytes = 2048

    with pytest.raises(ParentRunClosedError, match="max_output_bytes"):
        await repo.create(child)
    await repo.close()


@pytest.mark.asyncio
async def test_root_budget_caps_all_descendants_and_active_parallelism(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    root = spec("root")
    root.budget.max_children = 1
    root.budget.max_parallelism = 1
    await repo.create(root)
    child = spec("child", root="root", parent="root")
    child.budget.max_children = 1
    child.budget.max_parallelism = 1
    await repo.create(child)

    grandchild = spec("grandchild", root="root", parent="child")
    grandchild.budget.max_children = 1
    grandchild.budget.max_parallelism = 1
    with pytest.raises(ParentRunClosedError, match="root run child budget"):
        await repo.create(grandchild)
    await repo.close()


@pytest.mark.asyncio
async def test_root_parallelism_budget_rejects_second_active_child(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    root = spec("root")
    root.budget.max_children = 3
    root.budget.max_parallelism = 1
    await repo.create(root)
    first = spec("child-1", root="root", parent="root")
    first.budget.max_children = 3
    first.budget.max_parallelism = 1
    await repo.create(first)
    second = spec("child-2", root="root", parent="root")
    second.budget.max_children = 3
    second.budget.max_parallelism = 1

    with pytest.raises(ParentRunClosedError, match="parallelism budget"):
        await repo.create(second)
    await repo.transition("child-1", {AgentRunState.QUEUED}, AgentRunState.COMPLETED)
    _, created = await repo.create(second)
    assert created
    await repo.close()


def test_requested_budget_cannot_expand_server_defaults():
    budget = _validated_budget(
        {
            "max_output_bytes": AgentRunBudget().max_output_bytes * 10,
            "max_event_count": AgentRunBudget().max_event_count * 10,
        }
    )
    assert budget.max_output_bytes == AgentRunBudget().max_output_bytes
    assert budget.max_event_count == AgentRunBudget().max_event_count


@pytest.mark.asyncio
async def test_supervisor_journals_events_and_completes(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repo)

    async def runner(emit):
        await emit({"type": "text", "content": {"text": "hello"}})

    _, created = await supervisor.start(spec("root"), runner)
    assert created
    record = None
    for _ in range(50):
        record = await repo.get("root")
        if record and record.terminal:
            break
        await asyncio.sleep(0.01)
    assert record and record.state == AgentRunState.COMPLETED
    events = await repo.read_events("root", 0)
    assert [event.seq for event in events] == [1]
    assert events[0].event["content"]["text"] == "hello"
    await supervisor.shutdown()
    await repo.close()


@pytest.mark.asyncio
async def test_error_event_marks_run_failed_instead_of_completed(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repo)

    async def runner(emit):
        await emit({"type": "error", "content": {"message": "CLI failed"}})

    await supervisor.start(spec("root"), runner)
    record = await supervisor.wait_until_terminal("root", 1)
    assert record and record.state == AgentRunState.FAILED
    assert record.termination_reason == AgentRunTerminationReason.PROCESS_EXIT_ERROR.value
    await supervisor.shutdown()
    await repo.close()


@pytest.mark.asyncio
async def test_cancel_preserves_requested_reason(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repo)
    started = asyncio.Event()

    async def runner(_emit):
        started.set()
        await asyncio.Event().wait()

    await supervisor.start(spec("root"), runner)
    await started.wait()
    await supervisor.cancel("root", AgentRunTerminationReason.SESSION_DELETED)
    record = None
    for _ in range(50):
        record = await repo.get("root")
        if record and record.terminal:
            break
        await asyncio.sleep(0.01)
    assert record and record.state == AgentRunState.CANCELLED
    assert record.termination_reason == AgentRunTerminationReason.SESSION_DELETED.value
    await supervisor.shutdown()
    await repo.close()


@pytest.mark.asyncio
async def test_cancel_during_starting_transition_converges(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repo)
    entered_starting = asyncio.Event()
    release_starting = asyncio.Event()
    original_transition = repo.transition

    async def delayed_transition(run_id, expected, target, reason=None):
        changed = await original_transition(run_id, expected, target, reason)
        if target == AgentRunState.STARTING and changed:
            entered_starting.set()
            await release_starting.wait()
        return changed

    repo.transition = delayed_transition

    async def runner(_emit):
        raise AssertionError("runner must not start after cancellation")

    await supervisor.start(spec("root"), runner)
    await entered_starting.wait()
    await supervisor.cancel("root")
    release_starting.set()
    record = await supervisor.wait_until_terminal("root", 1)

    assert record and record.state == AgentRunState.CANCELLED
    assert record.termination_reason == AgentRunTerminationReason.USER_CANCELLED.value
    await supervisor.shutdown()
    await repo.close()


@pytest.mark.asyncio
async def test_cancel_racing_with_natural_completion_converges(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repo)
    completion_transition_entered = asyncio.Event()
    release_completion_transition = asyncio.Event()
    cancel_hook_entered = asyncio.Event()
    release_cancel_hook = asyncio.Event()
    original_transition = repo.transition

    async def delayed_completion(run_id, expected, target, reason=None):
        if target == AgentRunState.COMPLETED:
            completion_transition_entered.set()
            await release_completion_transition.wait()
        return await original_transition(run_id, expected, target, reason)

    repo.transition = delayed_completion

    async def runner(_emit):
        return None

    async def cancel_hook():
        cancel_hook_entered.set()
        await release_cancel_hook.wait()

    await supervisor.start(spec("root"), runner, cancel_hook)
    await completion_transition_entered.wait()
    cancel_task = asyncio.create_task(supervisor.cancel("root"))
    await cancel_hook_entered.wait()
    release_completion_transition.set()
    for _ in range(50):
        task = supervisor._tasks.get("root")
        if task is None or task.done():
            break
        await asyncio.sleep(0.01)
    release_cancel_hook.set()
    cancelled = await cancel_task

    assert cancelled and cancelled.state == AgentRunState.CANCELLED
    assert cancelled.termination_reason == AgentRunTerminationReason.USER_CANCELLED.value
    await supervisor.shutdown()
    await repo.close()


@pytest.mark.asyncio
async def test_cancelling_child_does_not_close_root_to_new_siblings(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repo, max_concurrent_runs=2)
    root_started = asyncio.Event()
    child_started = asyncio.Event()

    async def root_runner(_emit):
        root_started.set()
        await asyncio.Event().wait()

    async def child_runner(_emit):
        child_started.set()
        await asyncio.Event().wait()

    await supervisor.start(spec("root"), root_runner)
    await root_started.wait()
    await supervisor.start(spec("child-1", root="root", parent="root"), child_runner)
    await child_started.wait()
    await supervisor.cancel("child-1")
    await supervisor.wait_until_terminal("child-1", 1)

    _, created = await repo.create(spec("child-2", root="root", parent="root"))
    assert created

    await supervisor.cancel("root")
    await supervisor.shutdown()
    await repo.close()


@pytest.mark.asyncio
async def test_recovery_cancels_non_terminal_runs(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    await repo.create(spec("root"))
    await repo.transition("root", {AgentRunState.QUEUED}, AgentRunState.RUNNING)
    supervisor = RunSupervisor(repo)
    await supervisor.recover()
    record = await repo.get("root")
    assert record and record.state == AgentRunState.CANCELLED
    assert record.termination_reason == AgentRunTerminationReason.AGENTEND_RECOVERY.value
    events = await repo.read_events("root", 0)
    assert events[-1].event["content"]["termination_reason"] == "agentend_recovery"
    await repo.close()


@pytest.mark.asyncio
async def test_cancel_queued_run_reaches_terminal_state(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repo, max_concurrent_runs=1)
    started = asyncio.Event()

    async def blocking(_emit):
        started.set()
        await asyncio.Event().wait()

    async def queued(_emit):
        raise AssertionError("queued runner must never start")

    await supervisor.start(spec("first"), blocking)
    await started.wait()
    await supervisor.start(spec("second"), queued)
    cancelled = await supervisor.cancel("second")

    assert cancelled and cancelled.state == AgentRunState.CANCELLED
    assert cancelled.termination_reason == AgentRunTerminationReason.USER_CANCELLED.value
    events = await repo.read_events("second", 0)
    assert events[-1].event["content"]["termination_reason"] == "user_cancelled"

    await supervisor.cancel("first")
    await supervisor.shutdown()
    await repo.close()


@pytest.mark.asyncio
async def test_cancel_queued_parent_recursively_cancels_existing_child(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    root = spec("root")
    await repo.create(root)
    await repo.create(spec("child", root="root", parent="root"))
    supervisor = RunSupervisor(repo)

    cancelled = await supervisor.cancel("root")
    child = await repo.get("child")

    assert cancelled and cancelled.state == AgentRunState.CANCELLED
    assert child and child.state == AgentRunState.CANCELLED
    assert child.termination_reason == AgentRunTerminationReason.PARENT_CANCELLED.value
    await supervisor.shutdown()
    await repo.close()


@pytest.mark.asyncio
async def test_wall_timeout_invokes_process_cancel_hook(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repo)
    hook_called = asyncio.Event()

    async def blocking(_emit):
        await asyncio.Event().wait()

    async def cancel_hook():
        hook_called.set()

    timeout_spec = spec("timeout")
    timeout_spec.budget.wall_time_seconds = 0
    await supervisor.start(timeout_spec, blocking, cancel_hook)

    for _ in range(50):
        record = await repo.get("timeout")
        if record and record.terminal:
            break
        await asyncio.sleep(0.01)

    assert hook_called.is_set()
    assert record and record.state == AgentRunState.FAILED
    assert record.termination_reason == AgentRunTerminationReason.WALL_TIME_EXCEEDED.value
    await supervisor.shutdown()
    await repo.close()


@pytest.mark.asyncio
async def test_output_budget_fails_run(tmp_path: Path):
    repo = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    supervisor = RunSupervisor(repo)

    async def noisy(emit):
        await emit({"type": "text", "content": {"text": "too-large"}})

    limited = spec("output-limit")
    limited.budget.max_output_bytes = 1
    await supervisor.start(limited, noisy)
    for _ in range(50):
        record = await repo.get("output-limit")
        if record and record.terminal:
            break
        await asyncio.sleep(0.01)

    assert record and record.state == AgentRunState.FAILED
    assert record.termination_reason == AgentRunTerminationReason.OUTPUT_LIMIT_EXCEEDED.value
    await supervisor.shutdown()
    await repo.close()
