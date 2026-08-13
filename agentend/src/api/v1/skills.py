"""Skills 扫描端点 — 扫描 agent workspace 的 skills 目录。"""

import logging
import re
import shutil
import stat
import tempfile
import time
import uuid
import zipfile
import zlib
from io import BytesIO
from pathlib import Path

import yaml
from fastapi import APIRouter, Query, Request

from src.app.config import settings

logger = logging.getLogger(__name__)

router = APIRouter(prefix="/v1/skills", tags=["skills"])

# YAML frontmatter boundary; safe_load prevents arbitrary Python constructors.
_FM_RE = re.compile(r"^---[ \t]*\r?\n(.*?)\r?\n---[ \t]*(?:\r?\n|$)", re.DOTALL)

_AGENT_CONFIG_DIRS = {
    "claude-code": ".claude",
    "opencode": ".opencode",
    "codex": ".codex",
    "orchestrator": ".orchestrator",
}
_EXTERNAL_SKILL_AGENT_TYPES = frozenset({"claude-code", "opencode", "codex"})

MAX_SKILL_PACKAGE_BYTES = 12 * 1024 * 1024
MAX_SKILL_UNPACKED_BYTES = 50 * 1024 * 1024
MAX_SKILL_FILE_BYTES = 10 * 1024 * 1024
MAX_SKILL_COMPRESSION_RATIO = 100
MAX_SKILL_FILE_COUNT = 200
MAX_SKILL_NAME_LENGTH = 128
STALE_STAGING_SECONDS = 24 * 60 * 60


async def _read_limited_body(request: Request, maximum: int) -> bytes | None:
    """Read at most ``maximum`` bytes so oversized requests never accumulate."""
    data = bytearray()
    async for chunk in request.stream():
        if len(data) + len(chunk) > maximum:
            return None
        data.extend(chunk)
    return bytes(data)


def _parse_skill_md(skill_md_path: Path) -> dict | None:
    """解析 SKILL.md 的 YAML frontmatter，返回 {name, description} 或 None。"""
    try:
        text = skill_md_path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError):
        return None

    m = _FM_RE.match(text)
    if not m:
        return None

    try:
        values = yaml.safe_load(m.group(1))
    except yaml.YAMLError:
        return None
    if not isinstance(values, dict) or not isinstance(values.get("name"), str):
        return None
    description = values.get("description", "")
    if description is None:
        description = ""
    if not isinstance(description, str):
        return None

    name = values["name"].strip()
    if _validate_skill_name(name) is not None:
        return None
    description = description.strip()
    if len(description) > 4096:
        return None

    return {"name": name, "description": description}


def _find_skill_md(root: Path) -> Path | None:
    """Find SKILL.md at the package root or under its single Skill directory.

    Backend canonical ZIPs may use either layout.  The latter is deliberately
    restricted to one top-level directory with no sibling files, matching the
    backend validator's root-directory rule and avoiding ambiguous installs.
    """
    root_skill_md = root / "SKILL.md"
    if root_skill_md.is_file() and not root_skill_md.is_symlink():
        return root_skill_md

    try:
        entries = list(root.iterdir())
    except OSError:
        return None
    dirs = [entry for entry in entries if entry.is_dir() and not entry.is_symlink()]
    files = [entry for entry in entries if not entry.is_dir()]
    if len(dirs) != 1 or files:
        return None
    nested = dirs[0] / "SKILL.md"
    if nested.is_file() and not nested.is_symlink():
        return nested
    return None


def _scan_skills_dir(skills_dir: Path) -> list[dict]:
    """扫描 skills 目录并返回 skill 列表。"""
    builtin_names = set(settings.skills.manifest.keys())
    skills: list[dict] = []
    for entry in sorted(skills_dir.iterdir()):
        if entry.is_symlink() or not entry.is_dir():
            continue
        skill_md = entry / "SKILL.md"
        if skill_md.is_symlink() or not skill_md.is_file():
            continue
        parsed = _parse_skill_md(skill_md)
        if not parsed:
            continue
        is_builtin = parsed["name"] in builtin_names
        skills.append(
            {
                "name": parsed["name"],
                "description": parsed["description"],
                "builtin": is_builtin,
                "source": "builtin" if is_builtin else "hub",
            }
        )
    return skills


