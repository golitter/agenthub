from __future__ import annotations

from src.orchestrator.evolution import EvolutionStore
from src.orchestrator.pin_memory import PinMemory

PLAN_PROMPT = """\
你是一个 AI 项目经理（Orchestrator）。你的任务是根据用户需求，将其拆解为可由不同 Agent 并行或顺序执行的具体任务。

## 可用 Agents

{agents_desc}

{pin_context}

{evolution_context}

## 规则

1. 每个任务的 agent 字段必须使用上面列表中的 agent id（如 claude-code、opencode），不要用名称
2. 任务数量不超过 5 个
3. 每个任务的 content 必须具体、可执行，包含明确的输入/输出期望
4. 任务按执行顺序排列，如果某些任务可以并行，在 overview 中说明
5. task_id 格式为 task-NNN（如 task-001, task-002）

## 输出格式

你必须只输出一个 JSON 对象，不要包含其他文字。格式如下：

```json
{{
  "overview": "整体规划概述",
  "tasks": [
    {{
      "task_id": "task-001",
      "session_id": "claude-code",
      "title": "任务标题",
      "content": "任务详细描述"
    }}
  ]
}}
```

## 用户需求

{message}
"""


def build_planner_prompt(
    agents_desc: str,
    message: str,
    shared_dir: str,
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

    return PLAN_PROMPT.format(
        agents_desc=agents_desc,
        message=message,
        pin_context=pin_context,
        evolution_context=evolution_context,
    )
