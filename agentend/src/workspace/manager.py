import asyncio
import logging
from pathlib import Path

from src.app.agent_config import get_agent_config_dir
from src.schemas.request import AgentType
from src.skills.provisioner import SkillProvisioner
from src.workspace.db import DBReader
from src.workspace.git_ops import GitOps
from src.workspace.models import MergeResult, Workspace, WorkspaceStatus, task_branch_name
from src.workspace.store import WorkspaceStoreProtocol

logger = logging.getLogger(__name__)


class WorkspaceManager:
    def __init__(self, store: WorkspaceStoreProtocol) -> None:
        self._store = store
        self._git = GitOps()
        self._provisioner = SkillProvisioner()
        self._workspaces: dict[str, Workspace] = {}
        self._locks: dict[str, asyncio.Lock] = {}
        self._cleanup_task: asyncio.Task | None = None

    def _get_lock(self, task_id: str) -> asyncio.Lock:
        if task_id not in self._locks:
            self._locks[task_id] = asyncio.Lock()
        return self._locks[task_id]

    async def is_git_repo(self, path: str) -> bool:
        return await self._git.is_git_repo(path)

    async def ensure_git_repo(self, path: str) -> None:
        ok = await self._git.ensure_ready_repo(path)
        if not ok:
            raise RuntimeError(f"Failed to prepare git repo at {path}")

    async def default_branch(self, repo_path: str) -> str:
        return await self._git.default_branch(repo_path)

    async def _load_from_store(self) -> None:
        stored = await self._store.load_all()
        self._workspaces.update(stored)

    async def create_task_base(self, repo_path: str, task_id: str) -> str:
        """Create the task-base worktree for orchestrator read-only code access.

        Returns the absolute path of the task-base worktree.
        Idempotent -- safe to call multiple times for the same task_id.
        """
        async with self._get_lock(task_id):
            return await self._git.task_base_worktree_create(repo_path, task_id)

    async def create(
        self,
        repo_path: str,
        task_id: str,
        agent_name: str,
        session_id: str,
        agent_type: AgentType,
    ) -> Workspace:
        async with self._get_lock(task_id):
            task_branch = task_branch_name(task_id)
            await self._git.task_base_worktree_create(repo_path, task_id)

            existing = self._find_active(task_id, session_id)
            if existing:
                return existing

            ws = Workspace(
                task_id=task_id,
                agent_name=agent_name,
                agent_type=agent_type,
                session_id=session_id,
                repo_path=repo_path,
            )
            ok = await self._git.worktree_add(repo_path, ws.worktree_path, ws.branch_name, base_branch=task_branch)
            if not ok:
                raise RuntimeError(f"Failed to create worktree for {ws.branch_name}")

            # Provision skills and initialize shared directories
            worktrees_root = str(Path(repo_path).resolve().parent / "worktrees")
            self._provisioner.provision(ws.worktree_path, agent_type)
            self._provisioner.init_shared_dirs(worktrees_root, task_id, session_id)

            # Configure worktree-local excludes for agent config directory
            config_dir = get_agent_config_dir(agent_type)
            if config_dir:
                await self._git.setup_worktree_excludes(ws.worktree_path, [f"/{config_dir}"])

            self._workspaces[ws.id] = ws
            await self._store.save(ws)
            return ws

    def _find_active(self, task_id: str, session_id: str) -> Workspace | None:
        for ws in self._workspaces.values():
            if ws.task_id == task_id and ws.session_id == session_id and ws.status == WorkspaceStatus.ACTIVE:
                return ws
        return None

    def get(self, workspace_id: str) -> Workspace | None:
        return self._workspaces.get(workspace_id)

    def get_by_session(self, session_id: str) -> Workspace | None:
        """Find an active workspace by session_id."""
        for ws in self._workspaces.values():
            if ws.session_id == session_id and ws.status == WorkspaceStatus.ACTIVE:
                return ws
        return None

    def list(self) -> list[Workspace]:
        return list(self._workspaces.values())

    async def cleanup(self, workspace_id: str) -> bool:
        ws = self._workspaces.get(workspace_id)
        if not ws or ws.status != WorkspaceStatus.ACTIVE:
            return False
        async with self._get_lock(ws.task_id):
            ok = await self._git.worktree_remove(ws.worktree_path)
            if ok:
                await self._git.branch_delete(ws.repo_path, ws.branch_name)
                ws.status = WorkspaceStatus.CLEANED
                await self._store.save(ws)
            # Remove lock if no active workspaces remain for this task
            active = any(
                w.status == WorkspaceStatus.ACTIVE and w.task_id == ws.task_id for w in self._workspaces.values()
            )
            if not active:
                self._locks.pop(ws.task_id, None)
            return ok

    async def cleanup_by_task(self, task_id: str) -> int:
        count = 0
        repo_paths: set[str] = set()
        for ws in list(self._workspaces.values()):
            if ws.task_id == task_id and ws.status == WorkspaceStatus.ACTIVE:
                repo_paths.add(ws.repo_path)
                if await self.cleanup(ws.id):
                    count += 1
        # Always attempt to clean up task-base worktree and task branch,
        # even if no active workspaces were found (task may have been created but never run,
        # or workspaces were already cleaned by inactive cleanup)
        # Collect repo_path from cleaned workspaces AND from remaining inactive workspaces
        for ws in list(self._workspaces.values()):
            if ws.task_id == task_id and ws.repo_path:
                repo_paths.add(ws.repo_path)
        for repo_path in repo_paths:
            await self._git.task_base_worktree_remove(repo_path, task_id)
            await self._git.branch_delete(repo_path, task_branch_name(task_id))
        return count

    async def cleanup_task_branches(self, task_id: str, repo_path: str) -> bool:
        """Force cleanup task-base worktree and task branch using an explicit repo_path.
        Used when cleanup_by_task finds no active workspaces but branches still exist."""
        if not repo_path:
            return False
        await self._git.task_base_worktree_remove(repo_path, task_id)
        await self._git.branch_delete(repo_path, task_branch_name(task_id))
        # Also remove any remaining agent branches for this task
        try:
            branches = await self._git.list_branches(repo_path)
            for branch in branches:
                if branch.startswith("agent/") and task_id in branch:
                    await self._git.branch_delete(repo_path, branch)
        except Exception:
            pass
        return True

    async def commit(self, workspace_id: str, message: str) -> bool:
        ws = self._workspaces.get(workspace_id)
        if not ws:
            return False
        return await self._git.add_and_commit(ws.worktree_path, message)

    async def merge(self, workspace_id: str, target_branch: str | None = None) -> MergeResult:
        ws = self._workspaces.get(workspace_id)
        if not ws:
            return MergeResult(
                success=False,
                source_branch="",
                target_branch=target_branch or "",
                error="workspace not found",
            )
        async with self._get_lock(ws.task_id):
            target = target_branch or task_branch_name(ws.task_id)
            result = await self._git.merge_branch(ws.repo_path, ws.branch_name, target)
            if result.success and target == await self._git.default_branch(ws.repo_path):
                ws.status = WorkspaceStatus.MERGED
                await self._store.save(ws)
            return result

    async def merge_task_to_main(self, repo_path: str, task_id: str) -> MergeResult:
        target = await self._git.default_branch(repo_path)
        return await self._git.merge_branch(repo_path, task_branch_name(task_id), target)

    async def diff_task_to_main(self, repo_path: str, task_id: str) -> str:
        base = await self._git.default_branch(repo_path)
        return await self._git.diff_between(repo_path, base, task_branch_name(task_id))

    # Inactive cleanup

    async def start_inactive_cleanup(self, db_reader: DBReader, interval: int) -> None:
        if self._cleanup_task and not self._cleanup_task.done():
            return
        self._cleanup_task = asyncio.create_task(self._inactive_cleanup_loop(db_reader, interval))

    async def stop_inactive_cleanup(self) -> None:
        if self._cleanup_task and not self._cleanup_task.done():
            self._cleanup_task.cancel()
            try:
                await self._cleanup_task
            except asyncio.CancelledError:
                pass
            self._cleanup_task = None

    async def _inactive_cleanup_loop(self, db_reader: DBReader, interval: int) -> None:
        try:
            while True:
                await asyncio.sleep(interval)
                inactive_pairs = await db_reader.query_inactive_sessions()
                task_statuses = await db_reader.query_task_session_statuses()

                scanned = len(inactive_pairs)
                cleaned_sessions = 0
                cleaned_tasks = 0

                for session_id, task_id in inactive_pairs:
                    for ws in list(self._workspaces.values()):
                        if ws.session_id == session_id and ws.status == WorkspaceStatus.ACTIVE:
                            if await self.cleanup(ws.id):
                                cleaned_sessions += 1

                for task_id, statuses in task_statuses.items():
                    if statuses == {"inactive"}:
                        count = await self.cleanup_by_task(task_id)
                        if count > 0:
                            cleaned_tasks += 1

                logger.info(
                    "Inactive cleanup: scanned %d sessions, cleaned %d sessions, cleaned %d tasks",
                    scanned,
                    cleaned_sessions,
                    cleaned_tasks,
                )
        except asyncio.CancelledError:
            pass
