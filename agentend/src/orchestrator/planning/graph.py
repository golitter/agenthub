from __future__ import annotations

import json
import logging
import re
from pathlib import Path
from typing import TypedDict

import yaml
from langchain_core.messages import AIMessage, HumanMessage, SystemMessage, ToolMessage
from langchain_openai import ChatOpenAI
from langgraph.graph import StateGraph

from src.app.agent_config import get_agent_config_dir
from src.app.config import settings
from src.orchestrator.models import PlanOutput
from src.orchestrator.planning.prompts import build_planner_prompt
from src.orchestrator.planning.skill_loader import discover_skills, load_skill_l2
from src.orchestrator.planning.tools import build_tools

logger = logging.getLogger(__name__)


class GraphState(TypedDict):
    message: str
    agents: list[dict]
    task_id: str
    shared_dir: str
    allowed_read_dirs: list[str]
    plan: PlanOutput | None
    l1_skills: list[dict]
    selected_skill_names: list[str]
    l2_content: dict[str, str]
    l3_content: dict[str, str]


def _build_agents_desc(agents: list[dict]) -> str:
    lines = []
    for a in agents:
        aid = a.get("id", "unknown")
        agent_type = a.get("type", aid)
        name = a.get("name", aid)
        caps = a.get("capabilities", [])
        cap_str = ", ".join(caps) if caps else "通用"
        lines.append(f"- **{aid}**（{name}，类型: {agent_type}）: {cap_str}")
    return "\n".join(lines)


def _extract_json(text: str) -> dict | None:
    """Extract JSON from LLM response, handling markdown code blocks."""
    try:
        match = re.search(r"```(?:json)?\s*\n?(.*?)\n?```", text, re.DOTALL)
        if match:
            return json.loads(match.group(1))
        return json.loads(text)
    except json.JSONDecodeError:
        return None


# --- Progressive Disclosure Nodes ---


def _skills_dir(state: GraphState) -> Path:
    """Resolve orchestrator's skills directory: shared_dir/.orchestrator/skills/."""
    config_dir = get_agent_config_dir("orchestrator")
    return Path(state["shared_dir"]) / (config_dir or ".orchestrator") / "skills"


def discover_node(state: GraphState) -> dict:
    """L1: Scan skills directory for SKILL.md frontmatter metadata."""
    skills_dir = _skills_dir(state)
    l1_skills = discover_skills(skills_dir)
    return {"l1_skills": l1_skills}


def select_node(state: GraphState) -> dict:
    """L1→L2: Use one LLM call to semantically select relevant skills."""
    l1_skills = state.get("l1_skills", [])
    if not l1_skills:
        return {"selected_skill_names": []}

    skill_list = "\n".join(f"- {s['name']}: {s['description']}" for s in l1_skills)
    select_prompt = f"""Based on the user's task, select the most relevant skills from the list below.
Return ONLY a comma-separated list of skill names, nothing else.
If no skills are relevant, return an empty string.

Available skills:
{skill_list}

User task: {state["message"]}"""

    try:
        llm = ChatOpenAI(
            model=settings.llm.model,
            base_url=settings.llm.base_url,
            api_key=settings.llm.api_key,
            temperature=0,
        )
        response = llm.invoke([HumanMessage(content=select_prompt)])
        valid_names = {s["name"] for s in l1_skills}
        selected = [n.strip() for n in response.content.split(",") if n.strip() in valid_names]
        return {"selected_skill_names": selected}
    except Exception:
        return {"selected_skill_names": []}


def load_l2_node(state: GraphState) -> dict:
    """L2: Load SKILL.md body for each selected skill."""
    skills_dir = _skills_dir(state)
    selected = state.get("selected_skill_names", [])
    l2_content: dict[str, str] = {}
    for name in selected:
        content = load_skill_l2(name, skills_dir)
        if content:
            l2_content[name] = content
    return {"l2_content": l2_content}


# --- Plan Node (Tool-Calling Agent Loop) ---


def _find_tool(tools: list, name: str):
    for t in tools:
        if t.name == name:
            return t
    return None


def _clean_ai_message(msg: AIMessage) -> AIMessage:
    """Strip reasoning_content for non-thinking models (no-op if absent).

    For thinking models (deepseek-reasoner), reasoning_content MUST be preserved.
    This guard ensures compatibility when the model is swapped at runtime.
    """
    if "reasoning_content" not in msg.additional_kwargs:
        return msg
    kw = {k: v for k, v in msg.additional_kwargs.items() if k != "reasoning_content"}
    return AIMessage(content=msg.content, tool_calls=msg.tool_calls, additional_kwargs=kw, id=msg.id)


def plan_node(state: GraphState) -> dict:
    """Generate plan via tool-calling agent loop.

    Handles DeepSeek reasoning_content by stripping it between turns.
    Terminates when LLM responds without tool_calls or max_iterations reached.
    """
    try:
        llm = ChatOpenAI(
            model=settings.llm.model,
            base_url=settings.llm.base_url,
            api_key=settings.llm.api_key,
        )

        agents_desc = _build_agents_desc(state["agents"])
        tools = build_tools(state["shared_dir"], state.get("allowed_read_dirs"))
        llm_with_tools = llm.bind_tools(tools)

        system_prompt = build_planner_prompt(
            agents_desc=agents_desc,
            message=state["message"],
            shared_dir=state["shared_dir"],
            l2_content=state.get("l2_content", {}),
        )

        messages = [
            SystemMessage(content=system_prompt),
            HumanMessage(content=state["message"]),
        ]

        max_iterations = 10
        for i in range(max_iterations):
            response = llm_with_tools.invoke(messages)

            if not response.tool_calls:
                # LLM finished — parse PlanOutput
                extracted = _extract_json(response.content)
                if extracted is not None:
                    try:
                        plan = PlanOutput.model_validate(extracted)
                        return {"plan": plan}
                    except Exception as e:
                        logger.warning("Plan JSON validation failed: %s | raw: %.200s", e, response.content)
                else:
                    logger.warning("Plan response has no parseable JSON: %.200s", response.content)
                return {"plan": None}

            messages.append(_clean_ai_message(response))

            for tc in response.tool_calls:
                tool_fn = _find_tool(tools, tc["name"])
                if tool_fn is None:
                    result = f"Error: unknown tool '{tc['name']}'"
                else:
                    try:
                        result = tool_fn.invoke(tc["args"])
                    except Exception as e:
                        result = f"Error: {e}"
                wrapped = json.dumps({"tool": tc["name"], "args": tc["args"], "output": result}, ensure_ascii=False)
                messages.append(ToolMessage(content=wrapped, tool_call_id=tc["id"]))

        logger.warning("Plan node reached max_iterations=%d without PlanOutput", max_iterations)
        return {"plan": None}
    except Exception:
        logger.exception("Plan node failed unexpectedly")
        return {"plan": None}


# --- Write Shared Node ---


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
    graph.add_node("discover", discover_node)
    graph.add_node("select", select_node)
    graph.add_node("load_l2", load_l2_node)
    graph.add_node("plan", plan_node)
    graph.add_node("write_shared", write_shared_node)

    graph.set_entry_point("discover")
    graph.add_edge("discover", "select")
    graph.add_edge("select", "load_l2")
    graph.add_edge("load_l2", "plan")
    graph.add_conditional_edges("plan", lambda state: "write_shared" if state.get("plan") is not None else "__end__")
    graph.set_finish_point("write_shared")
    return graph.compile()
