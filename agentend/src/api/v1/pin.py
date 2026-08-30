import logging

from fastapi import APIRouter, Depends, HTTPException, Query
from pydantic import BaseModel, Field

from src.api.dependencies import get_workspace_manager
from src.orchestrator.memory.pin_memory import PinMemory
from src.workspace.manager import WorkspaceManager

router = APIRouter(prefix="/v1/pin", tags=["pin"])
logger = logging.getLogger(__name__)


class PinAddRequest(BaseModel):
    shared_dir: str = Field(min_length=1, max_length=4096, description="Registered task shared directory")
    content: str = Field(min_length=1, max_length=65536, description="Content to pin")
    title: str = Field(min_length=1, max_length=200, description="Pin title")


class PinRemoveRequest(BaseModel):
    shared_dir: str = Field(min_length=1, max_length=4096, description="Registered task shared directory")
    filename: str = Field(min_length=1, max_length=255, description="Filename to unpin")


class AnnouncementUnpinRequest(BaseModel):
    shared_dir: str = Field(min_length=1, max_length=4096, description="Registered task shared directory")
    content: str = Field(max_length=65536, description="Original announcement content")
    sender_name: str = Field(min_length=1, max_length=256, description="Who sent the announcement")


class PinAddExistingRequest(BaseModel):
    shared_dir: str = Field(description="Shared directory path")
    filename: str = Field(description="Existing filename in common/")
    title: str = Field(default="", description="Optional title override")


def _resolve_shared_dir(shared_dir: str, manager: WorkspaceManager) -> str:
    try:
        return manager.resolve_shared_dir(shared_dir)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc


def _pin_memory(shared_dir: str, manager: WorkspaceManager) -> PinMemory:
    resolved = _resolve_shared_dir(shared_dir, manager)
    return PinMemory(common_dir=f"{resolved}/memory/common")


@router.post("/add")
async def pin_add(req: PinAddRequest, manager: WorkspaceManager = Depends(get_workspace_manager)):
    pm = _pin_memory(req.shared_dir, manager)
    filename = await pm.pin(title=req.title, content=req.content)
    return {"filename": filename}


@router.post("/remove")
async def pin_remove(req: PinRemoveRequest, manager: WorkspaceManager = Depends(get_workspace_manager)):
    shared_dir = _resolve_shared_dir(req.shared_dir, manager)
    pm = PinMemory(common_dir=f"{shared_dir}/memory/common")
    removed = pm.unpin(req.filename)
    if not removed:
        raise HTTPException(status_code=404, detail=f"Pin not found: {req.filename}")

    return {"success": True, "removed": removed}


@router.post("/announcement-unpin")
async def announcement_unpin(
    req: AnnouncementUnpinRequest,
    manager: WorkspaceManager = Depends(get_workspace_manager),
):
    """Deprecated compatibility endpoint; Active Pin Snapshot owns validity."""
    _resolve_shared_dir(req.shared_dir, manager)
    logger.warning(
        "Deprecated announcement-unpin notification ignored for sender=%s",
        req.sender_name,
    )
    return {"success": True, "deprecated": True}


@router.get("/list")
async def pin_list(
    shared_dir: str = Query(min_length=1, max_length=4096),
    manager: WorkspaceManager = Depends(get_workspace_manager),
):
    pm = _pin_memory(shared_dir, manager)
    return {"pins": pm.list_pins()}
