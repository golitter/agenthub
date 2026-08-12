from __future__ import annotations

import subprocess
from datetime import datetime
from pathlib import Path

from langchain_core.tools import tool

from src.adapters.base import child_process_env
from src.app.agent_config import get_agent_config_dir
from src.app.config import settings
from src.orchestrator.planning.skill_loader import load_skill_l2, load_skill_resource


def _skills_dir(shared_dir: str) -> Path:
    """解析 orchestrator 的 skills 目录：shared_dir/.orchestrator/skills/。"""
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


def build_tools(
    shared_dir: str,
    allowed_read_dirs: list[str] | None = None,
    task_base_dir: str | None = None,
    process_env: dict[str, str] | None = None,
) -> list:
    """为 plan_node agent 循环构建工具列表。

    创建的工具会预绑定 shared_dir 和 skills_dir。
    read_file / list_dir 被限制在 allowed_read_dirs 范围内。
    write_file 被限制在 shared_dir 范围内。
    run_skill 在运行时根据 manifest 的键进行校验。
    """
    manifest = settings.skills.manifest
    shared_resolved = str(Path(shared_dir).resolve())
    read_dirs = allowed_read_dirs or [shared_resolved]
    skills_dir = _skills_dir(shared_dir)
    task_base_resolved = str(Path(task_base_dir).resolve()) if task_base_dir else None

    @tool
    def current_time() -> str:
        """返回当前本地日期和时间，用于报告或对时间敏感的回答。"""
        return _current_time_text()

    @tool
    def read_file(
        path: str,
        start_line: int = 1,
        line_count: int = 200,
        workspace_type: str = "shared",
    ) -> str:
        """读取允许的工作区目录内文件的一部分。

        Args:
            path: 相对于所选工作区根目录的文件路径。
            start_line: 开始读取的行号（从 1 开始计数，默认为 1）。
            line_count: 要读取的行数（默认 200，最大 500）。
            workspace_type: "shared"（共享元数据，默认）或 "taskbase"（任务代码仓库，只读）。

        返回带行号前缀的内容，以及一个标明所读范围的头部。
        若输出超过 16 000 个字符，大文件将被截断。
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

        # 构建带行号的输出
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
        """列出允许的工作区目录内的目录内容。

        Args:
            path: 相对于所选工作区根目录的目录路径。
            workspace_type: "shared"（共享元数据，默认）或 "taskbase"（任务代码仓库，只读）。
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
        """将内容写入共享工作区目录内的文件。"""
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
        """使用给定的 command 和 args 执行已注册的 skill 二进制文件。

        有效 skill：{valid_skills}。cwd 被锁定为 shared_dir。超时 30s。
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
                env=child_process_env(process_env),
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
        """从 skill 的 references/ 或 assets/ 目录加载 L3 资源文件。

        skill_name 必须在 skills.manifest 中。resource_path 不得包含 '..'。
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
        """按名称加载 skill 的详细内容。

        Args:
            skill_name: 来自「可用 Skills」列表的 skill 名称。
            level: "l2" 返回完整的 SKILL.md 正文；"l3" 返回资源文件（需要 resource_path）。
            resource_path: 当 level="l3" 时必填。相对于 skill 目录的路径（例如 "references/api.md"）。

        返回该 skill 的内容文本，或一条错误信息。
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
        """向某个可用的 Agent 提问，并等待其流式回答。

        Args:
            agent: 来自可用 Agents 列表的精确 Agent id。这是群成员 id，
                不是诸如 claude-code 或 opencode 这样的 agent 类型。
            question: 要发送给该 Agent 的具体问题。
        """
        return "ask_pending"

    @tool
    def plan_and_dispatch(overview: str, tasks: list[dict], merge_to_main: bool = False) -> str:
        """表示编排意图。当用户请求需要多 Agent 协作时调用此工具。

        Args:
            overview: 总体计划摘要，描述请求如何被分解。
            tasks: 任务 dict 的列表，每个 dict 包含 task_id、session_id、title、content。
            merge_to_main: 所有任务通过后，orchestrator 是否应请求将 task/{task_id} 合入 main。
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
