from __future__ import annotations

import asyncio
import contextvars
import json
import logging
import uuid
from pathlib import Path
from typing import Annotated, Any, TypedDict

import yaml
from langchain_core.messages import AIMessage, HumanMessage, SystemMessage, ToolMessage
from langchain_openai import ChatOpenAI
from langgraph.checkpoint.memory import MemorySaver
from langgraph.graph import END, StateGraph

from src.app.agent_config import get_agent_config_dir
from src.app.config import settings
from src.orchestrator.memory.evolution import EvolutionStore
from src.orchestrator.models import DispatchResult, PlanOutput, TaskDef
from src.orchestrator.planning.prompts import build_reason_prompt
from src.orchestrator.planning.skill_loader import (
    discover_skills,
    load_l2_content,
    select_skills,
)
from src.orchestrator.planning.tools import build_tools
from src.schemas.events import EventType, StreamEvent

logger = logging.getLogger(__name__)

_ASK_AGENT_EVENT_TIMEOUT_SECONDS = 180.0

_ask_event_queue_var: contextvars.ContextVar[asyncio.Queue | None] = contextvars.ContextVar(
    "ask_event_queue",
    default=None,
)
_backend_client_var: contextvars.ContextVar[Any] = contextvars.ContextVar(
    "backend_client",
    default=None,
)
_cwd_var: contextvars.ContextVar[str] = contextvars.ContextVar("cwd", default="")


def set_reason_runtime_context(
    *,
    ask_event_queue: asyncio.Queue | None,
    backend_client: Any,
    cwd: str,
) -> tuple[contextvars.Token, contextvars.Token, contextvars.Token]:
    return (
        _ask_event_queue_var.set(ask_event_queue),
        _backend_client_var.set(backend_client),
        _cwd_var.set(cwd),
    )


def reset_reason_runtime_context(tokens: tuple[contextvars.Token, contextvars.Token, contextvars.Token]) -> None:
    _ask_event_queue_var.reset(tokens[0])
    _backend_client_var.reset(tokens[1])
    _cwd_var.reset(tokens[2])


def _add(left: list, right: list) -> list:
    return left + right


def _add_one(left: int, right: int) -> int:
    return left + right


class GraphState(TypedDict):
    message: str
    agents: list[dict]
    task_id: str
    shared_dir: str
    allowed_read_dirs: list[str]
    output_type: str  # "text" | "plan" | "error"
    text: str
    plan: PlanOutput | None
    dispatch_results: list[DispatchResult]
    execution_waves: list[list[DispatchResult]]
    task_results: Annotated[list, _add]
    task_status: dict
    needs_replan: bool
    replan_reason: str
    summary: str
    iteration: Annotated[int, _add_one]
    max_iterations: int
    memory_messages: Annotated[list, _add]
    # skill_prepare intermediate state
    system_prompt: str
    orchestrator: dict


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


def _skills_dir(shared_dir: str) -> Path:
    config_dir = get_agent_config_dir("orchestrator")
    return Path(shared_dir) / (config_dir or ".orchestrator") / "skills"


def _find_tool(tools: list, name: str):
    for t in tools:
        if t.name == name:
            return t
    return None


def _clean_ai_message(msg: AIMessage) -> AIMessage:
    if "reasoning_content" not in msg.additional_kwargs:
        return msg
    kw = {k: v for k, v in msg.additional_kwargs.items() if k != "reasoning_content"}
    return AIMessage(content=msg.content, tool_calls=msg.tool_calls, additional_kwargs=kw, id=msg.id)


