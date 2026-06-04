"""Skills scan endpoint — scans agent workspace skills directory."""

import logging
import re
import shutil
import zipfile
from io import BytesIO
from pathlib import Path

from fastapi import APIRouter, Query, Request

from src.app.config import settings

logger = logging.getLogger(__name__)

router = APIRouter(prefix="/v1/skills", tags=["skills"])

# YAML frontmatter parser (lightweight, no full yaml dependency needed)
_FM_RE = re.compile(r"^---\s*\n(.*?)\n---", re.DOTALL)
_FM_NAME_RE = re.compile(r"^name:\s*(.+)$", re.MULTILINE)
_FM_DESC_RE = re.compile(r"^description:\s*(.+)$", re.MULTILINE)


def _parse_skill_md(skill_md_path: Path) -> dict | None:
    """Parse SKILL.md YAML frontmatter, return {name, description} or None."""
    try:
        text = skill_md_path.read_text(encoding="utf-8")
    except OSError:
        return None

    m = _FM_RE.match(text)
    if not m:
        return None

    fm = m.group(1)
    name_m = _FM_NAME_RE.search(fm)
    if not name_m:
        return None

    name = name_m.group(1).strip().strip("\"'")
    desc_m = _FM_DESC_RE.search(fm)
    description = desc_m.group(1).strip().strip("\"'") if desc_m else ""

    return {"name": name, "description": description}


def _scan_skills_dir(skills_dir: Path) -> list[dict]:
    """Scan a skills directory and return skill list."""
    builtin_names = set(settings.skills.manifest.keys())
    skills: list[dict] = []
    for entry in sorted(skills_dir.iterdir()):
        if not entry.is_dir():
            continue
        skill_md = entry / "SKILL.md"
        if not skill_md.is_file():
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
    """Resolve the skills directory for a given session + agent_type."""
    resolved = ""
    if session_id:
        ws_mgr = request.app.state.workspace_manager
        ws = ws_mgr.get_by_session(session_id)
        if ws:
            resolved = ws.worktree_path
    if not resolved:
        return None

    config_dir_map = {
        "claude-code": ".claude",
        "opencode": ".opencode",
        "codex": ".codex",
        "orchestrator": ".claude",
    }
    config_dir = config_dir_map.get(agent_type, ".claude")
    return Path(resolved) / config_dir / "skills"


@router.get("/{agent_type}")
async def scan_skills(
    request: Request,
    agent_type: str,
    session_id: str = Query("", description="Session ID to resolve workspace"),
    workspace_path: str = Query("", description="Absolute path to the agent worktree (fallback)"),
) -> list[dict]:
    """
    Scan workspace skills directory and return list of skills.
    Resolves workspace_path from session_id via workspace manager,
    falls back to explicit workspace_path query param.
    """
    # Resolve workspace path: prefer session_id lookup, fallback to explicit path
    resolved = ""
    if session_id:
        ws_mgr = request.app.state.workspace_manager
        ws = ws_mgr.get_by_session(session_id)
        if ws:
            resolved = ws.worktree_path
    if not resolved:
        resolved = workspace_path
    if not resolved:
        return []

    # Determine skills directory based on agent type
    config_dir_map = {
        "claude-code": ".claude",
        "opencode": ".opencode",
        "codex": ".codex",
        "orchestrator": ".claude",
    }
    config_dir = config_dir_map.get(agent_type, ".claude")
    skills_dir = Path(resolved) / config_dir / "skills"

    if not skills_dir.is_dir():
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
    Install a skill (zip archive) into the workspace skills directory.
    Backend sends the skill files as a zip in request body, Agentend extracts to worktree.
    """
    skills_dir = _resolve_skills_dir(request, agent_type, session_id)
    if skills_dir is None:
        return {"success": False, "error": "workspace not found for session"}

    # Ensure skills directory exists
    skills_dir.mkdir(parents=True, exist_ok=True)
    dest = skills_dir / skill_name

    # If destination already exists, remove it first
    if dest.exists():
        shutil.rmtree(dest)

    # Read zip from raw body
    content = await request.body()
    if not content:
        return {"success": False, "error": "no data provided"}

    try:
        zip_buf = BytesIO(content)
        with zipfile.ZipFile(zip_buf, "r") as zf:
            zf.extractall(dest)
    except zipfile.BadZipFile:
        return {"success": False, "error": "invalid zip file"}

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
    Remove a skill from the workspace skills directory.
    """
    skills_dir = _resolve_skills_dir(request, agent_type, session_id)
    if skills_dir is None:
        return {"success": False, "error": "workspace not found for session"}

    dest = skills_dir / skill_name
    if not dest.exists():
        return {"success": False, "error": f"skill directory not found: {dest}"}

    shutil.rmtree(dest)
    logger.info("removed skill %s from %s", skill_name, dest)
    return {"success": True, "skill": skill_name}
