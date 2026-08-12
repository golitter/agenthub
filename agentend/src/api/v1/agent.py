import asyncio
import logging
import uuid
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel, Field
from sse_starlette.sse import EventSourceResponse

from src.adapters.base import BaseAgentAdapter
from src.adapters.registry import AdapterRegistry
from src.api.dependencies import (
    get_adapter_registry,
    get_backend_client,
    get_rule_engine,
    get_session_manager,
    get_session_store,
    get_workspace_manager,
)
from src.app.config import settings
from src.clients.backend_client import BackendClient
from src.observability import trace_stream_events
from src.rules.engine import RuleEngine
from src.schemas.events import EventType
from src.schemas.request import AgentRequest, AgentType
from src.schemas.response import AgentResponse
from src.session.manager import SessionManager
from src.session.models import SessionState
from src.session.store import SessionMappingStore
from src.transport.sanitizer import sanitize_stream_event
from src.workspace.manager import WorkspaceManager

router = APIRouter(prefix="/v1/agent", tags=["agent"])
logger = logging.getLogger(__name__)


class ReviewRequest(BaseModel):
    session_id: str = Field(min_length=1)
    action: str = Field(pattern="^(approve|discuss|modify)$")
    content: str = ""


def _artifact_process_env(request: AgentRequest) -> dict[str, str]:
    """Return only the builtin render upload context for the child process."""
    message_id = (request.message_id or "").strip()
    token = (request.artifact_upload_token or "").strip()
    # AgentEnd cannot verify the Backend HMAC itself, but it can reject
    # malformed/unbounded direct requests before putting attacker-controlled
    # values into a child-process environment. The Backend-issued message ID
    # is always a canonical UUID and capability JWTs fit comfortably below the
    # conservative 4 KiB ceiling.
    if not message_id or not token or len(message_id) > 64 or len(token) > 4096:
        return {}
    try:
        if str(uuid.UUID(message_id)) != message_id.lower():
            return {}
    except ValueError:
        return {}
    endpoint = settings.backend.url.rstrip("/") + "/api/internal/artifacts"
    return {
        "AGENTHUB_ARTIFACT_ENDPOINT": endpoint,
        "AGENTHUB_ARTIFACT_TOKEN": token,
        "AGENTHUB_MESSAGE_ID": message_id,
    }


def _orchestrator_kwargs(request: AgentRequest, workspace_path: str = "") -> dict:
    """从 request.config 构建专属于 OrchestratorAdapter 的 kwargs。"""
    if request.agent_type != AgentType.ORCHESTRATOR:
        return {}
    config = request.config or {}
    task_id = config.get("task_id", request.task_id)
    repo_path = request.repo_path or config.get("repo_path", "")

    expected_shared_dir = ""
    task_base_path = ""
    if workspace_path:
        # workspace_path 形如 {repo}/worktrees/{task_id}/{session_id}
        # shared_dir 应为 {repo}/worktrees/{task_id}/shared/.agent
        expected_shared_dir = str((Path(workspace_path).resolve().parent / "shared" / ".agent").resolve())
        task_base_path = str((Path(workspace_path).resolve().parent / "task-base").resolve())
    elif repo_path:
        expected_shared_dir = str(
            (Path(repo_path).resolve().parent / "worktrees" / task_id / "shared" / ".agent").resolve()
        )
        task_base_path = str((Path(repo_path).resolve().parent / "worktrees" / task_id / "task-base").resolve())

    if config.get("shared_dir"):
        shared_dir = str(Path(config["shared_dir"]).resolve())
        if expected_shared_dir and shared_dir != expected_shared_dir:
            raise HTTPException(status_code=400, detail="shared_dir must be the task shared/.agent directory")
    elif expected_shared_dir:
        shared_dir = expected_shared_dir
    else:
        shared_dir = str((Path.cwd() / task_id / "shared" / ".agent").resolve())

    return {
        "agents": config.get("agents", []),
        "orchestrator": config.get("orchestrator", {}),
        "task_id": task_id,
        "shared_dir": shared_dir,
        "repo_path": repo_path,
        "soul_md": config.get("soul_md", ""),
        "task_base_path": task_base_path,
    }


async def _resolve_workspace(
    request: AgentRequest,
    workspace_mgr: WorkspaceManager,
) -> str:
    """返回 workspace_path，必要时自动创建 workspace。"""
    if request.agent_type == AgentType.ORCHESTRATOR:
        # 为 orchestrator 创建 task-base worktree 以供只读代码访问
        repo_path = request.repo_path or (request.config or {}).get("repo_path", "")
        if repo_path:
            try:
                await workspace_mgr.create_task_base(repo_path, request.task_id)
            except Exception:
                logger.exception("Failed to create task-base worktree for task %s", request.task_id)
        return ""
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
            agent_name=request.agent_type.value,
            session_id=request.session_id,
            agent_type=request.agent_type,
        )
        return ws.worktree_path
    return ""