def _resolve_skills_dir(request: Request, agent_type: str, session_id: str) -> Path | None:
    """根据给定的 session 与 agent_type 解析对应的 skills 目录。"""
    resolved = ""
    if session_id:
        ws_mgr = request.app.state.workspace_manager
        ws = ws_mgr.get_by_session(session_id)
        if ws:
            resolved = ws.worktree_path
    if not resolved:
        return None

    config_dir = _AGENT_CONFIG_DIRS.get(agent_type)
    if config_dir is None:
        return None
    return Path(resolved) / config_dir / "skills"


def _cleanup_stale_skill_staging(skills_dir: Path, *, recover_immediately: bool = False) -> None:
    """Recover or remove old atomic-install artifacts left by a crash.

    If a process stopped after moving the previous installation aside but
    before the new directory was renamed into place, restoring the backup is
    safer than deleting it: the old complete installation remains available
    until the next install attempt converges.  A backup is only discarded
    once the destination is known to exist.
    """
    cutoff = time.time() - STALE_STAGING_SECONDS
    try:
        entries = list(skills_dir.iterdir())
    except OSError:
        return
    for entry in entries:
        name = entry.name
        if not (name.startswith(".") and (".install-" in name or ".previous-" in name or ".remove-" in name)):
            continue
        try:
            info = entry.lstat()
            if stat.S_ISLNK(info.st_mode) or (not recover_immediately and info.st_mtime > cutoff):
                continue
            if ".previous-" in name:
                # The Skill name itself may contain ".previous-".  The
                # generated suffix is the final marker, so parse from the
                # right; splitting at the first marker could discard the
                # previous complete installation during startup recovery.
                skill_name = name[1:].rsplit(".previous-", 1)[0]
                if _validate_skill_name(skill_name) is None:
                    destination = skills_dir / skill_name
                    if destination.is_symlink():
                        logger.warning("leaving stale Skill backup beside symlink destination %s", destination)
                        continue
                    if not destination.exists():
                        try:
                            entry.rename(destination)
                            continue
                        except OSError:
                            logger.warning("failed to restore stale Skill backup %s", entry, exc_info=True)
                    elif destination.is_dir() or destination.is_file():
                        # A completed retry now owns the destination; the
                        # backup is no longer needed and can be discarded.
                        pass
            if stat.S_ISDIR(info.st_mode):
                shutil.rmtree(entry, ignore_errors=True)
            else:
                entry.unlink(missing_ok=True)
        except OSError:
            logger.warning("failed to clean stale Skill staging artifact %s", entry, exc_info=True)


def cleanup_stale_skill_staging_for_workspace(workspace_path: str, agent_type: str) -> None:
    """Clean crash leftovers for one trusted, already-resolved workspace.

    The startup/periodic caller obtains workspace paths from the persisted
    workspace manager rather than from request query parameters.  Refuse any
    symlinked path before iterating so cleanup cannot follow an operator- or
    package-created redirect outside the worktree.
    """
    config_dir = _AGENT_CONFIG_DIRS.get(agent_type)
    if not config_dir or not workspace_path:
        return
    skills_dir = Path(workspace_path) / config_dir / "skills"
    if _skill_directory_has_symlink(skills_dir) or not skills_dir.is_dir():
        return
    _cleanup_stale_skill_staging(skills_dir)


def recover_skill_staging_for_workspace(workspace_path: str, agent_type: str) -> None:
    """Immediately recover atomic-install artifacts during AgentEnd startup.

    A crash can leave a recent ``.previous-*`` backup while the destination
    directory is temporarily absent.  The periodic age-based cleanup must not
    touch a live install, but startup runs before this process serves requests,
    so it can safely restore that complete backup immediately.
    """
    config_dir = _AGENT_CONFIG_DIRS.get(agent_type)
    if not config_dir or not workspace_path:
        return
    skills_dir = Path(workspace_path) / config_dir / "skills"
    if _skill_directory_has_symlink(skills_dir) or not skills_dir.is_dir():
        return
    _cleanup_stale_skill_staging(skills_dir, recover_immediately=True)


