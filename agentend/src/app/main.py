from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from src.api.v1.agent import router as agent_router
from src.api.v1.health import router as health_router
from src.api.v1.pin import router as pin_router
from src.api.v1.session import router as session_router
from src.api.v1.validate import router as validate_router
from src.api.v1.workspace import router as workspace_router
from src.app.config import settings
from src.app.dependencies import (
    create_adapter_registry,
    create_db_reader,
    create_preview_manager,
    create_rule_engine,
    create_session_manager,
    create_session_store,
    create_workspace_manager,
)
from src.workspace.recovery import recover_workspaces


@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.adapter_registry = create_adapter_registry()
    app.state.session_manager = create_session_manager()
    app.state.session_store = create_session_store()
    app.state.rule_engine = create_rule_engine()
    app.state.workspace_manager = create_workspace_manager()
    app.state.preview_manager = create_preview_manager()

    # Startup: load persisted workspaces and recover
    ws_mgr = app.state.workspace_manager
    await ws_mgr._load_from_store()
    # Recover per unique repo_path
    repo_paths = {ws.repo_path for ws in ws_mgr.list()}
    for rp in repo_paths:
        await recover_workspaces(ws_mgr._git, ws_mgr._store, rp)

    # Startup: connect DB reader and begin inactive cleanup
    db_reader = create_db_reader()
    await db_reader.connect()
    await ws_mgr.start_inactive_cleanup(db_reader, interval=settings.workspace.cleanup_interval)

    yield

    # Shutdown: stop cleanup task and close DB connection
    await ws_mgr.stop_inactive_cleanup()
    await app.state.preview_manager.stop_all()
    await db_reader.close()


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
app.include_router(validate_router)


if __name__ == "__main__":
    # host/port/reload 均来自 config.yaml 的 server 分区
    uvicorn.run("src.app.main:app", host=settings.server.host, port=settings.server.port, reload=settings.server.reload)
