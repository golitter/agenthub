from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from src.api.v1.agent import router as agent_router
from src.api.v1.health import router as health_router
from src.api.v1.pin import router as pin_router
from src.api.v1.session import router as session_router
from src.api.v1.workspace import router as workspace_router
from src.app.config import settings
from src.app.dependencies import (
    create_adapter_registry,
    create_rule_engine,
    create_session_manager,
    create_session_store,
    create_workspace_manager,
)
from src.workspace.models import WorkspaceStatus
from src.workspace.recovery import recover_workspaces


@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.adapter_registry = create_adapter_registry()
    app.state.session_manager = create_session_manager()
    app.state.session_store = create_session_store()
    app.state.rule_engine = create_rule_engine()
    app.state.workspace_manager = create_workspace_manager()

    # Startup: load persisted workspaces and recover
    ws_mgr = app.state.workspace_manager
    await ws_mgr._load_from_store()
    # Recover per unique repo_path
    repo_paths = {ws.repo_path for ws in ws_mgr.list()}
    for rp in repo_paths:
        await recover_workspaces(ws_mgr._git, ws_mgr._store, rp)

    # Startup: begin TTL cleanup
    # check_interval 来自 config.yaml 的 workspace.ttl_check_interval
    await ws_mgr.start_ttl_cleanup(check_interval=settings.workspace.ttl_check_interval)

    yield

    # Shutdown: stop TTL task + cleanup all active workspaces
    await ws_mgr.stop_ttl_cleanup()
    task_ids = {ws.task_id for ws in ws_mgr.list() if ws.status == WorkspaceStatus.ACTIVE}
    for tid in task_ids:
        await ws_mgr.cleanup_by_task(tid)
    # Shutdown: clean up all active sessions
    mgr = app.state.session_manager
    for session in mgr.list():
        await mgr.destroy(session.id)


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

app.include_router(health_router)
app.include_router(session_router)
app.include_router(agent_router)
app.include_router(pin_router)
app.include_router(workspace_router)


if __name__ == "__main__":
    # host/port/reload 均来自 config.yaml 的 server 分区
    uvicorn.run("src.app.main:app", host=settings.server.host, port=settings.server.port, reload=settings.server.reload)
