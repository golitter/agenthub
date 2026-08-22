import asyncio
import logging
import os
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from src.api.v1.agent import router as agent_router
from src.api.v1.agents import router as agents_router
from src.api.v1.health import router as health_router
from src.api.v1.integration import conflict_router as conflict_router
from src.api.v1.integration import router as integration_router
from src.api.v1.pin import router as pin_router
from src.api.v1.resources import router as resources_router
from src.api.v1.runs import router as runs_router
from src.api.v1.session import router as session_router
from src.api.v1.skills import (
    cleanup_stale_skill_staging_for_workspace,
    recover_skill_staging_for_workspace,
    router as skills_router,
)
from src.api.v1.validate import router as validate_router
from src.api.v1.workspace import router as workspace_router
from src.app.config import settings
from src.app.dependencies import (
    create_adapter_registry,
    create_backend_client,
    create_db_reader,
    create_preview_manager,
    create_rule_engine,
    create_session_manager,
    create_session_store,
    create_workspace_manager,
)
from src.observability import shutdown_langfuse
from src.execution.repository import SQLiteRunRepository
from src.execution.supervisor import RunSupervisor
from src.integration.repository import IntegrationOperationRepository
from src.integration.service import IntegrationService
from src.integration.recovery import ConflictRecoveryCoordinator
from src.security.authentication import ServiceAuthMiddleware
from src.security.path_policy import PathPolicy
from src.security.startup_validation import is_loopback_host
from src.workspace.recovery import recover_workspaces

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(app: FastAPI):
    if settings.security.service_auth_enabled and not os.environ.get("AGENTEND_SERVICE_TOKEN"):
        raise RuntimeError("AGENTEND_SERVICE_TOKEN is required when service authentication is enabled")
    if settings.security.service_auth_enabled and not os.environ.get("BACKEND_SERVICE_TOKEN"):
        raise RuntimeError("BACKEND_SERVICE_TOKEN is required when service authentication is enabled")
    if settings.security.service_auth_enabled and not settings.security.allowed_repo_roots:
        raise RuntimeError("allowed_repo_roots is required when service authentication is enabled")
    loopback = is_loopback_host(settings.server.host)
    if not loopback and not settings.security.service_auth_enabled:
        raise RuntimeError("service authentication is required on non-loopback listeners")
    if settings.execution.sandbox.mode == "unsafe_process" and not settings.security.allow_unsafe_local_execution:
        raise RuntimeError("unsafe local execution is disabled")
    if settings.execution.sandbox.mode == "unsafe_process" and not loopback:
        raise RuntimeError("unsafe_process execution requires a loopback listener")
    app.state.adapter_registry = create_adapter_registry()
    app.state.session_manager = create_session_manager()
    app.state.session_store = create_session_store()
    app.state.rule_engine = create_rule_engine()
    app.state.workspace_manager = create_workspace_manager()
    app.state.preview_manager = create_preview_manager()
    app.state.backend_client = create_backend_client()
    app.state.path_policy = PathPolicy(settings.security.allowed_repo_roots)
    app.state.run_repository = SQLiteRunRepository(settings.execution.run_store_path)
    app.state.integration_repository = IntegrationOperationRepository(settings.execution.run_store_path)
    app.state.integration_service = IntegrationService(app.state.integration_repository)
    app.state.run_supervisor = RunSupervisor(
        app.state.run_repository,
        max_concurrent_runs=settings.execution.max_concurrent_runs,
    )
    preserve_run_ids = await app.state.integration_service.recoverable_root_run_ids()
    await app.state.run_supervisor.recover(preserve_run_ids)

    # 启动：加载已持久化的 workspace 并恢复
    ws_mgr = app.state.workspace_manager
    await ws_mgr._load_from_store()
    # 按唯一的 repo_path 逐一恢复
    repo_paths = {ws.repo_path for ws in ws_mgr.list()}
    for rp in repo_paths:
        await recover_workspaces(ws_mgr._git, ws_mgr._store, rp)
    # recover_workspaces reconciles the durable store independently of the
    # manager's initial snapshot; reload before operation recovery so a
    # cleaned worktree cannot remain an in-memory authorization record.
    await ws_mgr._load_from_store()
    await app.state.integration_service.recover_incomplete(
        app.state.run_repository,
        ws_mgr,
        execute_pending=settings.orchestrator.integration_service_execute_enabled,
    )
    app.state.conflict_recovery_coordinator = ConflictRecoveryCoordinator(
        app.state.integration_service,
        ws_mgr,
        app.state.backend_client,
        app.state.run_repository,
        app.state.run_supervisor,
        app.state.session_manager,
    )
    await app.state.conflict_recovery_coordinator.recover()

    # Recover atomic Skill installs before serving requests.  This is a
    # separate startup pass from the age-based periodic cleanup: a crash can
    # leave a recent .previous-* backup while the destination is absent, and
    # waiting 24 hours would unnecessarily hide the last complete install.
    for ws in ws_mgr.list():
        if getattr(ws.status, "value", ws.status) != "active":
            continue
        try:
            recover_skill_staging_for_workspace(ws.worktree_path, ws.agent_type)
        except Exception:
            logger.warning("failed to recover Skill staging for %s", ws.session_id, exc_info=True)

    async def _skill_staging_cleanup_loop():
        # Recover stale atomic-install backups both at startup and while the
        # AgentEnd process remains alive.  Workspace paths come from the
        # trusted manager; the cleanup helper still rejects symlinked roots.
        while True:
            for ws in ws_mgr.list():
                if getattr(ws.status, "value", ws.status) != "active":
                    continue
                try:
                    cleanup_stale_skill_staging_for_workspace(ws.worktree_path, ws.agent_type)
                except Exception:
                    # One damaged workspace must not terminate the process-wide
                    # recovery loop for all other active sessions.
                    logger.warning("failed to clean stale Skill staging for %s", ws.session_id, exc_info=True)
            await asyncio.sleep(60 * 60)

    skill_cleanup_task = asyncio.create_task(_skill_staging_cleanup_loop())

    # 启动：连接 DB reader 并开始不活跃清理
    db_reader = create_db_reader()
    await db_reader.connect()
    await ws_mgr.start_inactive_cleanup(db_reader, interval=settings.workspace.cleanup_interval)

    # 启动：向 Backend 上报内置 skill
    import re

    _fm_re = re.compile(r"^---\s*\n(.*?)\n---", re.DOTALL)
    _fm_name_re = re.compile(r"^name:\s*(.+)$", re.MULTILINE)
    _fm_desc_re = re.compile(r"^description:\s*(.+)$", re.MULTILINE)

    async def _report_builtin_skills():
        builtin_dir = settings.skills.builtin_dir_resolved
        if not builtin_dir.is_dir():
            logger.warning("Builtin skills dir not found: %s", builtin_dir)
            return
        skills = []
        for entry in sorted(builtin_dir.iterdir()):
            if not entry.is_dir():
                continue
            skill_md = entry / "SKILL.md"
            if not skill_md.is_file():
                continue
            try:
                text = skill_md.read_text(encoding="utf-8")
                m = _fm_re.match(text)
                if not m:
                    continue
                fm = m.group(1)
                name_m = _fm_name_re.search(fm)
                if not name_m:
                    continue
                name = name_m.group(1).strip().strip("\"'")
                desc_m = _fm_desc_re.search(fm)
                desc = desc_m.group(1).strip().strip("\"'") if desc_m else ""
                skills.append({"name": name, "description": desc, "builtin": True, "source": "builtin"})
            except Exception:
                logger.warning("Failed to parse %s", skill_md, exc_info=True)
        if skills:
            await app.state.backend_client.report_builtin_skills(skills)

    asyncio.create_task(_report_builtin_skills())

    yield

    # 关闭：停止清理任务并关闭连接
    skill_cleanup_task.cancel()
    try:
        await skill_cleanup_task
    except asyncio.CancelledError:
        pass
    await ws_mgr.stop_inactive_cleanup()
    await app.state.preview_manager.stop_all()
    await app.state.run_supervisor.shutdown()
    await app.state.integration_repository.close()
    await app.state.run_repository.close()
    await app.state.backend_client.close()
    await db_reader.close()
    await shutdown_langfuse()


# title/version 来自 config.yaml，便于运维统一修改
app = FastAPI(title=settings.app.title, version=settings.app.version, lifespan=lifespan)

# CORS 参数来自 config.yaml，不再硬编码
app.add_middleware(
    CORSMiddleware,
    allow_origins=settings.server.cors.origins,
    allow_credentials=settings.server.cors.credentials,
    allow_methods=settings.server.cors.methods,
    allow_headers=settings.server.cors.headers,
)
app.add_middleware(ServiceAuthMiddleware, enabled=settings.security.service_auth_enabled)

app.include_router(health_router)
app.include_router(integration_router)
app.include_router(conflict_router)
app.include_router(session_router)
app.include_router(agent_router)
app.include_router(agents_router)
app.include_router(pin_router)
app.include_router(workspace_router)
app.include_router(validate_router)
app.include_router(resources_router)
app.include_router(runs_router)
app.include_router(skills_router)


if __name__ == "__main__":
    # host/port/reload 均来自 config.yaml 的 server 分区
    uvicorn.run("src.app.main:app", host=settings.server.host, port=settings.server.port, reload=settings.server.reload)
