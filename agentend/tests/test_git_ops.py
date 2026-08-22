import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.schemas.request import AgentType
from src.workspace import git_ops as git_ops_module
from src.workspace.git_ops import GitOps
from src.workspace.manager import WorkspaceManager
from src.workspace.models import MergeResult, Workspace, WorkspaceStatus


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


@pytest.mark.asyncio
async def test_existing_workspace_refreshes_managed_skills(monkeypatch, tmp_path: Path) -> None:
    class FakeGit:
        async def task_base_worktree_create(self, repo_path: str, task_id: str) -> str:
            return str(tmp_path / "task-base")

    manager = WorkspaceManager(MemoryWorkspaceStore())
    manager._git = FakeGit()
    existing = Workspace(
        task_id="task-refresh",
        session_id="session-refresh",
        agent_type=AgentType.CODEX,
        repo_path=str(tmp_path),
    )
    manager._workspaces[existing.id] = existing
    calls: list[tuple[str, AgentType | None]] = []
    monkeypatch.setattr(
        manager._provisioner,
        "provision",
        lambda worktree_path, agent_type: calls.append((worktree_path, agent_type)),
    )

    result = await manager.create(
        str(tmp_path),
        "task-refresh",
        "codex",
        "session-refresh",
        AgentType.CODEX,
    )

    assert result is existing
    assert calls == [(existing.worktree_path, AgentType.CODEX)]


