import asyncio
import logging

logger = logging.getLogger(__name__)


class GitOps:
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

    async def task_branch_create(self, repo_path: str, task_id: str) -> bool:
        branch = f"task/{task_id}"
        ok, out = await self._run_git("branch", "--list", branch, cwd=repo_path)
        if ok and out.strip():
            return True
        ok, _ = await self._run_git("branch", branch, "main", cwd=repo_path)
        return ok

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

    async def merge_branch(self, repo_path: str, branch: str, target: str = "main") -> bool:
        ok, current = await self._run_git("rev-parse", "--abbrev-ref", "HEAD", cwd=repo_path)
        ok, _ = await self._run_git("checkout", target, cwd=repo_path)
        if not ok:
            return False
        ok, err = await self._run_git("merge", branch, cwd=repo_path)
        if not ok:
            await self._run_git("merge", "--abort", cwd=repo_path)
            await self._run_git("checkout", current.strip(), cwd=repo_path)
            return False
        return True

    async def get_current_branch(self, path: str) -> str:
        ok, out = await self._run_git("rev-parse", "--abbrev-ref", "HEAD", cwd=path)
        return out.strip() if ok else ""
