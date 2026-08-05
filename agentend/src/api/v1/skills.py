"""Skills 扫描端点 — 扫描 agent workspace 的 skills 目录。"""

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

# YAML frontmatter 解析器（轻量实现，无需完整 yaml 依赖）
_FM_RE = re.compile(r"^---\s*\n(.*?)\n---", re.DOTALL)
_FM_NAME_RE = re.compile(r"^name:\s*(.+)$", re.MULTILINE)
_FM_DESC_RE = re.compile(r"^description:\s*(.+)$", re.MULTILINE)


def _parse_skill_md(skill_md_path: Path) -> dict | None:
    """解析 SKILL.md 的 YAML frontmatter，返回 {name, description} 或 None。"""
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
    """扫描 skills 目录并返回 skill 列表。"""
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
    """根据给定的 session 与 agent_type 解析对应的 skills 目录。"""
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
        resolved = workspace_path
    if not resolved:
        return []

    # 根据 agent 类型确定 skills 目录
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
    将 skill（zip 压缩包）安装到 workspace 的 skills 目录中。
    Backend 以 zip 形式在请求体中发送 skill 文件，Agentend 解压到 worktree。
    """
    skills_dir = _resolve_skills_dir(request, agent_type, session_id)
    if skills_dir is None:
        return {"success": False, "error": "workspace not found for session"}

    # 确保 skills 目录存在
    skills_dir.mkdir(parents=True, exist_ok=True)
    dest = skills_dir / skill_name

    # 若目标已存在，先移除
    if dest.exists():
        shutil.rmtree(dest)

    # 从原始请求体读取 zip
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
    从 workspace 的 skills 目录中移除一个 skill。
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
