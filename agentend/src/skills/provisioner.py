import hashlib
import logging
import shutil
import tempfile
import uuid
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
    """将内置 skill 供给到 agent 工作区。"""

    def provision(self, worktree_path: str, agent_type: AgentType) -> None:
        target = _skill_target_dir(worktree_path, agent_type)
        if target is None:
            return

        _ensure_safe_skill_target(Path(worktree_path), target)

        # builtin 目录和 manifest 清单均来自 config.yaml 的 skills 分区
        builtin_dir = settings.skills.builtin_dir_resolved
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

            missing_files = [fname for fname in spec.get("file", []) if not (skill_dir / fname).is_file()]
            if missing_files:
                missing = ", ".join(str(skill_dir / fname) for fname in missing_files)
                raise FileNotFoundError(
                    f"Builtin skill {skill_name} is missing manifest file(s): {missing}. "
                    "Run `make skills build` from the repository root before provisioning."
                )

            dest = target / skill_name
            if dest.is_dir() and not dest.is_symlink() and _skill_matches_manifest(dest, skill_dir, spec):
                logger.info("Skill %s already exists in repo, skipping", dest)
                continue

            _atomic_refresh_skill(dest, skill_dir, spec)
            provisioned.append(skill_name)
            logger.info("Provisioned/refreshed skill %s to %s", skill_name, dest)

        if provisioned:
            logger.info("Provisioned %d skills to %s", len(provisioned), target)

    def init_shared_dirs(self, worktrees_root: str, task_id: str, session_id: str) -> None:
        shared_base = Path(worktrees_root) / task_id / "shared" / ".agent" / "memory"
        common_dir = shared_base / "common"
        session_dir = shared_base / session_id

        common_dir.mkdir(parents=True, exist_ok=True)
        session_dir.mkdir(parents=True, exist_ok=True)
        logger.info("Initialized shared dirs: %s, %s", common_dir, session_dir)


def _skill_matches_manifest(dest: Path, source: Path, spec: dict) -> bool:
    """Compare only managed files/directories, avoiding partial refreshes."""
    for filename in spec.get("file", []):
        target_file = dest / filename
        source_file = source / filename
        # A managed symlink is never considered an up-to-date install. Even
        # if it happens to point at identical bytes today, keeping it would
        # let a later workspace mutation redirect skill execution elsewhere.
        if (
            _managed_path_has_symlink(target_file, dest)
            or not target_file.is_file()
            or _file_digest(target_file) != _file_digest(source_file)
        ):
            return False
    for dirname in spec.get("dir", []):
        target_dir = dest / dirname
        source_dir = source / dirname
        if not _managed_tree_matches(target_dir, source_dir):
            return False
    return True


def _ensure_safe_skill_target(worktree: Path, target: Path) -> None:
    """Create the managed skills path without following workspace symlinks."""
    if worktree.is_symlink():
        raise ValueError(f"workspace root is a symlink: {worktree}")

    # `Path.exists()` follows links. A worktree may therefore look like a
    # normal directory even when one of its already-existing ancestors is a
    # symlink (for example, a user-provided workspace root under a linked
    # mount). Check the complete lexical parent chain before creating or
    # entering any managed component.
    ancestor = worktree
    while True:
        if ancestor.is_symlink():
            raise ValueError(f"workspace root contains a symlink: {ancestor}")
        parent = ancestor.parent
        if parent == ancestor:
            break
        ancestor = parent

    try:
        relative = target.relative_to(worktree)
    except ValueError as exc:
        raise ValueError(f"skill target escapes workspace: {target}") from exc
    if any(part in {"", ".", ".."} for part in relative.parts):
        raise ValueError(f"skill target contains an unsafe path component: {target}")

    # The orchestrator's shared directory is lazily created on its first run.
    # Walk up to an existing ancestor first, rejecting symlinks/non-directories,
    # then create only the missing components. This keeps the first-run path
    # safe without requiring callers to mkdir the workspace before provisioning.
    missing: list[Path] = []
    current = worktree
    while not current.exists():
        missing.append(current)
        parent = current.parent
        if parent == current:
            break
        current = parent
    if current.is_symlink():
        raise ValueError(f"workspace root contains a symlink: {current}")
    if not current.exists() or not current.is_dir():
        raise FileNotFoundError(f"workspace root is not a directory: {worktree}")
    for directory in reversed(missing):
        parent = directory.parent
        if parent.is_symlink() or not parent.is_dir():
            raise ValueError(f"workspace root contains an unsafe parent: {parent}")
        directory.mkdir(exist_ok=True)
        if directory.is_symlink() or not directory.is_dir():
            raise ValueError(f"workspace root contains an unsafe path: {directory}")

    current = worktree
    for component in relative.parts:
        current = current / component
        if current.is_symlink():
            raise ValueError(f"skill target contains a symlink: {current}")
        if current.exists() and not current.is_dir():
            raise FileExistsError(f"skill target parent is not a directory: {current}")
        current.mkdir(exist_ok=True)


