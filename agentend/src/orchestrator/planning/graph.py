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
from langgraph.config import get_config
from langgraph.graph import END, StateGraph

from src.app.agent_config import get_agent_config_dir
from src.app.config import settings
from src.orchestrator.agent_utils import dispatchable_agent_id, dispatchable_agent_ids
from src.orchestrator.memory.conversation_memory import ConversationMemoryStore
from src.orchestrator.memory.evolution import EvolutionStore
from src.orchestrator.models import DispatchResult, PlanOutput, TaskDef
from src.orchestrator.planning.prompts import build_reason_prompt
from src.orchestrator.planning.skill_loader import discover_skills
from src.orchestrator.planning.tools import build_tools
from src.schemas.events import EventType, StreamEvent

logger = logging.getLogger(__name__)

_ask_event_queue_var: contextvars.ContextVar[asyncio.Queue | None] = contextvars.ContextVar(
    "ask_event_queue",
    default=None,
)
_backend_client_var: contextvars.ContextVar[Any] = contextvars.ContextVar(
    "backend_client",
    default=None,
)
_cwd_var: contextvars.ContextVar[str] = contextvars.ContextVar("cwd", default="")
_artifact_process_env_var: contextvars.ContextVar[dict[str, str] | None] = contextvars.ContextVar(
    "artifact_process_env",
    default=None,
)
_root_run_id_var: contextvars.ContextVar[str] = contextvars.ContextVar("root_run_id", default="")
_parent_run_id_var: contextvars.ContextVar[str] = contextvars.ContextVar("parent_run_id", default="")
_run_budget_var: contextvars.ContextVar[dict] = contextvars.ContextVar("run_budget", default={})

_pending_reviews: dict[str, asyncio.Event] = {}
_review_results: dict[str, dict[str, str]] = {}


def set_reason_runtime_context(
    *,
    ask_event_queue: asyncio.Queue | None,
    backend_client: Any,
    cwd: str,
    artifact_process_env: dict[str, str] | None = None,
    root_run_id: str = "",
    parent_run_id: str = "",
    budget: dict | None = None,
) -> tuple[contextvars.Token, ...]:
    return (
        _ask_event_queue_var.set(ask_event_queue),
        _backend_client_var.set(backend_client),
        _cwd_var.set(cwd),
        _artifact_process_env_var.set(artifact_process_env or {}),
        _root_run_id_var.set(root_run_id),
        _parent_run_id_var.set(parent_run_id),
        _run_budget_var.set(budget or {}),
    )


def reset_reason_runtime_context(
    tokens: tuple[contextvars.Token, ...],
) -> None:
    _ask_event_queue_var.reset(tokens[0])
    _backend_client_var.reset(tokens[1])
    _cwd_var.reset(tokens[2])
    _artifact_process_env_var.reset(tokens[3])
    _root_run_id_var.reset(tokens[4])
    _parent_run_id_var.reset(tokens[5])
    _run_budget_var.reset(tokens[6])


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
    review_decision: str
    review_message: str
    needs_replan: bool
    replan_reason: str
    summary: str
    iteration: Annotated[int, _add_one]
    max_iterations: int
    memory_messages: Annotated[list, _add]
    # skill_prepare 的中间状态
    system_prompt: str
    pin_context: str
    evolution_context: str
    orchestrator_context: str
    orchestrator: dict
    task_base_path: str


def _skills_dir(shared_dir: str) -> Path:
    config_dir = get_agent_config_dir("orchestrator")
    return Path(shared_dir) / (config_dir or ".orchestrator") / "skills"


def _find_tool(tools: list, name: str):
    for t in tools:
        if t.name == name:
            return t
    return None


def _dispatchable_agent_ids(agents: list[dict] | None) -> set[str]:
    """Return the exact public ids accepted by ask/plan/dispatch paths."""
    return dispatchable_agent_ids(agents)


def _tool_args(tc: dict) -> dict:
    args = tc.get("args", {}) if isinstance(tc, dict) else {}
    return args if isinstance(args, dict) else {}


def _wrapped_tool_message(tc: dict, result: Any) -> ToolMessage:
    """Create one protocol-complete ToolMessage for a model tool call."""
    wrapped = json.dumps(
        {"tool": tc.get("name", ""), "args": _tool_args(tc), "output": result},
        ensure_ascii=False,
        default=str,
    )
    return ToolMessage(content=wrapped, tool_call_id=str(tc.get("id", "")))