async def _resolve_session(
    request: AgentRequest,
    session_mgr: SessionManager,
    session_store: SessionMappingStore,
    workspace_path: str = "",
) -> tuple[str, str, bool]:
    """返回 (internal_session_id, cli_session_id, is_resume)。

    - is_resume=False → 新建 CLI 会话，CLI 自行创建其 session ID
    - is_resume=True  → 使用已存储的 cli_session_id 恢复 CLI 会话
    """
    cli_session_id = session_store.get_cli_session_id(request.session_id, request.task_id)

    session = session_mgr.get(request.session_id)
    if not session:
        session = session_mgr.create(
            agent_type=request.agent_type,
            workspace_path=workspace_path,
        )

    return session.id, cli_session_id or "", bool(cli_session_id)


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
    workspace_mgr: WorkspaceManager | None = None,
    backend_client: BackendClient | None = None,
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
    if artifact_env := _artifact_process_env(request):
        stream_kwargs["process_env"] = artifact_env
    stream_kwargs.update(_orchestrator_kwargs(request, workspace_path))
    if workspace_path and request.agent_type != AgentType.ORCHESTRATOR:
        stream_kwargs["cwd"] = workspace_path
    if workspace_mgr and request.agent_type == AgentType.ORCHESTRATOR:
        stream_kwargs["workspace_mgr"] = workspace_mgr
    if backend_client and request.agent_type == AgentType.ORCHESTRATOR:
        stream_kwargs["backend_client"] = backend_client

    outcome = SessionState.COMPLETED
    try:
        raw_events = adapter.stream_chat(session_id, request.message, **stream_kwargs)
        # CLI 适配器对外暴露不透明事件；Orchestrator 直接 trace 其 LangGraph。
        if request.agent_type != AgentType.ORCHESTRATOR:
            # 仅允许经审核的关联元数据穿越可观测性边界。
            trace_inputs = {
                "message": request.message,
                "session_id": session_id,
                "task_id": request.task_id,
                "agent_type": request.agent_type.value,
            }
            event_stream = trace_stream_events(
                raw_events,
                run_name=f"{request.agent_type.value} session_id={session_id}",
                inputs=trace_inputs,
            )
        else:
            event_stream = raw_events
        async for event in event_stream:
            if event.type == EventType.INIT.value:
                real_cli_sid = event.content.get("cli_session_id", "")
                if real_cli_sid:
                    await session_store.set_cli_session_id(request.session_id, real_cli_sid, request.task_id)
            elif event.type == EventType.ERROR.value:
                outcome = SessionState.ERROR
            event = sanitize_stream_event(event)
            yield {
                "event": event.type,
                "data": event.model_dump_json(),
            }
    except asyncio.CancelledError:
        outcome = SessionState.INTERRUPTED
        raise
    except Exception:
        outcome = SessionState.ERROR
        raise
    finally:
        try:
            session_mgr.update_state(session_id, outcome)
        except ValueError:
            logger.exception("Failed to update session %s to %s", session_id, outcome.value)


@router.post("/stream")
async def agent_stream(
    request: AgentRequest,
    adapter_registry: AdapterRegistry = Depends(get_adapter_registry),
    rule_engine: RuleEngine = Depends(get_rule_engine),
    session_mgr: SessionManager = Depends(get_session_manager),
    session_store: SessionMappingStore = Depends(get_session_store),
    workspace_mgr: WorkspaceManager = Depends(get_workspace_manager),
    backend_client: BackendClient = Depends(get_backend_client),
) -> EventSourceResponse:
    # 并行发起 pinned_announcements 请求，与 workspace 解析重叠执行
    pinned_task = asyncio.create_task(backend_client.get_pinned_announcements(request.task_id))

    workspace_path = await _resolve_workspace(request, workspace_mgr)

    # 为非 orchestrator 的 agent 将 SOUL.md 写入其 worktree
    if workspace_path and request.agent_type != AgentType.ORCHESTRATOR:
        from src.app.agent_config import get_agent_config_dir

        config = request.config or {}
        soul_md = config.get("soul_md", "")
        if soul_md:
            config_dir = get_agent_config_dir(request.agent_type.value)
            if config_dir:
                soul_path = Path(workspace_path) / config_dir / "SOUL.md"
                soul_path.parent.mkdir(parents=True, exist_ok=True)
                soul_path.write_text(soul_md.replace(" ", ""), encoding="utf-8")

    # 等待 pinned_announcements 结果，失败时降级为空列表
    try:
        pinned_announcements = await pinned_task
    except Exception:
        logger.warning("get_pinned_announcements failed, using []", exc_info=True)
        pinned_announcements = []

    rule_ctx = {
        "message": request.message,
        "agent_type": request.agent_type,
        "workspace_path": workspace_path,
        "pinned_announcements": pinned_announcements,
        "allowed_tools": request.config.get("allowed_tools", []) if request.config else [],
        "group_chat_messages": request.group_chat_messages or [],
    }
    passed, rule_result = rule_engine.evaluate(rule_ctx)
    if not passed:
        raise HTTPException(status_code=400, detail=rule_result)

    adapter_cls = adapter_registry.get(request.agent_type)
    if request.agent_type == AgentType.ORCHESTRATOR:
        from src.adapters.orchestrator import OrchestratorAdapter

        adapter = OrchestratorAdapter(registry=adapter_registry)
    else:
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
            workspace_mgr,
            backend_client,
        )
    )


