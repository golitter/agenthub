from __future__ import annotations

from pathlib import Path

from server.config_io import ConfigService
from server.profiles import ProfileService


def test_profiles_are_a_catalog_of_discovered_template_pairs(project_root: Path) -> None:
    service = ProfileService(project_root, ConfigService(project_root, project_root / ".test-backups"))

    profiles = {profile["id"]: profile for profile in service.profiles()}

    assert profiles["local"]["available"] is True
    assert profiles["local"]["fileCount"] == 5
    assert profiles["docker"]["available"] is True
    assert profiles["docker"]["fileCount"] == 5
    assert all("serviceCount" not in profile for profile in profiles.values())


def test_profile_is_unavailable_without_an_example_file(project_root: Path) -> None:
    (project_root / "docker/configs/backend/.env.example").unlink()
    (project_root / "docker/configs/backend/config.example.yaml").unlink()
    (project_root / "agentend/.env.example").unlink()
    (project_root / "agentend/config.example.yaml").unlink()
    (project_root / "agentend/agents.example.json").unlink()
    service = ProfileService(project_root, ConfigService(project_root, project_root / ".test-backups"))

    docker = next(profile for profile in service.profiles() if profile["id"] == "docker")

    assert docker["available"] is False
    assert docker["fileCount"] == 0
    assert docker["missing"] == ["未发现 example 配置文件"]