def _write_shared_plan(
    shared_dir: str,
    task_id: str,
    plan: PlanOutput,
    dispatch_results: list[DispatchResult],
) -> None:
    """Write the orchestration plan into shared/.agent for taskctl consumers."""
    shared = Path(shared_dir).resolve()
    plans_dir = shared / "plans"
    plans_dir.mkdir(parents=True, exist_ok=True)
    (shared / "memory" / "common").mkdir(parents=True, exist_ok=True)

    (plans_dir / "overview.md").write_text(plan.overview, encoding="utf-8")

    config_tasks: list[dict] = []
    by_task_id = {dr.task_id: dr for dr in dispatch_results}
    for task in plan.tasks:
        plan_file = f"plans/{task.task_id}.md"
        dr = by_task_id.get(task.task_id)
        session_id = dr.real_session_id if dr and dr.real_session_id else task.session_id
        agent_id = dr.agent if dr else task.session_id
        agent_type = dr.agent_type if dr else ""

        body = "\n".join(
            [
                f"# {task.title or task.task_id}",
                "",
                f"- task_id: {task.task_id}",
                f"- agent: {agent_id}",
                f"- agent_type: {agent_type}",
                f"- session_id: {session_id}",
                "",
                "## Task",
                "",
                task.content,
                "",
            ]
        )
        (shared / plan_file).write_text(body, encoding="utf-8")
        config_tasks.append(
            {
                "task_id": task.task_id,
                "session_id": session_id,
                "agent": agent_id,
                "agent_type": agent_type,
                "file": plan_file,
            }
        )

    config = {
        "task_id": task_id,
        "overview_file": "plans/overview.md",
        "tasks": config_tasks,
    }
    (shared / "config.yaml").write_text(
        yaml.safe_dump(config, allow_unicode=True, sort_keys=False),
        encoding="utf-8",
    )


# --- Skill Prepare Node (fast, yields SSE event within seconds) ---


def skill_prepare_node(state: GraphState) -> dict:
    """L1 → L2 skill discovery + prompt construction. Runs in seconds."""
    skills_dir_path = _skills_dir(state["shared_dir"])
    l1_skills = discover_skills(skills_dir_path)

    selection_message = state["replan_reason"] if state.get("replan_reason") else state["message"]
    selected_names = select_skills(l1_skills, selection_message)
    l2 = load_l2_content(selected_names, skills_dir_path)

    agents_desc = _build_agents_desc(state["agents"])
    system_prompt = build_reason_prompt(
        agents_desc=agents_desc,
        message=state["message"],
        shared_dir=state["shared_dir"],
        l2_content=l2,
        replan_reason=state.get("replan_reason"),
    )

    return {"system_prompt": system_prompt}


# --- REASON Node (LLM tool-calling loop) ---


