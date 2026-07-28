import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.workspace import git_ops as git_ops_module
from src.workspace.git_ops import GitOps
from src.workspace.manager import WorkspaceManager
from src.workspace.models import Workspace, WorkspaceStatus


class MemoryWorkspaceStore:
    async def load_all(self) -> dict[str, Workspace]:
        return {}

    async def save(self, workspace: Workspace) -> None:
        pass

    async def delete(self, workspace_id: str) -> None:
        pass

    async def query_by_task(self, task_id: str) -> list[Workspace]:
        return []

    async def query_by_status(self, status: WorkspaceStatus) -> list[Workspace]:
        return []


def _git(repo: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", *args],
        cwd=repo,
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    return result.stdout.strip()


@pytest.mark.asyncio
async def test_task_base_worktree_create_initializes_unborn_master_repo(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-b", "master"], cwd=repo, check=True, stdout=subprocess.PIPE)

    worktree_path = await GitOps().task_base_worktree_create(str(repo), "task-empty")

    assert Path(worktree_path).is_dir()
    assert _git(repo, "branch", "--show-current") == "master"
    assert _git(repo, "rev-parse", "--verify", "master")
    assert _git(repo, "rev-parse", "--verify", "task/task-empty")

    task_head = _git(repo, "rev-parse", "task/task-empty")
    master_head = _git(repo, "rev-parse", "master")
    assert task_head == master_head
    assert _git(Path(worktree_path), "branch", "--show-current") == "task/task-empty"


@pytest.mark.asyncio
async def test_task_base_worktree_create_uses_existing_default_branch(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-b", "master"], cwd=repo, check=True, stdout=subprocess.PIPE)
    _git(repo, "config", "user.name", "Test User")
    _git(repo, "config", "user.email", "test@example.com")
    (repo / "README.md").write_text("# Test\n")
    _git(repo, "add", "-A")
    _git(repo, "commit", "-m", "initial")

    worktree_path = await GitOps().task_base_worktree_create(str(repo), "task-master")

    assert Path(worktree_path).is_dir()
    assert await GitOps().default_branch(str(repo)) == "master"
    assert _git(repo, "rev-parse", "task/task-master") == _git(repo, "rev-parse", "master")


@pytest.mark.asyncio
async def test_run_git_returns_failure_when_command_times_out(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setattr(git_ops_module, "GIT_COMMAND_TIMEOUT_SECONDS", 0.1)

    ok, output = await GitOps()._run_git(
        "-c",
        f'alias.wait=!{sys.executable} -c "import time; time.sleep(2)"',
        "wait",
        cwd=str(tmp_path),
    )

    assert ok is False
    assert "timed out" in output


@pytest.mark.asyncio
async def test_cleanup_task_branches_removes_agent_branches(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-b", "master"], cwd=repo, check=True, stdout=subprocess.PIPE)
    _git(repo, "config", "user.name", "Test User")
    _git(repo, "config", "user.email", "test@example.com")
    (repo / "README.md").write_text("# Test\n")
    _git(repo, "add", "-A")
    _git(repo, "commit", "-m", "initial")
    _git(repo, "branch", "task/task-clean")
    _git(repo, "branch", "agent/session-1/task-clean")
    _git(repo, "branch", "agent/session-2/other-task")
    _git(repo, "branch", "agent/session-3/not-task-clean")

    cleaned = await WorkspaceManager(MemoryWorkspaceStore()).cleanup_task_branches("task-clean", str(repo))

    assert cleaned is True
    branches = set(await GitOps().list_branches(str(repo)))
    assert "task/task-clean" not in branches
    assert "agent/session-1/task-clean" not in branches
    assert "agent/session-2/other-task" in branches
    assert "agent/session-3/not-task-clean" in branches
