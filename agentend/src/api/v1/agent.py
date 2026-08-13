import asyncio
import hashlib
import json
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
    get_path_policy,
    get_rule_engine,
    get_run_supervisor,
    get_session_manager,
    get_session_store,
    get_workspace_manager,
)
from src.app.config import settings
from src.clients.backend_client import BackendClient
from src.execution.models import RunSpec
from src.execution.repository import ParentRunClosedError, RunConflictError
from src.execution.supervisor import RunSupervisor
from src.generated.agent_run import AgentRunBudget
from src.observability import trace_stream_events
from src.rules.engine import RuleEngine
from src.schemas.events import EventType
from src.schemas.request import AgentRequest, AgentType
from src.schemas.response import AgentResponse
from src.security.path_policy import PathPolicy, PathPolicyError
from src.session.manager import SessionManager
from src.session.models import SessionState
from src.session.store import SessionMappingStore
from src.transport.sanitizer import sanitize_stream_event
from src.workspace.manager import WorkspaceManager

router = APIRouter(prefix="/v1/agent", tags=["agent"])
logger = logging.getLogger(__name__)


def _require_available_execution_backend() -> None:
    if settings.execution.sandbox.mode == "strict":
        raise HTTPException(
            status_code=503,
            detail="strict execution sandbox is not available on this AgentEnd build",
        )


def _request_fingerprint(request: AgentRequest, workspace_path: str, rule_result: dict) -> str:
    payload = {
        "request": request.model_dump(mode="json", exclude={"artifact_upload_token"}),
        "workspace_path": workspace_path,
        "rule_result": rule_result,
    }
    canonical = json.dumps(payload, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    return hashlib.sha256(canonical.encode("utf-8")).hexdigest()


def _validated_budget(raw: dict | None) -> AgentRunBudget:
    """Apply the caller's tighter limits without allowing server-limit expansion."""
    try:
        requested = AgentRunBudget.model_validate(raw or {})
    except Exception as exc:
        raise HTTPException(status_code=400, detail="invalid run budget") from exc
    values = requested.model_dump()
    if any(value <= 0 for value in values.values()):
        raise HTTPException(status_code=400, detail="run budget values must be positive")

    ceiling = AgentRunBudget().model_dump()
    values = {name: min(value, ceiling[name]) for name, value in values.items()}
    values["wall_time_seconds"] = min(values["wall_time_seconds"], settings.execution.timeout)
    values["max_turns"] = min(values["max_turns"], settings.execution.max_turns)
    return AgentRunBudget.model_validate(values)


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


def _write_soul_document(request: AgentRequest, workspace_path: str) -> None:
    """Persist the non-orchestrator identity document without changing its semantics."""
    if not workspace_path or request.agent_type == AgentType.ORCHESTRATOR:
        return
    from src.app.agent_config import get_agent_config_dir

    soul_md = (request.config or {}).get("soul_md", "")
    if not soul_md:
        return
    if not isinstance(soul_md, str):
        raise HTTPException(status_code=400, detail="soul_md must be a string")
    config_dir = get_agent_config_dir(request.agent_type.value)
    if not config_dir:
        return
    soul_path = Path(workspace_path) / config_dir / "SOUL.md"
    if soul_path.parent.is_symlink() or soul_path.is_symlink():
        raise HTTPException(status_code=400, detail="Agent config path must not be a symlink")
    soul_path.parent.mkdir(parents=True, exist_ok=True)
    try:
        PathPolicy.safe_open_parent(soul_path, Path(workspace_path))
    except PathPolicyError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    soul_path.write_text(soul_md, encoding="utf-8")


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
        "root_run_id": request.root_run_id or request.run_id or "",
        "parent_run_id": request.run_id or "",
        "budget": request.budget or {},
    }


