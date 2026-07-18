import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.workspace.git_ops import GitOps


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
