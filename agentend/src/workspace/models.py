import re
import uuid
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from pathlib import Path

from src.schemas.request import AgentType

_WORKSPACE_IDENTIFIER_RE = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9_-]{0,126}[A-Za-z0-9])?$")


def validate_workspace_identifier(value: str, field_name: str) -> str:
    """Validate identifiers before they become filesystem or Git ref components."""
    if not isinstance(value, str) or not _WORKSPACE_IDENTIFIER_RE.fullmatch(value):
        raise ValueError(
            f"{field_name} must be 1-128 ASCII letters, digits, underscores or hyphens "
            "and must start and end with a letter or digit"
        )
    return value


class WorkspaceStatus(str, Enum):
    ACTIVE = "active"
    MERGED = "merged"
    CLEANED = "cleaned"


def task_branch_name(task_id: str) -> str:
    validate_workspace_identifier(task_id, "task_id")
    return f"task/{task_id}"


def _generate_branch_name(session_id: str, task_id: str) -> str:
    validate_workspace_identifier(session_id, "session_id")
    validate_workspace_identifier(task_id, "task_id")
    return f"agent/{session_id}/{task_id}"


def _generate_worktree_path(repo_path: str, task_id: str, session_id: str) -> str:
    validate_workspace_identifier(task_id, "task_id")
    validate_workspace_identifier(session_id, "session_id")
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
    workspace_kind: str = "agent"
    conflict_id: str = ""
    attempt: int = 0

    def __post_init__(self):
        if self.task_id and self.session_id and not self.branch_name:
            self.branch_name = _generate_branch_name(self.session_id, self.task_id)
        if self.repo_path and self.task_id and self.session_id and not self.worktree_path:
            self.worktree_path = _generate_worktree_path(self.repo_path, self.task_id, self.session_id)


@dataclass
class MergeResult:
    success: bool
    source_branch: str
    target_branch: str
    conflict_files: list[str] = field(default_factory=list)
    error: str = ""
    aborted: bool = False
    error_code: str = ""
    source_commit: str = ""
    target_commit: str = ""
    merge_base: str = ""
    target_commit_after: str = ""
