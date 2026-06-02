import asyncio
from dataclasses import asdict
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException, Request
from fastapi.responses import FileResponse, PlainTextResponse
from pydantic import BaseModel

from src.api.dependencies import get_preview_manager, get_workspace_manager
from src.app.agent_config import get_agent_config_dir
from src.app.config import settings
from src.preview.server import PreviewManager
from src.schemas.request import AgentType
from src.workspace.manager import WorkspaceManager

router = APIRouter(prefix="/v1/workspace", tags=["workspace"])


class CreateWorkspaceRequest(BaseModel):
    repo_path: str
    task_id: str
    agent_name: str
    session_id: str
    agent_type: AgentType


class CommitRequest(BaseModel):
    message: str = "auto commit"


class MergeRequest(BaseModel):
    target_branch: str | None = None


class MergeTaskToMainRequest(BaseModel):
    repo_path: str


def _resolve_worktree_file(worktree_path: str, file_path: str) -> Path:
    """Resolve a file path within a workspace worktree, preventing path traversal."""
    base = Path(worktree_path).resolve()
    target = (base / file_path).resolve()
    if not str(target).startswith(str(base) + "/") and target != base:
        raise HTTPException(status_code=403, detail="Path traversal not allowed")
    return target


def _get_skill_exclusion_prefixes(agent_type: AgentType) -> list[str]:
    """Return path prefixes like '{config_dir}/skills/{name}/' for manifest skills."""
    config_dir = get_agent_config_dir(agent_type)
    if not config_dir:
        return []
    return [f"{config_dir}/skills/{name}/" for name in settings.skills.manifest]


async def _run_git(*args: str, cwd: str) -> tuple[bool, str]:
    """Run a git command and return (success, output)."""
    proc = await asyncio.create_subprocess_exec(
        "git",
        *args,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
        cwd=cwd,
    )
    stdout, stderr = await proc.communicate()
    if proc.returncode != 0:
        return False, stderr.decode().strip()
    return True, stdout.decode().strip()


