import logging

from src.workspace.git_ops import GitOps
from src.workspace.models import WorkspaceStatus
from src.workspace.store import WorkspaceStoreProtocol

logger = logging.getLogger(__name__)


def _is_task_base_worktree(path: str, branch: str) -> bool:
    return path.endswith("/task-base") and branch.startswith("task/")


def parse_worktree_list(output: str) -> list[tuple[str, str]]:
    results: list[tuple[str, str]] = []
    current_path = None
    current_branch = None
    for line in output.splitlines():
        if line.startswith("worktree "):
            current_path = line[len("worktree ") :]
        elif line.startswith("branch "):
            current_branch = line[len("branch ") :]
            if current_branch.startswith("refs/heads/"):
                current_branch = current_branch[len("refs/heads/") :]
        elif line == "" and current_path and current_branch:
            results.append((current_path, current_branch))
            current_path = None
            current_branch = None
    if current_path and current_branch:
        results.append((current_path, current_branch))
    return results


async def recover_workspaces(git_ops: GitOps, store: WorkspaceStoreProtocol, repo_path: str) -> tuple[int, int, int]:
    physical = await git_ops.worktree_list(repo_path)
    physical_map = {path: branch for path, branch in physical}

    stored = await store.load_all()

    recovered = 0
    cleaned = 0
    orphans_removed = 0

    # 将存储的 workspace 与物理 worktree 进行对账
    for ws_id, ws in stored.items():
        if ws.status != WorkspaceStatus.ACTIVE:
            continue
        if ws.worktree_path in physical_map:
            recovered += 1
        else:
            ws.status = WorkspaceStatus.CLEANED
            await store.save(ws)
            cleaned += 1

    # 清理孤立的 worktree —— 不在存储中的 worktree（跳过主工作树）
    stored_paths = {ws.worktree_path for ws in stored.values()}
    main_path = physical[0][0] if physical else None
    for path, branch in physical:
        if path == main_path:
            continue
        if _is_task_base_worktree(path, branch):
            continue
        if path not in stored_paths:
            logger.warning("Removing orphan worktree: %s (branch: %s)", path, branch)
            ok = await git_ops.worktree_remove(path)
            if ok:
                await git_ops.branch_delete(repo_path, branch)
                orphans_removed += 1

    logger.info(
        "Workspace recovery: %d recovered, %d cleaned, %d orphans removed",
        recovered,
        cleaned,
        orphans_removed,
    )
    return recovered, cleaned, orphans_removed
