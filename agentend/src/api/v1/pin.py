from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, Field

from src.orchestrator.pin_memory import PinMemory

router = APIRouter(prefix="/v1/pin", tags=["pin"])


class PinAddRequest(BaseModel):
    shared_dir: str = Field(description="Shared directory path (e.g. task-123/shared/.agent)")
    content: str = Field(description="Content to pin")
    title: str = Field(description="Pin title")


class PinRemoveRequest(BaseModel):
    shared_dir: str = Field(description="Shared directory path")
    filename: str = Field(description="Filename to unpin")


class PinAddExistingRequest(BaseModel):
    shared_dir: str = Field(description="Shared directory path")
    filename: str = Field(description="Existing filename in common/")
    title: str = Field(default="", description="Optional title override")


def _pin_memory(shared_dir: str) -> PinMemory:
    return PinMemory(common_dir=f"{shared_dir}/memory/common")


@router.post("/add")
async def pin_add(req: PinAddRequest):
    pm = _pin_memory(req.shared_dir)
    filename = await pm.pin(title=req.title, content=req.content)
    return {"filename": filename}


@router.post("/remove")
async def pin_remove(req: PinRemoveRequest):
    pm = _pin_memory(req.shared_dir)
    removed = pm.unpin(req.filename)
    if not removed:
        raise HTTPException(status_code=404, detail=f"Pin not found: {req.filename}")
    return {"success": True}


@router.get("/list")
async def pin_list(shared_dir: str):
    pm = _pin_memory(shared_dir)
    return {"pins": pm.list_pins()}
