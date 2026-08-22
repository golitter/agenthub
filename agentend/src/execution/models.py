from __future__ import annotations

import hashlib
import json
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from typing import Any

from src.generated.agent_run import AgentRunBudget, AgentRunState


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


@dataclass(frozen=True)
class RunSpec:
    run_id: str
    root_run_id: str
    task_id: str
    session_id: str
    workspace_id: str
    agent_type: str
    plan_task_id: str = ""
    integration_operation_id: str = ""
    workspace_handle: str = ""
    integration_attempt: int = 0
    request_fingerprint: str = ""
    parent_run_id: str | None = None
    message_id: str | None = None
    requested_by: str = "backend"
    budget: AgentRunBudget = field(default_factory=AgentRunBudget)

    def fingerprint(self) -> str:
        payload = asdict(self)
        payload["budget"] = self.budget.model_dump(mode="json")
        canonical = json.dumps(payload, sort_keys=True, separators=(",", ":"))
        return hashlib.sha256(canonical.encode()).hexdigest()


@dataclass
class RunRecord:
    spec: RunSpec
    state: AgentRunState = AgentRunState.QUEUED
    termination_reason: str | None = None
    created_at: str = field(default_factory=utc_now)
    started_at: str | None = None
    finished_at: str | None = None
    last_event_seq: int = 0
    admission_closed: bool = False
    runtime: dict[str, Any] = field(default_factory=dict)

    @property
    def terminal(self) -> bool:
        return self.state in {
            AgentRunState.COMPLETED,
            AgentRunState.FAILED,
            AgentRunState.CANCELLED,
        }
