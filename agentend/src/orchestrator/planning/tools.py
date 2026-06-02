from __future__ import annotations

import subprocess
from datetime import datetime
from pathlib import Path

from langchain_core.tools import tool

from src.app.agent_config import get_agent_config_dir
from src.app.config import settings
from src.orchestrator.planning.skill_loader import load_skill_l2, load_skill_resource


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


def _resolve_tool_path(path: str, base_dir: str) -> Path:
    target = Path(path)
    if not target.is_absolute():
        target = Path(base_dir) / target
    return target.resolve()


def _current_time_text() -> str:
    now = datetime.now().astimezone()
    return "\n".join(
        [
            f"当前日期: {now:%Y-%m-%d}",
            f"当前时间: {now:%Y-%m-%d %H:%M:%S %Z}",
            f"UTC offset: {now:%z}",
        ]
    )


def build_tools(shared_dir: str, allowed_read_dirs: list[str] | None = None, task_base_dir: str | None = None) -> list:
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
    task_base_resolved = str(Path(task_base_dir).resolve()) if task_base_dir else None

    @tool
    def current_time() -> str:
        """Return the current local date and time for reports or time-sensitive answers."""
        return _current_time_text()

    @tool
    def read_file(
        path: str,
        start_line: int = 1,
        line_count: int = 200,
        workspace_type: str = "shared",
    ) -> str:
        """Read a portion of a file within allowed workspace directories.

        Args:
            path: File path relative to the chosen workspace root.
            start_line: Line number to start reading from (1-indexed, default 1).
            line_count: Number of lines to read (default 200, max 500).
            workspace_type: "shared" (shared metadata, default) or "taskbase" (task code repo, read-only).

        Returns prefixed line numbers and a header showing which range was read.
        Large files are truncated if output exceeds 16 000 characters.
        """
        base = task_base_resolved if workspace_type == "taskbase" and task_base_resolved else shared_resolved
        file_path = _resolve_tool_path(path, base)
        if not _is_allowed(str(file_path), read_dirs):
            return "Error: path outside allowed directories"
        if start_line < 1:
            start_line = 1
        line_count = max(1, min(line_count, 500))
        try:
            all_lines = file_path.read_text(encoding="utf-8").splitlines()
        except FileNotFoundError:
            return f"Error: file not found: {path}"
        except Exception as e:
            return f"Error: {e}"

        total = len(all_lines)
        start_idx = start_line - 1
        if start_idx >= total:
            return f"(file has {total} lines, start_line={start_line} is out of range)"
        end_idx = min(start_idx + line_count, total)
        selected = all_lines[start_idx:end_idx]

        # Build output with line numbers
        out_lines: list[str] = []
        for i, content in enumerate(selected, start=start_line):
            out_lines.append(f"{i:>6}|{content}")

        max_chars = 16000
        header = f"[{path}  L{start_line}-{start_line + len(selected) - 1} / {total} total]"
        body = "\n".join(out_lines)
        if len(body) > max_chars:
            body = body[:max_chars] + f"\n... (truncated at {max_chars} chars, {len(body)} total)"
        return f"{header}\n{body}"

    @tool
    def list_dir(path: str, workspace_type: str = "shared") -> str:
        """List directory contents within allowed workspace directories.

        Args:
            path: Directory path relative to the chosen workspace root.
            workspace_type: "shared" (shared metadata, default) or "taskbase" (task code repo, read-only).
        """
        base = task_base_resolved if workspace_type == "taskbase" and task_base_resolved else shared_resolved
        target = _resolve_tool_path(path, base)
        if not _is_allowed(str(target), read_dirs):
            return "Error: path outside allowed directories"
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
        target = _resolve_tool_path(path, shared_resolved)
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
                timeout=settings.orchestrator.skill_execution_timeout,
            )
            output = result.stdout or result.stderr
            if len(output) > 4096:
                output = output[:4096] + "...(truncated)"
            return output.strip() or "(no output)"
        except subprocess.TimeoutExpired:
            return f"Error: skill execution timed out ({settings.orchestrator.skill_execution_timeout}s)"
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

    @tool
    def load_skill_detail(
        skill_name: str,
        level: str = "l2",
        resource_path: str = "",
    ) -> str:
        """Load detailed content for a skill by name.

        Args:
            skill_name: Skill name from the 可用 Skills list.
            level: "l2" returns the full SKILL.md body; "l3" returns a resource file (requires resource_path).
            resource_path: Required when level="l3". Path relative to the skill directory (e.g. "references/api.md").

        Returns the skill's content text, or an error message.
        """
        if level == "l2":
            body = load_skill_l2(skill_name, skills_dir)
            return body or f"Error: L2 content not found for skill '{skill_name}'"
        elif level == "l3":
            if not resource_path:
                return "Error: resource_path is required when level='l3'"
            return load_skill_resource(skill_name, resource_path, skills_dir)
        else:
            return "Error: level must be 'l2' or 'l3'"

    @tool
    def ask_agent(agent: str, question: str) -> str:
        """Ask a specific available Agent a question and wait for its streamed answer.

        Args:
            agent: Exact Agent id from the available Agents list. This is the group member id,
                not an agent type such as claude-code or opencode.
            question: Concrete question to send to that Agent.
        """
        return "ask_pending"

    @tool
    def plan_and_dispatch(overview: str, tasks: list[dict], merge_to_main: bool = False) -> str:
        """Signal orchestration intent. Call this when the user's request requires multi-agent collaboration.

        Args:
            overview: Overall plan summary describing how the request is decomposed.
            tasks: List of task dicts, each with task_id, session_id, title, content.
            merge_to_main: Whether orchestrator should request merging task/{task_id} into main after all tasks pass.
        """
        return "plan_generated"

    return [
        current_time,
        read_file,
        write_file,
        list_dir,
        run_skill,
        load_resource,
        load_skill_detail,
        ask_agent,
        plan_and_dispatch,
    ]
