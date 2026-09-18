from __future__ import annotations

import asyncio
import contextvars
import json
import logging
import re
import time
import uuid
from pathlib import Path
from typing import Annotated, Any, TypedDict

import yaml
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langchain_openai import ChatOpenAI
from langgraph.checkpoint.memory import MemorySaver
from langgraph.config import get_config
from langgraph.graph import StateGraph

from src.app.agent_config import get_agent_config_dir
from src.app.config import settings
from src.orchestrator.agent_utils import dispatchable_agent_id, dispatchable_agent_ids
from src.orchestrator.memory.context_compactor import (
    ContextCompactor,
    estimate_messages_tokens,
    estimate_text_tokens,
    estimate_tools_tokens,
)
from src.orchestrator.memory.conversation_memory import (
    ConversationMemoryError,
    ConversationMemoryStore,
    ConversationSummary,
    RevisionConflict,
)
from src.orchestrator.memory.evolution import EvolutionStore
from src.orchestrator.models import DispatchResult, PlanOutput, TaskDef, TaskResult
from src.orchestrator.planning.context_builder import (
    ActivePinSnapshot,
    build_reason_messages,
    render_active_pin_snapshot,
)
from src.orchestrator.planning.prompts import build_reason_prompt
from src.orchestrator.planning.skill_loader import discover_skills
from src.orchestrator.planning.tools import build_tools, filter_allowed_tools
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
_current_run_id_var: contextvars.ContextVar[str] = contextvars.ContextVar("current_run_id", default="")
_run_budget_var: contextvars.ContextVar[dict] = contextvars.ContextVar("run_budget", default={})
_execution_event_queue_var: contextvars.ContextVar[asyncio.Queue | None] = contextvars.ContextVar(
    "execution_event_queue",
    default=None,
)
_workspace_manager_var: contextvars.ContextVar[Any] = contextvars.ContextVar("workspace_manager", default=None)
_repo_path_var: contextvars.ContextVar[str] = contextvars.ContextVar("repo_path", default="")
_integration_service_var: contextvars.ContextVar[Any] = contextvars.ContextVar(
    "integration_service",
    default=None,
)

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
    current_run_id: str = "",
    budget: dict | None = None,
    execution_event_queue: asyncio.Queue | None = None,
    workspace_mgr: Any = None,
    repo_path: str = "",
    integration_service: Any = None,
) -> tuple[contextvars.Token, ...]:
    return (
        _ask_event_queue_var.set(ask_event_queue),
        _backend_client_var.set(backend_client),
        _cwd_var.set(cwd),
        _artifact_process_env_var.set(artifact_process_env or {}),
        _root_run_id_var.set(root_run_id),
        _parent_run_id_var.set(parent_run_id),
        _current_run_id_var.set(current_run_id),
        _run_budget_var.set(budget or {}),
        _execution_event_queue_var.set(execution_event_queue),
        _workspace_manager_var.set(workspace_mgr),
        _repo_path_var.set(repo_path),
        _integration_service_var.set(integration_service),
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
    _current_run_id_var.reset(tokens[6])
    _run_budget_var.reset(tokens[7])
    _execution_event_queue_var.reset(tokens[8])
    _workspace_manager_var.reset(tokens[9])
    _repo_path_var.reset(tokens[10])
    _integration_service_var.reset(tokens[11])


def _add(left: list, right: list) -> list:
    return left + right


def _merge_turn_messages(left: list, right: list) -> list:
    """Merge Run-local transcript deltas without restoring a prior Run.

    LangGraph's checkpointer may provide the previous value of a reducer
    channel when a caller accidentally reuses a ``thread_id``.  The first
    input of every AgentEnd Run is explicitly tagged as ``user_request``;
    treat that marker as a new transcript boundary and replace the stale
    checkpoint value.  Review/replan Human messages and AI/Tool deltas do not
    carry the marker and continue to append within the current Run.
    """
    incoming = list(right or [])
    if any(
        isinstance(message, HumanMessage)
        and (getattr(message, "additional_kwargs", None) or {}).get("memory_kind")
        == "user_request"
        for message in incoming
    ):
        return incoming
    return list(left or []) + incoming


def _add_one(left: int, right: int) -> int:
    return left + right


def _summary_from_state(value: Any) -> ConversationSummary:
    """Accept legacy in-process objects and checkpoint-safe summary dicts."""
    if value is None:
        return ConversationSummary()
    if isinstance(value, ConversationSummary):
        return value
    if isinstance(value, dict):
        return ConversationSummary.from_dict(value)
    raise ValueError("memory_summary must be a ConversationSummary or object")


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
    task_results: list
    task_status: dict
    review_decision: str
    review_message: str
    needs_replan: bool
    replan_reason: str
    summary: str
    iteration: int
    max_iterations: int
    memory_messages: list
    memory_revision: int
    # Keep checkpoint state JSON/msgpack friendly.  Direct helper callers may
    # still provide a ConversationSummary; nodes normalize both forms.
    memory_summary: dict
    turn_messages: Annotated[list, _merge_turn_messages]
    # skill_prepare 的中间状态
    system_prompt: str
    system_constraints: list[str]
    active_pin_snapshot: ActivePinSnapshot
    reference_contexts: list[str]
    capability_hints: list[str]
    allowed_tools: list[str] | None
    evolution_context: str
    orchestrator: dict
    task_base_path: str
    awaiting_user: bool
    final_status: str


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
        depends_on = raw_task.get("depends_on") or []
        if not isinstance(depends_on, list) or not all(isinstance(item, str) for item in depends_on):
            raise ValueError("depends_on must be a list of task IDs")
        tasks.append(
            TaskDef(
                task_id=raw_task.get("task_id") or f"task-{len(tasks) + 1:03d}",
                session_id=raw_task.get("session_id") or "",
                title=raw_task.get("title") or "",
                content=raw_task.get("content") or "",
                depends_on=list(depends_on),
                requires_integrated_dependencies=bool(raw_task.get("requires_integrated_dependencies", True)),
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


def _plan_dependency_error(plan: PlanOutput, user_message: str = "") -> str | None:
    """Reject malformed or text-only dependency graphs before review/dispatch."""
    task_ids = [task.task_id for task in plan.tasks]
    known_ids = set(task_ids)
    if len(known_ids) != len(task_ids):
        return "Error: invalid plan dependencies: task IDs must be unique"

    incoming: dict[str, int] = {task_id: 0 for task_id in task_ids}
    dependents: dict[str, list[str]] = {task_id: [] for task_id in task_ids}
    for task in plan.tasks:
        if len(set(task.depends_on)) != len(task.depends_on):
            return f"Error: invalid plan dependencies: {task.task_id} contains duplicate depends_on entries"
        for dependency in task.depends_on:
            if dependency not in known_ids:
                return (
                    f"Error: invalid plan dependencies: {task.task_id} depends on unknown task {dependency}"
                )
            if dependency == task.task_id:
                return f"Error: invalid plan dependencies: {task.task_id} depends on itself"
            incoming[task.task_id] += 1
            dependents[dependency].append(task.task_id)

    remaining = dict(incoming)
    while remaining:
        ready = [task_id for task_id, degree in remaining.items() if degree == 0]
        if not ready:
            return "Error: invalid plan dependencies: dependency graph contains a cycle"
        for task_id in ready:
            del remaining[task_id]
            for dependent in dependents[task_id]:
                if dependent in remaining:
                    remaining[dependent] -= 1

    dependency_claim = re.compile(
        r"前置条件|等待.{0,80}完成|依赖.{0,80}(?:完成|成功|集成)|在.{1,60}都完成后|"
        r"\bdepends_on\b|\bafter\s+(?:task|tasks|both|all)\b",
        re.IGNORECASE | re.DOTALL,
    )
    for task in plan.tasks:
        if not task.depends_on and dependency_claim.search(f"{task.title}\n{task.content}"):
            return (
                f"Error: invalid plan dependencies: {task.task_id} describes prerequisites in text "
                "but depends_on is empty; encode every prerequisite as a task ID"
            )

    if len(plan.tasks) > 1 and not any(task.depends_on for task in plan.tasks):
        if "depends_on" in user_message.lower():
            return (
                "Error: invalid plan dependencies: the user explicitly required depends_on edges "
                "but the plan contains none"
            )
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
                f"- depends_on: {', '.join(task.depends_on) if task.depends_on else '(none)'}",
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
                "depends_on": task.depends_on,
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

    # Evolution 是每轮重建的 reference，不进入持久化 transcript。
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
        "evolution_context": evolution_context,
    }


async def compact_context_node(state: GraphState) -> dict:
    """Compact old committed turns before Reason when the full prompt crosses the trigger."""
    snapshot = state.get("active_pin_snapshot")
    if not snapshot:
        return {
            "output_type": "error",
            "text": "Orchestrator 上下文准备失败：Active Pin Snapshot 缺失",
        }

    try:
        pin_tokens = estimate_text_tokens(
            render_active_pin_snapshot(snapshot, expected_task_id=state.get("task_id"))
        )
    except (KeyError, TypeError, ValueError) as exc:
        logger.warning("pin.snapshot_invalid=1 error_type=%s", exc.__class__.__name__)
        return {
            "output_type": "error",
            "text": "Orchestrator 上下文准备失败：Active Pin Snapshot 无效",
        }
    logger.info(
        "pin.snapshot_count=%d pin.snapshot_tokens=%d",
        len(snapshot.get("pins", [])),
        pin_tokens,
    )
    if pin_tokens > settings.orchestrator.active_pin_max_tokens:
        return {
            "output_type": "error",
            "text": "Orchestrator 上下文预算不足：Active Pin Snapshot 超过配置上限",
        }

    tools = filter_allowed_tools(
        build_tools(
            state["shared_dir"],
            state.get("allowed_read_dirs"),
            state.get("task_base_path"),
            _artifact_process_env_var.get(),
            agents=state.get("agents", []),
        ),
        state.get("allowed_tools"),
    )
    references = list(state.get("reference_contexts", []))
    if state.get("evolution_context"):
        references.append(state["evolution_context"])
    try:
        summary = _summary_from_state(state.get("memory_summary"))
    except (ConversationMemoryError, TypeError, ValueError) as exc:
        logger.warning("context.memory_summary_invalid=1 error_type=%s", exc.__class__.__name__)
        return {
            "output_type": "error",
            "text": "Orchestrator 上下文准备失败：历史摘要格式无效",
        }

    def estimated_total(
        history_summary: ConversationSummary,
        recent_messages: list,
    ) -> int:
        prompt_messages = build_reason_messages(
            system_prompt=state.get("system_prompt", ""),
            system_constraints=state.get("system_constraints", []),
            active_pin_snapshot=snapshot,
            history_summary=history_summary.content,
            reference_contexts=references,
            capability_hints=state.get("capability_hints", []),
            recent_messages=recent_messages,
            current_messages=state.get("turn_messages", []),
            expected_task_id=state.get("task_id"),
        )
        return (
            estimate_messages_tokens(prompt_messages)
            + estimate_tools_tokens(tools)
            + settings.orchestrator.context_output_reserve_tokens
        )

    memory_messages = list(state.get("memory_messages", []))
    revision = state.get("memory_revision", 0)
    store = ConversationMemoryStore(state["shared_dir"])

    for compaction_attempt in range(2):
        before = estimated_total(summary, memory_messages)
        logger.info("context.estimated_tokens_before=%d", before)
        if before <= settings.orchestrator.context_compaction_trigger_tokens:
            # A concurrent writer may have already compacted or otherwise
            # reduced the latest store.  Adopt that fresh view for this Run.
            if compaction_attempt:
                return {
                    "memory_messages": memory_messages,
                    "memory_summary": summary,
                    "memory_revision": revision,
                }
            return {}

        compaction_started = time.perf_counter()
        try:
            result = await ContextCompactor().compact(
                summary=summary,
                messages=memory_messages,
                recent_turns=settings.orchestrator.context_recent_turns,
                summary_max_tokens=settings.orchestrator.context_summary_max_tokens,
            )
        except Exception as exc:
            duration_ms = int((time.perf_counter() - compaction_started) * 1000)
            logger.warning(
                "context.compaction_failed=1 context.compaction_duration_ms=%d",
                duration_ms,
                exc_info=True,
            )
            if before <= settings.orchestrator.context_window_tokens:
                if compaction_attempt:
                    return {
                        "memory_messages": memory_messages,
                        "memory_summary": summary,
                        "memory_revision": revision,
                    }
                return {}
            detail = str(exc).strip() or exc.__class__.__name__
            return {
                "output_type": "error",
                "text": f"Orchestrator 上下文压缩失败且无法放入模型窗口：{detail}",
            }

        after = estimated_total(result.summary, list(result.recent_messages))
        duration_ms = int((time.perf_counter() - compaction_started) * 1000)
        logger.info(
            "context.estimated_tokens_after=%d context.compacted_message_count=%d "
            "context.summary_tokens=%d context.compaction_duration_ms=%d",
            after,
            result.compacted_message_count,
            estimate_text_tokens(result.summary.content),
            duration_ms,
        )
        if after > settings.orchestrator.context_window_tokens:
            return {
                "output_type": "error",
                "text": "Orchestrator 上下文压缩后仍超过模型窗口",
            }
        if after > settings.orchestrator.context_compaction_target_tokens:
            logger.warning(
                "context compaction did not reach target: after=%d target=%d",
                after,
                settings.orchestrator.context_compaction_target_tokens,
            )

        try:
            committed = store.commit(
                expected_revision=revision,
                summary=result.summary,
                recent_messages=result.recent_messages,
            )
            return {
                "memory_messages": list(committed.recent_messages),
                "memory_summary": committed.summary,
                "memory_revision": committed.revision,
            }
        except RevisionConflict:
            # Never use a stale summary after a CAS conflict.  Reload the
            # latest envelope and retry from that revision once; if it is still
            # too large, the next iteration performs a fresh compaction.
            logger.info(
                "context.revision_conflict=1; discarding compaction result attempt=%d",
                compaction_attempt + 1,
            )
            if compaction_attempt == 1:
                try:
                    latest = store.load()
                except Exception as exc:
                    return {
                        "output_type": "error",
                        "text": f"Orchestrator 无法读取并发更新后的上下文：{exc}",
                    }
                latest_total = estimated_total(latest.summary, list(latest.recent_messages))
                if latest_total <= settings.orchestrator.context_window_tokens:
                    return {
                        "memory_messages": list(latest.recent_messages),
                        "memory_summary": latest.summary,
                        "memory_revision": latest.revision,
                    }
                return {
                    "output_type": "error",
                    "text": "Orchestrator 上下文在并发更新后仍超过模型窗口",
                }
            try:
                latest = store.load()
            except Exception as exc:
                return {
                    "output_type": "error",
                    "text": f"Orchestrator 无法读取并发更新后的上下文：{exc}",
                }
            summary = latest.summary
            memory_messages = list(latest.recent_messages)
            revision = latest.revision
        except Exception as exc:
            # A failed write must not leak a half-applied in-memory result to
            # the next node.  The old store remains authoritative; surface a
            # structured error so the adapter can finish the Run without
            # silently pretending that compaction was durable.
            logger.warning(
                "context.compaction_commit_failed=1 error_type=%s",
                exc.__class__.__name__,
                exc_info=True,
            )
            return {
                "output_type": "error",
                "text": "Orchestrator 上下文压缩提交失败，原有会话记忆未修改",
            }

    return {
        "output_type": "error",
        "text": "Orchestrator 上下文压缩重试次数耗尽",
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
                parent_run_id=_current_run_id_var.get() or _parent_run_id_var.get(),
                current_run_id=_current_run_id_var.get(),
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
    # Keep this outside the guarded setup so an LLM/tool failure can still
    # return every protocol message produced before the failure to the
    # reducer and save_mem node.
    new_turn_messages: list = []
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
        tools = filter_allowed_tools(tools, state.get("allowed_tools"))
        tool_schema_tokens = estimate_tools_tokens(tools)
        llm_with_tools = llm.bind_tools(tools)

        snapshot = state.get("active_pin_snapshot")
        if not snapshot:
            raise ValueError("Active Pin Snapshot is required")
        history_summary = _summary_from_state(state.get("memory_summary"))
        references = list(state.get("reference_contexts", []))
        if state.get("evolution_context"):
            references.append(state["evolution_context"])
        messages = build_reason_messages(
            system_prompt=state.get("system_prompt", ""),
            system_constraints=state.get("system_constraints", []),
            active_pin_snapshot=snapshot,
            history_summary=history_summary.content,
            reference_contexts=references,
            capability_hints=state.get("capability_hints", []),
            recent_messages=state.get("memory_messages", []),
            current_messages=state.get("turn_messages", []),
            expected_task_id=state.get("task_id"),
        )
        max_iterations = settings.orchestrator.reason_max_iterations
        available_tool_names = {tool.name for tool in tools}
        force_dispatch = _requires_dispatch_intent(state) and "plan_and_dispatch" in available_tool_names
        forced_retry_used = False
        # This flag is local to one reason_node invocation. A new replan must
        # discover the current request-local snapshot again.
        agents_discovered = False
        try:
            llm_config = get_config()
        except RuntimeError:
            llm_config = None
        for i in range(max_iterations):
            current_tokens = (
                estimate_messages_tokens(messages)
                + tool_schema_tokens
                + settings.orchestrator.context_output_reserve_tokens
            )
            if current_tokens > settings.orchestrator.context_window_tokens:
                return {
                    "output_type": "error",
                    "text": "Orchestrator 当前工具调用轨迹超过模型上下文窗口",
                    "plan": None,
                    "turn_messages": new_turn_messages,
                }
            response = await llm_with_tools.ainvoke(messages, config=llm_config)
            clean_response = _clean_ai_message(response)
            # langchain_openai 会把 invalid_tool_calls 原样序列化回下一次请求的
            # tool_calls；若不为每个 id 补一条 ToolMessage 应答，DeepSeek 会以
            # 400 "insufficient tool messages following tool_calls message" 拒绝。
            invalid_calls = list(getattr(clean_response, "invalid_tool_calls", None) or [])
            if invalid_calls and not all(str(tc.get("id") or "") for tc in invalid_calls):
                # 缺 id 的非法调用无法用 ToolMessage 应答，直接从消息上丢弃，
                # 维持"assistant 的每个 tool_call_id 都有 tool 消息应答"不变量。
                clean_response = AIMessage(
                    content=clean_response.content,
                    tool_calls=clean_response.tool_calls,
                    additional_kwargs={
                        k: v
                        for k, v in clean_response.additional_kwargs.items()
                        if k != "tool_calls"
                    },
                    id=clean_response.id,
                )
                invalid_calls = [tc for tc in invalid_calls if str(tc.get("id") or "")]
            messages.append(clean_response)
            new_turn_messages.append(clean_response)
            for tc in invalid_calls:
                invalid_tool_message = ToolMessage(
                    content=(
                        f"Error: 工具 '{tc.get('name', 'unknown')}' 的参数无法解析，未执行："
                        f"{tc.get('error', '') or 'invalid arguments'}。请修正参数后重新调用。"
                    ),
                    tool_call_id=str(tc.get("id")),
                )
                messages.append(invalid_tool_message)
                new_turn_messages.append(invalid_tool_message)

            if not response.tool_calls:
                if force_dispatch and not forced_retry_used:
                    logger.warning("Reason node expected plan_and_dispatch but model returned text; forcing retry")
                    forced_retry_used = True
                    retry_message = HumanMessage(
                        content=(
                            "你刚才只是文字说明，没有真正调用 plan_and_dispatch。"
                            "当前用户请求需要分派给 Agent 执行。请立刻调用 "
                            "plan_and_dispatch 工具，不要输出纯文本。"
                        ),
                        additional_kwargs={"memory_kind": "reason_retry"},
                    )
                    messages.append(retry_message)
                    new_turn_messages.append(retry_message)
                    continue

                if force_dispatch:
                    logger.warning("Reason node still returned text after forced retry; generating fallback plan")
                    plan = _fallback_plan_from_text(state, response.content)
                    if plan is None:
                        return {
                            "output_type": "text",
                            "text": (
                                "当前没有可分派 Agent，无法执行该请求。"
                                "请先添加可用的子 Agent。"
                            ),
                            "plan": None,
                            "turn_messages": new_turn_messages,
                        }
                    return {
                        "output_type": "plan",
                        "text": "",
                        "plan": plan,
                        "turn_messages": new_turn_messages,
                    }

                return {
                    "output_type": "text",
                    "text": response.content,
                    "plan": None,
                    "turn_messages": new_turn_messages,
                }

            ask_calls = [tc for tc in response.tool_calls if tc.get("name") == "ask_agent"]
            discovery_in_batch = any(
                tc.get("name") == "list_available_agents" for tc in response.tool_calls
            )
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
                            if not plan_error:
                                plan_error = _plan_dependency_error(candidate, state.get("message", ""))
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
                messages.append(tool_message)
                new_turn_messages.append(tool_message)

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
                    "turn_messages": new_turn_messages,
                }

        logger.warning("Reason node reached max_iterations=%d", max_iterations)
        if force_dispatch and not _dispatchable_agent_ids(state.get("agents", [])):
            return {
                "output_type": "text",
                "text": "当前没有可分派 Agent，无法执行该请求。请先添加可用的子 Agent。",
                "plan": None,
                "turn_messages": new_turn_messages,
            }
        return {
            "output_type": "text",
            "text": "规划超时，请重新描述需求",
            "plan": None,
            "turn_messages": new_turn_messages,
        }
    except Exception as e:
        logger.exception("Reason node failed unexpectedly")
        error_text = str(e).strip() or e.__class__.__name__
        return {
            "output_type": "error",
            "text": f"Orchestrator 推理失败：{e.__class__.__name__}: {error_text}",
            "plan": None,
            "turn_messages": new_turn_messages,
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
        output = {
            "review_decision": result.get("action", "approve"),
            "review_message": result.get("content", ""),
        }
        if output["review_decision"] in {"discuss", "modify"}:
            if output["review_decision"] == "discuss":
                intro = (
                    "[Plan review feedback] 用户选择继续讨论，尚未批准上一版规划。"
                    "请直接回应讨论内容，不要执行任务。\n"
                )
            else:
                intro = (
                    "[Plan review feedback] 用户请求修改上一版规划，尚未批准执行。"
                    "请根据反馈修订规划，不要执行旧规划。\n"
                )
            output["turn_messages"] = [
                HumanMessage(
                    content=intro + output["review_message"],
                    additional_kwargs={"memory_kind": "plan_review"},
                )
            ]
        return output
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
    previous_results: dict[str, TaskResult] = {}
    for raw_result in state.get("task_results", []):
        try:
            previous = TaskResult.model_validate(raw_result)
        except Exception:
            continue
        previous_results[previous.task_id] = previous
    for dispatch in dispatch_results:
        previous = previous_results.get(dispatch.task_id)
        if previous is not None and not previous.success:
            dispatch.attempt = previous.attempt + 1
    waves = topological_sort(dispatch_results)
    try:
        _write_shared_plan(state["shared_dir"], state["task_id"], plan, dispatch_results)
    except Exception:
        logger.exception("Failed to write orchestrator plan into shared dir")

    return {"dispatch_results": dispatch_results, "execution_waves": waves}


# --- EXECUTE 节点 ---


async def execute_node(state: GraphState) -> dict:
    """在 Graph 内执行真实 child Run，并把运行时事件交给 Adapter 转译。

    Adapter 不再在 Graph 外复制这一段状态推进；返回值中的 task_results
    是 REVIEW/Evolve/Aggregate 唯一读取的权威结果。
    """
    from src.orchestrator.execution.engine import ExecutionEngine

    event_queue: asyncio.Queue | None = _execution_event_queue_var.get()
    backend_client = _backend_client_var.get()
    workspace_mgr = _workspace_manager_var.get()
    repo_path = _repo_path_var.get()
    task_id = state.get("task_id", "")
    cwd = _cwd_var.get()
    root_run_id = _root_run_id_var.get()
    parent_run_id = _parent_run_id_var.get()
    current_run_id = _current_run_id_var.get()
    budget = _run_budget_var.get()
    execution_waves = state.get("execution_waves", [])
    result_by_id: dict[str, TaskResult] = {}
    for raw_result in state.get("task_results", []):
        try:
            previous = TaskResult.model_validate(raw_result)
        except Exception:
            continue
        # A successful prior task is an immutable input to a replan. Failed
        # tasks are intentionally omitted so only the new attempt is reviewed.
        if previous.success:
            result_by_id[previous.task_id] = previous
    group_id = f"orch-{task_id}-{uuid.uuid4().hex[:8]}"

    async def publish(event: StreamEvent) -> None:
        event.content.setdefault("group_id", group_id)
        if event_queue is not None:
            await event_queue.put(event)

    def dispatch_announcement(wave: list[DispatchResult], wave_number: int) -> str:
        lines = [f"开始执行第 {wave_number} 组任务："]
        for dispatch in wave:
            plan_task_id = dispatch.plan_task_id or dispatch.task_id
            lines.extend(
                [
                    "",
                    f"{dispatch.mention} · {plan_task_id}",
                    dispatch.content,
                ]
            )
        return "\n".join(lines)

    if backend_client is None:
        # 保留无 Backend 的单元测试/开发模式，但仍返回完整双状态结果。
        for wave in execution_waves:
            for dispatch in wave:
                if dispatch.task_id in result_by_id and result_by_id[dispatch.task_id].success:
                    continue
                result = _task_result_from_dispatch(dispatch, task_id, mock=True)
                result_by_id[result.task_id] = result
                await publish(
                    StreamEvent.create(
                        EventType.RUNTIME_EXECUTING,
                        task_id=dispatch.plan_task_id or dispatch.task_id,
                        agent=dispatch.agent,
                        title=dispatch.content[:80],
                        status="running",
                    )
                )
                await publish(
                    StreamEvent.create(
                        EventType.RUNTIME_COMPLETED,
                        task_id=dispatch.plan_task_id or dispatch.task_id,
                        agent=dispatch.agent,
                        success=True,
                        status="completed",
                    )
                )
        return {"task_results": [result.model_dump() for result in result_by_id.values()]}

    engine = ExecutionEngine(
        backend_client=backend_client,
        workspace_mgr=workspace_mgr,
        repo_path=repo_path,
        task_id=task_id,
        shared_dir=state.get("shared_dir", ""),
        cwd=cwd,
        root_run_id=root_run_id,
        parent_run_id=parent_run_id,
        current_run_id=current_run_id,
        budget=budget,
        agents=state.get("agents", []),
        integration_service=_integration_service_var.get(),
    )

    for wave_number, wave in enumerate(execution_waves, start=1):
        runnable: list[DispatchResult] = []
        for dispatch in wave:
            if dispatch.task_id in result_by_id and result_by_id[dispatch.task_id].success:
                continue
            dependencies = [result_by_id.get(dep) for dep in dispatch.depends_on]
            blocked_dependency = next(
                (
                    result
                    for result in dependencies
                    if result is not None
                    and dispatch.requires_integrated_dependencies
                    and not result.success
                ),
                None,
            )
            if blocked_dependency is None:
                runnable.append(dispatch)
                continue

            result = _task_result_from_dispatch(
                dispatch,
                task_id,
                execution_status="blocked",
                integration_status="pending",
                error_type="blocked",
                error_code="dependency_failed",
                error_message=f"dependency {blocked_dependency.task_id} did not complete and integrate",
            )
            result_by_id[result.task_id] = result
            await publish(
                StreamEvent.create(
                    EventType.RUNTIME_COMPLETED,
                    task_id=dispatch.plan_task_id or dispatch.task_id,
                    agent=dispatch.agent,
                    success=False,
                    status="blocked",
                    error_code=result.error_code,
                    error_message=result.error_message,
                )
            )

        if not runnable:
            continue

        await publish(
            StreamEvent.create(
                EventType.TEXT,
                text=dispatch_announcement(runnable, wave_number),
                agent=str((state.get("orchestrator") or {}).get("name") or "Orchestrator"),
                agent_type="orchestrator",
                message_id=str(uuid.uuid5(uuid.NAMESPACE_URL, f"{group_id}:wave:{wave_number}")),
            )
        )

        async for event, result in engine.execute(runnable):
            await publish(event)
            if result is not None:
                result_by_id[result.task_id] = result

    return {"task_results": [result.model_dump() for result in result_by_id.values()]}


def _task_result_from_dispatch(
    dispatch: DispatchResult,
    root_task_id: str,
    *,
    mock: bool = False,
    execution_status: str = "completed",
    integration_status: str = "not_required",
    error_type: str = "",
    error_code: str = "",
    error_message: str = "",
) -> Any:
    from src.orchestrator.models import TaskResult

    return TaskResult(
        task_id=dispatch.plan_task_id or dispatch.task_id,
        root_task_id=root_task_id,
        agent=dispatch.agent,
        plan_task_id=dispatch.plan_task_id or dispatch.task_id,
        integration_operation_id=dispatch.integration_operation_id,
        integration_scope_id=dispatch.integration_scope_id or root_task_id,
        workspace_id=dispatch.workspace_handle,
        execution_status=execution_status,
        integration_status=integration_status,
        success=execution_status == "completed" and integration_status == "not_required",
        content=f"(mock) Task dispatched to {dispatch.mention}" if mock else "",
        error_type=error_type,
        error_code=error_code,
        error_message=error_message,
    )


# --- REVIEW 节点 ---


def review_node(state: GraphState) -> dict:
    """按执行/集成两个维度分类结果并决定是否重规划。"""
    task_results = state.get("task_results", [])
    failed = [tr for tr in task_results if tr.get("execution_status") in {"failed", "timeout", "cancelled", "blocked"}]
    integration_failed = [
        tr
        for tr in task_results
        if tr.get("integration_status") in {"failed", "awaiting_user"}
    ]
    awaiting_user = [tr for tr in task_results if tr.get("integration_status") == "awaiting_user"]

    iteration = state.get("iteration", 0)
    max_iterations = state.get("max_iterations", settings.orchestrator.replan_max_iterations)

    if awaiting_user:
        return {
            "needs_replan": False,
            "awaiting_user": True,
            "replan_reason": "",
            "final_status": "awaiting_user",
        }

    if not failed and not integration_failed:
        return {"needs_replan": False, "replan_reason": ""}

    if iteration >= max_iterations:
        logger.warning("Review: max_iterations=%d reached, accepting partial results", max_iterations)
        return {"needs_replan": False, "replan_reason": ""}

    failure_details = []
    for tr in [*failed, *integration_failed]:
        failure_details.append(
            f"- 任务 {tr.get('task_id', '?')} (agent: {tr.get('agent', '?')}): "
            f"{tr.get('error_message') or tr.get('content', '')[:200]}"
        )
    replan_reason = "以下任务执行或集成失败，请重新规划：\n" + "\n".join(failure_details)

    return {
        "needs_replan": True,
        "replan_reason": replan_reason,
        "iteration": iteration + 1,
        "turn_messages": [
            HumanMessage(
                content=f"[Execution/replan facts]\n{replan_reason}",
                additional_kwargs={"memory_kind": "replan_failure"},
            )
        ],
    }


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
    """CAS-append this Run's transcript before the graph completes."""
    try:
        turn_messages = state.get("turn_messages", [])
        if turn_messages:
            logger.info("context.turn_message_count=%d", len(turn_messages))
            store = ConversationMemoryStore(state["shared_dir"])
            store.append_turn(
                turn_messages,
                expected_revision=state.get("memory_revision", 0),
            )
    except Exception:
        logger.exception("save_mem_node: failed to persist conversation memory")
    return {}


async def final_aggregate_node(state: GraphState) -> dict:
    """只在根 Graph 通过 REVIEW 后生成一次最终汇总。"""
    from src.orchestrator.models import TaskResult
    from src.orchestrator.reporting.aggregator import Aggregator

    plan = state.get("plan")
    overview = plan.overview if plan else ""
    results = [TaskResult.model_validate(result) for result in state.get("task_results", [])]
    try:
        summary = await Aggregator().aggregate(results, overview)
    except Exception:
        logger.exception("final_aggregate_node failed")
        from src.orchestrator.reporting.aggregator import build_final_summary_block

        summary = build_final_summary_block(results) if results else overview
    output: dict = {"summary": summary}
    if summary:
        output["turn_messages"] = [
            AIMessage(
                content=summary,
                additional_kwargs={"memory_kind": "final_summary"},
            )
        ]
    return output


async def awaiting_user_node(state: GraphState) -> dict:
    """发布可恢复暂停事实；不生成 final_summary/done。"""
    event_queue: asyncio.Queue | None = _execution_event_queue_var.get()
    if event_queue is not None:
        await event_queue.put(
            StreamEvent.create(
                EventType.ORCHESTRATOR_PAUSED,
                task_id=state.get("task_id", ""),
                status="awaiting_user",
                reason="automatic conflict recovery exhausted",
                iteration=state.get("iteration", 0),
            )
        )
    return {"awaiting_user": True, "final_status": "awaiting_user"}


# --- 条件路由 ---


def route_by_output_type(state: GraphState) -> str:
    output_type = state.get("output_type", "error")
    if output_type == "text":
        return "save_mem"
    elif output_type == "plan":
        return "human_review"
    return "save_mem"


def route_after_compaction(state: GraphState) -> str:
    return "save_mem" if state.get("output_type") == "error" else "reason"


def route_by_review_decision(state: GraphState) -> str:
    decision = state.get("review_decision", "approve")
    if decision == "approve":
        return "dispatch"
    return "compact_context"


def route_by_review(state: GraphState) -> str:
    if state.get("awaiting_user", False):
        return "await_user"
    if state.get("needs_replan", False):
        return "skill_prepare"
    return "final_aggregate"


# --- Graph 构建器 ---


async def _skill_prepare_graph_node(state: GraphState) -> dict:
    """Run the synchronous skill-preparation helper in LangGraph's async lane."""
    return skill_prepare_node(state)


async def _compact_context_graph_node(state: GraphState) -> dict:
    """Run compaction and expose only checkpoint-safe summary data."""
    output = await compact_context_node(state)
    summary = output.get("memory_summary")
    if isinstance(summary, ConversationSummary):
        output = dict(output)
        output["memory_summary"] = summary.to_dict()
    return output


async def _dispatch_graph_node(state: GraphState) -> dict:
    """Run the synchronous dispatcher in LangGraph's async lane."""
    return dispatch_node(state)


async def _review_graph_node(state: GraphState) -> dict:
    """Run the synchronous review helper in LangGraph's async lane."""
    return review_node(state)


async def _evolve_graph_node(state: GraphState) -> dict:
    """Run the synchronous evolution helper in LangGraph's async lane."""
    return evolve_node(state)


async def _save_mem_graph_node(state: GraphState) -> dict:
    """Run the synchronous memory helper in LangGraph's async lane."""
    return save_mem_node(state)


async def _route_after_compaction_graph(state: GraphState) -> str:
    return route_after_compaction(state)


async def _route_by_output_type_graph(state: GraphState) -> str:
    return route_by_output_type(state)


async def _route_by_review_decision_graph(state: GraphState) -> str:
    return route_by_review_decision(state)


async def _route_by_review_graph(state: GraphState) -> str:
    return route_by_review(state)


def build_graph() -> StateGraph:
    graph = StateGraph(GraphState)

    # LangGraph's async stream runner must not transition directly from a
    # synchronous node to an async node.  The runtime version used by
    # AgentEnd can leave that hand-off waiting forever.  Keep the small pure
    # helpers synchronous for existing unit/API callers, and expose async
    # wrappers to the graph so every node and conditional path runs in the
    # same lane.
    graph.add_node("skill_prepare", _skill_prepare_graph_node)
    graph.add_node("compact_context", _compact_context_graph_node)
    graph.add_node("reason", reason_node)
    graph.add_node("human_review", human_review_node)
    graph.add_node("dispatch", _dispatch_graph_node)
    graph.add_node("execute", execute_node)
    graph.add_node("review", _review_graph_node)
    graph.add_node("final_aggregate", final_aggregate_node)
    graph.add_node("await_user", awaiting_user_node)
    graph.add_node("evolve", _evolve_graph_node)
    graph.add_node("save_mem", _save_mem_graph_node)

    graph.set_entry_point("skill_prepare")

    graph.add_edge("skill_prepare", "compact_context")
    graph.add_conditional_edges("compact_context", _route_after_compaction_graph)
    graph.add_conditional_edges("reason", _route_by_output_type_graph)
    graph.add_conditional_edges("human_review", _route_by_review_decision_graph)
    graph.add_edge("dispatch", "execute")
    graph.add_edge("execute", "review")
    graph.add_conditional_edges("review", _route_by_review_graph)
    graph.add_edge("evolve", "save_mem")
    graph.add_edge("final_aggregate", "evolve")
    graph.add_edge("await_user", "save_mem")
    graph.set_finish_point("save_mem")

    return graph.compile(checkpointer=MemorySaver())


def submit_plan_review(session_id: str, action: str, content: str = "") -> bool:
    event = _pending_reviews.get(session_id)
    if event is None:
        return False
    _review_results[session_id] = {"action": action, "content": content}
    event.set()
    return True


def has_pending_plan_review(session_id: str) -> bool:
    return session_id in _pending_reviews