async def _handle_ask_agent_call(state: GraphState, tc: dict) -> str:
    args = tc.get("args", {}) if isinstance(tc, dict) else {}
    requested_agent = str(args.get("agent", "")).strip()
    question = str(args.get("question", "")).strip()

    if not requested_agent:
        return "Error: ask_agent requires agent"
    if not question:
        return "Error: ask_agent requires question"

    agents = state.get("agents", [])
    agent_cfg = next((a for a in agents if requested_agent == str(a.get("id", ""))), None)
    if not agent_cfg:
        valid = ", ".join(str(a.get("id", "")) for a in agents)
        return f"Error: unknown agent id '{requested_agent}'. Use an exact id from the available Agents list: {valid}"

    agent_id = str(agent_cfg.get("id", requested_agent))
    agent_type = str(agent_cfg.get("type", agent_id))
    target_session_id = str(agent_cfg.get("session_id", ""))
    if not target_session_id:
        return f"Error: agent '{agent_id}' has no session_id"
    if agent_type == "orchestrator":
        return "Error: ask_agent cannot target orchestrator itself"

    backend_client = _backend_client_var.get()
    if backend_client is None:
        return "Error: backend client unavailable for ask_agent"

    question_id = f"q-{uuid.uuid4().hex[:12]}"
    source_cfg = state.get("orchestrator", {}) or {}
    source_agent = str(source_cfg.get("id") or source_cfg.get("name") or "orchestrator")
    source_agent_type = str(source_cfg.get("type") or "orchestrator")
    source_session_id = str(source_cfg.get("session_id") or "")
    queue: asyncio.Queue | None = _ask_event_queue_var.get()
    if queue is not None:
        await queue.put(
            StreamEvent.create(
                EventType.ASK_CARD_START,
                question_id=question_id,
                source_agent=source_agent,
                source_agent_type=source_agent_type,
                source_session_id=source_session_id,
                target_agent=agent_id,
                target_agent_type=agent_type,
                target_session_id=target_session_id,
                question=question,
            )
        )

    answer_parts: list[str] = []
    status = "completed"
    message_id = ""
    last_run_error: Exception | None = None
    for attempt in range(3):
        try:
            message_id = await backend_client.run_task(
                task_id=state["task_id"],
                session_id=target_session_id,
                message=question,
                agent_type=agent_type,
                cwd=_cwd_var.get(),
                skip_user_message=False,
            )
            last_run_error = None
            break
        except Exception as e:
            last_run_error = e
            logger.warning(
                "ask_agent run_task attempt %d/3 failed for agent=%s: %s",
                attempt + 1,
                agent_id,
                e,
            )
            if attempt < 2:
                await asyncio.sleep(1.0 * (attempt + 1))
    if last_run_error is not None:
        raise last_run_error

    try:
        stream = backend_client.stream_result(
            task_id=state["task_id"],
            message_id=message_id,
            session_id=target_session_id,
        )
        stream_iter = stream.__aiter__()
        deadline = asyncio.get_running_loop().time() + _ASK_AGENT_EVENT_TIMEOUT_SECONDS
        while True:
            remaining = deadline - asyncio.get_running_loop().time()
            if remaining <= 0:
                status = "failed"
                answer_parts.append("Error: ask_agent timed out waiting for subagent response")
                break
            try:
                event = await asyncio.wait_for(
                    stream_iter.__anext__(),
                    timeout=min(remaining, 30.0),
                )
            except StopAsyncIteration:
                break
            except asyncio.TimeoutError:
                continue

            event_type = event.get("type")
            if event_type == "heartbeat":
                continue
            content = event.get("content") or {}
            if event_type == EventType.TEXT.value:
                text = str(content.get("text", ""))
                answer_parts.append(text)
                if queue is not None and text:
                    await queue.put(
                        StreamEvent.create(
                            EventType.TEXT,
                            text=text,
                            agent=agent_id,
                            agent_type=agent_type,
                        )
                    )
            elif event_type == EventType.DONE.value:
                done_text = str(content.get("text", ""))
                if done_text and not answer_parts:
                    answer_parts.append(done_text)
                    if queue is not None:
                        await queue.put(
                            StreamEvent.create(
                                EventType.TEXT,
                                text=done_text,
                                agent=agent_id,
                                agent_type=agent_type,
                            )
                        )
                break
            elif event_type == EventType.ERROR.value:
                status = "failed"
                error_text = str(content.get("error") or content.get("message") or "Subagent error")
                answer_parts.append(error_text)
                if queue is not None:
                    await queue.put(
                        StreamEvent.create(
                            EventType.TEXT,
                            text=f"[Error] {error_text}",
                            agent=agent_id,
                            agent_type=agent_type,
                        )
                    )
                break
    except Exception as e:
        logger.exception("ask_agent failed: agent=%s session=%s", agent_id, target_session_id)
        status = "failed"
        answer_parts.append(f"Error: {e}")

    answer = "".join(answer_parts).strip()
    if not answer:
        answer = "(no answer)"
    summary = answer.replace("\n", " ")[:120]

    if queue is not None:
        await queue.put(
            StreamEvent.create(
                EventType.ASK_CARD_DONE,
                question_id=question_id,
                source_agent=source_agent,
                source_agent_type=source_agent_type,
                source_session_id=source_session_id,
                target_agent=agent_id,
                target_agent_type=agent_type,
                target_session_id=target_session_id,
                question=question,
                summary=summary,
                status=status,
            )
        )

    return answer


