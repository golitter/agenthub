import asyncio
import uuid

from fastapi import APIRouter, Depends, HTTPException
from sse_starlette.sse import EventSourceResponse

from src.adapters.base import BaseAgentAdapter
from src.adapters.registry import AdapterRegistry
from src.api.dependencies import get_adapter_registry, get_rule_engine, get_session_manager, get_session_store
from src.app.config import settings
from src.rules.engine import RuleEngine
from src.schemas.request import AgentRequest
from src.schemas.response import AgentResponse
from src.session.manager import SessionManager
from src.session.models import SessionState
from src.session.store import SessionMappingStore

router = APIRouter(prefix="/v1/agent", tags=["agent"])


async def _resolve_session(
    request: AgentRequest,
    session_mgr: SessionManager,
    session_store: SessionMappingStore,
) -> tuple[str, str | None, bool]:
    """Return (internal_session_id, cli_session_id, is_resume).

    - cli_session_id=None, is_resume=False → new CLI session (--session-id <new_uuid>)
    - cli_session_id=<id>, is_resume=True  → resume CLI session (--resume <id>)
    """
    if request.session_id:
        cli_session_id = session_store.get_cli_session_id(request.session_id)
        if cli_session_id:
            # Found mapping — resume existing CLI session
            session = session_mgr.get(request.session_id)
            if not session:
                session = session_mgr.create(
                    agent_type=request.agent_type,
                    workspace_path=request.workspace_path,
                )
            return session.id, cli_session_id, True

        # No mapping yet — assign a new CLI session UUID and persist
        new_cli_session_id = str(uuid.uuid4())
        session_store.set_cli_session_id(request.session_id, new_cli_session_id)

        session = session_mgr.get(request.session_id)
        if not session:
            session = session_mgr.create(
                agent_type=request.agent_type,
                workspace_path=request.workspace_path,
            )
        return session.id, new_cli_session_id, False

    # No session_id provided — create a fresh one-shot session
    session = session_mgr.create(
        agent_type=request.agent_type,
        workspace_path=request.workspace_path,
    )
    return session.id, None, False


def _build_rule_context(request: AgentRequest) -> dict:
    return {
        "message": request.message,
        "agent_type": request.agent_type,
        "workspace_path": request.workspace_path,
        "allowed_tools": request.config.get("allowed_tools", []) if request.config else [],
    }


async def _execute_stream(
    request: AgentRequest,
    adapter: BaseAgentAdapter,
    session_id: str,
    cli_session_id: str | None,
    is_resume: bool,
    rule_result: dict,
    session_mgr: SessionManager,
):
    session_mgr.update_state(session_id, SessionState.RUNNING)
    session_mgr.record_history(session_id, {"role": "user", "content": request.message})

    try:
        async for event in adapter.stream_chat(
            session_id,
            request.message,
            cli_session_id=cli_session_id,
            is_resume=is_resume,
            system_prompt_append="\n".join(rule_result.get("system_prompt_append", [])) or None,
            allowed_tools=rule_result.get("allowed_tools") or None,
            max_turns=rule_result.get("max_turns"),
        ):
            yield {
                "event": event.type,
                "data": event.model_dump_json(),
            }
    finally:
        session_mgr.update_state(session_id, SessionState.COMPLETED)


@router.post("/stream")
async def agent_stream(
    request: AgentRequest,
    adapter_registry: AdapterRegistry = Depends(get_adapter_registry),
    rule_engine: RuleEngine = Depends(get_rule_engine),
    session_mgr: SessionManager = Depends(get_session_manager),
    session_store: SessionMappingStore = Depends(get_session_store),
) -> EventSourceResponse:
    passed, rule_result = rule_engine.evaluate(_build_rule_context(request))
    if not passed:
        raise HTTPException(status_code=400, detail=rule_result)

    adapter_cls = adapter_registry.get(request.agent_type)
    adapter = adapter_cls()
    session_id, cli_session_id, is_resume = await _resolve_session(request, session_mgr, session_store)

    return EventSourceResponse(
        _execute_stream(request, adapter, session_id, cli_session_id, is_resume, rule_result, session_mgr)
    )


@router.post("/execute", response_model=AgentResponse)
async def agent_execute(
    request: AgentRequest,
    adapter_registry: AdapterRegistry = Depends(get_adapter_registry),
    rule_engine: RuleEngine = Depends(get_rule_engine),
    session_mgr: SessionManager = Depends(get_session_manager),
    session_store: SessionMappingStore = Depends(get_session_store),
) -> AgentResponse:
    passed, rule_result = rule_engine.evaluate(_build_rule_context(request))
    if not passed:
        raise HTTPException(status_code=400, detail=rule_result)

    adapter_cls = adapter_registry.get(request.agent_type)
    adapter = adapter_cls()
    session_id, cli_session_id, is_resume = await _resolve_session(request, session_mgr, session_store)

    session_mgr.update_state(session_id, SessionState.RUNNING)
    session_mgr.record_history(session_id, {"role": "user", "content": request.message})

    try:
        response = await asyncio.wait_for(
            adapter.chat(
                session_id,
                request.message,
                cli_session_id=cli_session_id,
                is_resume=is_resume,
                system_prompt_append="\n".join(rule_result.get("system_prompt_append", [])) or None,
                allowed_tools=rule_result.get("allowed_tools") or None,
                max_turns=rule_result.get("max_turns"),
            ),
            timeout=settings.EXECUTION_TIMEOUT,
        )

        session_mgr.update_state(session_id, SessionState.COMPLETED)
        session_mgr.record_history(session_id, {"role": "assistant", "content": response.content})

        if request.session_id:
            response.session_id = request.session_id

        return response
    except asyncio.TimeoutError:
        session_mgr.update_state(session_id, SessionState.ERROR)
        await adapter.interrupt(session_id)
        raise HTTPException(status_code=408, detail="execution timeout")
