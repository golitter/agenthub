import uuid
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from pathlib import Path

from src.schemas.request import AgentType


class WorkspaceStatus(str, Enum):
    ACTIVE = "active"
    MERGED = "merged"
    CLEANED = "cleaned"


def task_branch_name(task_id: str) -> str:
    return f"task/{task_id}"


def _generate_branch_name(session_id: str, task_id: str) -> str:
    return f"agent/{session_id}/{task_id}"


def _generate_worktree_path(repo_path: str, task_id: str, session_id: str) -> str:
    repo = Path(repo_path).resolve()
    return str(repo.parent / "worktrees" / task_id / session_id)


@dataclass
class Workspace:
    id: str = field(default_factory=lambda: str(uuid.uuid4()))
    task_id: str = ""
    agent_name: str = ""
    agent_type: AgentType | None = None
    repo_path: str = ""
    worktree_path: str = ""
    branch_name: str = ""
    session_id: str = ""
    container_id: str | None = None
    status: WorkspaceStatus = WorkspaceStatus.ACTIVE
    created_at: datetime = field(default_factory=datetime.now)

    def __post_init__(self):
        if self.task_id and self.session_id and not self.branch_name:
            self.branch_name = _generate_branch_name(self.session_id, self.task_id)
        if self.repo_path and self.task_id and self.session_id and not self.worktree_path:
            self.worktree_path = _generate_worktree_path(self.repo_path, self.task_id, self.session_id)