async def reason_node(state: GraphState) -> dict:
    """REASON node: LLM tool-calling loop.

    Determines output_type: "text" (chitchat) or "plan" (orchestration).
    """
    try:
        llm = ChatOpenAI(
            model=settings.llm.model,
            base_url=settings.llm.base_url,
            api_key=settings.llm.api_key,
        )
        tools = build_tools(state["shared_dir"], state.get("allowed_read_dirs"))
        llm_with_tools = llm.bind_tools(tools)

        # Use pre-built system prompt from skill_prepare_node
        system_prompt = state.get("system_prompt", "")
        messages: list = [SystemMessage(content=system_prompt)]

        for msg in state.get("memory_messages", []):
            messages.append(msg)

        messages.append(HumanMessage(content=state["message"]))

        if state.get("replan_reason"):
            messages.append(
                HumanMessage(content=f"[重规划请求] 以下任务执行失败，请重新规划：\n{state['replan_reason']}")
            )

        max_iterations = 10
        for i in range(max_iterations):
            response = await llm_with_tools.ainvoke(messages)

            if not response.tool_calls:
                return {
                    "output_type": "text",
                    "text": response.content,
                    "plan": None,
                    "memory_messages": [HumanMessage(content=state["message"]), response],
                }

            plan_call = None
            ask_calls = []
            other_calls = []
            for tc in response.tool_calls:
                if tc["name"] == "plan_and_dispatch":
                    plan_call = tc
                elif tc["name"] == "ask_agent":
                    ask_calls.append(tc)
                else:
                    other_calls.append(tc)

            if ask_calls:
                messages.append(_clean_ai_message(response))
                for tc in response.tool_calls:
                    if tc["name"] == "plan_and_dispatch":
                        continue
                    if tc["name"] == "ask_agent":
                        result = await _handle_ask_agent_call(state, tc)
                    else:
                        tool_fn = _find_tool(tools, tc["name"])
                        if tool_fn:
                            try:
                                result = tool_fn.invoke(tc["args"])
                            except Exception as e:
                                result = f"Error: {e}"
                        else:
                            result = f"Error: unknown tool '{tc['name']}'"
                    wrapped = json.dumps(
                        {"tool": tc["name"], "args": tc["args"], "output": result},
                        ensure_ascii=False,
                    )
                    messages.append(ToolMessage(content=wrapped, tool_call_id=tc["id"]))
                continue

            if plan_call is not None:
                args = plan_call["args"]
                overview = args.get("overview", "")
                raw_tasks = args.get("tasks", [])

                tasks = []
                for t in raw_tasks:
                    if isinstance(t, dict):
                        tasks.append(
                            TaskDef(
                                task_id=t.get("task_id", f"task-{len(tasks) + 1:03d}"),
                                session_id=t.get("session_id", ""),
                                title=t.get("title", ""),
                                content=t.get("content", ""),
                            )
                        )

                plan = PlanOutput(overview=overview, tasks=tasks)
                tool_messages = [
                    ToolMessage(content="plan_generated", tool_call_id=plan_call["id"]),
                ]
                for tc in other_calls:
                    tool_fn = _find_tool(tools, tc["name"])
                    if tool_fn:
                        try:
                            result = tool_fn.invoke(tc["args"])
                        except Exception as e:
                            result = f"Error: {e}"
                    else:
                        result = f"Error: unknown tool '{tc['name']}'"
                    wrapped = json.dumps(
                        {"tool": tc["name"], "args": tc["args"], "output": result},
                        ensure_ascii=False,
                    )
                    tool_messages.append(ToolMessage(content=wrapped, tool_call_id=tc["id"]))

                return {
                    "output_type": "plan",
                    "text": "",
                    "plan": plan,
                    "memory_messages": [
                        HumanMessage(content=state["message"]),
                        _clean_ai_message(response),
                        *tool_messages,
                    ],
                }

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
                wrapped = json.dumps(
                    {"tool": tc["name"], "args": tc["args"], "output": result},
                    ensure_ascii=False,
                )
                messages.append(ToolMessage(content=wrapped, tool_call_id=tc["id"]))

        logger.warning("Reason node reached max_iterations=%d", max_iterations)
        return {
            "output_type": "text",
            "text": "规划超时，请重新描述需求",
            "plan": None,
            "memory_messages": [HumanMessage(content=state["message"])],
        }
    except Exception:
        logger.exception("Reason node failed unexpectedly")
        return {
            "output_type": "error",
            "text": "Reasoning failed",
            "plan": None,
        }


# --- DISPATCH Node ---


