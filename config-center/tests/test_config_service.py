from __future__ import annotations

from pathlib import Path

import pytest

from server.config_io import ConfigConflict, ConfigError, ConfigService, ValidationError


def _file(config: dict, file_id: str) -> dict:
    return next(item for item in config["files"] if item["id"] == file_id)


def test_discovers_complete_example_actual_pairs(project_root: Path) -> None:
    service = ConfigService(project_root, project_root / ".test-backups")

    local = service.get_config("local")
    docker = service.get_config("docker")

    assert {item["path"] for item in local["files"]} == {
        "backend/.env", "backend/configs/config.yaml", "agentend/.env", "agentend/config.yaml", "agentend/agents.json"
    }
    assert {item["path"] for item in docker["files"]} == {
        "docker/configs/backend/.env", "docker/configs/backend/config.yaml", "agentend/.env", "agentend/config.yaml", "agentend/agents.json"
    }
    assert {item["kind"] for item in local["files"]} == {"env", "yaml", "json"}
    assert all("exampleContent" in item and "actualContent" in item for item in local["files"])


def test_example_changes_appear_without_schema_change(project_root: Path) -> None:
    example = project_root / "agentend/config.example.yaml"
    example.write_text(example.read_text(encoding="utf-8") + "\nfeature_flags:\n  streaming: true\n", encoding="utf-8")
    service = ConfigService(project_root, project_root / ".test-backups")

    config = _file(service.get_config("local"), "agentend_config_yaml")

    assert "feature_flags:" in config["exampleContent"]
    assert "feature_flags:" not in config["actualContent"]


def test_actual_file_content_includes_sensitive_values_for_direct_editing(project_root: Path) -> None:
    service = ConfigService(project_root, project_root / ".test-backups")
    backend = _file(service.get_config("local"), "backend_configs_config_yaml")

    assert "agenthub-demo-secret" in backend["actualContent"]
    assert "agenthub-demo-secret" not in backend["exampleContent"]


def test_save_writes_the_complete_actual_text_exactly(project_root: Path) -> None:
    service = ConfigService(project_root, project_root / ".test-backups")
    backend = _file(service.get_config("local"), "backend_configs_config_yaml")
    content = backend["actualContent"].replace("port: 8080", "port: 8181", 1)

    validation = service.validate(
        "local", {"files": {backend["id"]: {"revision": backend["revision"], "content": content}}}
    )
    saved = service.save(
        "local", {"files": {backend["id"]: {"revision": backend["revision"], "content": content}}}
    )

    assert validation["ok"] is True
    assert saved["saved"] == [backend["id"]]
    assert len(saved["backups"]) == 1
    assert (project_root / backend["path"]).read_text(encoding="utf-8") == content


@pytest.mark.parametrize(
    ("file_id", "invalid"),
    [
        ("agentend_config_yaml", "server: [\n"),
        ("agentend_agents_json", '{"agents":'),
    ],
)
def test_invalid_structured_file_is_rejected(project_root: Path, file_id: str, invalid: str) -> None:
    service = ConfigService(project_root, project_root / ".test-backups")
    item = _file(service.get_config("local"), file_id)
    payload = {"files": {file_id: {"revision": item["revision"], "content": invalid}}}

    validation = service.validate("local", payload)

    assert validation["ok"] is False
    assert validation["issues"][0]["code"] == "invalid_syntax"
    with pytest.raises(ValidationError):
        service.save("local", payload)


def test_missing_actual_file_can_be_created_from_example(project_root: Path) -> None:
    actual = project_root / "docker/configs/backend/config.yaml"
    actual.unlink()
    service = ConfigService(project_root, project_root / ".test-backups")
    item = _file(service.get_config("docker"), "docker_configs_backend_config_yaml")

    saved = service.save(
        "docker",
        {"files": {item["id"]: {"revision": "missing", "content": item["exampleContent"]}}},
    )

    assert saved["saved"] == [item["id"]]
    assert actual.read_text(encoding="utf-8") == item["exampleContent"]


def test_unknown_file_is_rejected(project_root: Path) -> None:
    service = ConfigService(project_root, project_root / ".test-backups")
    payload = {"files": {"../../outside": {"revision": "missing", "content": "x=1\n"}}}

    assert service.validate("local", payload)["issues"][0]["code"] == "unknown_file"
    with pytest.raises(ValidationError):
        service.save("local", payload)


def test_revision_conflict_preserves_external_edit(project_root: Path) -> None:
    service = ConfigService(project_root, project_root / ".test-backups")
    backend = _file(service.get_config("local"), "backend_env")
    path = project_root / "backend/.env"
    path.write_text(path.read_text(encoding="utf-8") + "\n# external\n", encoding="utf-8")

    with pytest.raises(ConfigConflict):
        service.save(
            "local",
            {"files": {backend["id"]: {"revision": backend["revision"], "content": backend["actualContent"] + "\nX=1\n"}}},
        )
    assert "# external" in path.read_text(encoding="utf-8")


def test_failure_after_replace_rolls_back(project_root: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    service = ConfigService(project_root, project_root / ".test-backups")
    backend = _file(service.get_config("local"), "backend_env")
    path = project_root / "backend/.env"
    before = path.read_bytes()
    real_atomic_write = service._atomic_write
    failed = False

    def fail_once(target: Path, content: bytes) -> None:
        nonlocal failed
        real_atomic_write(target, content)
        if target == path and not failed:
            failed = True
            raise OSError("injected failure after replace")

    monkeypatch.setattr(service, "_atomic_write", fail_once)
    with pytest.raises(OSError, match="after replace"):
        service.save(
            "local",
            {"files": {backend["id"]: {"revision": backend["revision"], "content": backend["actualContent"] + "\nNEW=1\n"}}},
        )
    assert path.read_bytes() == before


def test_backup_restore_round_trip(project_root: Path) -> None:
    service = ConfigService(project_root, project_root / ".test-backups")
    backend = _file(service.get_config("local"), "backend_env")
    changed = backend["actualContent"] + "\nNEW_VALUE=one\n"
    saved = service.save(
        "local", {"files": {backend["id"]: {"revision": backend["revision"], "content": changed}}}
    )
    current = _file(service.get_config("local"), backend["id"])

    service.restore("local", backend["id"], saved["backups"][0], current["revision"])

    assert "NEW_VALUE=one" not in (project_root / "backend/.env").read_text(encoding="utf-8")


def test_backup_root_cannot_escape_project(project_root: Path, tmp_path_factory: pytest.TempPathFactory) -> None:
    outside = tmp_path_factory.mktemp("outside-backups")
    with pytest.raises(ConfigError, match="inside the project root"):
        ConfigService(project_root, outside)