async def _resolve_workspace(
    request: AgentRequest,
    workspace_mgr: WorkspaceManager,
    path_policy: PathPolicy,
) -> str:
    """返回 workspace_path，必要时自动创建 workspace。"""
    if request.agent_type == AgentType.ORCHESTRATOR:
        # 为 orchestrator 创建 task-base worktree 以供只读代码访问
        repo_path = request.repo_path or (request.config or {}).get("repo_path", "")
        if repo_path:
            if path_policy.configured:
                try:
                    repo_path = str(path_policy.validate_managed_path(repo_path, "git_repo"))
                except PathPolicyError as exc:
                    raise HTTPException(status_code=400, detail=str(exc)) from exc
            try:
                await workspace_mgr.create_task_base(repo_path, request.task_id)
            except Exception:
                logger.exception("Failed to create task-base worktree for task %s", request.task_id)
        return ""
    if request.workspace_id:
        workspace = workspace_mgr.get(request.workspace_id)
        if not workspace or workspace.task_id != request.task_id or workspace.session_id != request.session_id:
            raise HTTPException(status_code=400, detail="workspace_id is not registered for this task/session")
        if path_policy.configured:
            try:
                return str(path_policy.resolve_repo(workspace.worktree_path))
            except PathPolicyError as exc:
                raise HTTPException(status_code=400, detail="registered workspace is outside configured roots") from exc
        return workspace.worktree_path
    if request.workspace_path:
        workspace = workspace_mgr.get_by_session(request.session_id)
        if not workspace or Path(workspace.worktree_path).resolve() != Path(request.workspace_path).resolve():
            raise HTTPException(status_code=400, detail="workspace_path must match a registered workspace")
        if path_policy.configured:
            try:
                return str(path_policy.resolve_repo(workspace.worktree_path))
            except PathPolicyError as exc:
                raise HTTPException(status_code=400, detail="registered workspace is outside configured roots") from exc
        return workspace.worktree_path
    if request.repo_path:
        repo_path = request.repo_path
        if path_policy.configured:
            try:
                repo_path = str(path_policy.validate_managed_path(repo_path, "git_repo"))
            except PathPolicyError as exc:
                raise HTTPException(status_code=400, detail=str(exc)) from exc
        if not await workspace_mgr.is_git_repo(repo_path):
            raise HTTPException(
                status_code=400,
                detail=f"repo_path is not a git repository: {repo_path}",
            )
        ws = await workspace_mgr.create(
            repo_path=repo_path,
            task_id=request.task_id,
            agent_name=request.agent_type.value,
            session_id=request.session_id,
            agent_type=request.agent_type,
        )
        if path_policy.configured:
            try:
                return str(path_policy.resolve_repo(ws.worktree_path))
            except PathPolicyError as exc:
                raise HTTPException(status_code=500, detail="created workspace is outside configured roots") from exc
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
            session_id=request.session_id,
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
    run_supervisor: RunSupervisor = Depends(get_run_supervisor),
    path_policy: PathPolicy = Depends(get_path_policy),
) -> EventSourceResponse:
    _require_available_execution_backend()
    # 并行发起 pinned_announcements 请求，与 workspace 解析重叠执行
    pinned_task = asyncio.create_task(backend_client.get_pinned_announcements(request.task_id))

    try:
        workspace_path = await _resolve_workspace(request, workspace_mgr, path_policy)
        _write_soul_document(request, workspace_path)
    except BaseException:
        pinned_task.cancel()
        await asyncio.gather(pinned_task, return_exceptions=True)
        raise

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

    run_id = request.run_id or str(uuid.uuid4())
    root_run_id = request.root_run_id or run_id
    workspace = workspace_mgr.get_by_session(request.session_id)
    workspace_id = request.workspace_id or (workspace.id if workspace else f"orchestrator:{request.task_id}")
    budget = _validated_budget(request.budget)
    spec = RunSpec(
        run_id=run_id,
        root_run_id=root_run_id,
        parent_run_id=request.parent_run_id,
        task_id=request.task_id,
        session_id=request.session_id,
        message_id=request.message_id,
        workspace_id=workspace_id,
        agent_type=request.agent_type.value,
        budget=budget,
        request_fingerprint=_request_fingerprint(request, workspace_path, rule_result),
    )

    async def runner(emit):
        async for item in _execute_stream(
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
        ):
            await emit(json.loads(item["data"]))

    async def cancel_adapter() -> None:
        await adapter.interrupt(session_id)

    try:
        await run_supervisor.start(spec, runner, cancel_adapter)
    except RunConflictError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except ParentRunClosedError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc

    async def journal_stream():
        after_seq = 0
        while True:
            events, record = await run_supervisor.wait_for_events(run_id, after_seq, timeout=15)
            for envelope in events:
                after_seq = envelope.seq
                event = envelope.event
                yield {
                    "id": str(envelope.seq),
                    "event": event.get("type", "message"),
                    "data": json.dumps(event, separators=(",", ":")),
                }
            if not record:
                return
            if record.terminal and after_seq >= record.last_event_seq:
                return

    return EventSourceResponse(journal_stream(), headers={"X-Agent-Run-ID": run_id})


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
    run_supervisor: RunSupervisor = Depends(get_run_supervisor),
    path_policy: PathPolicy = Depends(get_path_policy),
) -> AgentResponse:
    _require_available_execution_backend()
    # 并行发起 pinned_announcements 请求，与 workspace 解析重叠执行
    pinned_task = asyncio.create_task(backend_client.get_pinned_announcements(request.task_id))
    try:
        workspace_path = await _resolve_workspace(request, workspace_mgr, path_policy)
        _write_soul_document(request, workspace_path)
    except BaseException:
        pinned_task.cancel()
        await asyncio.gather(pinned_task, return_exceptions=True)
        raise

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

    run_id = request.run_id or str(uuid.uuid4())
    workspace = workspace_mgr.get_by_session(request.session_id)
    workspace_id = request.workspace_id or (workspace.id if workspace else f"orchestrator:{request.task_id}")
    budget = _validated_budget(request.budget)
    spec = RunSpec(
        run_id=run_id,
        root_run_id=request.root_run_id or run_id,
        parent_run_id=request.parent_run_id,
        task_id=request.task_id,
        session_id=request.session_id,
        message_id=request.message_id,
        workspace_id=workspace_id,
        agent_type=request.agent_type.value,
        budget=budget,
        request_fingerprint=_request_fingerprint(request, workspace_path, rule_result),
    )

    async def runner(emit):
        async for item in _execute_stream(
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
        ):
            await emit(json.loads(item["data"]))

    async def cancel_adapter() -> None:
        await adapter.interrupt(session_id)

    try:
        await run_supervisor.start(spec, runner, cancel_adapter)
    except RunConflictError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except ParentRunClosedError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc

    chunks: list[str] = []
    after_seq = 0
    while True:
        events, record = await run_supervisor.wait_for_events(run_id, after_seq, timeout=15)
        for envelope in events:
            after_seq = envelope.seq
            event = envelope.event
            if event.get("type") == EventType.TEXT.value:
                text = event.get("content", {}).get("text", "")
                if text:
                    chunks.append(text)
        if not record:
            raise HTTPException(status_code=404, detail="run not found")
        if record.terminal and after_seq >= record.last_event_seq:
            if record.state.value == "completed":
                return AgentResponse(session_id=request.session_id, content="".join(chunks), usage={})
            status_code = 408 if record.termination_reason == "wall_time_exceeded" else 409
            raise HTTPException(
                status_code=status_code,
                detail={
                    "run_id": run_id,
                    "state": record.state.value,
                    "termination_reason": record.termination_reason,
                },
            )
