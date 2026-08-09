from __future__ import annotations

import shutil
from pathlib import Path

import pytest


@pytest.fixture
def project_root(tmp_path: Path) -> Path:
    repository = Path(__file__).resolve().parents[2]
    tracked = [
        "backend/configs/config.yaml",
        "backend/configs/config.example.yaml",
        "backend/.env.example",
        "agentend/config.yaml",
        "agentend/config.example.yaml",
        "agentend/agents.json",
        "agentend/agents.example.json",
        "agentend/.env.example",
        "docker/configs/backend/config.yaml",
        "docker/configs/backend/config.example.yaml",
        "docker/configs/backend/.env.example",
        "docker/docker-compose.yml",
        "scripts/run.sh",
    ]
    for relative in tracked:
        source = repository / relative
        destination = tmp_path / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, destination)
    shutil.copy2(tmp_path / "backend/.env.example", tmp_path / "backend/.env")
    shutil.copy2(tmp_path / "agentend/.env.example", tmp_path / "agentend/.env")
    shutil.copy2(tmp_path / "docker/configs/backend/.env.example", tmp_path / "docker/configs/backend/.env")
    return tmp_path
