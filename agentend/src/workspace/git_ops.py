import asyncio
import logging
import shutil
import subprocess
import threading
from collections.abc import Awaitable, Callable
from pathlib import Path

from src.workspace.models import MergeResult, validate_workspace_identifier

logger = logging.getLogger(__name__)

GIT_COMMAND_TIMEOUT_SECONDS = 30.0


class GitOps:
    async def is_git_repo(self, path: str) -> bool:
        ok, _ = await self._run_git("rev-parse", "--is-inside-work-tree", cwd=path)
        return ok

    async def init_repo(self, path: str) -> bool:
        ok, _ = await self._run_git("init", cwd=path)
        if not ok:
            return False
        if not await self._ensure_user_config(path):
            return False
        ok, _ = await self._run_git("add", "-A", cwd=path)
        if not ok:
            return False
        ok, _ = await self._run_git("commit", "--allow-empty", "-m", "init", cwd=path)
        return ok

    async def ensure_ready_repo(self, path: str) -> bool:
        """确保 path 是一个带有有效 HEAD 提交的 git 仓库。

        `git worktree add -b <branch> <base>` 需要 <base> 能解析到一个
        已存在的提交。一个刚执行过 `git init` 的仓库虽然是 git 仓库，但还没有
        有效的 HEAD，因此这里初始化一个空提交，而不是等到后续才失败。
        """
        if not await self.is_git_repo(path):
            return await self.init_repo(path)
        if not await self._ensure_user_config(path):
            return False
        ok, _ = await self._run_git("rev-parse", "--verify", "HEAD", cwd=path)
        if ok:
            return True
        ok, _ = await self._run_git("add", "-A", cwd=path)
        if not ok:
            return False
        ok, _ = await self._run_git("commit", "--allow-empty", "-m", "init", cwd=path)
        return ok

    async def default_branch(self, repo_path: str) -> str:
        """返回用于 task worktree 的最佳本地基础分支。"""
        ok, out = await self._run_git("for-each-ref", "--format=%(refname:short)", "refs/heads/", cwd=repo_path)
        branches = [line.strip() for line in out.splitlines() if line.strip()] if ok else []

        ok, out = await self._run_git("symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD", cwd=repo_path)
        if ok and out.strip():
            remote_head = out.strip()
            remote_branch = remote_head.split("/", 1)[1] if "/" in remote_head else remote_head
            if remote_branch in branches:
                return remote_branch

        ok, out = await self._run_git("rev-parse", "--abbrev-ref", "HEAD", cwd=repo_path)
        current_branch = out.strip() if ok else ""
        if current_branch and current_branch != "HEAD":
            return current_branch

        for preferred in ("main", "master"):
            if preferred in branches:
                return preferred
        if branches:
            return branches[0]
        return "HEAD"

    async def _ensure_user_config(self, path: str) -> bool:
        ok, name = await self._run_git("config", "user.name", cwd=path)
        if not ok or not name.strip():
            ok, _ = await self._run_git("config", "user.name", "AgentHub", cwd=path)
            if not ok:
                return False
        ok, email = await self._run_git("config", "user.email", cwd=path)
        if not ok or not email.strip():
            ok, _ = await self._run_git("config", "user.email", "agent@agenthub.dev", cwd=path)
            if not ok:
                return False
        return True

    async def _run_git(self, *args: str, cwd: str | None = None) -> tuple[bool, str]:
        cmd = ["git", *args]
        result: subprocess.CompletedProcess[str] | None = None
        failure: BaseException | None = None

        def run() -> None:
            nonlocal result, failure
            try:
                result = subprocess.run(
                    cmd,
                    cwd=cwd,
                    check=False,
                    capture_output=True,
                    text=True,
                    timeout=GIT_COMMAND_TIMEOUT_SECONDS,
                )
            except BaseException as exc:
                failure = exc

        thread = threading.Thread(target=run, daemon=True)
        thread.start()
        loop = asyncio.get_running_loop()
        deadline = loop.time() + GIT_COMMAND_TIMEOUT_SECONDS + 1.0
        while thread.is_alive() and loop.time() < deadline:
            await asyncio.sleep(0.05)
        if thread.is_alive():
            logger.warning("git %s timed out after %.0fs", " ".join(args), GIT_COMMAND_TIMEOUT_SECONDS)
            return False, f"git command timed out after {GIT_COMMAND_TIMEOUT_SECONDS:.0f}s"

        if isinstance(failure, subprocess.TimeoutExpired):
            logger.warning("git %s timed out after %.0fs", " ".join(args), GIT_COMMAND_TIMEOUT_SECONDS)
            return False, f"git command timed out after {GIT_COMMAND_TIMEOUT_SECONDS:.0f}s"
        if failure is not None:
            logger.warning("git %s failed to start: %s", " ".join(args), failure)
            return False, str(failure)
        if result is None:
            logger.warning("git %s produced no result", " ".join(args))
            return False, "git command produced no result"
        if result.returncode != 0:
            err = result.stderr.strip() or result.stdout.strip()
            logger.warning("git %s failed: %s", " ".join(args), err)
            return False, err
        return True, result.stdout.strip()

    async def worktree_add(self, repo_path: str, path: str, branch: str, base_branch: str | None = None) -> bool:
        target_path = str(Path(path).resolve())
        for existing_path, existing_branch in await self.worktree_list(repo_path):
            if Path(existing_path).resolve() == Path(target_path):
                return existing_branch == branch

        path_obj = Path(target_path)
        if path_obj.exists():
            logger.warning("Removing stale worktree directory before recreation: %s", target_path)
            shutil.rmtree(path_obj)

        ok, out = await self._run_git("branch", "--list", branch, cwd=repo_path)
        if ok and out.strip():
            ok, _ = await self._run_git("worktree", "add", target_path, branch, cwd=repo_path)
        else:
            args = ["worktree", "add", target_path, "-b", branch]
            if base_branch:
                args.append(base_branch)
            ok, _ = await self._run_git(*args, cwd=repo_path)
        return ok

    async def worktree_remove(self, path: str) -> bool:
        ok, _ = await self._run_git("worktree", "remove", path, "--force", cwd=path)
        return ok

    async def branch_create(self, repo_path: str, name: str, base: str = "HEAD") -> bool:
        ok, _ = await self._run_git("branch", name, base, cwd=repo_path)
        return ok

    async def branch_delete(self, repo_path: str, name: str) -> bool:
        ok, _ = await self._run_git("branch", "-D", name, cwd=repo_path)
        return ok

    async def list_branches(self, repo_path: str) -> list[str]:
        ok, out = await self._run_git("for-each-ref", "--format=%(refname:short)", "refs/heads/", cwd=repo_path)
        if not ok:
            return []
        return [line.strip() for line in out.splitlines() if line.strip()]

    async def rev_parse(self, path: str, ref: str) -> str:
        ok, out = await self._run_git("rev-parse", ref, cwd=path)
        return out.strip() if ok else ""

    async def merge_base(self, path: str, left: str, right: str) -> str:
        ok, out = await self._run_git("merge-base", left, right, cwd=path)
        return out.strip() if ok else ""

    async def is_ancestor(self, path: str, ancestor: str, descendant: str) -> bool:
        """Return whether ``ancestor`` is already contained in ``descendant``."""
        ok, _ = await self._run_git(
            "merge-base",
            "--is-ancestor",
            ancestor,
            descendant,
            cwd=path,
        )
        return ok

    async def is_clean(self, path: str) -> bool:
        ok, output = await self._run_git("status", "--porcelain", cwd=path)
        return ok and not output.strip()

    async def adopt_branch(
        self,
        target_path: str,
        source_ref: str,
        target_ref: str,
        *,
        expected_source_commit: str = "",
        expected_target_commit: str = "",
    ) -> MergeResult:
        """Move a trusted task branch to an explicitly captured source head.

        This is the destructive ``accept_source`` decision.  It is deliberately
        narrower than a general reset: the target worktree must be clean and
        both refs are compared with the ConflictRecord snapshot immediately
        before the reset.  A concurrent integration therefore fails closed.
        """
        if not source_ref or not target_ref or any(
            value.startswith("-") or "\x00" in value or any(char.isspace() for char in value)
            for value in (source_ref, target_ref)
        ):
            return MergeResult(
                success=False,
                source_branch=source_ref,
                target_branch=target_ref,
                error="invalid Git ref",
                error_code="invalid_git_ref",
            )

        source_commit = await self.rev_parse(target_path, source_ref)
        target_commit = await self.rev_parse(target_path, target_ref)
        merge_base = await self.merge_base(target_path, target_ref, source_ref)
        facts = dict(
            success=False,
            source_branch=source_ref,
            target_branch=target_ref,
            source_commit=source_commit,
            target_commit=target_commit,
            merge_base=merge_base,
        )
        if not source_commit:
            return MergeResult(**facts, error="source branch or commit is missing", error_code="source_missing")
        if not target_commit:
            return MergeResult(**facts, error="target branch or commit is missing", error_code="target_missing")
        current_branch = await self.get_current_branch(target_path)
        if current_branch != target_ref:
            return MergeResult(
                **facts,
                error="target worktree is not checked out on the recorded task branch",
                error_code="operation_binding_mismatch",
            )
        if expected_source_commit and source_commit != expected_source_commit:
            return MergeResult(
                **facts,
                error="source branch moved after the conflict was recorded",
                error_code="source_moved",
            )
        if expected_target_commit and target_commit != expected_target_commit:
            if target_commit == source_commit:
                return MergeResult(
                    **facts,
                    success=True,
                    target_commit_after=target_commit,
                )
            return MergeResult(
                **facts,
                error="task branch moved after the conflict was recorded",
                error_code="target_moved",
            )
        if not await self.is_clean(target_path):
            return MergeResult(
                **facts,
                error="task worktree contains uncommitted changes",
                error_code="target_dirty",
            )
        ok, error = await self._run_git("reset", "--hard", source_ref, cwd=target_path)
        if not ok:
            return MergeResult(**facts, error=error or "failed to adopt source", error_code="adopt_source_failed")
        target_after = await self.rev_parse(target_path, target_ref)
        if target_after != source_commit:
            return MergeResult(
                **facts,
                target_commit_after=target_after,
                error="task branch did not move to the captured source commit",
                error_code="integration_state_uncertain",
            )
        return MergeResult(
            **facts,
            success=True,
            target_commit_after=target_after,
        )

    async def unmerged_files(self, path: str) -> list[str]:
        ok, out = await self._run_git("diff", "--name-only", "--diff-filter=U", cwd=path)
        if not ok:
            return []
        return [line.strip() for line in out.splitlines() if line.strip()]

    async def conflict_context(self, path: str, files: list[str]) -> str:
        sections: list[str] = []
        for file in files:
            parts = [f"FILE: {file}"]
            for label, args in (
                ("BASE", ("show", f":1:{file}")),
                ("OURS", ("show", f":2:{file}")),
                ("THEIRS", ("show", f":3:{file}")),
            ):
                ok, content = await self._run_git(*args, cwd=path)
                parts.append(f"--- {label} ---\n{content if ok else '(unavailable)'}")
            sections.append("\n".join(parts))
        return "\n\n".join(sections)

    async def prepare_resolver_merge(
        self,
        resolver_path: str,
        source_branch: str,
        target_branch: str,
    ) -> MergeResult:
        """Merge source into an isolated resolver worktree without aborting conflicts."""
        source_commit = await self.rev_parse(resolver_path, source_branch)
        target_commit = await self.rev_parse(resolver_path, target_branch)
        merge_base = await self.merge_base(resolver_path, target_branch, source_branch)
        if not source_commit:
            return MergeResult(
                success=False,
                source_branch=source_branch,
                target_branch=target_branch,
                error="source branch or commit is missing",
                error_code="source_missing",
                source_commit=source_commit,
                target_commit=target_commit,
                merge_base=merge_base,
            )

        ok, err = await self._run_git("merge", "--no-commit", "--no-ff", source_branch, cwd=resolver_path)
        if ok:
            return MergeResult(
                success=True,
                source_branch=source_branch,
                target_branch=target_branch,
                source_commit=source_commit,
                target_commit=target_commit,
                merge_base=merge_base,
            )

        conflicts = await self.unmerged_files(resolver_path)
        return MergeResult(
            success=False,
            source_branch=source_branch,
            target_branch=target_branch,
            conflict_files=conflicts,
            error=err,
            error_code="merge_conflict" if conflicts else "merge_failed",
            source_commit=source_commit,
            target_commit=target_commit,
            merge_base=merge_base,
            aborted=False,
        )

    async def task_branch_create(self, repo_path: str, task_id: str) -> bool:
        branch = f"task/{task_id}"
        ok, out = await self._run_git("branch", "--list", branch, cwd=repo_path)
        if ok and out.strip():
            return True
        if not await self.ensure_ready_repo(repo_path):
            return False
        base_branch = await self.default_branch(repo_path)
        ok, _ = await self._run_git("branch", branch, base_branch, cwd=repo_path)
        return ok

    async def task_base_worktree_create(self, repo_path: str, task_id: str) -> str:
        """在 worktrees/{task_id}/task-base 上基于 task/{task_id} 创建 task-base worktree。

        返回绝对路径。幂等 —— 若需要则创建 task 分支。
        """
        validate_workspace_identifier(task_id, "task_id")
        task_branch = f"task/{task_id}"
        worktree_path = str(Path(repo_path).resolve().parent / "worktrees" / task_id / "task-base")

        for path, branch in await self.worktree_list(repo_path):
            if path == worktree_path:
                if branch == task_branch:
                    return worktree_path
                raise RuntimeError(f"task-base path is already used by branch {branch}")

        path_obj = Path(worktree_path)
        if path_obj.exists():
            logger.warning("Removing stale task-base directory before recreation: %s", worktree_path)
            shutil.rmtree(path_obj)

        if not await self.ensure_ready_repo(repo_path):
            raise RuntimeError(f"Failed to prepare git repo for {task_id}")
        base_branch = await self.default_branch(repo_path)
        ok = await self.worktree_add(repo_path, worktree_path, task_branch, base_branch=base_branch)
        if not ok:
            raise RuntimeError(f"Failed to create task-base worktree for {task_id}")
        return worktree_path

    async def task_base_worktree_remove(self, repo_path: str, task_id: str) -> bool:
        """移除指定 task_id 的 task-base worktree。"""
        validate_workspace_identifier(task_id, "task_id")
        worktree_path = str(Path(repo_path).resolve().parent / "worktrees" / task_id / "task-base")
        if not Path(worktree_path).exists():
            return True
        return await self.worktree_remove(worktree_path)

    async def worktree_list(self, repo_path: str) -> list[tuple[str, str]]:
        ok, out = await self._run_git("worktree", "list", "--porcelain", cwd=repo_path)
        if not ok:
            return []
        results: list[tuple[str, str]] = []
        current_path = None
        current_branch = None
        for line in out.splitlines():
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

    async def add_and_commit(self, path: str, message: str) -> bool:
        ok, out = await self._run_git("status", "--porcelain", cwd=path)
        if ok and not out.strip():
            return False
        ok, _ = await self._run_git("add", "-A", cwd=path)
        if not ok:
            return False
        ok, _ = await self._run_git("commit", "-m", message, cwd=path)
        return ok

    async def commit_if_dirty(self, path: str, message: str) -> tuple[bool, str]:
        """Commit an Agent worktree when it has changes and retain failures.

        ``taskctl merge`` historically performed this step before resolving the
        source branch ref.  The Phase 2 service path must preserve that
        behavior because the Git merge only sees committed branch state.
        ``(True, \"\")`` also represents an already-clean worktree.
        """
        ok, output = await self._run_git("status", "--porcelain", cwd=path)
        if not ok:
            return False, output
        if not output.strip():
            return True, ""
        ok, output = await self._run_git("add", "-A", cwd=path)
        if not ok:
            return False, output
        ok, output = await self._run_git("commit", "-m", message, cwd=path)
        if not ok:
            return False, output
        return True, ""

    async def parents(self, path: str, commit: str) -> list[str]:
        """Return the parents of ``commit`` in Git's first-parent order."""
        ok, output = await self._run_git("rev-list", "--parents", "-n", "1", commit, cwd=path)
        if not ok:
            return []
        parts = output.split()
        return parts[1:]

    async def first_parent(self, path: str, commit: str) -> str:
        """Return the first parent of a commit, or an empty string if absent."""
        parents = await self.parents(path, commit)
        return parents[0] if parents else ""

    async def merge_branch(
        self,
        repo_path: str,
        branch: str,
        target: str | None = None,
        *,
        before_merge: Callable[[MergeResult], Awaitable[None]] | None = None,
    ) -> MergeResult:
        target = target or await self.default_branch(repo_path)
        source_commit = await self.rev_parse(repo_path, branch)
        target_commit = await self.rev_parse(repo_path, target)
        merge_base = await self.merge_base(repo_path, target, branch)
        if not source_commit:
            return MergeResult(
                success=False,
                source_branch=branch,
                target_branch=target,
                error="source branch or commit is missing",
                error_code="source_missing",
                source_commit=source_commit,
                target_commit=target_commit,
                merge_base=merge_base,
            )
        ok, current = await self._run_git("rev-parse", "--abbrev-ref", "HEAD", cwd=repo_path)
        if not ok:
            return MergeResult(
                success=False,
                source_branch=branch,
                target_branch=target,
                error=current,
                error_code="git_metadata_failed",
                source_commit=source_commit,
                target_commit=target_commit,
                merge_base=merge_base,
            )
        ok, _ = await self._run_git("checkout", target, cwd=repo_path)
        if not ok:
            return MergeResult(
                success=False,
                source_branch=branch,
                target_branch=target,
                error=f"failed to checkout {target}",
                error_code="target_checkout_failed",
                source_commit=source_commit,
                target_commit=target_commit,
                merge_base=merge_base,
            )
        if before_merge is not None:
            try:
                await before_merge(
                    MergeResult(
                        success=False,
                        source_branch=branch,
                        target_branch=target,
                        source_commit=source_commit,
                        target_commit=target_commit,
                        merge_base=merge_base,
                    )
                )
            except Exception:
                await self._run_git("checkout", current.strip(), cwd=repo_path)
                raise
        ok, err = await self._run_git("merge", branch, cwd=repo_path)
        if not ok:
            _, conflicts = await self._run_git("diff", "--name-only", "--diff-filter=U", cwd=repo_path)
            abort_ok, abort_err = await self._run_git("merge", "--abort", cwd=repo_path)
            await self._run_git("checkout", current.strip(), cwd=repo_path)
            error = err
            if not abort_ok and abort_err:
                error = f"{err}\nmerge --abort failed: {abort_err}".strip()
            return MergeResult(
                success=False,
                source_branch=branch,
                target_branch=target,
                conflict_files=[line.strip() for line in conflicts.splitlines() if line.strip()],
                error=error,
                aborted=abort_ok,
                error_code="merge_aborted_failed" if not abort_ok else ("merge_conflict" if conflicts else "merge_failed"),
                source_commit=source_commit,
                target_commit=target_commit,
                merge_base=merge_base,
                target_commit_after=target_commit if abort_ok else "",
            )
        target_commit_after = await self.rev_parse(repo_path, target)
        await self._run_git("checkout", current.strip(), cwd=repo_path)
        return MergeResult(
            success=True,
            source_branch=branch,
            target_branch=target,
            source_commit=source_commit,
            target_commit=target_commit,
            merge_base=merge_base,
            target_commit_after=target_commit_after,
        )

    async def diff_between(self, repo_path: str, base: str, head: str) -> str:
        ok, out = await self._run_git("diff", f"{base}...{head}", cwd=repo_path)
        return out if ok else ""

    async def get_current_branch(self, path: str) -> str:
        ok, out = await self._run_git("rev-parse", "--abbrev-ref", "HEAD", cwd=path)
        return out.strip() if ok else ""

    @staticmethod
    def resolve_git_dir(worktree_path: str) -> Path | None:
        dot_git = Path(worktree_path) / ".git"
        if dot_git.is_dir():
            return dot_git
        if dot_git.is_file():
            content = dot_git.read_text().strip()
            if content.startswith("gitdir: "):
                git_dir = Path(content[len("gitdir: ") :].strip())
                if not git_dir.is_absolute():
                    git_dir = Path(worktree_path) / git_dir
                if git_dir.is_dir():
                    return git_dir
        return None

    async def setup_worktree_excludes(self, worktree_path: str, patterns: list[str]) -> None:
        """为 worktree 配置独立的 excludesFile，不影响主仓库。"""
        await self._run_git("config", "extensions.worktreeConfig", "true", cwd=worktree_path)
        git_dir = self.resolve_git_dir(worktree_path)
        if not git_dir:
            logger.warning("Cannot resolve git dir for worktree: %s", worktree_path)
            return
        exclude_file = git_dir / "excludes"
        exclude_file.write_text("\n".join(patterns) + "\n")
        await self._run_git("config", "--worktree", "core.excludesFile", str(exclude_file), cwd=worktree_path)
        logger.info("Set worktree excludesFile to %s with patterns %s", exclude_file, patterns)
