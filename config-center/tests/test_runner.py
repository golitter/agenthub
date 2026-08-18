from __future__ import annotations

import subprocess
from pathlib import Path

import pytest

from server.config_io import ConfigError
from server.runner import ApplyService


def test_local_apply_uses_existing_make_restart(project_root: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    commands: list[tuple[str, ...]] = []

    def run(command: tuple[str, ...], **_kwargs: object) -> subprocess.CompletedProcess[str]:
        commands.append(command)
        return subprocess.CompletedProcess(command, 0, "services restarted", "")

    monkeypatch.setattr("server.runner.subprocess.run", run)

    result = ApplyService(project_root).apply("local")

    assert result["ok"] is True
    assert commands == [("make", "restart")]


def test_docker_apply_runs_deployment_then_restarts_agentend(project_root: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    commands: list[tuple[str, ...]] = []

    def run(command: tuple[str, ...], **_kwargs: object) -> subprocess.CompletedProcess[str]:
        commands.append(command)
        return subprocess.CompletedProcess(command, 0, "ok", "")

    monkeypatch.setattr("server.runner.subprocess.run", run)

    result = ApplyService(project_root).apply("docker")

    assert result["ok"] is True
    assert commands == [("make", "docker", "up"), ("make", "restart-agentend")]


def test_apply_stops_after_failed_command(project_root: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        "server.runner.subprocess.run",
        lambda command, **_kwargs: subprocess.CompletedProcess(command, 7, "", "failed"),
    )

    result = ApplyService(project_root).apply("docker")

    assert result["ok"] is False
    assert result["exitCode"] == 7
    assert result["commands"] == [["make", "docker", "up"]]


def test_apply_rejects_unknown_profile(project_root: Path) -> None:
    with pytest.raises(ConfigError):
        ApplyService(project_root).apply("remote")
