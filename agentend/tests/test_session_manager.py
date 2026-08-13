from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.generated.session import SessionState
from src.session.manager import SessionManager


def test_external_session_identity_is_preserved_and_reusable():
    manager = SessionManager()
    session = manager.create("codex", session_id="backend-session")

    assert session.id == "backend-session"
    assert manager.get("backend-session") is session

    manager.update_state(session.id, SessionState.RUNNING)
    manager.update_state(session.id, SessionState.COMPLETED)
    manager.update_state(session.id, SessionState.RUNNING)
    manager.update_state(session.id, SessionState.ERROR)
    manager.update_state(session.id, SessionState.RUNNING)
    manager.update_state(session.id, SessionState.INTERRUPTED)
    manager.update_state(session.id, SessionState.RUNNING)
