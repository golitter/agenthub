from __future__ import annotations

import json
import re
from pathlib import Path
from typing import TypedDict

import yaml
from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI
from langgraph.graph import StateGraph

from src.app.config import settings
from src.orchestrator.models import PlanOutput
from src.orchestrator.prompts import PLAN_PROMPT


class GraphState(TypedDict):
    message: str
    agents: list[dict]
    task_id: str
    shared_dir: str
    plan: PlanOutput | None


def _build_agents_desc(agents: list[dict]) -> str:
    lines = []
    for a in agents:
        aid = a.get("id", "unknown")
        name = a.get("name", aid)
        caps = a.get("capabilities", [])
        cap_str = ", ".join(caps) if caps else "通用"
        lines.append(f"- **{aid}**（{name}）: {cap_str}")
    return "\n".join(lines)


def _extract_json(text: str) -> dict:
    """Extract JSON from LLM response, handling markdown code blocks."""
    match = re.search(r"```(?:json)?\s*\n?(.*?)\n?```", text, re.DOTALL)
    if match:
        return json.loads(match.group(1))
    return json.loads(text)


def plan_node(state: GraphState) -> dict:
    llm = ChatOpenAI(
        model=settings.llm.model,
        base_url=settings.llm.base_url,
        api_key=settings.llm.api_key,
    )

    agents_desc = _build_agents_desc(state["agents"])
    prompt = PLAN_PROMPT.format(agents_desc=agents_desc, message=state["message"])

    response = llm.invoke([HumanMessage(content=prompt)])
    plan = PlanOutput.model_validate(_extract_json(response.content))
    return {"plan": plan}


def _build_session_map(agents: list[dict]) -> dict[str, str]:
    """Map agent id → real session_id, e.g. 'claude-code' → 'cc-orch-test'."""
    return {a["id"]: a["session_id"] for a in agents if "session_id" in a}


def write_shared_node(state: GraphState) -> dict:
    plan = state["plan"]
    assert plan is not None

    session_map = _build_session_map(state["agents"])

    shared = Path(state["shared_dir"])
    plans_dir = shared / "plans"
    plans_dir.mkdir(parents=True, exist_ok=True)

    # plans/overview.md — 整体规划（taskctl summary 可读）
    (plans_dir / "overview.md").write_text(f"# 规划概述\n\n{plan.overview}", encoding="utf-8")

    # plans/task-NNN.md — 每个任务的详细说明（taskctl summary 可读）
    task_entries = []
    for idx, task in enumerate(plan.tasks, start=1):
        filename = f"task-{idx:03d}.md"
        real_session = session_map.get(task.session_id, task.session_id)
        (plans_dir / filename).write_text(
            f"# {task.title}\n\n> agent: {task.session_id}\n\n{task.content}",
            encoding="utf-8",
        )
        task_entries.append({"task_id": task.task_id, "session_id": real_session, "file": f"plans/{filename}"})

    # config.yaml — declarative task index
    config = {"task_id": state["task_id"], "tasks": task_entries}
    (shared / "config.yaml").write_text(
        yaml.dump(config, allow_unicode=True, default_flow_style=False),
        encoding="utf-8",
    )

    return {}


def build_graph() -> StateGraph:
    graph = StateGraph(GraphState)
    graph.add_node("plan", plan_node)
    graph.add_node("write_shared", write_shared_node)
    graph.set_entry_point("plan")
    graph.add_edge("plan", "write_shared")
    graph.set_finish_point("write_shared")
    return graph.compile()