@pytest.mark.asyncio
async def test_conflicting_parallel_agent_merges_abort_and_preserve_lineage(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-b", "master"], cwd=repo, check=True, stdout=subprocess.PIPE)
    _git(repo, "config", "user.name", "Test User")
    _git(repo, "config", "user.email", "test@example.com")
    (repo / "1.md").write_text("# hell\n")
    _git(repo, "add", "-A")
    _git(repo, "commit", "-m", "initial")

    ops = GitOps()
    task_base = await ops.task_base_worktree_create(str(repo), "task-conflict")
    agent_a = tmp_path / "agent-a"
    agent_b = tmp_path / "agent-b"
    assert await ops.worktree_add(str(repo), str(agent_a), "agent/session-a/task-conflict", "task/task-conflict")
    assert await ops.worktree_add(str(repo), str(agent_b), "agent/session-b/task-conflict", "task/task-conflict")

    (agent_a / "1.md").write_text("# hell\n\n## Alice\n")
    (agent_b / "1.md").write_text("# hell\n\n## 阿a\n")
    assert await ops.add_and_commit(str(agent_a), "alice change")
    assert await ops.add_and_commit(str(agent_b), "a change")

    first = await ops.merge_branch(task_base, "agent/session-a/task-conflict", "task/task-conflict")
    assert first.success is True
    second = await ops.merge_branch(task_base, "agent/session-b/task-conflict", "task/task-conflict")
    assert second.success is False
    assert second.error_code == "merge_conflict"
    assert second.aborted is True
    assert second.conflict_files == ["1.md"]
    assert await ops.unmerged_files(task_base) == []
    assert _git(Path(task_base), "status", "--porcelain") == ""
    assert "Alice" in (Path(task_base) / "1.md").read_text()
    assert _git(repo, "rev-parse", "agent/session-b/task-conflict")


@pytest.mark.asyncio
async def test_workspace_manager_merge_commits_dirty_source_before_integration(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-b", "master"], cwd=repo, check=True, stdout=subprocess.PIPE)
    _git(repo, "config", "user.name", "Test User")
    _git(repo, "config", "user.email", "test@example.com")
    (repo / "README.md").write_text("# Test\n")
    _git(repo, "add", "-A")
    _git(repo, "commit", "-m", "initial")

    ops = GitOps()
    task_base = await ops.task_base_worktree_create(str(repo), "task-dirty")
    agent_path = tmp_path / "agent-dirty"
    branch = "agent/session-dirty/task-dirty"
    assert await ops.worktree_add(str(repo), str(agent_path), branch, "task/task-dirty")
    (agent_path / "README.md").write_text("# Test\n\nmanaged by Agent\n")

    workspace = Workspace(
        task_id="task-dirty",
        agent_name="agent",
        agent_type=AgentType.CODEX,
        repo_path=str(repo),
        worktree_path=str(agent_path),
        branch_name=branch,
        session_id="session-dirty",
    )
    manager = WorkspaceManager(MemoryWorkspaceStore())
    manager._workspaces[workspace.id] = workspace

    result = await manager.merge(workspace.id)

    assert result.success is True
    assert result.source_commit
    assert result.target_commit_after
    assert "managed by Agent" in (Path(task_base) / "README.md").read_text()
    assert _git(agent_path, "status", "--porcelain") == ""


@pytest.mark.asyncio
async def test_probe_uses_pre_merge_intent_for_fast_forward_and_noop(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-b", "main"], cwd=repo, check=True, stdout=subprocess.PIPE)
    _git(repo, "config", "user.name", "Test User")
    _git(repo, "config", "user.email", "test@example.com")
    (repo / "README.md").write_text("# Test\n")
    _git(repo, "add", "-A")
    _git(repo, "commit", "-m", "initial")

    ops = GitOps()
    task_base = await ops.task_base_worktree_create(str(repo), "task-recovery")
    agent_path = tmp_path / "agent-recovery"
    agent_branch = "agent/session-recovery/task-recovery"
    assert await ops.worktree_add(str(repo), str(agent_path), agent_branch, "task/task-recovery")
    (agent_path / "README.md").write_text("# Test\n\nfast-forward\n")
    assert await ops.add_and_commit(str(agent_path), "fast-forward change")

    workspace = Workspace(
        task_id="task-recovery",
        agent_name="agent",
        agent_type=AgentType.CODEX,
        repo_path=str(repo),
        worktree_path=str(agent_path),
        branch_name=agent_branch,
        session_id="session-recovery",
    )
    manager = WorkspaceManager(MemoryWorkspaceStore())
    manager._workspaces[workspace.id] = workspace
    snapshots: list[MergeResult] = []

    async def capture(snapshot: MergeResult) -> None:
        snapshots.append(snapshot)

    merged = await manager.merge(workspace.id, before_merge=capture)
    assert merged.success is True
    assert snapshots and snapshots[0].target_commit
    probe = await manager.probe_integration(
        workspace.id,
        expected_source_commit=snapshots[0].source_commit,
        expected_target_commit_before=snapshots[0].target_commit,
    )
    assert probe.success is True
    assert probe.target_commit == merged.target_commit
    assert probe.target_commit_after == merged.target_commit_after

    noop_path = tmp_path / "agent-noop"
    noop_branch = "agent/session-noop/task-recovery"
    assert await ops.worktree_add(str(repo), str(noop_path), noop_branch, "task/task-recovery")
    noop_workspace = Workspace(
        task_id="task-recovery",
        agent_name="agent",
        agent_type=AgentType.CODEX,
        repo_path=str(repo),
        worktree_path=str(noop_path),
        branch_name=noop_branch,
        session_id="session-noop",
    )
    manager._workspaces[noop_workspace.id] = noop_workspace
    noop_snapshots: list[MergeResult] = []

    async def capture_noop(snapshot: MergeResult) -> None:
        noop_snapshots.append(snapshot)

    noop_merge = await manager.merge(noop_workspace.id, before_merge=capture_noop)
    assert noop_merge.success is True
    assert noop_snapshots and noop_snapshots[0].target_commit == noop_merge.target_commit_after
    noop_probe = await manager.probe_integration(
        noop_workspace.id,
        expected_source_commit=noop_snapshots[0].source_commit,
        expected_target_commit_before=noop_snapshots[0].target_commit,
    )
    assert noop_probe.success is True
    assert noop_probe.target_commit == noop_probe.target_commit_after == noop_merge.target_commit_after


@pytest.mark.asyncio
async def test_resolver_worktree_merges_both_sides_without_polluting_task_base(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    repo.mkdir()
    subprocess.run(["git", "init", "-b", "master"], cwd=repo, check=True, stdout=subprocess.PIPE)
    _git(repo, "config", "user.name", "Test User")
    _git(repo, "config", "user.email", "test@example.com")
    (repo / "1.md").write_text("# hell\n")
    _git(repo, "add", "-A")
    _git(repo, "commit", "-m", "initial")

    ops = GitOps()
    task_base = await ops.task_base_worktree_create(str(repo), "task-resolver")
    agent_a = tmp_path / "agent-a"
    agent_b = tmp_path / "agent-b"
    assert await ops.worktree_add(str(repo), str(agent_a), "agent/session-a/task-resolver", "task/task-resolver")
    assert await ops.worktree_add(str(repo), str(agent_b), "agent/session-b/task-resolver", "task/task-resolver")
    (agent_a / "1.md").write_text("# hell\n\n## Alice\n")
    (agent_b / "1.md").write_text("# hell\n\n## 阿a\n")
    assert await ops.add_and_commit(str(agent_a), "alice change")
    assert await ops.add_and_commit(str(agent_b), "a change")
    assert (await ops.merge_branch(task_base, "agent/session-a/task-resolver", "task/task-resolver")).success

    manager = WorkspaceManager(MemoryWorkspaceStore())
    resolver, preparation = await manager.create_resolver(
        repo_path=str(repo),
        task_id="task-resolver",
        conflict_id="conflict-1",
        attempt=0,
        source_branch="agent/session-b/task-resolver",
        resolver_session_id="session-resolver",
        agent_type=AgentType.CODEX,
        agent_name="resolver",
    )
    assert preparation.success is False
    assert preparation.error_code == "merge_conflict"
    assert preparation.conflict_files == ["1.md"]
    assert await ops.unmerged_files(task_base) == []

    resolver_file = Path(resolver.worktree_path) / "1.md"
    resolver_file.write_text("# hell\n\n## Alice\n\n## 阿a\n")
    assert await manager.resolver_commit(resolver.id, "resolve both sections")
    assert await manager.resolver_unmerged_files(resolver.id) == []
    merged = await manager.merge_resolver(resolver.id, preparation.target_commit)
    assert merged.success is True
    resolved = (Path(task_base) / "1.md").read_text()
    assert "Alice" in resolved
    assert "阿a" in resolved
    assert await ops.unmerged_files(task_base) == []
