from fastapi import APIRouter, Depends, HTTPException

from src.api.dependencies import get_run_supervisor, get_session_manager
from src.app.config import settings
from src.execution.supervisor import RunSupervisor
from src.generated.agent_run import AgentRunTerminationReason
from src.session.manager import SessionManager

router = APIRouter(prefix="/v1/session", tags=["session"])


@router.get("")
async def list_sessions(
    mgr: SessionManager = Depends(get_session_manager),
) -> list[dict]:
    sessions = mgr.list()
    return [
        {
            "session_id": s.id,
            "agent_type": s.agent_type,
            "state": s.state.value,
            "created_at": s.created_at.isoformat(),
            "last_active": s.last_active.isoformat(),
        }
        for s in sessions
    ]


@router.get("/{session_id}")
async def get_session(
    session_id: str,
    mgr: SessionManager = Depends(get_session_manager),
) -> dict:
    session = mgr.get(session_id)
    if not session:
        raise HTTPException(status_code=404, detail="Session not found")
    return {
        "session_id": session.id,
        "agent_type": session.agent_type,
        "state": session.state.value,
        "workspace_path": session.workspace_path,
        "created_at": session.created_at.isoformat(),
        "last_active": session.last_active.isoformat(),
        "history_count": len(session.history),
    }


@router.post("/{session_id}/interrupt")
async def interrupt_session(
    session_id: str,
    mgr: SessionManager = Depends(get_session_manager),
    supervisor: RunSupervisor = Depends(get_run_supervisor),
) -> dict:
    session = mgr.get(session_id)
    if not session:
        raise HTTPException(status_code=404, detail="Session not found")
    cancelled = await supervisor.cancel_session(session_id, AgentRunTerminationReason.USER_CANCELLED)
    if not cancelled:
        return {"message": "session has no supervised run", "cancelled_runs": 0}
    records = [
        await supervisor.wait_until_terminal(
            record.spec.run_id,
            settings.execution.process_terminate_timeout + 1,
        )
        for record in cancelled
    ]
    converged = all(record and record.terminal for record in records)
    return {
        "message": "session interrupted" if converged else "session cancellation pending",
        "cancelled_runs": len(cancelled),
        "converged": converged,
    }


@router.delete("/{session_id}")
async def delete_session(
    session_id: str,
    mgr: SessionManager = Depends(get_session_manager),
    supervisor: RunSupervisor = Depends(get_run_supervisor),
) -> dict:
    cancelled = await supervisor.cancel_session(session_id, AgentRunTerminationReason.SESSION_DELETED)
    for record in cancelled:
        await supervisor.wait_until_terminal(
            record.spec.run_id,
            settings.execution.process_terminate_timeout + 1,
        )
    destroyed = await mgr.destroy(session_id)
    if not destroyed:
        raise HTTPException(status_code=404, detail="Session not found")
    return {"message": "session destroyed"}
