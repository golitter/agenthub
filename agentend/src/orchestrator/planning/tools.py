from __future__ import annotations

import subprocess
from pathlib import Path

from langchain_core.tools import tool

from src.app.agent_config import get_agent_config_dir
from src.app.config import settings
from src.orchestrator.planning.skill_loader import load_skill_resource


def _skills_dir(shared_dir: str) -> Path:
    """Resolve orchestrator's skills directory: shared_dir/.orchestrator/skills/."""
    config_dir = get_agent_config_dir("orchestrator")
    return Path(shared_dir) / (config_dir or ".orchestrator") / "skills"


def _resolve_skill_binary(skill_name: str, skills_dir: Path) -> Path | None:
    binary = skills_dir / skill_name / skill_name
    return binary if binary.is_file() else None


def _is_relative_to(target: Path, base: Path) -> bool:
    try:
        target.relative_to(base)
        return True
    except ValueError:
        return False


def _is_allowed(path: str, allowed_dirs: list[str]) -> bool:
    target = Path(path).resolve()
    return any(_is_relative_to(target, Path(d).resolve()) for d in allowed_dirs)


def build_tools(shared_dir: str, allowed_read_dirs: list[str] | None = None) -> list:
    """Build the tool list for the plan_node agent loop.

    Creates tools with shared_dir and skills_dir pre-bound.
    read_file / list_dir are restricted to allowed_read_dirs.
    write_file is restricted to shared_dir.
    run_skill validates against manifest keys at runtime.
    """
    manifest = settings.skills.manifest
    shared_resolved = str(Path(shared_dir).resolve())
    read_dirs = allowed_read_dirs or [shared_resolved]
    skills_dir = _skills_dir(shared_dir)

    @tool
    def read_file(path: str) -> str:
        """Read a file within allowed workspace directories."""
        if not _is_allowed(path, read_dirs):
            return "Error: path outside allowed directories"
        try:
            return Path(path).resolve().read_text(encoding="utf-8")
        except FileNotFoundError:
            return f"Error: file not found: {path}"
        except Exception as e:
            return f"Error: {e}"

    @tool
    def list_dir(path: str) -> str:
        """List directory contents within allowed workspace directories."""
        if not _is_allowed(path, read_dirs):
            return "Error: path outside allowed directories"
        target = Path(path).resolve()
        if not target.is_dir():
            return f"Error: directory not found: {path}"
        entries = []
        for child in sorted(target.iterdir()):
            name = child.name + ("/" if child.is_dir() else "")
            entries.append(name)
        return "\n".join(entries)

    @tool
    def write_file(path: str, content: str) -> str:
        """Write content to a file within the shared workspace directory."""
        target = Path(path).resolve()
        base = Path(shared_resolved)
        try:
            target.relative_to(base)
        except ValueError:
            return "Error: path outside shared_dir"
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")
        return "OK"

    @tool
    def run_skill(
        skill: str,
        command: str,
        skill_args: str = "",
    ) -> str:
        """Execute a registered skill binary with the given command and args.

        Valid skills: {valid_skills}. cwd is locked to shared_dir. Timeout 30s.
        """
        if skill not in manifest:
            return f"Error: unknown skill '{skill}'"
        binary = _resolve_skill_binary(skill, skills_dir)
        if binary is None:
            return f"Error: skill binary not found for '{skill}'"
        cmd_parts = [str(binary), command]
        if skill_args:
            cmd_parts.append(skill_args)
        try:
            result = subprocess.run(
                cmd_parts,
                cwd=shared_resolved,
                capture_output=True,
                text=True,
                timeout=30,
            )
            output = result.stdout or result.stderr
            if len(output) > 4096:
                output = output[:4096] + "...(truncated)"
            return output.strip() or "(no output)"
        except subprocess.TimeoutExpired:
            return "Error: skill execution timed out (30s)"
        except Exception as e:
            return f"Error: {e}"

    @tool
    def load_resource(skill_name: str, resource_path: str) -> str:
        """Load an L3 resource file from a skill's references/ or assets/ directory.

        skill_name must be in skills.manifest. resource_path must not contain '..'.
        """
        if skill_name not in manifest:
            return f"Error: unknown skill '{skill_name}'"
        return load_skill_resource(skill_name, resource_path, skills_dir)

    return [read_file, write_file, list_dir, run_skill, load_resource]