def _skill_directory_has_symlink(path: Path) -> bool:
    """Reject a skills root redirected outside the workspace by a symlink."""
    current = path
    # Check skills/, the agent config directory, and the worktree itself.  Do
    # not resolve the path: resolution would hide the link we are trying to
    # reject.
    for _ in range(3):
        try:
            if current.is_symlink():
                return True
        except OSError:
            return True
        current = current.parent
    return False


def _validate_skill_name(skill_name: str) -> str | None:
    """验证路由中的技能名，避免把路径参数变成任意文件系统路径。"""
    if not skill_name or not skill_name.strip() or skill_name in {".", ".."}:
        return "skill name is required"
    if len(skill_name) > MAX_SKILL_NAME_LENGTH:
        return "skill name is too long"
    if "/" in skill_name or "\\" in skill_name or any(ord(ch) < 32 or ord(ch) == 127 for ch in skill_name):
        return "skill name is invalid"
    return None


def _safe_zip_entry_name(name: str) -> tuple[str | None, str | None]:
    """返回规范化的 POSIX zip 路径；拒绝绝对路径、穿越和 Windows 分隔符。"""
    if (
        not name
        or "\\" in name
        or name.startswith("/")
        or name != name.strip()
        or any(ord(ch) < 32 or ord(ch) == 127 for ch in name)
    ):
        return None, "invalid zip entry path"
    # Keep the archive spelling canonical instead of letting PurePosixPath
    # silently collapse aliases such as ``foo//bar`` or ``foo/../bar``.
    # Exactly one trailing slash is allowed for an explicit directory entry;
    # it is preserved in the returned value so a file and directory cannot
    # be treated as unrelated entries during collision checks.
    trailing_slashes = len(name) - len(name.rstrip("/"))
    if trailing_slashes > 1:
        return None, "non-canonical zip entry path"
    has_trailing_slash = trailing_slashes == 1
    raw = name[:-1] if has_trailing_slash else name
    if not raw:
        return None, "empty zip entry path"
    parts = raw.split("/")
    if not parts or any(part in {"", ".", ".."} for part in parts) or ":" in parts[0]:
        return None, "invalid zip entry path"
    normalized = "/".join(parts)
    if has_trailing_slash:
        normalized += "/"
    if normalized != name:
        return None, "non-canonical zip entry path"
    return normalized, None


def _extract_skill_zip(content: bytes, target: Path) -> str | None:
    """在隔离临时目录中解压，并同时限制路径、文件数和展开大小。"""
    seen: set[str] = set()
    total_size = 0
    file_count = 0
    try:
        with zipfile.ZipFile(BytesIO(content), "r") as zf:
            for info in zf.infolist():
                clean_name, path_error = _safe_zip_entry_name(info.filename)
                if path_error:
                    return path_error
                assert clean_name is not None
                collision_name = clean_name.rstrip("/").casefold()
                if collision_name in seen:
                    return f"duplicate zip entry: {clean_name}"
                seen.add(collision_name)

                raw_mode = (info.external_attr >> 16) & 0o777777
                mode = raw_mode & 0o170000
                if mode and not stat.S_ISREG(mode) and not stat.S_ISDIR(mode):
                    return "special file types are not allowed"
                if info.flag_bits & 0x1:
                    return "encrypted zip entries are not allowed"

                dest = target / clean_name
                if info.is_dir():
                    dest.mkdir(parents=True, exist_ok=True)
                    continue

                file_count += 1
                if file_count > MAX_SKILL_FILE_COUNT:
                    return f"too many files: exceeds {MAX_SKILL_FILE_COUNT}"
                if info.file_size > MAX_SKILL_FILE_BYTES:
                    return "skill file exceeds size limit"
                if info.file_size and (
                    not info.compress_size
                    or info.file_size > info.compress_size * MAX_SKILL_COMPRESSION_RATIO
                ):
                    return f"compression ratio exceeds {MAX_SKILL_COMPRESSION_RATIO}:1"
                if info.file_size > MAX_SKILL_UNPACKED_BYTES - total_size:
                    return "unpacked skill package exceeds size limit"
                dest.parent.mkdir(parents=True, exist_ok=True)
                written = 0
                with zf.open(info, "r") as source, dest.open("xb") as output:
                    while True:
                        chunk = source.read(min(1024 * 1024, MAX_SKILL_UNPACKED_BYTES - total_size - written + 1))
                        if not chunk:
                            break
                        written += len(chunk)
                        if total_size + written > MAX_SKILL_UNPACKED_BYTES:
                            return "unpacked skill package exceeds size limit"
                        output.write(chunk)
                # Canonical Backend packages carry only an explicit execute
                # bit.  Reapply that bit while normalizing all other host
                # permissions, so scripts remain usable without trusting
                # owner/group/world-writable ZIP metadata.
                dest.chmod(0o755 if raw_mode & 0o111 else 0o644)
                total_size += written
    except (OSError, RuntimeError, ValueError, KeyError, EOFError, zlib.error, zipfile.BadZipFile, zipfile.LargeZipFile) as exc:
        return f"invalid skill zip: {exc}"

    skill_md = _find_skill_md(target)
    if skill_md is None:
        return "missing SKILL.md"
    metadata = _parse_skill_md(skill_md)
    if not metadata or metadata["name"] == "":
        return "invalid SKILL.md frontmatter"
    # Mirror the Backend's one-layout rule so a package accepted by a
    # compromised or older Backend cannot install an ambiguous tree.
    top_entries = list(target.iterdir())
    top_dirs = [entry for entry in top_entries if entry.is_dir() and not entry.is_symlink()]
    top_files = [entry for entry in top_entries if not entry.is_dir()]
    if skill_md.parent == target:
        # Root-level SKILL.md is itself the Skill root; references/scripts may
        # legitimately live in any number of sibling directories.
        pass
    else:
        if len(top_dirs) != 1 or top_files or top_dirs[0].name != metadata["name"]:
            return "top-level Skill directory must match SKILL.md.name"
    return None


