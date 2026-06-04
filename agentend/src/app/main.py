import logging
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from src.api.v1.agent import router as agent_router
from src.api.v1.health import router as health_router
from src.api.v1.pin import router as pin_router
from src.api.v1.resources import router as resources_router
from src.api.v1.session import router as session_router
from src.api.v1.skills import router as skills_router
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
from src.workspace.recovery import recover_workspaces

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.adapter_registry = create_adapter_registry()
    app.state.session_manager = create_session_manager()
    app.state.session_store = create_session_store()
    app.state.rule_engine = create_rule_engine()
    app.state.workspace_manager = create_workspace_manager()
    app.state.preview_manager = create_preview_manager()
    app.state.backend_client = create_backend_client()

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

    # Startup: report builtin skills to Backend
    import asyncio
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

    # Shutdown: stop cleanup task and close connections
    await ws_mgr.stop_inactive_cleanup()
    await app.state.preview_manager.stop_all()
    await app.state.backend_client.close()
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
app.include_router(resources_router)
app.include_router(skills_router)


if __name__ == "__main__":
    # host/port/reload 均来自 config.yaml 的 server 分区
    uvicorn.run("src.app.main:app", host=settings.server.host, port=settings.server.port, reload=settings.server.reload)
