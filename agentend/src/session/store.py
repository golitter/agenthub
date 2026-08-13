import asyncio
import json
from pathlib import Path

from src.app.config import settings
from src.persistence import atomic_write_text


class SessionMappingStore:
    def __init__(self, path: Path | None = None) -> None:
        # 默认路径来自 config.yaml 的 session.store_path
        self._path = path or Path(settings.session.store_path)
        self._mappings: dict[str, str] = {}
        self._lock = asyncio.Lock()
        self._load()

    def _load(self) -> None:
        if self._path.exists():
            try:
                loaded = json.loads(self._path.read_text())
                if not isinstance(loaded, dict):
                    raise ValueError("session mapping store must contain an object")
                self._mappings = {
                    key: value for key, value in loaded.items() if isinstance(key, str) and isinstance(value, str)
                }
            except (json.JSONDecodeError, OSError, ValueError):
                self._mappings = {}
        else:
            self._mappings = {}

    def _save(self) -> None:
        atomic_write_text(self._path, json.dumps(self._mappings, indent=2))

    @staticmethod
    def _key(session_id: str, task_id: str) -> str:
        return f"{session_id}::{task_id}"

    def get_cli_session_id(self, session_id: str, task_id: str = "") -> str | None:
        return self._mappings.get(self._key(session_id, task_id))

    async def set_cli_session_id(self, session_id: str, cli_session_id: str, task_id: str = "") -> None:
        async with self._lock:
            self._mappings[self._key(session_id, task_id)] = cli_session_id
            self._save()

    async def delete(self, session_id: str, task_id: str = "") -> None:
        async with self._lock:
            self._mappings.pop(self._key(session_id, task_id), None)
            self._save()
