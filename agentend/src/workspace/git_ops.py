import asyncio
import logging
import shutil
import subprocess
import threading
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

    async def merge_branch(self, repo_path: str, branch: str, target: str | None = None) -> MergeResult:
        target = target or await self.default_branch(repo_path)
        ok, current = await self._run_git("rev-parse", "--abbrev-ref", "HEAD", cwd=repo_path)
        if not ok:
            return MergeResult(success=False, source_branch=branch, target_branch=target, error=current)
        ok, _ = await self._run_git("checkout", target, cwd=repo_path)
        if not ok:
            return MergeResult(
                success=False,
                source_branch=branch,
                target_branch=target,
                error=f"failed to checkout {target}",
            )
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
            )
        await self._run_git("checkout", current.strip(), cwd=repo_path)
        return MergeResult(success=True, source_branch=branch, target_branch=target)

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
