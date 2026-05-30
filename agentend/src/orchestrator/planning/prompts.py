from __future__ import annotations

from src.orchestrator.memory.evolution import EvolutionStore
from src.orchestrator.memory.pin_memory import PinMemory

REASON_PROMPT = """\
你是一个对话式任务编排器。你可以直接回答用户的问题，也可以协调多个 Agent 来完成复杂任务。

## 可用 Agents

{agents_desc}

{skills_section}

{tools_section}

{pin_context}

{evolution_context}

{replan_section}

## 规则

### 判断逻辑

- 如果用户的问题是闲聊、简单问答、或不需要多 Agent 协作就能回答的问题 → 直接用文本回复
- 如果用户的请求需要多个 Agent 协作、或涉及代码生成/审查/分析等复杂任务 → 调用 `plan_and_dispatch` 工具
- 你可以先使用工具（如 read_file、list_dir）收集信息，再决定是直接回复还是编排

### Agents 与 Skills 的区别（极其重要）

- **Agents** 是执行者。每个任务的 `session_id` **必须且只能**填「可用 Agents」列表中的 agent id（加粗的名称）。
- **Skills** 是工具，不是 Agent，绝不能把 skill 名称填入 session_id。
  需要 Skill 时，应将任务分配给 Agent，在 content 中指示调用对应 Skill。
- 错误示例：`"session_id": "render"` ← render 是 Skill 不是 Agent
- 正确示例：`"session_id": "claude-code", "content":
  "使用 render skill 的 html-render 命令生成笑脸 HTML 卡片"`

### 通用规则

1. 直接回复时，用清晰、简洁的中文回答
2. 编排时，任务数量不超过 5 个
3. 每个任务的 content 必须具体、可执行，包含明确的输入/输出期望
4. task_id 格式为 task-NNN（如 task-001, task-002）
5. session_id 只能使用「可用 Agents」列表中的 id

## 用户需求

{message}
"""


def build_reason_prompt(
    agents_desc: str,
    message: str,
    shared_dir: str,
    l2_content: dict[str, str] | None = None,
    replan_reason: str | None = None,
) -> str:
    pin_context = ""
    evolution_context = ""

    try:
        pm = PinMemory(common_dir=f"{shared_dir}/memory/common")
        pin_context = pm.get_context()
    except Exception:
        pass

    try:
        evo = EvolutionStore(shared_dir)
        evolution_context = evo.get_recent_experience(5)
    except Exception:
        pass

    if l2_content:
        skill_parts = []
        for name, body in l2_content.items():
            skill_parts.append(f"### {name}\n\n{body}")
        skills_section = "## 可用 Skills\n\n" + "\n\n".join(skill_parts)
    else:
        skills_section = "## 可用 Skills\n\n(无)"

    tools_section = (
        "## 可用工具\n\n"
        "你可以使用以下工具来收集信息：\n"
        "- `current_time()`: 获取当前本地日期和时间；涉及报告日期、今天、明天等时间问题时必须调用\n"
        "- `read_file(path, start_line=1, line_count=200)`: 读取文件指定行范围（带行号，默认前 200 行，最多 500 行）\n"
        "- `write_file(path, content)`: 写入文件到共享目录\n"
        "- `list_dir(path)`: 列出目录内容（仅限 shared 目录；相对路径从 shared_dir 解析）\n"
        "- `run_skill(skill, command, args)`: 执行已注册的 skill 命令\n"
        "- `load_resource(skill_name, resource_path)`: 加载 skill 的参考资源文件\n"
        "- `plan_and_dispatch(overview, tasks)`: 编排多 Agent 任务（当需要多 Agent 协作时调用）\n"
    )

    replan_section = ""
    if replan_reason:
        replan_section = "## 重规划上下文\n\n这是前一次规划的失败反馈，请据此调整规划：\n" + replan_reason

    return REASON_PROMPT.format(
        agents_desc=agents_desc,
        message=message,
        pin_context=pin_context,
        evolution_context=evolution_context,
        skills_section=skills_section,
        tools_section=tools_section,
        replan_section=replan_section,
    )
