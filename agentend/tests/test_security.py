import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.adapters.base import child_process_env
from src.api.v1.agent import _write_soul_document
from src.orchestrator.memory.pin_memory import PinMemory
from src.schemas.request import AgentRequest, AgentType
from src.security.path_policy import PathPolicy, PathPolicyError
from src.security.startup_validation import is_loopback_host
from src.workspace.manager import WorkspaceManager
from src.workspace.models import Workspace, validate_workspace_identifier


def test_path_policy_rejects_prefix_collision_and_symlink_escape(tmp_path: Path):
    allowed = tmp_path / "allowed"
    allowed.mkdir()
    repo = allowed / "repo"
    repo.mkdir()
    outside = tmp_path / "allowed-escape"
    outside.mkdir()
    policy = PathPolicy([str(allowed)])
    assert policy.resolve_repo(str(repo)) == repo.resolve()
    with pytest.raises(PathPolicyError):
        policy.resolve_repo(str(outside))

    link = allowed / "link"
    link.symlink_to(outside, target_is_directory=True)
    with pytest.raises(PathPolicyError):
        policy.resolve_repo(str(link))


def test_path_policy_rejects_relative_and_broad_roots():
    with pytest.raises(PathPolicyError):
        PathPolicy(["relative"])
    with pytest.raises(PathPolicyError):
        PathPolicy(["/"])


def test_child_process_env_removes_control_plane_secrets(monkeypatch):
    monkeypatch.setenv("AGENTEND_SERVICE_TOKEN", "agentend-secret")
    monkeypatch.setenv("BACKEND_SERVICE_TOKEN", "backend-secret")
    monkeypatch.setenv("CREDENTIAL_BROKER_KEY", "broker-secret")
    monkeypatch.setenv("PATH", "/usr/bin")

    env = child_process_env()

    assert env["PATH"] == "/usr/bin"
    assert "AGENTEND_SERVICE_TOKEN" not in env
    assert "BACKEND_SERVICE_TOKEN" not in env
    assert "CREDENTIAL_BROKER_KEY" not in env


def test_unsafe_listener_loopback_detection():
    assert is_loopback_host("localhost")
    assert is_loopback_host("127.0.0.2")
    assert is_loopback_host("::1")
    assert not is_loopback_host("0.0.0.0")
    assert not is_loopback_host("agentend.internal")


@pytest.mark.parametrize("value", ["../escape", "..", "a/b", "a\\b", "bad name", "-leading"])
def test_workspace_identifier_rejects_path_and_git_ref_injection(value: str):
    with pytest.raises(ValueError):
        validate_workspace_identifier(value, "task_id")
    with pytest.raises(ValueError):
        Workspace(repo_path="/tmp/repo", task_id=value, session_id="session-1")


def test_agent_request_rejects_unsafe_workspace_identifiers():
    with pytest.raises(ValueError):
        AgentRequest(task_id="../../escape", session_id="session-1", message="hello")
    with pytest.raises(ValueError):
        AgentRequest(task_id="task-1", session_id="../escape", message="hello")


def test_soul_document_preserves_spaces(tmp_path: Path):
    request = AgentRequest(
        task_id="task-1",
        session_id="session-1",
        message="hello",
        agent_type=AgentType.CODEX,
        config={"soul_md": "You are a careful reviewer."},
    )

    _write_soul_document(request, str(tmp_path))

    assert (tmp_path / ".codex" / "SOUL.md").read_text() == "You are a careful reviewer."


def test_workspace_manager_only_resolves_registered_task_shared_dir(tmp_path: Path):
    worktree = tmp_path / "worktrees" / "task-1" / "session-1"
    worktree.mkdir(parents=True)
    workspace = Workspace(
        task_id="task-1",
        session_id="session-1",
        agent_name="codex",
        agent_type=AgentType.CODEX,
        repo_path=str(tmp_path / "repo"),
        worktree_path=str(worktree),
    )
    manager = WorkspaceManager(store=None)  # type: ignore[arg-type]
    manager._workspaces[workspace.id] = workspace

    expected = worktree.parent / "shared" / ".agent"
    assert manager.resolve_shared_dir(str(expected)) == str(expected.resolve())
    with pytest.raises(ValueError):
        manager.resolve_shared_dir(str(tmp_path / "arbitrary"))


def test_pin_memory_rejects_filename_escape_and_symlink(tmp_path: Path):
    common = tmp_path / "common"
    common.mkdir()
    outside = tmp_path / "secret.md"
    outside.write_text("secret")
    (common / "escape.md").symlink_to(outside)
    memory = PinMemory(common)

    assert memory.get_full_content("../secret.md") is None
    assert memory.get_full_content("escape.md") is None
