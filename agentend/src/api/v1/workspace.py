from dataclasses import asdict

from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel

from src.api.dependencies import get_workspace_manager
from src.workspace.manager import WorkspaceManager

router = APIRouter(prefix="/v1/workspace", tags=["workspace"])


class CreateWorkspaceRequest(BaseModel):
    repo_path: str
    task_id: str
    agent_name: str


class CommitRequest(BaseModel):
    message: str


class MergeRequest(BaseModel):
    target_branch: str = "main"


@router.post("/create")
async def create_workspace(
    req: CreateWorkspaceRequest,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    try:
        ws = await mgr.create(req.repo_path, req.task_id, req.agent_name)
        return asdict(ws)
    except RuntimeError as e:
        raise HTTPException(status_code=500, detail=str(e))


@router.post("/{workspace_id}/commit")
async def commit_workspace(
    workspace_id: str,
    req: CommitRequest,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    ws = mgr.get(workspace_id)
    if not ws:
        raise HTTPException(status_code=404, detail="Workspace not found")
    ok = await mgr.commit(workspace_id, req.message)
    return {"success": ok}


@router.post("/{workspace_id}/merge")
async def merge_workspace(
    workspace_id: str,
    req: MergeRequest,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    ws = mgr.get(workspace_id)
    if not ws:
        raise HTTPException(status_code=404, detail="Workspace not found")
    ok = await mgr.merge(workspace_id, req.target_branch)
    if not ok:
        return {"success": False, "error": "merge conflict"}
    return {"success": True}


@router.delete("/{workspace_id}")
async def delete_workspace(
    workspace_id: str,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    ok = await mgr.cleanup(workspace_id)
    if not ok:
        raise HTTPException(status_code=404, detail="Workspace not found or already cleaned")
    return {"success": True}


@router.get("")
async def list_workspaces(
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    return [asdict(ws) for ws in mgr.list()]
