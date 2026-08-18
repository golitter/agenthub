import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.app.agent_config import get_agent_cli_path, get_agent_config_path


def test_cli_path_uses_environment_override(monkeypatch):
    monkeypatch.setenv("PI_CLI_PATH", "/tmp/pi")

    assert get_agent_cli_path("pi") == "/tmp/pi"


def test_cli_path_falls_back_to_manifest(monkeypatch):
    monkeypatch.delenv("PI_CLI_PATH", raising=False)

    assert get_agent_cli_path("pi") == "pi"


def test_config_path_uses_environment_override(monkeypatch):
    monkeypatch.setenv("PI_CONFIG_PATH", "/tmp/pi/settings.json")

    assert get_agent_config_path("pi") == "/tmp/pi/settings.json"
