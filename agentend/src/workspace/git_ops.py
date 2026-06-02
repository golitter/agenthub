import asyncio
import logging
import shutil
from pathlib import Path

from src.workspace.models import MergeResult

logger = logging.getLogger(__name__)


class GitOps:
    async def is_git_repo(self, path: str) -> bool:
        ok, _ = await self._run_git("rev-parse", "--is-inside-work-tree", cwd=path)
        return ok

    async def init_repo(self, path: str) -> bool:
        ok, _ = await self._run_git("init", cwd=path)
        if not ok:
            return False
        ok, _ = await self._run_git("add", "-A", cwd=path)
        if not ok:
            return False
        ok, _ = await self._run_git("commit", "-m", "init", cwd=path)
        if not ok:
            return False
        ok, _ = await self._run_git("branch", "-M", "main", cwd=path)
        return ok

    async def _run_git(self, *args: str, cwd: str | None = None) -> tuple[bool, str]:
        cmd = ["git", *args]
        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            cwd=cwd,
        )
        stdout, stderr = await proc.communicate()
        if proc.returncode != 0:
            logger.warning("git %s failed: %s", " ".join(args), stderr.decode().strip())
            return False, stderr.decode().strip()
        return True, stdout.decode().strip()

    async def worktree_add(self, repo_path: str, path: str, branch: str, base_branch: str | None = None) -> bool:
        ok, out = await self._run_git("branch", "--list", branch, cwd=repo_path)
        if ok and out.strip():
            ok, _ = await self._run_git("worktree", "add", path, branch, cwd=repo_path)
        else:
            args = ["worktree", "add", path, "-b", branch]
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

    async def task_branch_create(self, repo_path: str, task_id: str) -> bool:
        branch = f"task/{task_id}"
        ok, out = await self._run_git("branch", "--list", branch, cwd=repo_path)
        if ok and out.strip():
            return True
        ok, _ = await self._run_git("branch", branch, "main", cwd=repo_path)
        return ok

    async def task_base_worktree_create(self, repo_path: str, task_id: str) -> str:
        """Create a task-base worktree at worktrees/{task_id}/task-base on task/{task_id}.

        Returns the absolute path. Idempotent -- creates the task branch if needed.
        """
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

        ok = await self.worktree_add(repo_path, worktree_path, task_branch, base_branch="main")
        if not ok:
            raise RuntimeError(f"Failed to create task-base worktree for {task_id}")
        return worktree_path

    async def task_base_worktree_remove(self, repo_path: str, task_id: str) -> bool:
        """Remove the task-base worktree for a given task_id."""
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

    async def merge_branch(self, repo_path: str, branch: str, target: str = "main") -> MergeResult:
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