def _agent_discovery_succeeded(result: Any) -> bool:
    if not isinstance(result, str):
        return False
    try:
        payload = json.loads(result)
    except (TypeError, json.JSONDecodeError):
        return False
    return isinstance(payload, dict) and isinstance(payload.get("agents"), list)


def _plan_from_tool_call(tc: dict) -> PlanOutput:
    args = _tool_args(tc)
    raw_tasks = args.get("tasks", [])
    if raw_tasks is None:
        raw_tasks = []
    elif not isinstance(raw_tasks, list):
        raise ValueError("tasks must be a list")

    tasks: list[TaskDef] = []
    for raw_task in raw_tasks:
        if not isinstance(raw_task, dict):
            raise ValueError("each task must be an object")
        tasks.append(
            TaskDef(
                task_id=raw_task.get("task_id") or f"task-{len(tasks) + 1:03d}",
                session_id=raw_task.get("session_id") or "",
                title=raw_task.get("title") or "",
                content=raw_task.get("content") or "",
            )
        )

    return PlanOutput(
        overview=args.get("overview") or "",
        tasks=tasks,
        merge_to_main=bool(args.get("merge_to_main", False)),
    )


def _plan_agent_id_error(plan: PlanOutput, agents: list[dict] | None) -> str | None:
    valid_ids = _dispatchable_agent_ids(agents)
    invalid_ids = [task.session_id for task in plan.tasks if task.session_id not in valid_ids]
    if not invalid_ids:
        return None

    invalid_text = ", ".join(repr(agent_id) for agent_id in invalid_ids)
    valid_text = ", ".join(sorted(valid_ids)) or "(none)"
    return f"Error: unknown agent id(s): {invalid_text}. Valid agent ids: {valid_text}"


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
    """将编排计划写入 shared/.agent，供 taskctl 消费者使用。"""
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


def _requires_dispatch_intent(state: GraphState) -> bool:
    """启发式护栏：用于模型声称要分派但未发出工具调用的情况。"""
    if state.get("review_decision") == "discuss":
        return False
    if state.get("review_decision") == "modify":
        return True

    message = state.get("message", "")
    if state.get("review_message") or state.get("replan_reason"):
        return True

    lowered = message.lower()
    explicit_terms = (
        "plan",
        "plan_and_dispatch",
        "规划",
        "分派",
        "调度",
        "执行者",
        "实现者",
        "agent",
    )
    action_terms = (
        "修改",
        "改为",
        "改成",
        "提交",
        "commit",
        "运行",
        "执行",
        "生成",
        "创建",
        "修复",
        "实现",
    )
    code_target_terms = (
        "readme",
        ".md",
        ".py",
        ".go",
        ".ts",
        ".tsx",
        ".js",
        ".jsx",
        ".css",
        ".html",
        "文件",
        "代码",
        "仓库",
        "项目",
    )
    if any(term in lowered or term in message for term in action_terms) and any(
        term in lowered or term in message for term in code_target_terms
    ):
        return True
    return any(term in lowered or term in message for term in explicit_terms) and any(
        term in lowered or term in message for term in action_terms
    )


def _default_dispatch_agent_id(agents: list[dict]) -> str | None:
    for agent in agents:
        if not isinstance(agent, dict):
            continue
        agent_id = dispatchable_agent_id(agent)
        if agent_id:
            return agent_id
    return None


def _fallback_plan_from_text(state: GraphState, text: Any) -> PlanOutput | None:
    agent_id = _default_dispatch_agent_id(state.get("agents", []))
    if not agent_id:
        return None

    content = state.get("message", "")
    overview_text = str(text).strip()
    if not overview_text:
        overview_text = f"将用户请求分派给 {agent_id} 执行。"
    return PlanOutput(
        overview=overview_text,
        merge_to_main=False,
        tasks=[
            TaskDef(
                task_id="task-001",
                session_id=agent_id,
                title="执行用户请求",
                content=content,
            )
        ],
    )


# --- 技能准备节点（快速，数秒内产出 SSE 事件）---


