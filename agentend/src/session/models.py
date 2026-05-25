import asyncio
from dataclasses import dataclass, field
from datetime import datetime

from src.generated.session import SessionState

_VALID_TRANSITIONS: dict[SessionState, set[SessionState]] = {
    SessionState.IDLE: {SessionState.RUNNING},
    SessionState.RUNNING: {SessionState.COMPLETED, SessionState.INTERRUPTED, SessionState.ERROR},
    SessionState.COMPLETED: set(),
    SessionState.INTERRUPTED: set(),
    SessionState.ERROR: set(),
    SessionState.INACTIVE: set(),
}


@dataclass
class Session:
    id: str
    agent_type: str
    state: SessionState = SessionState.IDLE
    process: asyncio.subprocess.Process | None = None
    workspace_path: str = ""
    created_at: datetime = field(default_factory=datetime.now)
    last_active: datetime = field(default_factory=datetime.now)
    history: list[dict] = field(default_factory=list)
    metadata: dict = field(default_factory=dict)
