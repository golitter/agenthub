from __future__ import annotations

from collections.abc import Awaitable, Callable
from datetime import datetime
from typing import Any

from src.execution.models import RunRecord, RunSpec
from src.execution.repository import SQLiteRunRepository
from src.execution.supervisor import CancelHook, Runner, RunSupervisor
from src.generated.agent_run import AgentRunBudget, AgentRunTerminationReason

from .transcript import build_transcript

RunnerFactory = Callable[[RunSpec], tuple[Runner, CancelHook | None]]
IntegrationFactProvider = Callable[[str], Awaitable[dict[str, Any]]]


class EvalRunTimeoutError(RuntimeError):
    pass


class AgentHubEvalRunner:
    """Run bridge that keeps Eval identity in EvalStore, not RunSpec."""

    def __init__(
        self,
        supervisor: RunSupervisor,
        run_repository: SQLiteRunRepository,
        runner_factory: RunnerFactory,
        integration_fact_provider: IntegrationFactProvider | None = None,
    ) -> None:
        self.supervisor = supervisor
        self.run_repository = run_repository
        self.runner_factory = runner_factory
        self.integration_fact_provider = integration_fact_provider

    async def execute(
        self,
        *,
        run_id: str,
        task_id: str,
        session_id: str,
        workspace_id: str,
        agent_type: str,
        budget: AgentRunBudget,
        timeout_seconds: float,
    ) -> tuple[RunRecord, dict[str, Any]]:
        spec = RunSpec(
            run_id=run_id,
            root_run_id=run_id,
            task_id=task_id,
            session_id=session_id,
            workspace_id=workspace_id,
            agent_type=agent_type,
            requested_by="eval",
            request_fingerprint=f"eval:{run_id}",
            budget=budget,
        )
        runner, cancel_hook = self.runner_factory(spec)
        await self.supervisor.start(spec, runner, cancel_hook=cancel_hook)
        record = await self.supervisor.wait_until_terminal(run_id, timeout_seconds)
        if record is None:
            raise KeyError(run_id)
        if not record.terminal:
            await self.supervisor.cancel(run_id, AgentRunTerminationReason.WALL_TIME_EXCEEDED)
            record = await self.run_repository.get(run_id)
            raise EvalRunTimeoutError(f"eval run did not converge before timeout: {run_id}")
        facts = await self.collect_facts(record)
        return record, facts

    async def collect_facts(self, record: RunRecord) -> dict[str, Any]:
        events = await self.run_repository.read_events(record.spec.run_id, 0, limit=5000)
        # read_events caps a single call at 5000 rows; long runs would silently
        # drop the trailing `done` event (final text, usage, trace id), so the
        # tail window is read and merged explicitly.
        last_seq = record.last_event_seq or 0
        if last_seq > 5000:
            tail = await self.run_repository.read_events(
                record.spec.run_id, max(0, last_seq - 5000), limit=5000
            )
            seen = {envelope.seq for envelope in events}
            events = events + [envelope for envelope in tail if envelope.seq not in seen]
            events.sort(key=lambda envelope: envelope.seq)
        total_tokens: int | None = None
        trace_id: str | None = None
        first_action_at: float | None = None
        first_text_at: float | None = None
        last_text: str | None = None
        for envelope in events:
            event = envelope.event
            event_type = event.get("type")
            content = event.get("content", {}) if isinstance(event.get("content"), dict) else {}
            if event_type in {"text", "tool_call", "error", "done"} and first_action_at is None:
                first_action_at = envelope.timestamp
            if event_type == "text":
                if first_text_at is None:
                    first_text_at = envelope.timestamp
                text = content.get("text")
                if isinstance(text, str) and text.strip():
                    last_text = text
            if event_type == "done":
                usage = content.get("usage")
                if isinstance(usage, dict):
                    value = usage.get("total", usage.get("total_tokens"))
                    if isinstance(value, (int, float)) and not isinstance(value, bool):
                        total_tokens = int(value)
                candidate_trace = content.get("trace_id")
                if isinstance(candidate_trace, str):
                    trace_id = candidate_trace
                candidate_final = content.get("final_text")
                if isinstance(candidate_final, str) and candidate_final.strip():
                    last_text = candidate_final
        # Deterministic structural compression: text stays readable, tool
        # activity is reduced to call headers plus bounded result excerpts.
        transcript_text = build_transcript(envelope.event for envelope in events)
        if last_text is not None and len(last_text) > 20_000:
            final_text = last_text[:20_000] + "…[truncated]"
        else:
            final_text = last_text
        started = _timestamp(record.started_at)
        finished = _timestamp(record.finished_at)
        retries = await self._retry_count(record.spec.run_id)
        facts = {
            "root_run_id": record.spec.run_id,
            "trace_id": trace_id,
            "run_state": record.state.value,
            "termination_reason": record.termination_reason,
            "duration_seconds": finished - started if started is not None and finished is not None else None,
            "ttfa_seconds": first_action_at - started if started is not None and first_action_at is not None else None,
            "ttft_seconds": first_text_at - started if started is not None and first_text_at is not None else None,
            "total_tokens": total_tokens,
            "retry_count": retries,
            "event_cursor": record.last_event_seq,
            "final_text": final_text,
            "transcript_text": transcript_text or None,
        }
        if self.integration_fact_provider is not None:
            facts.update(await self.integration_fact_provider(record.spec.run_id))
        return facts

    async def _retry_count(self, root_run_id: str) -> int:
        queue = [root_run_id]
        attempts: dict[str, int] = {}
        while queue:
            parent = queue.pop()
            for child in await self.run_repository.children(parent):
                queue.append(child.spec.run_id)
                logical_id = child.spec.plan_task_id or child.spec.run_id
                attempts[logical_id] = max(attempts.get(logical_id, 0), child.spec.integration_attempt)
        return sum(attempts.values())


def _timestamp(value: str | None) -> float | None:
    if not value:
        return None
    return datetime.fromisoformat(value).timestamp()