def _atomic_refresh_skill(dest: Path, source: Path, spec: dict) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=f".{dest.name}.install-", dir=str(dest.parent)))
    backup = dest.parent / f".{dest.name}.previous-{uuid.uuid4().hex}"
    try:
        # Preserve files outside the manifest. Only managed files are
        # replaced below, so a workspace can carry local notes or extensions.
        if dest.is_dir() and not dest.is_symlink():
            shutil.copytree(str(dest), str(staging), dirs_exist_ok=True, symlinks=True)
        for filename in spec.get("file", []):
            target_file = staging / filename
            _ensure_safe_parent(staging, target_file.parent)
            if target_file.is_symlink() or target_file.exists():
                _remove_path(target_file)
            shutil.copy2(str(source / filename), str(target_file))
        for dirname in spec.get("dir", []):
            source_dir = source / dirname
            if source_dir.is_dir():
                target_dir = staging / dirname
                _ensure_safe_parent(staging, target_dir.parent)
                if target_dir.is_symlink() or (target_dir.exists() and not target_dir.is_dir()):
                    _remove_path(target_dir)
                _merge_managed_tree(source_dir, target_dir)
        if dest.exists() or dest.is_symlink():
            dest.rename(backup)
        staging.rename(dest)
    except Exception:
        if (dest.exists() or dest.is_symlink()) and not staging.exists():
            _remove_path(dest)
        if (backup.exists() or backup.is_symlink()) and not (dest.exists() or dest.is_symlink()):
            backup.rename(dest)
        raise
    finally:
        if staging.exists() or staging.is_symlink():
            _remove_path(staging)
        if backup.exists() or backup.is_symlink():
            _remove_path(backup)


def _file_digest(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _managed_tree_matches(dest: Path, source: Path) -> bool:
    """Compare source-managed paths while ignoring extra workspace files."""
    if source.is_symlink() or not source.is_dir() or dest.is_symlink() or not dest.is_dir():
        return False
    for source_child in source.iterdir():
        target_child = dest / source_child.name
        if source_child.is_symlink():
            return False
        if source_child.is_dir():
            if not _managed_tree_matches(target_child, source_child):
                return False
            continue
        if not source_child.is_file() or target_child.is_symlink() or not target_child.is_file():
            return False
        if _file_digest(target_child) != _file_digest(source_child):
            return False
    return True


def _merge_managed_tree(source: Path, dest: Path) -> None:
    """Copy managed source entries without deleting unknown destination files."""
    if source.is_symlink() or not source.is_dir():
        raise ValueError(f"builtin managed directory is not a real directory: {source}")
    if dest.is_symlink():
        _remove_path(dest)
    elif dest.exists() and not dest.is_dir():
        _remove_path(dest)
    dest.mkdir(parents=True, exist_ok=True)
    for source_child in source.iterdir():
        target_child = dest / source_child.name
        if source_child.is_symlink():
            raise ValueError(f"builtin managed directory contains a symlink: {source_child}")
        if source_child.is_dir():
            _merge_managed_tree(source_child, target_child)
            continue
        if not source_child.is_file():
            raise ValueError(f"builtin managed directory contains unsupported entry: {source_child}")
        _ensure_safe_parent(dest, target_child.parent)
        if target_child.is_symlink() or target_child.is_dir():
            _remove_path(target_child)
        shutil.copy2(str(source_child), str(target_child))


def _managed_path_has_symlink(path: Path, root: Path) -> bool:
    try:
        path.relative_to(root)
    except ValueError:
        return True
    current = path
    while current != root:
        if current.is_symlink():
            return True
        current = current.parent
    return root.is_symlink()


def _remove_path(path: Path) -> None:
    if path.is_symlink() or path.is_file():
        path.unlink(missing_ok=True)
    elif path.exists():
        shutil.rmtree(path, ignore_errors=True)


def _ensure_safe_parent(root: Path, parent: Path) -> None:
    """Create staging parents without ever following an existing symlink."""
    try:
        relative = parent.relative_to(root)
    except ValueError as exc:
        raise ValueError(f"managed path escapes staging root: {parent}") from exc
    if any(part in {"", ".", ".."} for part in relative.parts):
        raise ValueError(f"managed path contains an unsafe component: {parent}")
    current = root
    for component in relative.parts:
        current = current / component
        if current.is_symlink():
            current.unlink()
        elif current.exists() and not current.is_dir():
            raise FileExistsError(f"managed parent is not a directory: {current}")
        current.mkdir(exist_ok=True)
