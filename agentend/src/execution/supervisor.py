from __future__ import annotations

import asyncio
import json
import time
from collections.abc import Awaitable, Callable

from src.execution.models import RunRecord, RunSpec
from src.execution.repository import SQLiteRunRepository
from src.generated.agent_run import AgentRunState, AgentRunTerminationReason

Emit = Callable[[dict], Awaitable[None]]
Runner = Callable[[Emit], Awaitable[None]]
CancelHook = Callable[[], Awaitable[None]]


class RunSupervisor:
    def __init__(self, repository: SQLiteRunRepository, max_concurrent_runs: int = 4) -> None:
        self.repository = repository
        self._slots = asyncio.Semaphore(max(1, max_concurrent_runs))
        self._tasks: dict[str, asyncio.Task[None]] = {}
        self._cancel_hooks: dict[str, CancelHook] = {}
        self._changed = asyncio.Condition()

    async def start(self, spec: RunSpec, runner: Runner, cancel_hook: CancelHook | None = None) -> tuple[RunRecord, bool]:
        record, created = await self.repository.create(spec)
        if not created:
            return record, False
        if cancel_hook:
            self._cancel_hooks[spec.run_id] = cancel_hook
        task = asyncio.create_task(self._run(spec, runner), name=f"agent-run:{spec.run_id}")
        self._tasks[spec.run_id] = task
        task.add_done_callback(lambda _task, rid=spec.run_id: self._forget(rid))
        return record, True

    async def resume(
        self,
        run_id: str,
        runner: Runner,
        cancel_hook: CancelHook | None = None,
    ) -> tuple[RunRecord | None, bool]:
        """Resume a durable conflict-paused root Run in this process.

        The original graph producer is intentionally not recreated here. The
        conflict coordinator supplies a bounded continuation runner that owns
        the persisted Resolver/manual action and emits the remaining root
        events through the same Run journal/SSE connection.
        """
        record = await self.repository.get(run_id)
        if not record:
            return None, False
        if record.state != AgentRunState.AWAITING_RESOLUTION:
            return record, False
        current_task = self._tasks.get(run_id)
        if current_task and not current_task.done():
            return record, False
        await self.repository.open_admission(run_id)
        transitioned = await self.repository.transition(
            run_id,
            {AgentRunState.AWAITING_RESOLUTION},
            AgentRunState.RUNNING,
        )
        if not transitioned:
            return await self.repository.get(run_id), False
        if cancel_hook:
            self._cancel_hooks[run_id] = cancel_hook
        task = asyncio.create_task(
            self._run(record.spec, runner, resumed=True),
            name=f"agent-run-resume:{run_id}",
        )
        self._tasks[run_id] = task
        task.add_done_callback(lambda _task, rid=run_id: self._forget(rid))
        return await self.repository.get(run_id), True

    def _forget(self, run_id: str) -> None:
        self._tasks.pop(run_id, None)
        self._cancel_hooks.pop(run_id, None)

    async def _run(self, spec: RunSpec, runner: Runner, resumed: bool = False) -> None:
        try:
            await self._run_inner(spec, runner, resumed=resumed)
        except asyncio.CancelledError:
            record = await self.repository.get(spec.run_id)
            if record and not record.terminal:
                reason = record.termination_reason or AgentRunTerminationReason.USER_CANCELLED.value
                await self.repository.append_event(
                    spec.run_id,
                    {"type": "error", "content": {"message": "run cancelled", "termination_reason": reason}},
                    time.time(),
                )
                await self.repository.transition(
                    spec.run_id,
                    {
                        AgentRunState.QUEUED,
                        AgentRunState.STARTING,
                        AgentRunState.RUNNING,
                        AgentRunState.CANCELLING,
                        AgentRunState.AWAITING_RESOLUTION,
                    },
                    AgentRunState.CANCELLED,
                    reason,
                )
                async with self._changed:
                    self._changed.notify_all()
            raise

    async def _run_inner(self, spec: RunSpec, runner: Runner, *, resumed: bool = False) -> None:
        async with self._slots:
            if not resumed:
                if not await self.repository.transition(
                    spec.run_id, {AgentRunState.QUEUED}, AgentRunState.STARTING
                ):
                    return
                await self.repository.transition(spec.run_id, {AgentRunState.STARTING}, AgentRunState.RUNNING)
            output_bytes = 0
            terminal_error_reason: str | None = None
            awaiting_resolution = False

            async def emit(event: dict) -> None:
                nonlocal output_bytes, terminal_error_reason, awaiting_resolution
                record = await self.repository.get(spec.run_id)
                if not record or record.terminal:
                    return
                if record.last_event_seq >= spec.budget.max_event_count:
                    raise RuntimeError(AgentRunTerminationReason.EVENT_LIMIT_EXCEEDED.value)
                output_bytes += len(json.dumps(event, separators=(",", ":"), ensure_ascii=False).encode("utf-8"))
                if output_bytes > spec.budget.max_output_bytes:
                    raise RuntimeError(AgentRunTerminationReason.OUTPUT_LIMIT_EXCEEDED.value)
                await self.repository.append_event(spec.run_id, event, time.time())
                if event.get("type") == "error":
                    content = event.get("content")
                    reason = content.get("termination_reason") if isinstance(content, dict) else None
                    allowed = {item.value for item in AgentRunTerminationReason}
                    terminal_error_reason = (
                        reason if reason in allowed else AgentRunTerminationReason.PROCESS_EXIT_ERROR.value
                    )
                elif event.get("type") == "orchestrator_paused":
                    # The graph deliberately ends its producer after this
                    # event, but the root Run must remain durable and
                    # resumable instead of being mistaken for completed.
                    awaiting_resolution = True
                async with self._changed:
                    self._changed.notify_all()

            try:
                await asyncio.wait_for(runner(emit), timeout=spec.budget.wall_time_seconds)
                if awaiting_resolution and not terminal_error_reason:
                    await self.repository.transition(
                        spec.run_id,
                        {AgentRunState.RUNNING},
                        AgentRunState.AWAITING_RESOLUTION,
                    )
                elif terminal_error_reason:
                    await self.repository.transition(
                        spec.run_id,
                        {AgentRunState.RUNNING},
                        AgentRunState.FAILED,
                        terminal_error_reason,
                    )
                else:
                    await self.repository.transition(
                        spec.run_id, {AgentRunState.RUNNING}, AgentRunState.COMPLETED
                    )
            except asyncio.TimeoutError:
                await self._invoke_cancel_hook(spec.run_id)
                await self.repository.append_event(
                    spec.run_id,
                    {
                        "type": "error",
                        "content": {
                            "message": "run wall time exceeded",
                            "termination_reason": AgentRunTerminationReason.WALL_TIME_EXCEEDED.value,
                        },
                    },
                    time.time(),
                )
                await self.repository.transition(
                    spec.run_id,
                    {AgentRunState.RUNNING, AgentRunState.STARTING},
                    AgentRunState.FAILED,
                    AgentRunTerminationReason.WALL_TIME_EXCEEDED.value,
                )
            except Exception as exc:
                reason = str(exc)
                allowed = {item.value for item in AgentRunTerminationReason}
                if reason not in allowed:
                    reason = AgentRunTerminationReason.PROCESS_EXIT_ERROR.value
                await self.repository.append_event(
                    spec.run_id,
                    {"type": "error", "content": {"message": "run failed", "termination_reason": reason}},
                    time.time(),
                )
                await self.repository.transition(
                    spec.run_id,
                    {AgentRunState.RUNNING, AgentRunState.STARTING},
                    AgentRunState.FAILED,
                    reason,
                )
            finally:
                async with self._changed:
                    self._changed.notify_all()

    async def _invoke_cancel_hook(self, run_id: str) -> None:
        hook = self._cancel_hooks.get(run_id)
        if not hook:
            return
        try:
            await hook()
        except Exception:
            # Cancellation must still converge even when the process adapter
            # has already exited or its best-effort cleanup fails.
            pass

    async def cancel(
        self,
        run_id: str,
        reason: AgentRunTerminationReason = AgentRunTerminationReason.USER_CANCELLED,
    ) -> RunRecord | None:
        record = await self.repository.get(run_id)
        if not record or record.terminal:
            return record
        if run_id == record.spec.root_run_id:
            await self.repository.close_admission(record.spec.root_run_id)
        await self.repository.close_admission(run_id)

        # Descendants can already be admitted while a parent is queued.  The
        # root admission fence above prevents new descendants from racing this
        # snapshot, so every existing branch must be cancelled before return.
        for child in await self.repository.children(run_id):
            if not child.terminal:
                await self.cancel(child.spec.run_id, AgentRunTerminationReason.PARENT_CANCELLED)

        if record.state == AgentRunState.QUEUED:
            await self.repository.append_event(
                run_id,
                {"type": "error", "content": {"message": "run cancelled", "termination_reason": reason.value}},
                time.time(),
            )
            await self.repository.transition(
                run_id,
                {AgentRunState.QUEUED},
                AgentRunState.CANCELLED,
                reason.value,
            )
            task = self._tasks.get(run_id)
            if task and not task.done():
                task.cancel()
            async with self._changed:
                self._changed.notify_all()
            return await self.repository.get(run_id)

        if record.state == AgentRunState.AWAITING_RESOLUTION:
            await self.repository.append_event(
                run_id,
                {"type": "error", "content": {"message": "run cancelled", "termination_reason": reason.value}},
                time.time(),
            )
            await self.repository.transition(
                run_id,
                {AgentRunState.AWAITING_RESOLUTION},
                AgentRunState.CANCELLED,
                reason.value,
            )
            async with self._changed:
                self._changed.notify_all()
            return await self.repository.get(run_id)

        await self.repository.transition(
            run_id,
            {AgentRunState.QUEUED, AgentRunState.STARTING, AgentRunState.RUNNING},
            AgentRunState.CANCELLING,
            reason.value,
        )
        await self._invoke_cancel_hook(run_id)
        task = self._tasks.get(run_id)
        if task and not task.done():
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)

        # The runner can finish between RUNNING -> CANCELLING and task.cancel().
        # In that race its normal RUNNING -> COMPLETED CAS deliberately loses;
        # cancellation must therefore perform the final convergence itself.
        current = await self.repository.get(run_id)
        if current and not current.terminal:
            await self.repository.append_event(
                run_id,
                {"type": "error", "content": {"message": "run cancelled", "termination_reason": reason.value}},
                time.time(),
            )
            await self.repository.transition(
                run_id,
                {AgentRunState.CANCELLING, AgentRunState.STARTING, AgentRunState.RUNNING},
                AgentRunState.CANCELLED,
                reason.value,
            )
            async with self._changed:
                self._changed.notify_all()
        return await self.repository.get(run_id)

    async def cancel_session(
        self,
        session_id: str,
        reason: AgentRunTerminationReason = AgentRunTerminationReason.USER_CANCELLED,
    ) -> list[RunRecord]:
        cancelled: list[RunRecord] = []
        for record in await self.repository.list_active_by_session(session_id):
            current = await self.cancel(record.spec.run_id, reason)
            if current:
                cancelled.append(current)
        return cancelled

    async def wait_for_events(
        self, run_id: str, after_seq: int, timeout: float = 15.0
    ) -> tuple[list, RunRecord | None]:
        async with self._changed:
            events = await self.repository.read_events(run_id, after_seq)
            record = await self.repository.get(run_id)
            if events or not record or record.terminal:
                return events, record
            try:
                await asyncio.wait_for(self._changed.wait(), timeout=timeout)
            except asyncio.TimeoutError:
                pass
        return await self.repository.read_events(run_id, after_seq), await self.repository.get(run_id)

    async def wait_until_terminal(self, run_id: str, timeout: float) -> RunRecord | None:
        deadline = asyncio.get_running_loop().time() + max(0.0, timeout)
        while True:
            async with self._changed:
                record = await self.repository.get(run_id)
                if not record or record.terminal:
                    return record
                remaining = deadline - asyncio.get_running_loop().time()
                if remaining <= 0:
                    return record
                try:
                    await asyncio.wait_for(self._changed.wait(), timeout=remaining)
                except asyncio.TimeoutError:
                    return await self.repository.get(run_id)

    async def recover(self, preserve_run_ids: set[str] | None = None) -> None:
        # ``awaiting_resolution`` is an intentional durable pause. It must
        # survive AgentEnd restart; only in-flight execution states are
        # converged to recovery cancellation here.
        preserved = preserve_run_ids or set()
        for record in await self.repository.list_active():
            if record.state == AgentRunState.AWAITING_RESOLUTION:
                continue
            if record.spec.run_id in preserved:
                await self.repository.append_event(
                    record.spec.run_id,
                    {
                        "type": "orchestrator_paused",
                        "content": {
                            "task_id": record.spec.task_id,
                            "run_id": record.spec.run_id,
                            "root_run_id": record.spec.root_run_id,
                            "status": "awaiting_user",
                            "reason": "resolver continuation scheduled after AgentEnd recovery",
                        },
                    },
                    time.time(),
                )
                await self.repository.transition(
                    record.spec.run_id,
                    {record.state},
                    AgentRunState.AWAITING_RESOLUTION,
                )
                continue
            await self.repository.append_event(
                record.spec.run_id,
                {
                    "type": "error",
                    "content": {
                        "message": "run cancelled during AgentEnd recovery",
                        "termination_reason": AgentRunTerminationReason.AGENTEND_RECOVERY.value,
                    },
                },
                time.time(),
            )
            await self.repository.transition(
                record.spec.run_id,
                {record.state},
                AgentRunState.CANCELLED,
                AgentRunTerminationReason.AGENTEND_RECOVERY.value,
            )

    async def shutdown(self) -> None:
        active = list(await self.repository.list_active())
        for record in active:
            if record.state == AgentRunState.AWAITING_RESOLUTION:
                # A deliberate conflict pause owns no process resources. Keep
                # it in SQLite across a graceful AgentEnd restart so a later
                # manual action can inspect or resume it.
                continue
            await self.cancel(record.spec.run_id, AgentRunTerminationReason.AGENTEND_SHUTDOWN)
        tasks = list(self._tasks.values())
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
