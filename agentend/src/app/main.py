from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from src.api.v1.agent import router as agent_router
from src.api.v1.health import router as health_router
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
    await ws_mgr.start_ttl_cleanup(check_interval=settings.WORKSPACE_TTL_CHECK_INTERVAL)

    yield

    # Shutdown: stop TTL task + cleanup all active workspaces
    await ws_mgr.stop_ttl_cleanup()
    for ws in ws_mgr.list():
        if ws.status == WorkspaceStatus.ACTIVE:
            await ws_mgr.cleanup(ws.id)
    # Shutdown: clean up all active sessions
    mgr = app.state.session_manager
    for session in mgr.list():
        await mgr.destroy(session.id)


app = FastAPI(title="AgentEnd Runtime", version="0.1.0", lifespan=lifespan)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(health_router)
app.include_router(session_router)
app.include_router(agent_router)
app.include_router(workspace_router)


if __name__ == "__main__":
    uvicorn.run("src.app.main:app", host=settings.HOST, port=settings.PORT, reload=True)
