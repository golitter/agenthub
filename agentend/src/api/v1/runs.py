from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Query

from src.api.dependencies import get_run_supervisor
from src.execution.supervisor import RunSupervisor
from src.generated.agent_run import AgentRunState, AgentRunTerminationReason, CancelAgentRunRequest

router = APIRouter(prefix="/v1/runs", tags=["runs"])


def _status(record) -> dict:
    return {
        "run_id": record.spec.run_id,
        "root_run_id": record.spec.root_run_id,
        "parent_run_id": record.spec.parent_run_id,
        "state": record.state.value,
        "termination_reason": record.termination_reason,
        "budget": record.spec.budget.model_dump(mode="json"),
        "last_event_seq": record.last_event_seq,
        "created_at": record.created_at,
        "started_at": record.started_at,
        "finished_at": record.finished_at,
    }


@router.get("")
async def list_active_runs(supervisor: RunSupervisor = Depends(get_run_supervisor)) -> list[dict]:
    return [_status(record) for record in await supervisor.repository.list_active()]


@router.get("/{run_id}")
async def get_run(run_id: str, supervisor: RunSupervisor = Depends(get_run_supervisor)) -> dict:
    record = await supervisor.repository.get(run_id)
    if not record:
        raise HTTPException(status_code=404, detail="Run not found")
    return _status(record)


@router.get("/{run_id}/events")
async def get_run_events(
    run_id: str,
    after_seq: int = Query(0, ge=0),
    wait_seconds: float = Query(0, ge=0, le=30),
    supervisor: RunSupervisor = Depends(get_run_supervisor),
) -> dict:
    record = await supervisor.repository.get(run_id)
    if not record:
        raise HTTPException(status_code=404, detail="Run not found")
    if wait_seconds:
        events, record = await supervisor.wait_for_events(run_id, after_seq, wait_seconds)
    else:
        events = await supervisor.repository.read_events(run_id, after_seq)
    return {
        "events": [event.model_dump(mode="json") for event in events],
        "run": _status(record),
    }


@router.post("/{run_id}/cancel")
async def cancel_run(
    run_id: str,
    request: CancelAgentRunRequest | None = None,
    supervisor: RunSupervisor = Depends(get_run_supervisor),
) -> dict:
    reason = request.reason if request and request.reason else AgentRunTerminationReason.USER_CANCELLED
    record = await supervisor.cancel(run_id, reason)
    if not record:
        raise HTTPException(status_code=404, detail="Run not found")
    current = await supervisor.repository.get(run_id)
    state = current.state if current else AgentRunState.CANCELLED
    return {"run_id": run_id, "state": state.value, "accepted": True}
