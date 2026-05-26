import json
from functools import lru_cache
from pathlib import Path

_CONFIG_PATH = Path(__file__).resolve().parent.parent.parent / "agents.json"


@lru_cache(maxsize=1)
def _load() -> dict:
    return json.loads(_CONFIG_PATH.read_text(encoding="utf-8"))


def _get(agent_type: str) -> dict:
    return _load()["agents"].get(agent_type, {})


def get_agent_config_dir(agent_type: str) -> str | None:
    return _get(agent_type).get("config_dir")


def get_agent_event_type(agent_type: str) -> str | None:
    return _get(agent_type).get("event_type")


def get_agent_cli_path(agent_type: str) -> str | None:
    return _get(agent_type).get("cli_path")
