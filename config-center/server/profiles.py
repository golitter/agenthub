from __future__ import annotations

from pathlib import Path
from typing import Any

from server.config_io import ConfigService


class ProfileService:
    """Expose the two template roots; runtime orchestration lives outside this tool."""

    def __init__(self, project_root: Path, config: ConfigService):
        self.project_root = project_root.resolve()
        self.config = config

    def profiles(self) -> list[dict[str, Any]]:
        definitions = (
            ("local", "本地配置", "backend 与 agentend 的 example/actual 配置"),
            ("docker", "Docker 配置", "docker/configs 与 agentend 的 example/actual 配置"),
        )
        profiles: list[dict[str, Any]] = []
        for profile_id, title, description in definitions:
            file_count = len(self.config.file_specs(profile_id))
            profiles.append(
                {
                    "id": profile_id,
                    "title": title,
                    "description": description,
                    "available": file_count > 0,
                    "missing": [] if file_count else ["未发现 example 配置文件"],
                    "fileCount": file_count,
                }
            )
        return profiles
