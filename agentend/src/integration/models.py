from __future__ import annotations

from datetime import datetime, timezone
from typing import Literal

from pydantic import BaseModel, Field


IntegrationOperationStatus = Literal[
    "pending",
    "integrating",
    "merged",
    "conflict",
    "partial",
    "failed",
    "cancelled",
    "integration_state_uncertain",
]


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


class IntegrationOperation(BaseModel):
    integration_operation_id: str
    root_run_id: str
    parent_run_id: str = ""
    run_id: str
    plan_task_id: str
    attempt: int = Field(default=0, ge=0)
    session_id: str
    workspace_id: str
    workspace_handle: str = ""
    integration_scope_id: str
    status: IntegrationOperationStatus = "pending"
    result_digest: str = ""
    error_code: str = ""
    error_message: str = ""
    created_at: str = Field(default_factory=utc_now)
    started_at: str = ""
    finished_at: str = ""
    row_version: int = Field(default=0, ge=0)

    @property
    def terminal(self) -> bool:
        return self.status in {
            "merged",
            "partial",
            "conflict",
            "failed",
            "cancelled",
            "integration_state_uncertain",
        }


class IntegrationIntent(BaseModel):
    """Durable pre-Git snapshot for crash-safe integration recovery."""

    integration_operation_id: str
    workspace_id: str
    integration_scope_id: str
    source_branch: str
    source_commit: str
    target_branch: str
    target_commit_before: str
    merge_base: str
    created_at: str = Field(default_factory=utc_now)


class GitIntegrationRecord(BaseModel):
    record_id: str
    integration_operation_id: str
    workspace_id: str
    integration_scope_id: str
    status: str
    source_branch: str = ""
    source_commit: str = ""
    target_branch: str = ""
    target_commit_before: str = ""
    target_commit_after: str = ""
    merge_base: str = ""
    conflict_files: list[str] = Field(default_factory=list)
    aborted: bool = False
    git_exit_code: int | None = None
    error_code: str = ""
    error_message: str = ""
    started_at: str = ""
    finished_at: str = ""
    created_at: str = Field(default_factory=utc_now)


class ConflictRecord(BaseModel):
    """Durable conflict projection linked to one original operation.

    Git refs are retained here for recovery/audit only.  They are never part
    of the ordinary Orchestrator projection.
    """

    conflict_id: str
    root_run_id: str = ""
    original_operation_id: str
    plan_task_id: str
    integration_scope_id: str
    workspace_id: str
    status: str
    attempt: int = Field(default=0, ge=0)
    source_branch: str = ""
    source_commit: str = ""
    target_branch: str = ""
    target_commit: str = ""
    merge_base: str = ""
    conflict_files: list[str] = Field(default_factory=list)
    resolver_agent: str = ""
    resolver_session_id: str = ""
    resolver_branch: str = ""
    resolver_run_id: str = ""
    last_error_code: str = ""
    last_error_message: str = ""
    created_at: str = Field(default_factory=utc_now)
    updated_at: str = Field(default_factory=utc_now)
    row_version: int = Field(default=0, ge=0)


class ResolutionAttempt(BaseModel):
    resolution_attempt_id: str
    conflict_id: str
    original_operation_id: str
    resolver_run_id: str = ""
    resolver_workspace_id: str = ""
    attempt: int = Field(default=0, ge=0)
    status: str
    expected_target_commit: str = ""
    resolver_commit: str = ""
    error_code: str = ""
    error_message: str = ""
    created_at: str = Field(default_factory=utc_now)
    finished_at: str = ""


class ResolutionIntegrationRecord(BaseModel):
    """Immutable Git fact for a resolver branch being merged into task."""

    resolution_record_id: str
    conflict_id: str
    original_operation_id: str
    root_run_id: str
    plan_task_id: str
    integration_scope_id: str
    resolver_run_id: str
    resolver_workspace_id: str
    attempt: int = Field(default=0, ge=0)
    status: str
    source_branch: str = ""
    source_commit: str = ""
    target_branch: str = ""
    target_commit_before: str = ""
    target_commit_after: str = ""
    merge_base: str = ""
    conflict_files: list[str] = Field(default_factory=list)
    aborted: bool = False
    error_code: str = ""
    error_message: str = ""
    started_at: str = ""
    finished_at: str = ""
    created_at: str = Field(default_factory=utc_now)


class ConflictActionRecord(BaseModel):
    """Durable idempotency/audit record for a human conflict action."""

    action_id: str
    conflict_id: str
    action: str
    task_id: str
    session_id: str
    root_run_id: str
    expected_attempt: int = Field(default=0, ge=0)
    idempotency_key: str
    status: str = "accepted"
    result_json: str = ""
    error_code: str = ""
    error_message: str = ""
    created_at: str = Field(default_factory=utc_now)
    finished_at: str = ""


class IntegrationProjection(BaseModel):
    integration_operation_id: str
    plan_task_id: str
    run_id: str
    attempt: int
    status: str
    conflict_id: str = ""
    conflict_files: list[str] = Field(default_factory=list)
    error_code: str = ""
    error_message: str = ""
    sequence: int = 0
    finished_at: str = ""
