import asyncio
import json
import logging
from pathlib import Path
from typing import Protocol, runtime_checkable

from src.app.config import settings
from src.persistence import atomic_write_text
from src.workspace.models import Workspace, WorkspaceStatus

logger = logging.getLogger(__name__)


@runtime_checkable
class WorkspaceStoreProtocol(Protocol):
    async def load_all(self) -> dict[str, Workspace]: ...
    async def save(self, workspace: Workspace) -> None: ...
    async def delete(self, workspace_id: str) -> None: ...
    async def query_by_task(self, task_id: str) -> list[Workspace]: ...
    async def query_by_status(self, status: WorkspaceStatus) -> list[Workspace]: ...


class JsonFileWorkspaceStore:
    def __init__(self, path: Path | None = None) -> None:
        # 默认路径来自 config.yaml 的 workspace.store_path
        self._path = path or Path(settings.workspace.store_path)
        self._data: dict[str, dict] = {}
        self._lock = asyncio.Lock()
        self._load()

    def _load(self) -> None:
        if self._path.exists():
            try:
                raw = json.loads(self._path.read_text())
                if not isinstance(raw, dict):
                    raise ValueError("workspace store must contain an object")
                self._data = {
                    key: value
                    for key, value in raw.items()
                    if isinstance(key, str) and isinstance(value, dict)
                }
            except (json.JSONDecodeError, OSError, ValueError):
                logger.warning("Corrupted store file %s, starting empty", self._path)
                self._data = {}
        else:
            self._data = {}

    def _flush(self) -> None:
        atomic_write_text(self._path, json.dumps(self._data, indent=2, default=str))

    @staticmethod
    def _to_workspace(raw: dict) -> Workspace:
        from datetime import datetime

        raw_copy = dict(raw)
        if "status" in raw_copy and isinstance(raw_copy["status"], str):
            raw_copy["status"] = WorkspaceStatus(raw_copy["status"])
        if "created_at" in raw_copy and isinstance(raw_copy["created_at"], str):
            raw_copy["created_at"] = datetime.fromisoformat(raw_copy["created_at"])
        return Workspace(**raw_copy)

    async def load_all(self) -> dict[str, Workspace]:
        workspaces: dict[str, Workspace] = {}
        for key, value in self._data.items():
            try:
                workspaces[key] = self._to_workspace(value)
            except (TypeError, ValueError):
                logger.warning("Ignoring invalid workspace record %s in %s", key, self._path, exc_info=True)
        return workspaces

    async def save(self, workspace: Workspace) -> None:
        from dataclasses import asdict

        async with self._lock:
            raw = asdict(workspace)
            raw["status"] = workspace.status.value
            raw["created_at"] = workspace.created_at.isoformat()
            self._data[workspace.id] = raw
            self._flush()

    async def delete(self, workspace_id: str) -> None:
        async with self._lock:
            self._data.pop(workspace_id, None)
            self._flush()

    async def query_by_task(self, task_id: str) -> list[Workspace]:
        all_ws = await self.load_all()
        return [ws for ws in all_ws.values() if ws.task_id == task_id]

    async def query_by_status(self, status: WorkspaceStatus) -> list[Workspace]:
        all_ws = await self.load_all()
        return [ws for ws in all_ws.values() if ws.status == status]
