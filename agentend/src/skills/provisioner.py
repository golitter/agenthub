import logging
import shutil
from pathlib import Path

import yaml

logger = logging.getLogger(__name__)

# Map agent_name to its config directory name
_AGENT_SKILL_DIRS: dict[str, str] = {
    "claude-code": ".claude",
    "opencode": ".opencode",
}

_BUILTIN_SKILLS_DIR = Path(__file__).parent / "builtin"
_MANIFEST_PATH = _BUILTIN_SKILLS_DIR / "manifest.yaml"


def _load_manifest() -> dict:
    if not _MANIFEST_PATH.is_file():
        return {}
    with _MANIFEST_PATH.open() as f:
        return yaml.safe_load(f) or {}


def _skill_target_dir(worktree_path: str, agent_name: str) -> Path | None:
    config_dir = _AGENT_SKILL_DIRS.get(agent_name)
    if not config_dir:
        logger.warning("Unknown agent_name %s, skipping skill provisioning", agent_name)
        return None
    return Path(worktree_path) / config_dir / "skills"


class SkillProvisioner:
    """Provisions builtin skills into agent workspaces."""

    def provision(self, worktree_path: str, task_id: str, agent_name: str) -> None:
        target = _skill_target_dir(worktree_path, agent_name)
        if target is None:
            return

        builtin_dir = _BUILTIN_SKILLS_DIR
        if not builtin_dir.is_dir():
            logger.warning("Builtin skills directory not found: %s", builtin_dir)
            return

        manifest = _load_manifest()
        provisioned: list[str] = []
        for skill_name, spec in manifest.items():
            skill_dir = builtin_dir / skill_name
            if not skill_dir.is_dir():
                logger.warning("Manifest skill %s not found in %s", skill_name, builtin_dir)
                continue
            dest = target / skill_name
            if dest.exists():
                logger.info("Skill %s already exists in repo, skipping", dest)
                continue
            dest.mkdir(parents=True, exist_ok=True)

            for fname in spec.get("file", []):
                src_file = skill_dir / fname
                if src_file.is_file():
                    shutil.copy2(str(src_file), str(dest / fname))

            for dname in spec.get("dir", []):
                src_dir = skill_dir / dname
                if src_dir.is_dir():
                    shutil.copytree(str(src_dir), str(dest / dname))

            provisioned.append(skill_name)
            logger.info("Provisioned skill %s to %s", skill_name, dest)

        if provisioned:
            self._write_git_exclude(worktree_path, agent_name, provisioned)

    def _write_git_exclude(self, worktree_path: str, agent_name: str, skill_names: list[str]) -> None:
        """Add provisioned skill paths to .git/info/exclude so they're never committed.

        Only excludes the specific skill subdirectories we provisioned, not the
        entire config directory — the repo may already have its own tracked content
        under .claude/ or .opencode/.
        """
        config_dir = _AGENT_SKILL_DIRS.get(agent_name)
        if not config_dir:
            return
        exclude_file = Path(worktree_path) / ".git" / "info" / "exclude"
        if not exclude_file.parent.is_dir():
            logger.warning("Cannot write git exclude: %s not found", exclude_file.parent)
            return
        existing = exclude_file.read_text() if exclude_file.exists() else ""
        lines = set(existing.splitlines())
        entries = [f"/{config_dir}/skills/{name}" for name in skill_names]
        new_entries = [e for e in entries if e not in lines]
        if new_entries:
            with exclude_file.open("a") as f:
                if existing and not existing.endswith("\n"):
                    f.write("\n")
                f.write("\n".join(new_entries) + "\n")
            logger.info("Added %s to %s", new_entries, exclude_file)

    def init_shared_dirs(self, worktrees_root: str, task_id: str, agent_name: str) -> None:
        shared_base = Path(worktrees_root) / task_id / "shared" / ".agent" / "memory"
        common_dir = shared_base / "common"
        agent_dir = shared_base / agent_name

        common_dir.mkdir(parents=True, exist_ok=True)
        agent_dir.mkdir(parents=True, exist_ok=True)
        logger.info("Initialized shared dirs: %s, %s", common_dir, agent_dir)
