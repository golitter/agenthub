from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Query

from src.api.dependencies import (
    get_conflict_recovery_coordinator,
    get_integration_service,
    get_run_supervisor,
)
from src.execution.supervisor import RunSupervisor
from src.generated.agent_run import AgentRunState, AgentRunTerminationReason, CancelAgentRunRequest
from src.integration.errors import (
    ERROR_CAPABILITY_INVALID,
    ERROR_INTEGRATION_RESULT_INVALID,
    ERROR_OPERATION_NOT_FOUND,
    IntegrationError,
)
from src.integration.service import IntegrationService
from src.integration.recovery import ConflictActionInput, ConflictRecoveryCoordinator
from pydantic import BaseModel, Field

router = APIRouter(prefix="/v1/runs", tags=["runs"])


class ResumeRunRequest(BaseModel):
    action: str = Field(min_length=1, max_length=64)
    task_id: str = Field(min_length=1, max_length=128)
    session_id: str = Field(min_length=1, max_length=128)
    root_run_id: str = Field(min_length=1, max_length=128)
    conflict_id: str = Field(min_length=1, max_length=128)
    expected_attempt: int = Field(ge=0, le=1000)
    confirmation: bool = False
    idempotency_key: str = Field(default="", max_length=128)
    resolver_agent: str = Field(default="", max_length=128)


def _status(record) -> dict:
    return {
        "run_id": record.spec.run_id,
        "root_run_id": record.spec.root_run_id,
        "parent_run_id": record.spec.parent_run_id,
        "plan_task_id": record.spec.plan_task_id,
        "integration_operation_id": record.spec.integration_operation_id,
        "workspace_id": record.spec.workspace_id,
        "workspace_handle": record.spec.workspace_handle,
        "integration_attempt": record.spec.integration_attempt,
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
    integration_service: IntegrationService = Depends(get_integration_service),
) -> dict:
    reason = request.reason if request and request.reason else AgentRunTerminationReason.USER_CANCELLED
    # Fence every known operation before stopping the process. This closes the
    # pending -> cancelled window so a late taskctl capability cannot start Git
    # after the user has cancelled the parent Run.
    async def cancel_operation_tree(current_run_id: str) -> None:
        current = await supervisor.repository.get(current_run_id)
        if current and current.spec.integration_operation_id:
            await integration_service.cancel(current.spec.integration_operation_id, reason.value)
        for child in await supervisor.repository.children(current_run_id):
            await cancel_operation_tree(child.spec.run_id)

    existing = await supervisor.repository.get(run_id)
    if not existing:
        raise HTTPException(status_code=404, detail="Run not found")
    await cancel_operation_tree(run_id)
    record = await supervisor.cancel(run_id, reason)
    if not record:
        raise HTTPException(status_code=404, detail="Run not found")
    current = await supervisor.repository.get(run_id)
    state = current.state if current else AgentRunState.CANCELLED
    return {"run_id": run_id, "state": state.value, "accepted": True}


@router.post("/{run_id}/resume")
async def resume_run(
    run_id: str,
    request: ResumeRunRequest,
    coordinator: ConflictRecoveryCoordinator = Depends(get_conflict_recovery_coordinator),
) -> dict:
    if request.root_run_id != run_id:
        raise HTTPException(status_code=409, detail="root_run_id does not match path")
    try:
        return await coordinator.handle_action(
            ConflictActionInput(
                action=request.action,
                task_id=request.task_id,
                session_id=request.session_id,
                root_run_id=request.root_run_id,
                conflict_id=request.conflict_id,
                expected_attempt=request.expected_attempt,
                confirmation=request.confirmation,
                idempotency_key=request.idempotency_key,
                resolver_agent=request.resolver_agent,
            )
        )
    except IntegrationError as exc:
        if exc.code == ERROR_OPERATION_NOT_FOUND:
            status = 404
        elif exc.code == ERROR_INTEGRATION_RESULT_INVALID:
            status = 400
        elif exc.code == ERROR_CAPABILITY_INVALID:
            status = 401
        else:
            status = 409
        raise HTTPException(status_code=status, detail={"code": exc.code, "message": exc.message}) from exc
