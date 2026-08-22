from __future__ import annotations

import asyncio
import fcntl
import logging
from collections.abc import Awaitable, Callable
from contextlib import asynccontextmanager
from pathlib import Path

from src.app.agent_config import get_agent_config_dir
from src.schemas.request import AgentType
from src.skills.provisioner import SkillProvisioner
from src.workspace.db import DBReader
from src.workspace.git_ops import GitOps
from src.workspace.models import MergeResult, Workspace, WorkspaceStatus, task_branch_name
from src.workspace.models import validate_workspace_identifier
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

    @asynccontextmanager
    async def _integration_file_lock(self, repo_path: str, task_id: str):
        """Coordinate Git integration with the standalone taskctl process.

        The lock path intentionally matches taskctl's ``shared/.agent`` lock.
        The asyncio lock above only protects one AgentEnd process; this file
        lock is the cross-process part of the same per-task serialization
        boundary.
        """
        validate_workspace_identifier(task_id, "task_id")
        lock_path = (
            Path(repo_path).resolve().parent
            / "worktrees"
            / task_id
            / "shared"
            / ".agent"
            / "integration.lock"
        )
        # These operations are intentionally non-blocking: ``LOCK_NB`` below
        # only probes the advisory lock and yields between retries. Keeping
        # the descriptor operations on the event-loop thread also avoids
        # platform-specific hangs in filesystem-backed async test sandboxes.
        lock_path.parent.mkdir(parents=True, exist_ok=True)
        lock_file = lock_path.open("a+")
        acquired = False
        try:
            while not acquired:
                try:
                    fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                    acquired = True
                except BlockingIOError:
                    await asyncio.sleep(0.05)
            yield
        finally:
            if acquired:
                fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)
            lock_file.close()

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
        # Startup recovery can mark a previously persisted workspace as cleaned.
        # Replace the in-memory snapshot so stale active records cannot continue
        # authorizing an integration after their physical worktree disappeared.
        self._workspaces.clear()
        self._workspaces.update(stored)

    async def create_task_base(self, repo_path: str, task_id: str) -> str:
        """为 orchestrator 创建用于只读代码访问的 task-base worktree。

        返回 task-base worktree 的绝对路径。
        幂等 —— 对同一个 task_id 可安全地多次调用。
        """
        async with self._get_lock(task_id):
            async with self._integration_file_lock(repo_path, task_id):
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
            async with self._integration_file_lock(repo_path, task_id):
                task_branch = task_branch_name(task_id)
                await self._git.task_base_worktree_create(repo_path, task_id)

                existing = self._find_active(task_id, session_id)
                if existing:
                    # Existing workspaces must still pass through the managed-skill
                    # refresh path.  Otherwise a long-lived AgentEnd process keeps
                    # running the old builtin render binary forever after a deploy.
                    self._provisioner.provision(existing.worktree_path, existing.agent_type)
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

                # 供给 skill 并初始化共享目录
                worktrees_root = str(Path(repo_path).resolve().parent / "worktrees")
                self._provisioner.provision(ws.worktree_path, agent_type)
                self._provisioner.init_shared_dirs(worktrees_root, task_id, session_id)

                # 为 agent 配置目录配置 worktree 本地的 excludes
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
        """按 session_id 查找活跃的 workspace。"""
        for ws in self._workspaces.values():
            if ws.session_id == session_id and ws.status == WorkspaceStatus.ACTIVE:
                return ws
        return None

    def get_by_task_and_session(self, task_id: str, session_id: str) -> Workspace | None:
        """Resolve one active workspace without crossing task scopes."""
        for ws in self._workspaces.values():
            if (
                ws.task_id == task_id
                and ws.session_id == session_id
                and ws.status == WorkspaceStatus.ACTIVE
            ):
                return ws
        return None

    def get_by_session_and_path(self, session_id: str, worktree_path: str) -> Workspace | None:
        """Resolve normal and resolver workspaces for the same DB session."""
        expected = Path(worktree_path).resolve()
        for ws in self._workspaces.values():
            if (
                ws.session_id == session_id
                and ws.status == WorkspaceStatus.ACTIVE
                and Path(ws.worktree_path).resolve() == expected
            ):
                return ws
        return None

    def list(self) -> list[Workspace]:
        return list(self._workspaces.values())

    def resolve_shared_dir(self, raw_path: str) -> str:
        """Resolve a task shared directory from registered active workspaces.

        Pin endpoints must not turn a caller-provided directory into an
        arbitrary filesystem write primitive. The shared directory is not a
        standalone workspace record, so derive its one trusted location from
        an active session worktree belonging to the same task.
        """
        if not raw_path or "\x00" in raw_path or not Path(raw_path).is_absolute():
            raise ValueError("shared_dir must be an absolute registered task directory")
        candidate = Path(raw_path).resolve(strict=False)
        for workspace in self._workspaces.values():
            if workspace.status != WorkspaceStatus.ACTIVE:
                continue
            expected = (Path(workspace.worktree_path).resolve().parent / "shared" / ".agent").resolve(
                strict=False
            )
            if candidate == expected:
                return str(candidate)
        raise ValueError("shared_dir is not registered for an active workspace")

    async def cleanup(self, workspace_id: str) -> bool:
        ws = self._workspaces.get(workspace_id)
        if not ws or ws.status != WorkspaceStatus.ACTIVE:
            return False
        async with self._get_lock(ws.task_id):
            async with self._integration_file_lock(ws.repo_path, ws.task_id):
                ok = await self._git.worktree_remove(ws.worktree_path)
                if ok:
                    await self._git.branch_delete(ws.repo_path, ws.branch_name)
                    ws.status = WorkspaceStatus.CLEANED
                    await self._store.save(ws)
                # 若该 task 已无活跃 workspace，则移除锁
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
        # 始终尝试清理 task-base worktree 和 task 分支，
        # 即使没有找到活跃 workspace（task 可能已创建但从未运行，
        # 或 workspace 已被非活跃清理流程清理）
        # 从已清理的 workspace 以及剩余的非活跃 workspace 中收集 repo_path
        for ws in list(self._workspaces.values()):
            if ws.task_id == task_id and ws.repo_path:
                repo_paths.add(ws.repo_path)
        for repo_path in repo_paths:
            async with self._get_lock(task_id):
                async with self._integration_file_lock(repo_path, task_id):
                    await self._git.task_base_worktree_remove(repo_path, task_id)
                    await self._git.branch_delete(repo_path, task_branch_name(task_id))
        return count

    async def cleanup_task_branches(self, task_id: str, repo_path: str) -> bool:
        """使用显式 repo_path 强制清理 task-base worktree 和 task 分支。
        当 cleanup_by_task 未找到活跃 workspace 但分支仍然存在时使用。"""
        if not repo_path:
            return False
        async with self._get_lock(task_id):
            async with self._integration_file_lock(repo_path, task_id):
                await self._git.task_base_worktree_remove(repo_path, task_id)
                await self._git.branch_delete(repo_path, task_branch_name(task_id))
                # 同时移除该 task 剩余的所有 agent 分支
                try:
                    branches = await self._git.list_branches(repo_path)
                    for branch in branches:
                        if branch.startswith("agent/") and branch.endswith(f"/{task_id}"):
                            await self._git.branch_delete(repo_path, branch)
                except Exception:
                    pass
                return True

    async def create_resolver(
        self,
        repo_path: str,
        task_id: str,
        conflict_id: str,
        attempt: int,
        source_branch: str,
        resolver_session_id: str,
        agent_type: AgentType,
        agent_name: str,
    ) -> tuple[Workspace, MergeResult]:
        """Create an isolated resolver worktree and prepare the merge scene.

        The task-base worktree is never left in a conflicted state. The resolver
        branch is intentionally registered as a workspace so Backend can pass
        its path through the normal trusted workspace validation.
        """
        validate_workspace_identifier(task_id, "task_id")
        validate_workspace_identifier(conflict_id, "conflict_id")
        validate_workspace_identifier(resolver_session_id, "resolver_session_id")
        if attempt < 0:
            raise ValueError("attempt must be non-negative")

        async with self._get_lock(task_id):
            task_branch = task_branch_name(task_id)
            async with self._integration_file_lock(repo_path, task_id):
                await self._git.task_base_worktree_create(repo_path, task_id)
            resolver_branch = f"resolve/{task_id}/{conflict_id}/{attempt}"
            resolver_path = str(
                Path(repo_path).resolve().parent
                / "worktrees"
                / task_id
                / f"resolver-{conflict_id}-{attempt}"
            )
            existing = self.get_by_session_and_path(resolver_session_id, resolver_path)
            if existing:
                async with self._integration_file_lock(repo_path, task_id):
                    preparation = await self._git.prepare_resolver_merge(
                        resolver_path, source_branch, task_branch
                    )
                return existing, preparation

            ws = Workspace(
                task_id=task_id,
                agent_name=agent_name,
                agent_type=agent_type,
                repo_path=repo_path,
                worktree_path=resolver_path,
                branch_name=resolver_branch,
                session_id=resolver_session_id,
                workspace_kind="resolver",
                conflict_id=conflict_id,
                attempt=attempt,
            )
            async with self._integration_file_lock(repo_path, task_id):
                ok = await self._git.worktree_add(
                    repo_path,
                    resolver_path,
                    resolver_branch,
                    base_branch=task_branch,
                )
            if not ok:
                raise RuntimeError(f"Failed to create resolver worktree for {resolver_branch}")

            self._provisioner.provision(resolver_path, agent_type)
            config_dir = get_agent_config_dir(agent_type)
            if config_dir:
                await self._git.setup_worktree_excludes(resolver_path, [f"/{config_dir}"])
            self._workspaces[ws.id] = ws
            await self._store.save(ws)

            async with self._integration_file_lock(repo_path, task_id):
                preparation = await self._git.prepare_resolver_merge(
                    resolver_path, source_branch, task_branch
                )
            return ws, preparation

    async def resolver_commit(self, workspace_id: str, message: str) -> bool:
        ws = self._workspaces.get(workspace_id)
        if not ws or ws.workspace_kind != "resolver":
            return False
        async with self._get_lock(ws.task_id):
            async with self._integration_file_lock(ws.repo_path, ws.task_id):
                return await self._git.add_and_commit(ws.worktree_path, message)

    async def current_commit(self, workspace_id: str) -> str:
        """Return the authoritative HEAD for a registered workspace."""
        ws = self._workspaces.get(workspace_id)
        if not ws:
            return ""
        return await self._git.rev_parse(ws.worktree_path, ws.branch_name)

    async def current_task_commit(self, workspace_id: str) -> str:
        """Return the current task-branch head for a registered workspace."""
        ws = self._workspaces.get(workspace_id)
        if not ws:
            return ""
        async with self._get_lock(ws.task_id):
            async with self._integration_file_lock(ws.repo_path, ws.task_id):
                return await self._git.rev_parse(ws.repo_path, task_branch_name(ws.task_id))

    async def adopt_source(
        self,
        workspace_id: str,
        source_branch: str,
        *,
        expected_source_commit: str = "",
        expected_target_commit: str = "",
    ) -> MergeResult:
        """Apply the explicit ``accept_source`` decision to the task branch."""
        ws = self._workspaces.get(workspace_id)
        if not ws or ws.status != WorkspaceStatus.ACTIVE:
            return MergeResult(
                success=False,
                source_branch=source_branch,
                target_branch=task_branch_name(ws.task_id) if ws else "",
                error="workspace is missing or inactive",
                error_code="workspace_missing",
            )
        async with self._get_lock(ws.task_id):
            async with self._integration_file_lock(ws.repo_path, ws.task_id):
                target_branch = task_branch_name(ws.task_id)
                target_worktree = str(
                    Path(ws.repo_path).resolve().parent / "worktrees" / ws.task_id / "task-base"
                )
                # ``accept_source`` is an explicit destructive decision.  It
                # must never fall back to the repository's currently checked
                # out worktree: that path may be main/master, and resetting it
                # would mutate a branch outside the recorded integration scope.
                if not Path(target_worktree).is_dir():
                    return MergeResult(
                        success=False,
                        source_branch=source_branch,
                        target_branch=target_branch,
                        error="trusted task-base worktree is missing",
                        error_code="workspace_missing",
                    )
                return await self._git.adopt_branch(
                    target_worktree,
                    source_branch,
                    target_branch,
                    expected_source_commit=expected_source_commit,
                    expected_target_commit=expected_target_commit,
                )

    async def probe_integration(
        self,
        workspace_id: str,
        *,
        expected_source_commit: str = "",
        expected_target_commit_before: str = "",
        expected_merge_base: str = "",
    ) -> MergeResult:
        """Inspect an interrupted integration without invoking ``git merge``."""
        ws = self._workspaces.get(workspace_id)
        if not ws or ws.status != WorkspaceStatus.ACTIVE:
            return MergeResult(
                success=False,
                source_branch="",
                target_branch="",
                error="workspace is missing or inactive",
                error_code="workspace_missing",
            )

        # A probe is part of recovery, but it still reads the same task branch
        # that merge mutates. Serialize the observation with the scope lock so
        # a live merge cannot be mistaken for a clean, not-yet-merged state.
        async with self._get_lock(ws.task_id):
            return await self._probe_integration_locked(
                ws,
                expected_source_commit=expected_source_commit,
                expected_target_commit_before=expected_target_commit_before,
                expected_merge_base=expected_merge_base,
            )

    async def _probe_integration_locked(
        self,
        ws: Workspace,
        *,
        expected_source_commit: str = "",
        expected_target_commit_before: str = "",
        expected_merge_base: str = "",
    ) -> MergeResult:
        async with self._integration_file_lock(ws.repo_path, ws.task_id):
            target = task_branch_name(ws.task_id)
            source_commit = await self._git.rev_parse(ws.worktree_path, ws.branch_name)
            target_commit = await self._git.rev_parse(ws.repo_path, target)
            merge_base = await self._git.merge_base(ws.repo_path, target, ws.branch_name)
            facts = dict(
                success=False,
                source_branch=ws.branch_name,
                target_branch=target,
                source_commit=source_commit,
                target_commit=target_commit,
                merge_base=merge_base,
            )
            if not source_commit:
                return MergeResult(**facts, error="source branch or commit is missing", error_code="source_missing")
            if not target_commit:
                return MergeResult(**facts, error="target branch or commit is missing", error_code="merge_failed")
            if expected_source_commit and source_commit != expected_source_commit:
                return MergeResult(
                    **facts,
                    error="source branch moved after integration intent was recorded",
                    error_code="integration_state_uncertain",
                )
            if expected_target_commit_before:
                intent_merge_base = await self._git.merge_base(
                    ws.repo_path,
                    expected_target_commit_before,
                    ws.branch_name,
                )
                if expected_merge_base and intent_merge_base != expected_merge_base:
                    return MergeResult(
                        **facts,
                        error="merge base changed after integration intent was recorded",
                        error_code="integration_state_uncertain",
                    )
                audited_merge_base = expected_merge_base or intent_merge_base
                if target_commit == expected_target_commit_before:
                    if await self._git.is_ancestor(ws.repo_path, source_commit, target_commit):
                        return MergeResult(
                            **(
                                facts
                                | {
                                    "success": True,
                                    "target_commit": target_commit,
                                    "target_commit_after": target_commit,
                                    "merge_base": audited_merge_base,
                                }
                            )
                        )
                    return MergeResult(**facts, error="source is not contained in target", error_code="not_merged")
                if not await self._git.is_ancestor(
                    ws.repo_path,
                    expected_target_commit_before,
                    target_commit,
                ):
                    return MergeResult(
                        **facts,
                        error="target moved before integration could be reconciled",
                        error_code="integration_state_uncertain",
                    )
                parents = await self._git.parents(ws.repo_path, target_commit)
                if target_commit == source_commit:
                    # Fast-forward merge: the task branch now points at the
                    # exact source head captured in the intent.
                    return MergeResult(
                        **(
                            facts
                            | {
                                "success": True,
                                "target_commit": expected_target_commit_before,
                                "target_commit_after": target_commit,
                                "merge_base": audited_merge_base,
                            }
                        )
                    )
                if len(parents) >= 2 and parents[0] == expected_target_commit_before and parents[1] == source_commit:
                    # Non-fast-forward merge created from the exact captured
                    # target and source heads.
                    return MergeResult(
                        **(
                            facts
                            | {
                                "success": True,
                                "target_commit": expected_target_commit_before,
                                "target_commit_after": target_commit,
                                "merge_base": audited_merge_base,
                            }
                        )
                    )
                return MergeResult(
                    **facts,
                    error="target changed without a provable integration result",
                    error_code="integration_state_uncertain",
                )
            if await self._git.is_ancestor(ws.repo_path, source_commit, target_commit):
                # A post-restart probe has no pre-merge snapshot in memory.
                # Recover the before/after pair only when the commit graph
                # proves that this source produced the current target. An
                # already-contained source is otherwise an idempotent no-op;
                # treating an unrelated historical merge as a new merge would
                # create a false Git audit record after a restart.
                parents = await self._git.parents(ws.repo_path, target_commit)
                if len(parents) >= 2 and parents[1] == source_commit:
                    # Non-fast-forward merge created by `git merge source`.
                    target_before = parents[0]
                else:
                    return MergeResult(
                        **facts,
                        error="Git contains the source but no durable intent proves this operation moved the target",
                        error_code="integration_state_uncertain",
                    )
                return MergeResult(
                    **(
                        facts
                        | {
                            "success": True,
                            "target_commit": target_before,
                            "target_commit_after": target_commit,
                        }
                    )
                )

            target_worktree = Path(ws.repo_path).resolve().parent / "worktrees" / ws.task_id / "task-base"
            clean_path = str(target_worktree) if target_worktree.is_dir() else ws.repo_path
            if await self._git.is_clean(clean_path):
                return MergeResult(**facts, error="source is not contained in target", error_code="not_merged")
            return MergeResult(
                **facts,
                error="target worktree contains uncommitted or unresolved state",
                error_code="integration_state_uncertain",
            )

    async def resolver_unmerged_files(self, workspace_id: str) -> list[str]:
        ws = self._workspaces.get(workspace_id)
        if not ws or ws.workspace_kind != "resolver":
            return []
        return await self._git.unmerged_files(ws.worktree_path)

    async def resolver_conflict_context(self, workspace_id: str, files: list[str]) -> str:
        ws = self._workspaces.get(workspace_id)
        if not ws or ws.workspace_kind != "resolver":
            return ""
        return await self._git.conflict_context(ws.worktree_path, files)

    async def merge_resolver(
        self,
        workspace_id: str,
        expected_target_commit: str,
    ) -> MergeResult:
        ws = self._workspaces.get(workspace_id)
        if not ws or ws.workspace_kind != "resolver":
            return MergeResult(
                success=False,
                source_branch="",
                target_branch="",
                error="resolver workspace not found",
                error_code="resolver_workspace_missing",
            )
        async with self._get_lock(ws.task_id):
            async with self._integration_file_lock(ws.repo_path, ws.task_id):
                target = task_branch_name(ws.task_id)
                current_target = await self._git.rev_parse(ws.repo_path, target)
                if expected_target_commit and current_target != expected_target_commit:
                    return MergeResult(
                        success=False,
                        source_branch=ws.branch_name,
                        target_branch=target,
                        target_commit=current_target,
                        error="task branch moved while resolver was running",
                        error_code="target_moved",
                    )
                target_worktree = str(
                    Path(ws.repo_path).resolve().parent / "worktrees" / ws.task_id / "task-base"
                )
                if not Path(target_worktree).is_dir():
                    return MergeResult(
                        success=False,
                        source_branch=ws.branch_name,
                        target_branch=target,
                        target_commit=current_target,
                        error="trusted task-base worktree is missing",
                        error_code="workspace_missing",
                    )
                if await self._git.get_current_branch(target_worktree) != target:
                    return MergeResult(
                        success=False,
                        source_branch=ws.branch_name,
                        target_branch=target,
                        target_commit=current_target,
                        error="task-base worktree is not checked out on the task branch",
                        error_code="operation_binding_mismatch",
                    )
                return await self._git.merge_branch(target_worktree, ws.branch_name, target)

    async def commit(self, workspace_id: str, message: str) -> bool:
        ws = self._workspaces.get(workspace_id)
        if not ws:
            return False
        async with self._get_lock(ws.task_id):
            async with self._integration_file_lock(ws.repo_path, ws.task_id):
                return await self._git.add_and_commit(ws.worktree_path, message)

    async def merge(
        self,
        workspace_id: str,
        target_branch: str | None = None,
        *,
        before_merge: Callable[[MergeResult], Awaitable[None]] | None = None,
    ) -> MergeResult:
        ws = self._workspaces.get(workspace_id)
        if not ws:
            return MergeResult(
                success=False,
                source_branch="",
                target_branch=target_branch or "",
                error="workspace not found",
            )
        async with self._get_lock(ws.task_id):
            async with self._integration_file_lock(ws.repo_path, ws.task_id):
                target = target_branch or task_branch_name(ws.task_id)
                target_worktree = str(
                    Path(ws.repo_path).resolve().parent / "worktrees" / ws.task_id / "task-base"
                )
                if target == task_branch_name(ws.task_id):
                    if not Path(target_worktree).is_dir():
                        return MergeResult(
                            success=False,
                            source_branch=ws.branch_name,
                            target_branch=target,
                            error="trusted task-base worktree is missing",
                            error_code="workspace_missing",
                        )
                    merge_path = target_worktree
                else:
                    # An explicit non-task target is an administrative action;
                    # retain the existing repository-path behavior only for
                    # that explicit caller-owned target branch.
                    merge_path = ws.repo_path
                committed, commit_error = await self._git.commit_if_dirty(
                    ws.worktree_path,
                    "auto: merge前自动提交",
                )
                if not committed:
                    return MergeResult(
                        success=False,
                        source_branch=ws.branch_name,
                        target_branch=target,
                        error=commit_error or "failed to commit source worktree",
                        error_code="commit_failed",
                    )
                result = await self._git.merge_branch(
                    merge_path,
                    ws.branch_name,
                    target,
                    before_merge=before_merge,
                )
                if result.success and target == await self._git.default_branch(ws.repo_path):
                    ws.status = WorkspaceStatus.MERGED
                    await self._store.save(ws)
                return result

    async def merge_task_to_main(self, repo_path: str, task_id: str) -> MergeResult:
        # task -> main is an explicit second-stage action, but it still shares
        # the same per-scope Git serialization boundary with taskctl and the
        # normal AgentEnd integration path.
        validate_workspace_identifier(task_id, "task_id")
        async with self._get_lock(task_id):
            async with self._integration_file_lock(repo_path, task_id):
                target = await self._git.default_branch(repo_path)
                return await self._git.merge_branch(repo_path, task_branch_name(task_id), target)

    async def diff_task_to_main(self, repo_path: str, task_id: str) -> str:
        base = await self._git.default_branch(repo_path)
        return await self._git.diff_between(repo_path, base, task_branch_name(task_id))

    # 非活跃清理

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
