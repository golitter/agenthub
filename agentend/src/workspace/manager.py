import asyncio
import logging
from datetime import datetime

from src.workspace.git_ops import GitOps
from src.workspace.models import Workspace, WorkspaceStatus, task_branch_name
from src.workspace.store import WorkspaceStoreProtocol

logger = logging.getLogger(__name__)


class WorkspaceManager:
    def __init__(self, store: WorkspaceStoreProtocol, ttl_seconds: int = 3600) -> None:
        self._store = store
        self._ttl = ttl_seconds
        self._git = GitOps()
        self._workspaces: dict[str, Workspace] = {}
        self._locks: dict[str, asyncio.Lock] = {}
        self._ttl_task: asyncio.Task | None = None

    def _get_lock(self, task_id: str) -> asyncio.Lock:
        if task_id not in self._locks:
            self._locks[task_id] = asyncio.Lock()
        return self._locks[task_id]

    async def _load_from_store(self) -> None:
        stored = await self._store.load_all()
        self._workspaces.update(stored)

    async def create(self, repo_path: str, task_id: str, agent_name: str) -> Workspace:
        async with self._get_lock(task_id):
            task_branch = task_branch_name(task_id)
            ok = await self._git.task_branch_create(repo_path, task_id)
            if not ok:
                raise RuntimeError(f"Failed to create task branch {task_branch}")

            ws = Workspace(
                task_id=task_id,
                agent_name=agent_name,
                repo_path=repo_path,
            )
            ok = await self._git.worktree_add(repo_path, ws.worktree_path, ws.branch_name, base_branch=task_branch)
            if not ok:
                raise RuntimeError(f"Failed to create worktree for {ws.branch_name}")

            self._workspaces[ws.id] = ws
            await self._store.save(ws)
            return ws

    def get(self, workspace_id: str) -> Workspace | None:
        return self._workspaces.get(workspace_id)

    def list(self) -> list[Workspace]:
        return list(self._workspaces.values())

    async def cleanup(self, workspace_id: str) -> bool:
        ws = self._workspaces.get(workspace_id)
        if not ws or ws.status != WorkspaceStatus.ACTIVE:
            return False
        async with self._get_lock(ws.task_id):
            ok = await self._git.worktree_remove(ws.worktree_path)
            if ok:
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
        for ws in list(self._workspaces.values()):
            if ws.task_id == task_id and ws.status == WorkspaceStatus.ACTIVE:
                if await self.cleanup(ws.id):
                    count += 1
        return count

    async def commit(self, workspace_id: str, message: str) -> bool:
        ws = self._workspaces.get(workspace_id)
        if not ws:
            return False
        return await self._git.add_and_commit(ws.worktree_path, message)

    async def merge(self, workspace_id: str, target_branch: str | None = None) -> bool:
        ws = self._workspaces.get(workspace_id)
        if not ws:
            return False
        async with self._get_lock(ws.task_id):
            target = target_branch or task_branch_name(ws.task_id)
            ok = await self._git.merge_branch(ws.repo_path, ws.branch_name, target)
            if ok and target == "main":
                ws.status = WorkspaceStatus.MERGED
                await self._store.save(ws)
            return ok

    # TTL cleanup

    async def start_ttl_cleanup(self, check_interval: int = 300) -> None:
        if self._ttl_task and not self._ttl_task.done():
            return
        self._ttl_task = asyncio.create_task(self._ttl_cleanup_loop(check_interval))

    async def stop_ttl_cleanup(self) -> None:
        if self._ttl_task and not self._ttl_task.done():
            self._ttl_task.cancel()
            try:
                await self._ttl_task
            except asyncio.CancelledError:
                pass
            self._ttl_task = None

    async def _ttl_cleanup_loop(self, check_interval: int) -> None:
        try:
            while True:
                await asyncio.sleep(check_interval)
                active = await self._store.query_by_status(WorkspaceStatus.ACTIVE)
                cleaned = 0
                now = datetime.now()
                for ws in active:
                    age = (now - ws.created_at).total_seconds()
                    if age > self._ttl:
                        await self.cleanup(ws.id)
                        cleaned += 1
                logger.info(
                    "TTL cleanup: checked %d workspaces, cleaned %d expired",
                    len(active),
                    cleaned,
                )
        except asyncio.CancelledError:
            pass