def skill_prepare_node(state: GraphState) -> dict:
    """L1 技能发现 + 提示词构建。数秒内完成。"""
    skills_dir_path = _skills_dir(state["shared_dir"])
    l1_skills = discover_skills(skills_dir_path)

    # Pin 上下文现在由 PinRule 通过 system_prompt_append → state["pin_context"] 提供
    # 只有 evolution 上下文在本地计算
    evolution_context = ""
    try:
        evo = EvolutionStore(state["shared_dir"])
        evolution_context = evo.get_recent_experience(5)
    except Exception:
        pass

    # 系统提示词仅包含身份 + 规则 + 工具（不含动态上下文）
    system_prompt = build_reason_prompt(
        shared_dir=state["shared_dir"],
        l1_skills=l1_skills,
        task_base_path=state.get("task_base_path", ""),
    )

    return {
        "system_prompt": system_prompt,
        "pin_context": state.get("pin_context", ""),
        "evolution_context": evolution_context,
    }


# --- REASON 节点（LLM 工具调用循环）---


async def _handle_ask_agent_call(state: GraphState, tc: dict) -> str:
    args = tc.get("args", {}) if isinstance(tc, dict) else {}
    requested_agent = str(args.get("agent", "")).strip()
    question = str(args.get("question", "")).strip()

    if not requested_agent:
        return "Error: ask_agent requires agent"
    if not question:
        return "Error: ask_agent requires question"

    agents = state.get("agents", [])
    valid_ids = _dispatchable_agent_ids(agents)
    agent_cfg = next(
        (
            a
            for a in agents
            if isinstance(a, dict) and dispatchable_agent_id(a) == requested_agent
        ),
        None,
    )
    if requested_agent not in valid_ids or not agent_cfg:
        valid = ", ".join(sorted(valid_ids)) or "(none)"
        return f"Error: unknown agent id '{requested_agent}'. Valid agent ids: {valid}"

    agent_id = dispatchable_agent_id(agent_cfg)
    agent_type = str(agent_cfg.get("type") or agent_id).strip()
    target_session_id = str(agent_cfg.get("session_id") or "").strip()
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
    child_run_id = str(uuid.uuid4())
    for attempt in range(3):
        try:
            child_run = await backend_client.run_task(
                task_id=state["task_id"],
                session_id=target_session_id,
                message=question,
                agent_type=agent_type,
                cwd=_cwd_var.get(),
                skip_user_message=True,
                root_run_id=_root_run_id_var.get(),
                parent_run_id=_parent_run_id_var.get(),
                budget=_run_budget_var.get(),
                run_id=child_run_id,
            )
            message_id = child_run.message_id
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
        deadline = asyncio.get_running_loop().time() + settings.orchestrator.ask_agent_timeout
        while True:
            remaining = deadline - asyncio.get_running_loop().time()
            if remaining <= 0:
                status = "failed"
                answer_parts.append("Error: ask_agent timed out waiting for subagent response")
                break
            try:
                event = await asyncio.wait_for(
                    stream_iter.__anext__(),
                    timeout=min(remaining, settings.orchestrator.ask_agent_stream_chunk_timeout),
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
                            message_id=message_id,
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
                                message_id=message_id,
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
                            message_id=message_id,
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
    """REASON 节点：LLM 工具调用循环。

    决定 output_type："text"（闲聊）或 "plan"（编排）。
    """
    try:
        llm = ChatOpenAI(
            model=settings.llm.model,
            base_url=settings.llm.base_url,
            api_key=settings.llm.api_key,
            timeout=settings.orchestrator.llm_request_timeout,
        )
        tools = build_tools(
            state["shared_dir"],
            state.get("allowed_read_dirs"),
            state.get("task_base_path"),
            _artifact_process_env_var.get(),
            agents=state.get("agents", []),
        )
        llm_with_tools = llm.bind_tools(tools)

        # 使用 skill_prepare_node 预先构建的系统提示词
        system_prompt = state.get("system_prompt", "")
        messages: list = [SystemMessage(content=system_prompt)]

        # 动态上下文消息 —— 每轮重新注入，不持久化
        if state.get("pin_context"):
            messages.append(SystemMessage(content=state["pin_context"]))
        if state.get("evolution_context"):
            messages.append(SystemMessage(content=state["evolution_context"]))
        if state.get("replan_reason"):
            messages.append(
                HumanMessage(content=f"[重规划请求] 以下任务执行失败，请重新规划：\n{state['replan_reason']}")
            )
        if state.get("orchestrator_context"):
            messages.append(HumanMessage(content=state["orchestrator_context"]))

        # 持久化的对话记忆
        for msg in state.get("memory_messages", []):
            messages.append(msg)

        # 当前用户消息
        messages.append(HumanMessage(content=state["message"]))

        if state.get("review_message"):
            decision = state.get("review_decision", "")
            if decision == "discuss":
                feedback_intro = (
                    "[规划审查反馈] 用户选择继续讨论，尚未批准上一版规划。"
                    "请直接回应用户的讨论内容，不要执行任务；"
                    "只有当用户明确要求修改规划时才产出新的 plan_and_dispatch：\n"
                )
            else:
                feedback_intro = (
                    "[规划审查反馈] 用户请求修改上一版规划，尚未批准执行。"
                    "请根据反馈产出修订后的 plan_and_dispatch，不要执行旧规划：\n"
                )
            messages.append(HumanMessage(content=feedback_intro + state["review_message"]))

        max_iterations = settings.orchestrator.reason_max_iterations
        force_dispatch = _requires_dispatch_intent(state)
        forced_retry_used = False
        # This flag is local to one reason_node invocation. A new replan must
        # discover the current request-local snapshot again.
        agents_discovered = False
        try:
            llm_config = get_config()
        except RuntimeError:
            llm_config = None
        for i in range(max_iterations):
            response = await llm_with_tools.ainvoke(messages, config=llm_config)

            if not response.tool_calls:
                if force_dispatch and not forced_retry_used:
                    logger.warning("Reason node expected plan_and_dispatch but model returned text; forcing retry")
                    forced_retry_used = True
                    messages.append(_clean_ai_message(response))
                    messages.append(
                        HumanMessage(
                            content=(
                                "你刚才只是文字说明，没有真正调用 plan_and_dispatch。"
                                "当前用户请求需要分派给 Agent 执行。请立刻调用 "
                                "plan_and_dispatch 工具，不要输出纯文本。"
                            )
                        )
                    )
                    continue

                if force_dispatch:
                    logger.warning("Reason node still returned text after forced retry; generating fallback plan")
                    plan = _fallback_plan_from_text(state, response.content)
                    if plan is None:
                        return {
                            "output_type": "text",
                            "text": "当前没有可分派 Agent，无法执行该请求。请先添加可用的子 Agent。",
                            "plan": None,
                            "memory_messages": [
                                HumanMessage(content=state["message"]),
                                _clean_ai_message(response),
                            ],
                        }
                    return {
                        "output_type": "plan",
                        "text": "",
                        "plan": plan,
                        "memory_messages": [
                            HumanMessage(content=state["message"]),
                            _clean_ai_message(response),
                        ],
                    }

                return {
                    "output_type": "text",
                    "text": response.content,
                    "plan": None,
                    "memory_messages": [HumanMessage(content=state["message"]), response],
                }

            ask_calls = [tc for tc in response.tool_calls if tc.get("name") == "ask_agent"]
            discovery_in_batch = any(
                tc.get("name") == "list_available_agents" for tc in response.tool_calls
            )
            clean_response = _clean_ai_message(response)
            messages.append(clean_response)
            batch_tool_messages: list[ToolMessage] = []
            discovered_this_round = False
            accepted_plan: PlanOutput | None = None

            for tc in response.tool_calls:
                tool_name = tc.get("name", "")
                args = _tool_args(tc)

                if tool_name == "list_available_agents":
                    tool_fn = _find_tool(tools, tool_name)
                    if tool_fn is None:
                        result = f"Error: unknown tool '{tool_name}'"
                    else:
                        try:
                            result = tool_fn.invoke(args)
                        except Exception as e:
                            result = f"Error: {e}"
                    discovered_this_round = discovered_this_round or _agent_discovery_succeeded(result)

                elif tool_name == "ask_agent":
                    if not agents_discovered or discovery_in_batch:
                        result = (
                            "Error: list_available_agents must complete in a previous tool round "
                            "before using ask_agent or plan_and_dispatch."
                        )
                    else:
                        try:
                            result = await _handle_ask_agent_call(state, tc)
                        except Exception as e:
                            logger.exception("ask_agent tool call failed")
                            result = f"Error: ask_agent failed: {e}"

                elif tool_name == "plan_and_dispatch":
                    try:
                        candidate = _plan_from_tool_call(tc)
                    except Exception as e:
                        candidate = None
                        result = f"Error: invalid plan arguments: {e}"
                    else:
                        needs_discovery = bool(candidate.tasks) and (
                            not agents_discovered or discovery_in_batch
                        )
                        if needs_discovery:
                            result = (
                                "Error: list_available_agents must complete in a previous tool round "
                                "before using ask_agent or plan_and_dispatch."
                            )
                        elif ask_calls:
                            # Do not accept a plan generated before the result
                            # of a same-message ask_agent call is available.
                            result = (
                                "Error: ask_agent must complete before plan_and_dispatch; "
                                "submit the plan in a later tool round."
                            )
                        else:
                            plan_error = _plan_agent_id_error(candidate, state.get("agents", []))
                            if plan_error:
                                result = plan_error
                            else:
                                accepted_plan = accepted_plan or candidate
                                result = "plan_generated"

                else:
                    tool_fn = _find_tool(tools, tool_name)
                    if tool_fn is None:
                        result = f"Error: unknown tool '{tool_name}'"
                    else:
                        try:
                            result = tool_fn.invoke(args)
                        except Exception as e:
                            result = f"Error: {e}"

                if tool_name == "plan_and_dispatch" and result == "plan_generated":
                    tool_message = ToolMessage(content="plan_generated", tool_call_id=str(tc.get("id", "")))
                elif tool_name == "list_available_agents" and _agent_discovery_succeeded(result):
                    # Keep the discovery contract directly parseable by the
                    # model instead of nesting the JSON under an audit wrapper.
                    tool_message = ToolMessage(content=result, tool_call_id=str(tc.get("id", "")))
                else:
                    tool_message = _wrapped_tool_message(tc, result)
                batch_tool_messages.append(tool_message)
                messages.append(tool_message)

            # A discovery result becomes usable only after this whole
            # assistant tool batch has completed. Thus discovery + use in one
            # AIMessage is always rejected and retried in the next round.
            if discovered_this_round:
                agents_discovered = True

            if accepted_plan is not None:
                return {
                    "output_type": "plan",
                    "text": "",
                    "plan": accepted_plan,
                    "memory_messages": [
                        HumanMessage(content=state["message"]),
                        clean_response,
                        *batch_tool_messages,
                    ],
                }

        logger.warning("Reason node reached max_iterations=%d", max_iterations)
        if force_dispatch and not _dispatchable_agent_ids(state.get("agents", [])):
            return {
                "output_type": "text",
                "text": "当前没有可分派 Agent，无法执行该请求。请先添加可用的子 Agent。",
                "plan": None,
                "memory_messages": [HumanMessage(content=state["message"])],
            }
        return {
            "output_type": "text",
            "text": "规划超时，请重新描述需求",
            "plan": None,
            "memory_messages": [HumanMessage(content=state["message"])],
        }
    except Exception as e:
        logger.exception("Reason node failed unexpectedly")
        error_text = str(e).strip() or e.__class__.__name__
        return {
            "output_type": "error",
            "text": f"Orchestrator 推理失败：{e.__class__.__name__}: {error_text}",
            "plan": None,
        }


# --- DISPATCH 节点 ---


async def human_review_node(state: GraphState) -> dict:
    """在计划生成后暂停，等待用户批准或提出修改意见。"""
    orchestrator_cfg = state.get("orchestrator", {}) or {}
    session_id = str(orchestrator_cfg.get("session_id") or state.get("task_id", ""))
    if not session_id:
        return {"review_decision": "approve", "review_message": ""}

    event = asyncio.Event()
    _pending_reviews[session_id] = event

    queue: asyncio.Queue | None = _ask_event_queue_var.get()
    if queue is None:
        _pending_reviews.pop(session_id, None)
        return {"review_decision": "approve", "review_message": ""}

    plan = state.get("plan")
    review_key = f"{session_id}:{uuid.uuid4().hex}"
    await queue.put(
        StreamEvent.create(
            EventType.PLAN_REVIEW,
            session_id=session_id,
            task_id=state.get("task_id", ""),
            review_key=review_key,
            plan=plan.model_dump() if plan else {},
            waves=[],
        )
    )

    try:
        await asyncio.wait_for(event.wait(), timeout=settings.orchestrator.review_timeout)
        result = _review_results.get(session_id, {})
        return {
            "review_decision": result.get("action", "approve"),
            "review_message": result.get("content", ""),
        }
    except asyncio.TimeoutError:
        logger.warning("Plan review timed out for session=%s; auto-approving", session_id)
        return {
            "review_decision": "approve",
            "review_message": "审查超时，自动继续执行。",
        }
    finally:
        _pending_reviews.pop(session_id, None)
        _review_results.pop(session_id, None)


async def wait_for_external_review(session_id: str) -> dict[str, str]:
    event = asyncio.Event()
    _pending_reviews[session_id] = event
    try:
        await asyncio.wait_for(event.wait(), timeout=settings.orchestrator.review_timeout)
        return _review_results.get(session_id, {})
    except asyncio.TimeoutError:
        logger.warning("External review timed out for session=%s; auto-approving", session_id)
        return {"action": "approve", "content": "审查超时，自动继续执行。"}
    finally:
        _pending_reviews.pop(session_id, None)
        _review_results.pop(session_id, None)


def dispatch_node(state: GraphState) -> dict:
    """将 PlanOutput 转换为 DispatchResults，并按拓扑排序分波次。"""
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


# --- REVIEW 节点 ---


def review_node(state: GraphState) -> dict:
    """检查任务结果是否有失败。若存在失败且未达迭代上限，则设置 needs_replan。"""
    task_results = state.get("task_results", [])
    failed = [tr for tr in task_results if not tr.get("success", True)]

    iteration = state.get("iteration", 0)
    max_iterations = state.get("max_iterations", settings.orchestrator.replan_max_iterations)

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


# --- EVOLVE 节点 ---


def evolve_node(state: GraphState) -> dict:
    """将编排经验记录到 EvolutionStore。"""
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


# --- SAVE_MEM 节点 ---


def save_mem_node(state: GraphState) -> dict:
    """在 graph 完成前将 memory_messages 持久化到基于文件的存储。

    使用 ``replace_messages`` 直接写入权威状态，
    避免 ``save_messages``（读取 + 追加）可能导致的重复。
    """
    try:
        memory_messages = state.get("memory_messages", [])
        if memory_messages:
            store = ConversationMemoryStore(state["shared_dir"])
            store.replace_messages(memory_messages)
    except Exception:
        logger.exception("save_mem_node: failed to persist conversation memory")
    return {}


# --- 条件路由 ---


def route_by_output_type(state: GraphState) -> str:
    output_type = state.get("output_type", "error")
    if output_type == "text":
        return "save_mem"
    elif output_type == "plan":
        return "human_review"
    else:
        return END


def route_by_review_decision(state: GraphState) -> str:
    decision = state.get("review_decision", "approve")
    if decision == "approve":
        return "dispatch"
    return "reason"


def route_by_review(state: GraphState) -> str:
    if state.get("needs_replan", False):
        return "skill_prepare"
    return "evolve"


# --- Graph 构建器 ---


def build_graph() -> StateGraph:
    graph = StateGraph(GraphState)

    graph.add_node("skill_prepare", skill_prepare_node)
    graph.add_node("reason", reason_node)
    graph.add_node("human_review", human_review_node)
    graph.add_node("dispatch", dispatch_node)
    graph.add_node("execute", _execute_placeholder)
    graph.add_node("review", review_node)
    graph.add_node("evolve", evolve_node)
    graph.add_node("save_mem", save_mem_node)

    graph.set_entry_point("skill_prepare")

    graph.add_edge("skill_prepare", "reason")
    graph.add_conditional_edges("reason", route_by_output_type)
    graph.add_conditional_edges("human_review", route_by_review_decision)
    graph.add_edge("dispatch", "execute")
    graph.add_edge("execute", "review")
    graph.add_conditional_edges("review", route_by_review)
    graph.add_edge("evolve", "save_mem")
    graph.set_finish_point("save_mem")

    return graph.compile()


def _execute_placeholder(state: GraphState) -> dict:
    """占位 execute 节点 —— 实际执行由 OrchestratorAdapter 处理。"""
    return {"task_results": []}


def submit_plan_review(session_id: str, action: str, content: str = "") -> bool:
    event = _pending_reviews.get(session_id)
    if event is None:
        return False
    _review_results[session_id] = {"action": action, "content": content}
    event.set()
    return True


def has_pending_plan_review(session_id: str) -> bool:
    return session_id in _pending_reviews
