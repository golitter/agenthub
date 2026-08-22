from __future__ import annotations

import asyncio
import json
import logging
from collections.abc import AsyncIterator
from pathlib import Path

from src.adapters.base import BaseAgentAdapter
from src.adapters.registry import AdapterRegistry
from src.app.config import settings
from src.clients.backend_client import BackendClient
from src.observability import create_orchestrator_callback, observation_attributes
from src.orchestrator.execution.engine import ExecutionEngine
from src.orchestrator.memory.conversation_memory import ConversationMemoryStore
from src.orchestrator.models import DispatchResult, TaskResult
from src.orchestrator.planning.graph import (
    build_graph,
    reset_reason_runtime_context,
    set_reason_runtime_context,
)
from src.orchestrator.prompts.group_chat import build_group_chat_context
from src.schemas.events import EventType, StreamEvent
from src.schemas.response import AgentResponse
from src.skills.provisioner import SkillProvisioner

logger = logging.getLogger(__name__)


def _task_failure_block(result: TaskResult) -> str:
    payload = {
        "task_id": result.task_id,
        "agent": result.agent,
        "reason": result.error_message or "任务失败",
        "failureType": "timeout" if result.error_type == "timeout" else "error",
        "conflictFiles": result.conflict_files,
    }
    return "```aka_yhy\ntype: task_failure\njson: " + json.dumps(payload, ensure_ascii=False) + "\n```"


def _child_result_text(result: TaskResult) -> str:
    parts: list[str] = []
    if result.content.strip():
        parts.append(result.content.strip())
    if not result.success:
        parts.append(_task_failure_block(result))
    return "\n\n".join(parts)


def _build_observability_config(
    session_id: str,
    task_id: str,
    iteration: int,
) -> tuple[dict, dict]:
    metadata = {
        "task_id": task_id,
        "session_id": session_id,
        "agent_type": "orchestrator",
        "iteration": iteration,
    }
    config: dict = {
        "configurable": {"thread_id": session_id},
        "run_name": f"orchestrator iteration={iteration}",
        "metadata": metadata,
    }
    callback = create_orchestrator_callback()
    if callback is not None:
        config["callbacks"] = [callback]
    return config, metadata


