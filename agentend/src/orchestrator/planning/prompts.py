from __future__ import annotations

from src.orchestrator.memory.evolution import EvolutionStore
from src.orchestrator.memory.pin_memory import PinMemory

REASON_PROMPT = """\
你是一个对话式任务编排器。你可以直接回答用户的问题，也可以协调多个 Agent 来完成相应任务。

## 可用 Agents

{agents_desc}

{soul_section}

{skills_section}

{tools_section}

{pin_context}

{evolution_context}

{replan_section}

{orchestrator_context}

{workspace_section}

## 规则

### 判断逻辑

- 如果用户的问题是闲聊、简单问答、或不需要多 Agent 协作就能回答的问题 → 直接用文本回复
- 如果用户的请求需要多个 Agent 协作、或涉及代码生成/审查/分析等复杂任务 → 调用 `plan_and_dispatch` 工具
- 如果用户要求某个可用 Agent 执行实际动作（修改文件、运行命令、提交、检查仓库、生成产物等），
  即使只需要一个 Agent，也必须调用 `plan_and_dispatch`
- 如果用户明确提到“规划”“plan”“分派”“执行者/实现者去做”等意图，必须调用 `plan_and_dispatch`，不要只用文字描述“我会调用”
- 你可以先使用工具（如 read_file、list_dir）收集信息，再决定是直接回复还是编排
- 当规划需要某个 Agent 的专业判断、代码环境确认或方案建议时，可以先调用 `ask_agent(agent, question)`
  咨询该 Agent；拿到回答后再继续判断是否直接回复或调用 `plan_and_dispatch`
- `ask_agent` 的 `agent` 参数必须填写「可用 Agents」列表里加粗的 agent id；不要填写类型名（如
  `claude-code`）、orchestrator、展示别名或 skill 名称

### Agents 与 Skills 的区别（极其重要）

- **Agents** 是执行者。每个任务的 `session_id` **必须且只能**填「可用 Agents」列表中的 agent id（加粗的名称）。
- **Skills** 是工具，不是 Agent，绝不能把 skill 名称填入 session_id。
  需要 Skill 时，应将任务分配给 Agent，在 content 中指示调用对应 Skill。
- 错误示例：`"session_id": "render"` ← render 是 Skill 不是 Agent
- 错误示例：`ask_agent(agent="claude-code", ...)` ← claude-code 是类型，不是群里的 Agent id
- 正确示例：`"session_id": "执行者", "content":
  "使用 render skill 的 html-render 命令生成笑脸 HTML 卡片"`

### main 分支合并决策

- `plan_and_dispatch` 的 `merge_to_main` 参数由你决定，表示所有任务成功后是否请求将
  `task/{{task_id}}` 合入 `main`
- 只有用户明确要求“合入 main / 提交到 main / 最终合并 / 发布最终结果”，
  或你确认本轮目标就是完成并落地到 main 时，才设置 `merge_to_main=true`
- 如果只是开发、实验、检查、讨论、生成草稿、局部修复，或用户没有明确要求合入 main，
  设置 `merge_to_main=false`
- sub-agent 只负责把自己的分支合入 task 分支；合入 main 的请求由 orchestrator 决策，AgentEnd 执行
- 当用户后续只要求“合入 main / 确认合并到 main”且不需要新的代码修改时，
  调用 `plan_and_dispatch(overview, tasks=[], merge_to_main=true)`

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
    l1_skills: list[dict] | None = None,
    replan_reason: str | None = None,
    orchestrator_context: str = "",
    task_base_path: str = "",
) -> str:
    from pathlib import Path

    pin_context = ""
    evolution_context = ""
    soul_section = ""

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

    # Load orchestrator's own SOUL.md from shared directory
    try:
        shared_path = Path(shared_dir)
        orchestrator_soul = shared_path / "SOUL.md"
        if orchestrator_soul.is_file():
            content = orchestrator_soul.read_text(encoding="utf-8").strip()
            if content:
                soul_section = f"## 我的身份 (SOUL.md)\n\n{content}"
    except Exception:
        pass

    if l1_skills:
        parts = [f"- **{s['name']}**: {s['description']}" for s in l1_skills]
        skills_section = "## 可用 Skills\n\n" + "\n".join(parts)
    else:
        skills_section = "## 可用 Skills\n\n(无)"

    workspace_section = ""
    if task_base_path:
        workspace_section = (
            "## 工作区目录\n\n"
            "你可以读取两个目录（使用相对路径，通过 `workspace_type` 参数区分）：\n"
            "- **任务代码仓库（只读）**: `workspace_type='taskbase'`，包含完整项目代码，"
            "你可以浏览代码结构、读取源文件来了解项目并做出精准的规划\n"
            "- **共享元数据（可读写）**: `workspace_type='shared'`（默认），"
            "包含 plans/、memory/、SOUL.md、.orchestrator/skills/ 等编排相关文件\n\n"
            "### 子 Agent 工作流程\n\n"
            "每个子 Agent 拥有独立的代码分支和 worktree 目录（基于任务分支创建，文件结构相同），"
            "它们在各自的 worktree 中修改代码，完成后 merge 回任务分支。\n\n"
            "分发任务时：\n"
            "- 使用**相对路径**指定要修改的文件（如 `README.md`、`frontend/src/App.tsx`），"
            "不要使用绝对路径——子 Agent 会在自己的 worktree 中找到同名文件\n"
            "- content 应包含明确的文件路径和修改要求（改什么、改成什么样）\n"
            "- 你无需关心子 Agent 的 worktree 路径，系统会自动分配\n"
        )

    tools_section = (
        "## 可用工具\n\n"
        "你可以使用以下工具来收集信息：\n"
        "- `current_time()`: 获取当前本地日期和时间；涉及报告日期、今天、明天等时间问题时必须调用\n"
        "- `read_file(path, start_line=1, line_count=200, workspace_type='shared')`: 读取文件指定行范围（带行号）；"
        "`workspace_type='shared'`（默认）读共享目录，`workspace_type='taskbase'` 读任务代码仓库\n"
        "- `write_file(path, content)`: 写入文件到共享目录\n"
        "- `list_dir(path, workspace_type='shared')`: 列出目录内容；`workspace_type` 同上\n"
        "- `run_skill(skill, command, args)`: 执行已注册的 skill 命令\n"
        "- `load_skill_detail(skill_name, level='l2', resource_path='')`: 加载 skill 详情；"
        "level='l2' 返回 SKILL.md 完整正文，level='l3' 需配合 resource_path 加载资源文件\n"
        "- `ask_agent(agent, question)`: 向指定 Agent 提问并等待回答，用于规划阶段收集专业意见\n"
        "- `plan_and_dispatch(overview, tasks, merge_to_main=false)`: 编排多 Agent 任务；"
        "`merge_to_main` 表示任务成功后是否请求合入 main\n"
    )

    replan_section = ""
    if replan_reason:
        replan_section = "## 重规划上下文\n\n这是前一次规划的失败反馈，请据此调整规划：\n" + replan_reason

    return REASON_PROMPT.format(
        agents_desc=agents_desc,
        message=message,
        pin_context=pin_context,
        evolution_context=evolution_context,
        soul_section=soul_section,
        skills_section=skills_section,
        tools_section=tools_section,
        workspace_section=workspace_section,
        replan_section=replan_section,
        orchestrator_context=orchestrator_context,
    )
