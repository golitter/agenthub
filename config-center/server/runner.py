from __future__ import annotations

import subprocess
from pathlib import Path
from typing import Any

from server.config_io import ConfigError


class ApplyService:
    def __init__(self, project_root: Path):
        self.project_root = project_root.resolve()

    def apply(self, profile: str) -> dict[str, Any]:
        if profile == "local":
            commands = [("make", "restart")]
        elif profile == "docker":
            commands = [("make", "docker-up"), ("make", "restart-agentend")]
        else:
            raise ConfigError(f"unknown profile: {profile}")

        output: list[str] = []
        executed: list[list[str]] = []
        for command in commands:
            try:
                completed = subprocess.run(
                    command,
                    cwd=self.project_root,
                    capture_output=True,
                    text=True,
                    timeout=600,
                    check=False,
                )
            except subprocess.TimeoutExpired as exc:
                raise ConfigError(f"运行超时：{' '.join(command)}") from exc
            except OSError as exc:
                raise ConfigError(f"无法执行：{' '.join(command)}") from exc
            executed.append(list(command))
            combined = "\n".join(part.strip() for part in (completed.stdout, completed.stderr) if part.strip())
            if combined:
                output.append(f"$ {' '.join(command)}\n{combined}")
            if completed.returncode != 0:
                return {
                    "ok": False,
                    "profile": profile,
                    "commands": executed,
                    "exitCode": completed.returncode,
                    "output": "\n\n".join(output)[-65536:],
                }
        return {
            "ok": True,
            "profile": profile,
            "commands": executed,
            "exitCode": 0,
            "output": "\n\n".join(output)[-65536:],
        }