def dispatch_node(state: GraphState) -> dict:
    """Convert PlanOutput to DispatchResults and topologically sort into waves."""
    from src.orchestrator.execution.dispatcher import Dispatcher, topological_sort

    plan = state["plan"]
    if not plan:
        return {"dispatch_results": [], "execution_waves": []}

    dispatcher = Dispatcher(state["agents"])
    dispatch_results = dispatcher.dispatch(plan)
    waves = topological_sort(dispatch_results)
    try:
        _write_shared_plan(state["shared_dir"], state["task_id"], plan, dispatch_results)
    except Exception:
        logger.exception("Failed to write orchestrator plan into shared dir")

    return {"dispatch_results": dispatch_results, "execution_waves": waves}


# --- REVIEW Node ---


def review_node(state: GraphState) -> dict:
    """Check task results for failures. Set needs_replan if failures exist and under iteration limit."""
    task_results = state.get("task_results", [])
    failed = [tr for tr in task_results if not tr.get("success", True)]

    iteration = state.get("iteration", 0)
    max_iterations = state.get("max_iterations", 3)

    if not failed:
        return {"needs_replan": False, "replan_reason": ""}

    if iteration >= max_iterations:
        logger.warning("Review: max_iterations=%d reached, accepting partial results", max_iterations)
        return {"needs_replan": False, "replan_reason": ""}

    failure_details = []
    for tr in failed:
        failure_details.append(
            f"- 任务 {tr.get('task_id', '?')} (agent: {tr.get('agent', '?')}): {tr.get('content', '')[:200]}"
        )
    replan_reason = "以下任务执行失败，请重新规划：\n" + "\n".join(failure_details)

    return {"needs_replan": True, "replan_reason": replan_reason, "iteration": 1}


# --- EVOLVE Node ---


def evolve_node(state: GraphState) -> dict:
    """Record orchestration experience in EvolutionStore."""
    try:
        evolution = EvolutionStore(state["shared_dir"])
        plan = state.get("plan")
        overview = plan.overview if plan else ""
        task_results = state.get("task_results", [])
        all_success = all(tr.get("success", True) for tr in task_results)
        results_summary = "; ".join(
            f"{tr.get('task_id', '?')}: {'✅' if tr.get('success') else '❌'}" for tr in task_results
        )
        evolution.record(
            message=state["message"],
            plan_summary=overview[:200],
            results_summary=results_summary[:200],
            success=all_success,
            agent_performance=[
                {
                    "agent_id": tr.get("agent", ""),
                    "success": tr.get("success", False),
                    "duration": tr.get("duration", 0),
                }
                for tr in task_results
            ],
        )
    except Exception:
        logger.exception("Evolve node failed")
    return {}


# --- SAVE_MEM Node ---


def save_mem_node(state: GraphState) -> dict:
    return {}


# --- Conditional Routers ---


def route_by_output_type(state: GraphState) -> str:
    output_type = state.get("output_type", "error")
    if output_type == "text":
        return "save_mem"
    elif output_type == "plan":
        return "dispatch"
    else:
        return END


def route_by_review(state: GraphState) -> str:
    if state.get("needs_replan", False):
        return "skill_prepare"
    return "evolve"


# --- Graph Builder ---


def build_graph() -> StateGraph:
    graph = StateGraph(GraphState)

    graph.add_node("skill_prepare", skill_prepare_node)
    graph.add_node("reason", reason_node)
    graph.add_node("dispatch", dispatch_node)
    graph.add_node("execute", _execute_placeholder)
    graph.add_node("review", review_node)
    graph.add_node("evolve", evolve_node)
    graph.add_node("save_mem", save_mem_node)

    graph.set_entry_point("skill_prepare")

    graph.add_edge("skill_prepare", "reason")
    graph.add_conditional_edges("reason", route_by_output_type)
    graph.add_edge("dispatch", "execute")
    graph.add_edge("execute", "review")
    graph.add_conditional_edges("review", route_by_review)
    graph.add_edge("evolve", "save_mem")
    graph.set_finish_point("save_mem")

    memory = MemorySaver()
    return graph.compile(checkpointer=memory)


def _execute_placeholder(state: GraphState) -> dict:
    """Placeholder execute node — actual execution is handled by OrchestratorAdapter."""
    return {"task_results": []}
