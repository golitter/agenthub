import uuid
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from pathlib import Path


class WorkspaceStatus(str, Enum):
    ACTIVE = "active"
    MERGED = "merged"
    CLEANED = "cleaned"


def task_branch_name(task_id: str) -> str:
    return f"task/{task_id}"


def _generate_branch_name(agent_name: str, task_id: str) -> str:
    return f"agent/{agent_name}/{task_id}"


def _generate_worktree_path(repo_path: str, task_id: str, agent_name: str) -> str:
    repo = Path(repo_path).resolve()
    return str(repo.parent / "worktrees" / task_id / agent_name)


@dataclass
class Workspace:
    id: str = field(default_factory=lambda: str(uuid.uuid4()))
    task_id: str = ""
    agent_name: str = ""
    repo_path: str = ""
    worktree_path: str = ""
    branch_name: str = ""
    session_id: str | None = None
    container_id: str | None = None
    status: WorkspaceStatus = WorkspaceStatus.ACTIVE
    created_at: datetime = field(default_factory=datetime.now)

    def __post_init__(self):
        if self.task_id and self.agent_name and not self.branch_name:
            self.branch_name = _generate_branch_name(self.agent_name, self.task_id)
        if self.repo_path and self.task_id and self.agent_name and not self.worktree_path:
            self.worktree_path = _generate_worktree_path(self.repo_path, self.task_id, self.agent_name)
