import asyncio
import uuid
from datetime import datetime

from src.session.models import _VALID_TRANSITIONS, Session, SessionState


class SessionManager:
    def __init__(self) -> None:
        self._sessions: dict[str, Session] = {}

    def create(self, agent_type: str, metadata: dict | None = None, workspace_path: str = "") -> Session:
        session_id = str(uuid.uuid4())
        session = Session(
            id=session_id,
            agent_type=agent_type,
            workspace_path=workspace_path,
            metadata=metadata or {},
        )
        self._sessions[session_id] = session
        return session

    def get(self, session_id: str) -> Session | None:
        return self._sessions.get(session_id)

    def list(self) -> list[Session]:
        return list(self._sessions.values())

    def update_state(self, session_id: str, new_state: SessionState) -> Session:
        session = self._sessions.get(session_id)
        if not session:
            raise ValueError(f"Session not found: {session_id}")

        allowed = _VALID_TRANSITIONS.get(session.state, set())
        if new_state not in allowed:
            raise ValueError(f"Invalid state transition: {session.state.value} -> {new_state.value}")

        session.state = new_state
        session.last_active = datetime.now()
        return session

    async def destroy(self, session_id: str) -> bool:
        session = self._sessions.get(session_id)
        if not session:
            return False

        if session.process and session.process.returncode is None:
            session.process.terminate()
            try:
                await asyncio.wait_for(session.process.wait(), timeout=5.0)
            except asyncio.TimeoutError:
                session.process.kill()

        del self._sessions[session_id]
        return True

    def record_history(self, session_id: str, entry: dict) -> None:
        session = self._sessions.get(session_id)
        if session:
            session.history.append(entry)
            session.last_active = datetime.now()
