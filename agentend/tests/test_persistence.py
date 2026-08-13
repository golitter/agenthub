import asyncio
import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.schemas.request import AgentType
from src.session.store import SessionMappingStore
from src.workspace.models import Workspace
from src.workspace.store import JsonFileWorkspaceStore


def test_json_stores_recover_from_valid_non_object_json(tmp_path: Path):
    session_path = tmp_path / "sessions.json"
    workspace_path = tmp_path / "workspaces.json"
    session_path.write_text("[]")
    workspace_path.write_text("[]")

    sessions = SessionMappingStore(session_path)
    workspaces = JsonFileWorkspaceStore(workspace_path)

    assert sessions.get_cli_session_id("missing") is None
    assert asyncio.run(workspaces.load_all()) == {}


@pytest.mark.asyncio
async def test_session_mapping_concurrent_updates_remain_complete(tmp_path: Path):
    path = tmp_path / "sessions.json"
    store = SessionMappingStore(path)

    await asyncio.gather(
        *(store.set_cli_session_id(f"session-{index}", f"cli-{index}", "task-1") for index in range(20))
    )

    reloaded = SessionMappingStore(path)
    for index in range(20):
        assert reloaded.get_cli_session_id(f"session-{index}", "task-1") == f"cli-{index}"
    assert not list(tmp_path.glob(".sessions.json.*"))


@pytest.mark.asyncio
async def test_workspace_store_skips_only_invalid_record(tmp_path: Path):
    path = tmp_path / "workspaces.json"
    store = JsonFileWorkspaceStore(path)
    workspace = Workspace(
        task_id="task-1",
        session_id="session-1",
        agent_name="codex",
        agent_type=AgentType.CODEX,
        repo_path=str(tmp_path / "repo"),
    )
    await store.save(workspace)

    raw = json.loads(path.read_text())
    raw["invalid"] = {"status": "not-a-status"}
    path.write_text(json.dumps(raw))
    reloaded = JsonFileWorkspaceStore(path)

    loaded = await reloaded.load_all()
    assert list(loaded) == [workspace.id]