@router.post("/create")
async def create_workspace(
    req: CreateWorkspaceRequest,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    try:
        ws = await mgr.create(
            repo_path=req.repo_path,
            task_id=req.task_id,
            agent_name=req.agent_name,
            session_id=req.session_id,
            agent_type=req.agent_type,
        )
        return asdict(ws)
    except RuntimeError as e:
        raise HTTPException(status_code=500, detail=str(e))


@router.get("/{workspace_id}/files/{file_path:path}")
async def read_file(
    workspace_id: str,
    file_path: str,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    ws = mgr.get(workspace_id)
    if not ws:
        raise HTTPException(status_code=404, detail="Workspace not found")
    target = _resolve_worktree_file(ws.worktree_path, file_path)
    if not target.is_file():
        raise HTTPException(status_code=404, detail="File not found")
    return FileResponse(str(target), filename=target.name)


@router.put("/{workspace_id}/files/{file_path:path}")
async def write_file(
    workspace_id: str,
    file_path: str,
    request: Request,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    ws = mgr.get(workspace_id)
    if not ws:
        raise HTTPException(status_code=404, detail="Workspace not found")
    target = _resolve_worktree_file(ws.worktree_path, file_path)
    target.parent.mkdir(parents=True, exist_ok=True)
    content = await request.body()
    target.write_bytes(content)
    return {"success": True}


@router.get("/{workspace_id}/diff")
async def get_diff(
    workspace_id: str,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    ws = mgr.get(workspace_id)
    if not ws:
        raise HTTPException(status_code=404, detail="Workspace not found")

    skill_prefixes = _get_skill_exclusion_prefixes(ws.agent_type) if ws.agent_type else []

    # Tracked changes — exclude skill paths via pathspec
    diff_args = ["diff", "HEAD"]
    for prefix in skill_prefixes:
        diff_args.extend([":!" + prefix.rstrip("/")])
    ok, output = await _run_git(*diff_args, cwd=ws.worktree_path)
    if not ok:
        raise HTTPException(status_code=500, detail=output)

    # Untracked files — generate diff blocks for each
    ok2, untracked = await _run_git("ls-files", "--others", "--exclude-standard", cwd=ws.worktree_path)
    if ok2 and untracked:
        parts = [output] if output else []
        base = Path(ws.worktree_path)
        for rel in untracked.splitlines():
            rel = rel.strip()
            if not rel:
                continue
            if any(rel.startswith(pre) for pre in skill_prefixes):
                continue
            fp = base / rel
            if not fp.is_file():
                continue
            try:
                content = fp.read_text(errors="replace")
            except OSError:
                continue
            lines = content.splitlines()
            parts.append(
                f"diff --git a/{rel} b/{rel}\n"
                f"new file mode 100644\n"
                f"index 0000000..0000000\n"
                f"--- /dev/null\n"
                f"+++ b/{rel}\n"
                f"@@ -0,0 +1,{len(lines)} @@\n" + "\n".join(f"+{line}" for line in lines) + "\n"
            )
        output = "\n".join(parts)

    return PlainTextResponse(output)


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


@router.post("/{workspace_id}/revert")
async def revert_workspace(
    workspace_id: str,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    ws = mgr.get(workspace_id)
    if not ws:
        raise HTTPException(status_code=404, detail="Workspace not found")
    ok, err = await _run_git("checkout", "HEAD", "--", ".", cwd=ws.worktree_path)
    if not ok:
        raise HTTPException(status_code=500, detail=err)
    return {"success": True}


@router.post("/{workspace_id}/preview/start")
async def start_preview(
    workspace_id: str,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
    preview_mgr: PreviewManager = Depends(get_preview_manager),
):
    ws = mgr.get(workspace_id)
    if not ws:
        raise HTTPException(status_code=404, detail="Workspace not found")
    srv = await preview_mgr.start(workspace_id, ws.worktree_path)
    return {"url": srv.url, "port": srv.port}


@router.post("/{workspace_id}/preview/stop")
async def stop_preview(
    workspace_id: str,
    preview_mgr: PreviewManager = Depends(get_preview_manager),
):
    await preview_mgr.stop(workspace_id)
    return {"success": True}


@router.post("/{workspace_id}/merge")
async def merge_workspace(
    workspace_id: str,
    req: MergeRequest,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    ws = mgr.get(workspace_id)
    if not ws:
        raise HTTPException(status_code=404, detail="Workspace not found")
    result = await mgr.merge(workspace_id, req.target_branch)
    return asdict(result)


@router.post("/task/{task_id}/merge-to-main")
async def merge_task_to_main(
    task_id: str,
    req: MergeTaskToMainRequest,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    result = await mgr.merge_task_to_main(req.repo_path, task_id)
    return asdict(result)


@router.delete("/{workspace_id}")
async def delete_workspace(
    workspace_id: str,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
    preview_mgr: PreviewManager = Depends(get_preview_manager),
):
    await preview_mgr.stop(workspace_id)
    ok = await mgr.cleanup(workspace_id)
    if not ok:
        raise HTTPException(status_code=404, detail="Workspace not found or already cleaned")
    return {"success": True}


@router.get("")
async def list_workspaces(
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    return [asdict(ws) for ws in mgr.list()]


@router.get("/by-session/{session_id}")
async def get_workspace_by_session(
    session_id: str,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    for ws in mgr.list():
        if ws.session_id == session_id and ws.status.value == "active":
            return asdict(ws)
    raise HTTPException(status_code=404, detail="No active workspace for this session")


# ─── Git Info ───────────────────────────────────────────────────


@router.get("/task/{task_id}/git-info")
async def get_task_git_info(
    task_id: str,
    mgr: WorkspaceManager = Depends(get_workspace_manager),
):
    """Return real git branches and commits for a task's workspaces."""
    from src.workspace.models import task_branch_name

    # Find all workspaces for this task
    task_workspaces = [ws for ws in mgr.list() if ws.task_id == task_id]
    if not task_workspaces:
        raise HTTPException(status_code=404, detail=f"No workspaces found for task {task_id}")

    # Use the repo_path from any workspace (they share the same repo)
    repo_path = task_workspaces[0].repo_path
    if not repo_path:
        raise HTTPException(status_code=500, detail="Workspace has no repo_path")

    # 1. Build branch list from workspace records + git verification
    task_branch = task_branch_name(task_id)
    workspace_branches = [task_branch]
    for ws in task_workspaces:
        if ws.branch_name and ws.branch_name not in workspace_branches:
            workspace_branches.append(ws.branch_name)

    ok, branch_out = await _run_git("for-each-ref", "--format=%(refname:short)", "refs/heads/", cwd=repo_path)
    git_branches = set(b.strip() for b in branch_out.splitlines() if b.strip()) if ok else set()

    # Merge: expected workspace branches + git branches matching this task.
    # Keep expected branches even if their git refs are currently missing so the
    # UI can surface the missing task branch instead of silently hiding it.
    relevant_branches_set = set(workspace_branches)
    for b in git_branches:
        if b == "main" or b == task_branch or b.endswith(f"/{task_id}"):
            relevant_branches_set.add(b)

    # Order: agent branches first, then task, then main (most specific first for commit lane assignment)
    agent_branches = sorted(b for b in relevant_branches_set if b.startswith("agent/"))
    task_branches = [b for b in relevant_branches_set if b.startswith("task/")]
    main_branch = ["main"] if "main" in relevant_branches_set else []
    relevant_branches = agent_branches + task_branches + main_branch
    if not relevant_branches:
        relevant_branches = ["main"]

    # 2. Build hash→lane mapping
    #    Strategy: iterate agent → task → main. Each branch overwrites shared commits.
    #    Result: agent-only → agent, task-only → task, main-only → main.
    #    (agent's rev-list includes task+main commits, but task/main overwrite them back)
    branch_hash_map: dict[str, str] = {}  # full_hash → lane
    for branch in relevant_branches:
        ok, rev_out = await _run_git("rev-list", branch, "--max-count=100", cwd=repo_path)
        if not ok or not rev_out.strip():
            continue
        for h in rev_out.strip().splitlines():
            h = h.strip()
            if h:
                branch_hash_map[h] = branch

    # 3. Get ALL commits in topological order with parent info
    #    %P = parent hashes (space-separated), %H = full hash, etc.
    commits: list[dict] = []
    ok, log_out = await _run_git(
        "log",
        "--all",
        "--topo-order",
        "--format=%H|%h|%s|%an|%ar|%P",
        "--max-count=30",
        cwd=repo_path,
    )
    if ok and log_out.strip():
        for line in log_out.strip().splitlines():
            parts = line.split("|", 5)
            if len(parts) < 6:
                continue
            full_hash, short_hash, msg, author, time_ago, parents_str = parts
            lane = branch_hash_map.get(full_hash, "main")
            # Parse parent hashes (space-separated, may be empty for initial commit)
            parent_hashes = [p for p in parents_str.split() if p]
            # Only include commits relevant to our branches
            if lane != "main" or any(p in branch_hash_map for p in parent_hashes) or not parent_hashes:
                commits.append(
                    {
                        "hash": short_hash,
                        "fullHash": full_hash,
                        "msg": msg,
                        "author": author,
                        "lane": lane,
                        "time": time_ago,
                        "parentHashes": parent_hashes,
                    }
                )

    # 3. For each branch, get its tip commit (head)
    branches_info = []
    for b in relevant_branches:
        ok, tip = await _run_git("log", "-1", "--format=%h|%s|%an|%ar", b, cwd=repo_path)
        if ok and tip.strip():
            head_parts = tip.strip().split("|", 3)
            branches_info.append(
                {
                    "name": b,
                    "headHash": head_parts[0] if len(head_parts) > 0 else "",
                    "headMsg": head_parts[1] if len(head_parts) > 1 else "",
                    "headAuthor": head_parts[2] if len(head_parts) > 2 else "",
                    "headTime": head_parts[3] if len(head_parts) > 3 else "",
                    "exists": b in git_branches,
                }
            )
        else:
            branches_info.append(
                {
                    "name": b,
                    "headHash": "",
                    "headMsg": "",
                    "headAuthor": "",
                    "headTime": "",
                    "exists": False,
                }
            )

    return {
        "repoPath": repo_path,
        "branches": branches_info,
        "commits": commits,
    }
