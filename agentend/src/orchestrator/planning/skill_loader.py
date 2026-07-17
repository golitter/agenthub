from __future__ import annotations

import re
from pathlib import Path

import yaml
from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI
from langgraph.config import get_config

from src.app.config import settings


def _parse_frontmatter(text: str) -> dict | None:
    """Parse YAML frontmatter (--- ... ---) from text. Returns dict or None."""
    match = re.match(r"^---\s*\n(.*?)\n---", text, re.DOTALL)
    if not match:
        return None
    try:
        return yaml.safe_load(match.group(1))
    except yaml.YAMLError:
        return None


def _strip_frontmatter(text: str) -> str:
    """Remove YAML frontmatter and return body text."""
    return re.sub(r"^---\s*\n.*?\n---\s*\n?", "", text, count=1, flags=re.DOTALL)


def discover_skills(builtin_dir: str | Path) -> list[dict]:
    """Scan builtin_dir subdirectories for SKILL.md files with valid frontmatter.

    Returns list of dicts with 'name' and 'description' keys.
    Skips directories without SKILL.md or without a 'name' field.
    """
    builtin = Path(builtin_dir)
    if not builtin.is_dir():
        return []

    skills: list[dict] = []
    for child in sorted(builtin.iterdir()):
        if not child.is_dir():
            continue
        skill_md = child / "SKILL.md"
        if not skill_md.is_file():
            continue
        fm = _parse_frontmatter(skill_md.read_text(encoding="utf-8"))
        if not fm or "name" not in fm:
            continue
        skills.append(
            {
                "name": fm["name"],
                "description": fm.get("description", ""),
            }
        )
    return skills


def load_skill_l2(skill_name: str, builtin_dir: str | Path) -> str:
    """Load full SKILL.md body (excluding frontmatter) for a given skill."""
    skill_md = Path(builtin_dir) / skill_name / "SKILL.md"
    if not skill_md.is_file():
        return ""
    return _strip_frontmatter(skill_md.read_text(encoding="utf-8")).strip()


def load_skill_resource(skill_name: str, resource_path: str, builtin_dir: str | Path) -> str:
    """Load a resource file from a skill's directory.

    resource_path must not contain '..' (path traversal check).
    Reads from builtin_dir/skill_name/resource_path.
    """
    if ".." in resource_path:
        return "Error: invalid resource path"

    full_path = Path(builtin_dir) / skill_name / resource_path
    try:
        return full_path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return f"Error: resource file not found: {resource_path}"
    except Exception as e:
        return f"Error: {e}"


def select_skills(l1_skills: list[dict], message: str, config: dict | None = None) -> list[str]:
    """Use one LLM call to semantically select relevant skills from L1 metadata."""
    if not l1_skills:
        return []

    skill_list = "\n".join(f"- {s['name']}: {s['description']}" for s in l1_skills)
    select_prompt = f"""Based on the user's task, select the most relevant skills from the list below.
Return ONLY a comma-separated list of skill names, nothing else.
If no skills are relevant, return an empty string.

Available skills:
{skill_list}

User task: {message}"""

    if config is None:
        try:
            config = get_config()
        except RuntimeError:
            config = None
    try:
        llm = ChatOpenAI(
            model=settings.llm.model,
            base_url=settings.llm.base_url,
            api_key=settings.llm.api_key,
            temperature=0,
        )
        response = llm.invoke([HumanMessage(content=select_prompt)], config=config)
        valid_names = {s["name"] for s in l1_skills}
        return [n.strip() for n in response.content.split(",") if n.strip() in valid_names]
    except Exception:
        return []


def load_l2_content(selected_names: list[str], builtin_dir: str | Path) -> dict[str, str]:
    """Load SKILL.md body for each selected skill."""
    content: dict[str, str] = {}
    for name in selected_names:
        body = load_skill_l2(name, builtin_dir)
        if body:
            content[name] = body
    return content
