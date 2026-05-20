import asyncio
import json
import logging
from pathlib import Path
from typing import Protocol, runtime_checkable

from src.workspace.models import Workspace, WorkspaceStatus

logger = logging.getLogger(__name__)

_DEFAULT_STORE_PATH = Path("logs/workspaces.json")


@runtime_checkable
class WorkspaceStoreProtocol(Protocol):
    async def load_all(self) -> dict[str, Workspace]: ...
    async def save(self, workspace: Workspace) -> None: ...
    async def delete(self, workspace_id: str) -> None: ...
    async def query_by_task(self, task_id: str) -> list[Workspace]: ...
    async def query_by_status(self, status: WorkspaceStatus) -> list[Workspace]: ...


class JsonFileWorkspaceStore:
    def __init__(self, path: Path | None = None) -> None:
        self._path = path or _DEFAULT_STORE_PATH
        self._data: dict[str, dict] = {}
        self._lock = asyncio.Lock()
        self._load()

    def _load(self) -> None:
        if self._path.exists():
            try:
                raw = json.loads(self._path.read_text())
                self._data = {k: v for k, v in raw.items()}
            except (json.JSONDecodeError, OSError):
                logger.warning("Corrupted store file %s, starting empty", self._path)
                self._data = {}
        else:
            self._data = {}

    def _flush(self) -> None:
        self._path.parent.mkdir(parents=True, exist_ok=True)
        self._path.write_text(json.dumps(self._data, indent=2, default=str))

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
        return {k: self._to_workspace(v) for k, v in self._data.items()}

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
