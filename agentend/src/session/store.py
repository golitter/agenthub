import json
from pathlib import Path

_DEFAULT_STORE_PATH = Path("logs/session_mappings.json")


class SessionMappingStore:
    def __init__(self, path: Path | None = None) -> None:
        self._path = path or _DEFAULT_STORE_PATH
        self._mappings: dict[str, str] = {}
        self._load()

    def _load(self) -> None:
        if self._path.exists():
            try:
                self._mappings = json.loads(self._path.read_text())
            except (json.JSONDecodeError, OSError):
                self._mappings = {}
        else:
            self._mappings = {}

    def _save(self) -> None:
        self._path.parent.mkdir(parents=True, exist_ok=True)
        self._path.write_text(json.dumps(self._mappings, indent=2))

    @staticmethod
    def _key(session_id: str, task_id: str) -> str:
        return f"{session_id}::{task_id}"

    def get_cli_session_id(self, session_id: str, task_id: str = "") -> str | None:
        return self._mappings.get(self._key(session_id, task_id))

    def set_cli_session_id(self, session_id: str, cli_session_id: str, task_id: str = "") -> None:
        self._mappings[self._key(session_id, task_id)] = cli_session_id
        self._save()

    def delete(self, session_id: str, task_id: str = "") -> None:
        self._mappings.pop(self._key(session_id, task_id), None)
        self._save()
