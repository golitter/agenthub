import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.app.config import BackendConfig


def test_backend_config_keeps_yaml_value_without_override(monkeypatch) -> None:
    monkeypatch.delenv("AGENTHUB_BACKEND_URL", raising=False)

    config = BackendConfig(url="http://localhost:8080")

    assert config.url == "http://localhost:8080"


def test_backend_config_allows_local_environment_override(monkeypatch) -> None:
    monkeypatch.setenv("AGENTHUB_BACKEND_URL", "http://127.0.0.1:28080")

    config = BackendConfig(url="http://localhost:8080")

    assert config.url == "http://127.0.0.1:28080"


def test_backend_config_ignores_empty_environment_override(monkeypatch) -> None:
    monkeypatch.setenv("AGENTHUB_BACKEND_URL", "")

    config = BackendConfig(url="http://localhost:8080")

    assert config.url == "http://localhost:8080"
