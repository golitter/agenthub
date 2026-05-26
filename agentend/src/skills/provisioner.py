import logging
import shutil
from pathlib import Path

from src.app.agent_config import get_agent_config_dir
from src.app.config import settings
from src.schemas.request import AgentType

logger = logging.getLogger(__name__)


def _skill_target_dir(worktree_path: str, agent_type: AgentType) -> Path | None:
    config_dir = get_agent_config_dir(agent_type)
    if not config_dir:
        logger.warning("Unknown agent_type %s, skipping skill provisioning", agent_type)
        return None
    return Path(worktree_path) / config_dir / "skills"


class SkillProvisioner:
    """Provisions builtin skills into agent workspaces."""

    def provision(self, worktree_path: str, agent_type: AgentType) -> None:
        target = _skill_target_dir(worktree_path, agent_type)
        if target is None:
            return

        # builtin 目录和 manifest 清单均来自 config.yaml 的 skills 分区
        builtin_dir = Path(settings.skills.builtin_dir)
        if not builtin_dir.is_dir():
            logger.warning("Builtin skills directory not found: %s", builtin_dir)
            return

        manifest = settings.skills.manifest
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
            logger.info("Provisioned %d skills to %s", len(provisioned), target)

    def init_shared_dirs(self, worktrees_root: str, task_id: str, session_id: str) -> None:
        shared_base = Path(worktrees_root) / task_id / "shared" / ".agent" / "memory"
        common_dir = shared_base / "common"
        session_dir = shared_base / session_id

        common_dir.mkdir(parents=True, exist_ok=True)
        session_dir.mkdir(parents=True, exist_ok=True)
        logger.info("Initialized shared dirs: %s, %s", common_dir, session_dir)