@router.get("/{agent_type}")
async def scan_skills(
    request: Request,
    agent_type: str,
    session_id: str = Query("", description="Session ID to resolve workspace"),
    workspace_path: str = Query("", description="Absolute path to the agent worktree (fallback)"),
) -> list[dict]:
    """
    扫描 workspace 的 skills 目录并返回 skill 列表。
    通过 workspace manager 根据 session_id 解析 workspace_path，
    若无则回退到显式传入的 workspace_path 查询参数。
    """
    # 解析 workspace 路径：优先通过 session_id 查找，否则回退到显式路径
    resolved = ""
    if session_id:
        ws_mgr = request.app.state.workspace_manager
        ws = ws_mgr.get_by_session(session_id)
        if ws:
            resolved = ws.worktree_path
    if not resolved:
        # A caller-supplied filesystem path is only accepted when it resolves
        # to a WorkspaceManager-owned worktree. This prevents the authenticated
        # service from becoming an arbitrary host filesystem scanner.
        candidate = Path(workspace_path).resolve() if workspace_path else None
        ws_mgr = request.app.state.workspace_manager
        for workspace in ws_mgr.list():
            if candidate and Path(workspace.worktree_path).resolve() == candidate:
                resolved = workspace.worktree_path
                break
    if not resolved:
        return []

    # 根据 agent 类型确定 skills 目录
    config_dir = _AGENT_CONFIG_DIRS.get(agent_type)
    if config_dir is None:
        return []
    skills_dir = Path(resolved) / config_dir / "skills"

    if _skill_directory_has_symlink(skills_dir) or not skills_dir.is_dir():
        return []

    return _scan_skills_dir(skills_dir)