class OrchestratorAdapter(BaseAgentAdapter):
    def __init__(self, registry: AdapterRegistry | None = None) -> None:
        self._graph = build_graph()
        self._registry = registry

    async def create_session(self, session_id: str) -> None:
        pass

    async def chat(self, session_id: str, message: str, **kwargs) -> AgentResponse:
        chunks: list[str] = []
        async for event in self.stream_chat(session_id, message, **kwargs):
            if event.type in (EventType.TEXT.value, EventType.PLANNING.value):
                text = event.content.get("text", event.content.get("node", ""))
                if text:
                    chunks.append(text)
            elif event.type == EventType.DONE.value:
                text = event.content.get("text", "")
                if text:
                    chunks.append(text)
        return AgentResponse(session_id=session_id, content="\n".join(chunks), usage={})

    async def stream_chat(self, session_id: str, message: str, **kwargs) -> AsyncIterator[StreamEvent]:
        agents = kwargs["agents"]
        orchestrator = kwargs.get("orchestrator", {})
        task_id = kwargs["task_id"]
        shared_dir = kwargs["shared_dir"]
        cwd = kwargs.get("cwd", "")
        repo_path = kwargs.get("repo_path", "")
        workspace_mgr = kwargs.get("workspace_mgr")
        backend_client: BackendClient | None = kwargs.get("backend_client")
        soul_md = kwargs.get("soul_md", "")
        task_base_path = kwargs.get("task_base_path", "")
        system_prompt_append = kwargs.get("system_prompt_append")
        root_run_id = kwargs.get("root_run_id", "")
        parent_run_id = kwargs.get("parent_run_id", "")
        current_run_id = kwargs.get("current_run_id", "")
        budget = kwargs.get("budget") or {}
        integration_service = kwargs.get("integration_service")

        # Orchestrator 是协调者而非代码工作者。将其规划
        # 工具限定在 shared/.agent 范围内；sub-agent 在各自的 worktree 中读写代码。
        # Orchestrator 可以读取 task-base worktree 以获取代码上下文。
        allowed_read_dirs = [str(Path(shared_dir).resolve())]
        if task_base_path:
            allowed_read_dirs.append(str(Path(task_base_path).resolve()))

        SkillProvisioner().provision(shared_dir, "orchestrator")

        # 将 orchestrator 自己的 SOUL.md 写入 shared 目录
        shared_path = Path(shared_dir)
        shared_path.mkdir(parents=True, exist_ok=True)
        if soul_md:
            if not isinstance(soul_md, str):
                raise ValueError("soul_md must be a string")
            (shared_path / "SOUL.md").write_text(soul_md, encoding="utf-8")

        # 查询 Orchestrator 自身的跨 agent 窗口上下文
        orchestrator_context = ""
        if backend_client:
            orch_session_id = orchestrator.get("session_id", "")
            if orch_session_id:
                window = await backend_client.get_agent_window_messages(task_id, orch_session_id)
                if window:
                    orchestrator_context = build_group_chat_context(cross_round_messages=window)

        # ── 重规划循环：迭代而非递归 ──
        # 每次迭代运行一次全新的 graph 执行（skill_prepare → reason → … → END）。
        # 当任务失败时，循环会构建重规划消息并开始新一轮迭代。
        current_message = message
        current_iteration = 0
        max_iterations = settings.orchestrator.replan_max_iterations

        while True:
            ask_event_queue: asyncio.Queue[StreamEvent] = asyncio.Queue()
            runtime_event_queue: asyncio.Queue[StreamEvent] = asyncio.Queue()

            initial_state = {
                "message": current_message,
                "agents": agents,
                "orchestrator": orchestrator,
                "task_id": task_id,
                "shared_dir": shared_dir,
                "allowed_read_dirs": allowed_read_dirs,
                "task_base_path": task_base_path,
                "output_type": "",
                "text": "",
                "plan": None,
                "dispatch_results": [],
                "execution_waves": [],
                "task_results": [],
                "task_status": {},
                "review_decision": "",
                "review_message": "",
                "needs_replan": False,
                "replan_reason": "",
                "summary": "",
                "iteration": current_iteration,
                "max_iterations": max_iterations,
                "awaiting_user": False,
                "final_status": "",
                "memory_messages": ConversationMemoryStore(shared_dir).load_messages(),
                "pin_context": system_prompt_append or "",
                "orchestrator_context": orchestrator_context,
            }

            current_state: dict = dict(initial_state)

            config, trace_metadata = _build_observability_config(session_id, task_id, current_iteration)
            try:
                update_queue: asyncio.Queue[dict | Exception | None] = asyncio.Queue()

                async def _produce_graph_updates() -> None:
                    tokens = set_reason_runtime_context(
                        ask_event_queue=ask_event_queue,
                        backend_client=backend_client,
                        cwd=cwd,
                        artifact_process_env=kwargs.get("process_env"),
                        root_run_id=root_run_id,
                        parent_run_id=parent_run_id,
                        current_run_id=current_run_id,
                        budget=budget,
                        execution_event_queue=runtime_event_queue,
                        workspace_mgr=workspace_mgr,
                        repo_path=repo_path,
                        integration_service=integration_service,
                    )
                    try:
                        with observation_attributes(
                            trace_name=f"orchestrator session_id={session_id}",
                            session_id=session_id,
                            metadata=trace_metadata,
                            tags=["orchestrator"],
                        ):
                            async for chunk in self._graph.astream(
                                initial_state,
                                config=config,
                                stream_mode="updates",
                            ):
                                await update_queue.put(chunk)
                    except Exception as e:
                        await update_queue.put(e)
                    finally:
                        reset_reason_runtime_context(tokens)
                        await update_queue.put(None)

                producer = asyncio.create_task(_produce_graph_updates())
                graph_finished = False

                while not graph_finished:
                    update_task = asyncio.create_task(update_queue.get())
                    ask_task = asyncio.create_task(ask_event_queue.get())
                    runtime_task = asyncio.create_task(runtime_event_queue.get())
                    done, pending = await asyncio.wait(
                        {update_task, ask_task, runtime_task},
                        return_when=asyncio.FIRST_COMPLETED,
                    )
                    for task in pending:
                        task.cancel()
                    if pending:
                        await asyncio.gather(*pending, return_exceptions=True)

                    for event_task in (ask_task, runtime_task):
                        if event_task in done:
                            yield event_task.result()

                    if update_task not in done:
                        continue

                    item = update_task.result()
                    if item is None:
                        graph_finished = True
                        continue
                    if isinstance(item, Exception):
                        raise item

                    chunk = item
                    node_name = next(iter(chunk))
                    node_output = chunk[node_name]

                    if not isinstance(node_output, dict):
                        continue

                    current_state.update(node_output)

                    if node_name == "skill_prepare":
                        yield StreamEvent.create(EventType.PLANNING, node="skill_prepare")

                    elif node_name == "reason":
                        for ev in await self._handle_reason(node_output):
                            yield ev

                    elif node_name == "dispatch":
                        for dr in node_output.get("dispatch_results", []):
                            yield StreamEvent.create(
                                EventType.PLANNING,
                                node="dispatch",
                                dispatch=dr.model_dump(),
                            )

                    elif node_name == "execute":
                        # execute_node 已经把真实 TaskResult 和 runtime 事件写入 Graph/queue。
                        yield StreamEvent.create(EventType.PLANNING, node="execute", status="completed")

                    elif node_name == "review":
                        if node_output.get("needs_replan"):
                            yield StreamEvent.create(
                                EventType.PLANNING,
                                node="review",
                                status="replan",
                                reason=node_output.get("replan_reason", ""),
                            )

                    elif node_name == "final_aggregate":
                        summary = node_output.get("summary", "")
                        if summary:
                            yield StreamEvent.create(
                                EventType.TEXT,
                                text=summary,
                                agent="Orchestrator",
                                agent_type="orchestrator",
                            )

                # Graph END 后才允许发送唯一根 done；暂停态只保留结构化事件。
                await producer
                while not ask_event_queue.empty():
                    yield ask_event_queue.get_nowait()
                while not runtime_event_queue.empty():
                    yield runtime_event_queue.get_nowait()
                if not current_state.get("awaiting_user"):
                    yield self._build_done_event(current_state)
                break

            except Exception:
                logger.exception("Orchestrator stream_chat failed")
                yield StreamEvent.create(EventType.ERROR, error="Orchestrator internal error")
                yield StreamEvent.create(EventType.DONE, text="")
                return  # 致命错误 → 立即退出

    async def _handle_reason(self, node_output: dict) -> list[StreamEvent]:
        """将 reason 节点的输出转换为 SSE 事件。"""
        events: list[StreamEvent] = []
        output_type = node_output.get("output_type", "")

        if output_type == "text":
            text = node_output.get("text", "")
            if not text:
                return events
            events.append(
                StreamEvent.create(
                    EventType.TEXT,
                    text=text,
                    agent="Orchestrator",
                    agent_type="orchestrator",
                )
            )
        elif output_type == "plan":
            plan = node_output.get("plan")
            if plan:
                events.append(StreamEvent.create(EventType.PLANNING, node="reason", status="plan_generated"))
        elif output_type == "error":
            error_text = (
                node_output.get("error")
                or node_output.get("message")
                or node_output.get("text")
                or "Orchestrator internal error"
            )
            events.append(StreamEvent.create(EventType.ERROR, error=str(error_text)))
        return events

    async def _stream_wave(
        self,
        engine: ExecutionEngine,
        wave: list[DispatchResult],
    ) -> AsyncIterator[tuple[StreamEvent, TaskResult | None]]:
        """实时流式产出单个波次的事件。

        同一波次内的任务并行执行；事件在到达时即被产出。
        """
        if len(wave) <= 1:
            for dispatch in wave:
                async for item in engine.execute([dispatch]):
                    yield item
            return

        # 并行：通过 queue 扇出，到达即产出
        queue: asyncio.Queue[tuple | BaseException | None] = asyncio.Queue()

        async def _run(dispatch: DispatchResult) -> None:
            try:
                async for item in engine.execute([dispatch]):
                    await queue.put(item)
            except asyncio.CancelledError:
                raise
            except BaseException as exc:
                await queue.put(exc)

        tasks = [asyncio.create_task(_run(d)) for d in wave]

        async def _drain() -> None:
            try:
                await asyncio.gather(*tasks, return_exceptions=True)
            finally:
                await queue.put(None)

        drain_task = asyncio.create_task(_drain())

        try:
            while True:
                item = await queue.get()
                if item is None:
                    break
                if isinstance(item, BaseException):
                    raise item
                yield item
            await drain_task
        finally:
            for task in tasks:
                if not task.done():
                    task.cancel()
            if not drain_task.done():
                drain_task.cancel()
            await asyncio.gather(*tasks, drain_task, return_exceptions=True)

    def _build_done_event(self, current_state: dict) -> StreamEvent:
        output_type = current_state.get("output_type", "text")
        if output_type == "text":
            return StreamEvent.create(EventType.DONE, text=current_state.get("text", ""))
        return StreamEvent.create(EventType.DONE, text=current_state.get("summary", ""))

    async def interrupt(self, session_id: str) -> bool:
        return False

    async def destroy_session(self, session_id: str) -> None:
        pass
