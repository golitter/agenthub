import asyncio

from fastapi import APIRouter, Depends, HTTPException
from sse_starlette.sse import EventSourceResponse

from src.adapters.base import BaseAgentAdapter
from src.adapters.registry import AdapterRegistry
from src.api.dependencies import (
    get_adapter_registry,
    get_rule_engine,
    get_session_manager,
    get_session_store,
    get_workspace_manager,
)
from src.app.config import settings
from src.rules.engine import RuleEngine
from src.schemas.events import EventType
from src.schemas.request import AgentRequest
from src.schemas.response import AgentResponse
from src.session.manager import SessionManager
from src.session.models import SessionState
from src.session.store import SessionMappingStore
from src.workspace.manager import WorkspaceManager

router = APIRouter(prefix="/v1/agent", tags=["agent"])


async def _resolve_workspace(
    request: AgentRequest,
    workspace_mgr: WorkspaceManager,
) -> str:
    """Return workspace_path, auto-creating workspace if needed."""
    if request.workspace_path:
        return request.workspace_path
    if request.repo_path:
        if not await workspace_mgr.is_git_repo(request.repo_path):
            raise HTTPException(
                status_code=400,
                detail=f"repo_path is not a git repository: {request.repo_path}",
            )
        ws = await workspace_mgr.create(
            repo_path=request.repo_path,
            task_id=request.task_id,
            agent_name=str(request.agent_type),
        )
        return ws.worktree_path
    return ""


async def _resolve_session(
    request: AgentRequest,
    session_mgr: SessionManager,
    session_store: SessionMappingStore,
    workspace_path: str = "",
) -> tuple[str, str, bool]:
    """Return (internal_session_id, cli_session_id, is_resume).

    - is_resume=False → new CLI session, CLI creates its own session ID
    - is_resume=True  → resume CLI session with stored cli_session_id
    """
    cli_session_id = session_store.get_cli_session_id(request.session_id, request.task_id)

    session = session_mgr.get(request.session_id)
    if not session:
        session = session_mgr.create(
            agent_type=request.agent_type,
            workspace_path=workspace_path,
        )

    return session.id, cli_session_id or "", bool(cli_session_id)


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
    cli_session_id: str,
    is_resume: bool,
    rule_result: dict,
    session_mgr: SessionManager,
    session_store: SessionMappingStore,
    workspace_path: str = "",
):
    session_mgr.update_state(session_id, SessionState.RUNNING)
    session_mgr.record_history(session_id, {"role": "user", "content": request.message})

    stream_kwargs: dict = {
        "cli_session_id": cli_session_id,
        "is_resume": is_resume,
        "system_prompt_append": "\n".join(rule_result.get("system_prompt_append", [])) or None,
        "allowed_tools": rule_result.get("allowed_tools") or None,
        "max_turns": rule_result.get("max_turns"),
    }
    if workspace_path:
        stream_kwargs["cwd"] = workspace_path

    try:
        async for event in adapter.stream_chat(session_id, request.message, **stream_kwargs):
            if event.type == EventType.INIT.value:
                real_cli_sid = event.content.get("cli_session_id", "")
                if real_cli_sid:
                    session_store.set_cli_session_id(request.session_id, real_cli_sid, request.task_id)
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
    workspace_mgr: WorkspaceManager = Depends(get_workspace_manager),
) -> EventSourceResponse:
    passed, rule_result = rule_engine.evaluate(_build_rule_context(request))
    if not passed:
        raise HTTPException(status_code=400, detail=rule_result)

    workspace_path = await _resolve_workspace(request, workspace_mgr)

    adapter_cls = adapter_registry.get(request.agent_type)
    adapter = adapter_cls()
    session_id, cli_session_id, is_resume = await _resolve_session(
        request,
        session_mgr,
        session_store,
        workspace_path,
    )

    return EventSourceResponse(
        _execute_stream(
            request,
            adapter,
            session_id,
            cli_session_id,
            is_resume,
            rule_result,
            session_mgr,
            session_store,
            workspace_path,
        )
    )


@router.post("/execute", response_model=AgentResponse)
async def agent_execute(
    request: AgentRequest,
    adapter_registry: AdapterRegistry = Depends(get_adapter_registry),
    rule_engine: RuleEngine = Depends(get_rule_engine),
    session_mgr: SessionManager = Depends(get_session_manager),
    session_store: SessionMappingStore = Depends(get_session_store),
    workspace_mgr: WorkspaceManager = Depends(get_workspace_manager),
) -> AgentResponse:
    passed, rule_result = rule_engine.evaluate(_build_rule_context(request))
    if not passed:
        raise HTTPException(status_code=400, detail=rule_result)

    workspace_path = await _resolve_workspace(request, workspace_mgr)

    adapter_cls = adapter_registry.get(request.agent_type)
    adapter = adapter_cls()
    session_id, cli_session_id, is_resume = await _resolve_session(
        request,
        session_mgr,
        session_store,
        workspace_path,
    )

    session_mgr.update_state(session_id, SessionState.RUNNING)
    session_mgr.record_history(session_id, {"role": "user", "content": request.message})

    chat_kwargs: dict = {
        "cli_session_id": cli_session_id,
        "is_resume": is_resume,
        "system_prompt_append": "\n".join(rule_result.get("system_prompt_append", [])) or None,
        "allowed_tools": rule_result.get("allowed_tools") or None,
        "max_turns": rule_result.get("max_turns"),
    }
    if workspace_path:
        chat_kwargs["cwd"] = workspace_path

    async def _collect() -> str:
        chunks: list[str] = []
        async for event in adapter.stream_chat(session_id, request.message, **chat_kwargs):
            if event.type == EventType.INIT.value:
                real_cli_sid = event.content.get("cli_session_id", "")
                if real_cli_sid:
                    session_store.set_cli_session_id(request.session_id, real_cli_sid, request.task_id)
            elif event.type == EventType.TEXT.value:
                text = event.content.get("text", "")
                if text:
                    chunks.append(text)
        return "".join(chunks)

    try:
        content = await asyncio.wait_for(_collect(), timeout=settings.EXECUTION_TIMEOUT)

        session_mgr.update_state(session_id, SessionState.COMPLETED)
        session_mgr.record_history(session_id, {"role": "assistant", "content": content})

        return AgentResponse(session_id=request.session_id, content=content, usage={})
    except asyncio.TimeoutError:
        session_mgr.update_state(session_id, SessionState.ERROR)
        await adapter.interrupt(session_id)
        raise HTTPException(status_code=408, detail="execution timeout")