@router.post("/{agent_type}/{skill_name}/install")
async def install_skill(
    request: Request,
    agent_type: str,
    skill_name: str,
    session_id: str = Query(..., description="Session ID to resolve workspace"),
) -> dict:
    """
    将 skill（zip 压缩包）安装到 workspace 的 skills 目录中。
    Backend 以 zip 形式在请求体中发送 skill 文件，Agentend 解压到 worktree。
    """
    if agent_type not in _EXTERNAL_SKILL_AGENT_TYPES:
        return {"success": False, "error": "agent type does not support external Skill import"}
    name_error = _validate_skill_name(skill_name)
    if name_error:
        return {"success": False, "error": name_error}
    skills_dir = _resolve_skills_dir(request, agent_type, session_id)
    if skills_dir is None:
        return {"success": False, "error": "workspace not found for session"}
    if _skill_directory_has_symlink(skills_dir):
        return {"success": False, "error": "skill directory must not contain symlinks"}

    skills_dir.mkdir(parents=True, exist_ok=True)
    _cleanup_stale_skill_staging(skills_dir)
    content = await _read_limited_body(request, MAX_SKILL_PACKAGE_BYTES)
    if content is None:
        return {"success": False, "error": "skill package exceeds compressed size limit"}
    if not content:
        return {"success": False, "error": "no data provided"}

    staging_dir = Path(tempfile.mkdtemp(prefix=f".{skill_name}.install-", dir=skills_dir))
    staging: Path | None = staging_dir
    try:
        error = _extract_skill_zip(content, staging_dir)
        if error:
            return {"success": False, "error": error}

        skill_md_path = _find_skill_md(staging_dir)
        installed_name = _parse_skill_md(skill_md_path) if skill_md_path else None
        if not installed_name or installed_name["name"] != skill_name:
            return {"success": False, "error": "SKILL.md name does not match route skill name"}

        # Backend normally sends a canonical package with SKILL.md at the
        # archive root.  Accept the validator's single top-level-directory
        # form too, but move that directory itself into the destination so we
        # do not accidentally install skills/demo/demo/SKILL.md.
        source_dir = skill_md_path.parent if skill_md_path else staging_dir

        dest = skills_dir / skill_name
        backup: Path | None = None
        if dest.exists() or dest.is_symlink():
            backup = skills_dir / f".{skill_name}.previous-{uuid.uuid4().hex}"
            dest.rename(backup)
        try:
            source_dir.rename(dest)
        except OSError:
            if backup is not None and not dest.exists():
                backup.rename(dest)
            raise
        if backup is not None:
            if backup.is_dir() and not backup.is_symlink():
                shutil.rmtree(backup, ignore_errors=True)
            else:
                backup.unlink(missing_ok=True)
        staging = None if source_dir == staging_dir else staging_dir
    except OSError as exc:
        logger.warning("install skill %s failed during atomic replace", skill_name, exc_info=True)
        return {"success": False, "error": f"install skill failed: {exc}"}
    finally:
        if staging is not None:
            shutil.rmtree(staging, ignore_errors=True)

    logger.info("installed skill %s to %s", skill_name, dest)
    return {"success": True, "skill": skill_name, "path": str(dest)}


@router.delete("/{agent_type}/{skill_name}")
async def remove_skill(
    request: Request,
    agent_type: str,
    skill_name: str,
    session_id: str = Query(..., description="Session ID to resolve workspace"),
) -> dict:
    """
    从 workspace 的 skills 目录中移除一个 skill。
    """
    if agent_type not in _EXTERNAL_SKILL_AGENT_TYPES:
        return {"success": False, "error": "agent type does not support external Skill removal"}
    name_error = _validate_skill_name(skill_name)
    if name_error:
        return {"success": False, "error": name_error}
    skills_dir = _resolve_skills_dir(request, agent_type, session_id)
    if skills_dir is None:
        return {"success": False, "error": "workspace not found for session"}
    if _skill_directory_has_symlink(skills_dir):
        return {"success": False, "error": "skill directory must not contain symlinks"}

    dest = skills_dir / skill_name
    # Removal is idempotent: a retry after a successful delete must not turn
    # into an application-level failure.  Keep broken symlinks and other
    # non-directory entries as errors so we never silently remove an unsafe
    # target.
    if not dest.exists() and not dest.is_symlink():
        return {"success": True, "skill": skill_name, "already_removed": True}
    if dest.is_symlink() or not dest.is_dir():
        return {"success": False, "error": f"skill directory not found: {dest}"}

    tombstone = skills_dir / f".{skill_name}.remove-{uuid.uuid4().hex}"
    try:
        dest.rename(tombstone)
        shutil.rmtree(tombstone)
    except OSError as exc:
        if tombstone.exists() and not dest.exists():
            try:
                tombstone.rename(dest)
            except OSError:
                logger.exception("failed to restore skill %s after remove error", skill_name)
        return {"success": False, "error": f"remove skill failed: {exc}"}
    logger.info("removed skill %s from %s", skill_name, dest)
    return {"success": True, "skill": skill_name}