@router.post("/review")
async def submit_review(request: ReviewRequest):
    from src.orchestrator.planning.graph import submit_plan_review

    if not submit_plan_review(request.session_id, request.action, request.content):
        raise HTTPException(status_code=404, detail="No pending review for this session")
    return {"status": "ok"}


@router.post("/execute", response_model=AgentResponse)
async def agent_execute(
    request: AgentRequest,
    adapter_registry: AdapterRegistry = Depends(get_adapter_registry),
    rule_engine: RuleEngine = Depends(get_rule_engine),
    session_mgr: SessionManager = Depends(get_session_manager),
    session_store: SessionMappingStore = Depends(get_session_store),
    workspace_mgr: WorkspaceManager = Depends(get_workspace_manager),
    backend_client: BackendClient = Depends(get_backend_client),
) -> AgentResponse:
    # 并行发起 pinned_announcements 请求，与 workspace 解析重叠执行
    pinned_task = asyncio.create_task(backend_client.get_pinned_announcements(request.task_id))
    workspace_path = await _resolve_workspace(request, workspace_mgr)

    # 等待 pinned_announcements 结果，失败时降级为空列表
    try:
        pinned_announcements = await pinned_task
    except Exception:
        logger.warning("get_pinned_announcements failed, using []", exc_info=True)
        pinned_announcements = []

    rule_ctx = {
        "message": request.message,
        "agent_type": request.agent_type,
        "workspace_path": workspace_path,
        "pinned_announcements": pinned_announcements,
        "allowed_tools": request.config.get("allowed_tools", []) if request.config else [],
        "group_chat_messages": request.group_chat_messages or [],
    }
    passed, rule_result = rule_engine.evaluate(rule_ctx)
    if not passed:
        raise HTTPException(status_code=400, detail=rule_result)

    adapter_cls = adapter_registry.get(request.agent_type)
    if request.agent_type == AgentType.ORCHESTRATOR:
        from src.adapters.orchestrator import OrchestratorAdapter

        adapter = OrchestratorAdapter(registry=adapter_registry)
    else:
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
    if artifact_env := _artifact_process_env(request):
        chat_kwargs["process_env"] = artifact_env
    chat_kwargs.update(_orchestrator_kwargs(request, workspace_path))
    if workspace_path and request.agent_type != AgentType.ORCHESTRATOR:
        chat_kwargs["cwd"] = workspace_path
    if request.agent_type == AgentType.ORCHESTRATOR:
        chat_kwargs["workspace_mgr"] = workspace_mgr
        chat_kwargs["backend_client"] = backend_client

    async def _collect() -> str:
        chunks: list[str] = []
        async for event in adapter.stream_chat(session_id, request.message, **chat_kwargs):
            if event.type == EventType.INIT.value:
                real_cli_sid = event.content.get("cli_session_id", "")
                if real_cli_sid:
                    await session_store.set_cli_session_id(request.session_id, real_cli_sid, request.task_id)
            elif event.type == EventType.TEXT.value:
                text = event.content.get("text", "")
                if text:
                    chunks.append(text)
        return "".join(chunks)

    try:
        # 执行超时来自 config.yaml 的 execution.timeout
        content = await asyncio.wait_for(_collect(), timeout=settings.execution.timeout)

        session_mgr.update_state(session_id, SessionState.COMPLETED)
        session_mgr.record_history(session_id, {"role": "assistant", "content": content})

        return AgentResponse(session_id=request.session_id, content=content, usage={})
    except asyncio.TimeoutError:
        session_mgr.update_state(session_id, SessionState.ERROR)
        await adapter.interrupt(session_id)
        raise HTTPException(status_code=408, detail="execution timeout")
