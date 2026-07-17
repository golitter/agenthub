from __future__ import annotations

import asyncio
import json
import logging
import uuid
from collections.abc import AsyncIterator
from pathlib import Path

from src.adapters.base import BaseAgentAdapter
from src.adapters.registry import AdapterRegistry
from src.clients.backend_client import BackendClient
from src.observability import create_orchestrator_callback, observation_attributes
from src.orchestrator.execution.engine import ExecutionEngine
from src.orchestrator.memory.conversation_memory import ConversationMemoryStore
from src.orchestrator.memory.evolution import EvolutionStore
from src.orchestrator.models import DispatchResult, TaskResult
from src.orchestrator.planning.graph import (
    build_graph,
    reset_reason_runtime_context,
    set_reason_runtime_context,
    wait_for_external_review,
)
from src.orchestrator.prompts.group_chat import build_group_chat_context
from src.orchestrator.reporting.aggregator import Aggregator
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

        # Orchestrator is a coordinator, not a code worker. Keep its planning
        # tools scoped to shared/.agent; sub-agents read/edit code in their own worktrees.
        # Orchestrator can read the task-base worktree for code context.
        allowed_read_dirs = [str(Path(shared_dir).resolve())]
        if task_base_path:
            allowed_read_dirs.append(str(Path(task_base_path).resolve()))

        SkillProvisioner().provision(shared_dir, "orchestrator")

        # Write orchestrator's own SOUL.md to shared directory
        shared_path = Path(shared_dir)
        shared_path.mkdir(parents=True, exist_ok=True)
        if soul_md:
            (shared_path / "SOUL.md").write_text(soul_md.replace(" ", ""), encoding="utf-8")


        # Query Orchestrator's own cross-agent window context
        orchestrator_context = ""
        if backend_client:
            orch_session_id = orchestrator.get("session_id", "")
            if orch_session_id:
                window = await backend_client.get_agent_window_messages(task_id, orch_session_id)
                if window:
                    orchestrator_context = build_group_chat_context(cross_round_messages=window)

        # ── Replan loop: iterative instead of recursive ──
        # Each iteration runs a fresh graph execution (skill_prepare → reason → … → END).
        # When tasks fail, the loop builds a replan message and starts a new iteration.
        current_message = message
        current_iteration = 0
        max_iterations = 3

        while True:
            ask_event_queue: asyncio.Queue[StreamEvent] = asyncio.Queue()

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
                replan_needed = False
                replan_reason_out = ""

                while not graph_finished:
                    update_task = asyncio.create_task(update_queue.get())
                    ask_task = asyncio.create_task(ask_event_queue.get())
                    done, pending = await asyncio.wait(
                        {update_task, ask_task},
                        return_when=asyncio.FIRST_COMPLETED,
                    )
                    for task in pending:
                        task.cancel()
                    if pending:
                        await asyncio.gather(*pending, return_exceptions=True)

                    if ask_task in done:
                        yield ask_task.result()

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
                        async for event in self._handle_execute(
                            current_state,
                            backend_client,
                            agents,
                            task_id,
                            shared_dir,
                            cwd,
                            repo_path,
                            workspace_mgr,
                            config,
                        ):
                            yield event
                        replan_reason_out = self._build_replan_reason(current_state)
                        if replan_reason_out and current_iteration < max_iterations:
                            yield StreamEvent.create(
                                EventType.PLANNING,
                                node="review",
                                status="replan",
                                reason=replan_reason_out,
                            )
                            replan_needed = True
                            producer.cancel()
                            await asyncio.gather(producer, return_exceptions=True)
                            break  # break inner while → continue outer replan loop

                    elif node_name == "review":
                        if node_output.get("needs_replan"):
                            yield StreamEvent.create(
                                EventType.PLANNING,
                                node="review",
                                status="replan",
                                reason=node_output.get("replan_reason", ""),
                            )

                    elif node_name == "evolve":
                        yield self._build_done_event(current_state)

                    elif node_name == "save_mem":
                        yield self._build_done_event(current_state)

                # ── Post-iteration routing ──
                if not replan_needed:
                    # Normal completion — drain remaining events and exit
                    await producer
                    while not ask_event_queue.empty():
                        yield ask_event_queue.get_nowait()
                    break  # exit replan loop

                # Replan: build new message for next iteration
                current_message = (
                    f"{message}\n\n[重规划请求]\n{replan_reason_out}\n\n"
                    "请重新规划冲突修复任务，优先保留已完成工作的意图，"
                    "并让负责的 sub-agent 修复后再次执行 taskctl merge。"
                )
                current_iteration += 1
                # continue outer while → fresh graph execution

            except Exception:
                logger.exception("Orchestrator stream_chat failed")
                yield StreamEvent.create(EventType.ERROR, error="Orchestrator internal error")
                yield StreamEvent.create(EventType.DONE, text="")
                return  # fatal error → exit immediately

    async def _handle_reason(self, node_output: dict) -> list[StreamEvent]:
        """Convert reason node output to SSE events."""
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

    async def _handle_execute(
        self,
        current_state: dict,
        backend_client: BackendClient | None,
        agents: list[dict],
        task_id: str,
        shared_dir: str,
        cwd: str,
        repo_path: str,
        workspace_mgr,
        runnable_config: dict | None,
    ) -> AsyncIterator[StreamEvent]:
        """Execute tasks wave-by-wave, yielding SSE events in real-time."""
        execution_waves = current_state.get("execution_waves", [])
        dispatch_results = current_state.get("dispatch_results", [])
        plan = current_state.get("plan")
        overview = plan.overview if plan else ""
        group_id = f"orch-{task_id}-{uuid.uuid4().hex[:8]}"

        task_results: list[TaskResult] = []
        dispatch_map = {dr.task_id: dr for dr in dispatch_results}

        def _attach_group(event: StreamEvent) -> StreamEvent:
            event.content["group_id"] = group_id
            return event

        if backend_client:
            engine = ExecutionEngine(
                backend_client=backend_client,
                workspace_mgr=workspace_mgr,
                repo_path=repo_path,
                task_id=task_id,
                shared_dir=shared_dir,
                cwd=cwd,
            )

            for wave in execution_waves:
                async for event, result in self._stream_wave(engine, wave):
                    yield _attach_group(event)
                    if result is not None:
                        task_results.append(result)
                        dr = dispatch_map.get(result.task_id)
                        result_text = _child_result_text(result)
                        if not result_text:
                            continue
                        orchestrator_cfg = current_state.get("orchestrator", {}) or {}
                        source_agent = orchestrator_cfg.get("id") or orchestrator_cfg.get("name") or "Orchestrator"
                        source_session_id = orchestrator_cfg.get("session_id", "")
                        target_agent_type = dr.agent_type if dr else "unknown"
                        target_session_id = dr.real_session_id if dr else ""
                        question = dr.content if dr else result.task_id
                        question_id = f"dispatch-{result.task_id}"
                        yield _attach_group(
                            StreamEvent.create(
                                EventType.ASK_CARD_START,
                                question_id=question_id,
                                source_agent=source_agent,
                                source_agent_type="orchestrator",
                                source_session_id=source_session_id,
                                target_agent=result.agent,
                                target_agent_type=target_agent_type,
                                target_session_id=target_session_id,
                                question=question,
                            )
                        )
                        yield _attach_group(
                            StreamEvent.create(
                                EventType.ASK_CARD_DONE,
                                question_id=question_id,
                                source_agent=source_agent,
                                source_agent_type="orchestrator",
                                source_session_id=source_session_id,
                                target_agent=result.agent,
                                target_agent_type=target_agent_type,
                                target_session_id=target_session_id,
                                question=question,
                                summary=result_text.replace("\n", " ")[:120],
                                status="completed" if result.success else "failed",
                            )
                        )
                        yield _attach_group(
                            StreamEvent.create(
                                EventType.TEXT,
                                text=result_text,
                                agent=result.agent,
                                agent_type=target_agent_type,
                                message_id=result.message_id,
                            )
                        )

            async for event, merge_result in self._review_and_merge_task_to_main_if_ready(
                workspace_mgr,
                repo_path,
                task_id,
                task_results,
                plan.merge_to_main if plan else False,
                current_state,
            ):
                yield _attach_group(event)
                if merge_result is not None:
                    task_results.append(merge_result)
                    result_text = _child_result_text(merge_result)
                    if result_text:
                        yield _attach_group(
                            StreamEvent.create(
                                EventType.TEXT,
                                text=result_text,
                                agent="Orchestrator",
                                agent_type="orchestrator",
                            )
                        )

        elif execution_waves:
            for dr in dispatch_results:
                tr = TaskResult(
                    task_id=dr.task_id,
                    agent=dr.agent,
                    success=True,
                    content=f"(mock) Task dispatched to {dr.mention}",
                )
                task_results.append(tr)
                yield _attach_group(
                    StreamEvent.create(
                        EventType.TEXT,
                        text=tr.content,
                        agent=tr.agent,
                        agent_type=dr.agent_type,
                    )
                )

        # Aggregate
        aggregator = Aggregator()
        aggregated = await aggregator.aggregate(task_results, overview, config=runnable_config)

        if aggregated:
            yield _attach_group(
                StreamEvent.create(
                    EventType.TEXT,
                    text=aggregated,
                    agent="Orchestrator",
                    agent_type="orchestrator",
                )
            )

        # Update local state for downstream nodes
        current_state["task_results"] = [
            {
                "task_id": tr.task_id,
                "agent": tr.agent,
                "success": tr.success,
                "content": tr.content,
                "duration": tr.duration,
                "error_type": tr.error_type,
                "error_message": tr.error_message,
                "conflict_files": tr.conflict_files,
            }
            for tr in task_results
        ]
        current_state["summary"] = aggregated or overview

        # Record evolution directly (since graph's evolve_node can't see real results)
        try:
            evolution = EvolutionStore(shared_dir)
            all_success = all(tr.success for tr in task_results)
            results_summary = "; ".join(f"{tr.task_id}: {'✅' if tr.success else '❌'}" for tr in task_results)
            evolution.record(
                message=current_state["message"],
                plan_summary=overview[:200],
                results_summary=results_summary[:200],
                success=all_success,
                agent_performance=[
                    {"agent_id": tr.agent, "success": tr.success, "duration": tr.duration} for tr in task_results
                ],
            )
        except Exception:
            logger.exception("Evolution recording failed")

    async def _stream_wave(
        self,
        engine: ExecutionEngine,
        wave: list[DispatchResult],
    ) -> AsyncIterator[tuple[StreamEvent, TaskResult | None]]:
        """Stream events for a single wave in real-time.

        Tasks within a wave run in parallel; events are yielded as they arrive.
        """
        if len(wave) <= 1:
            for dispatch in wave:
                async for item in engine.execute([dispatch]):
                    yield item
            return

        # Parallel: fan out via queue, yield as they arrive
        queue: asyncio.Queue[tuple | None] = asyncio.Queue()

        async def _run(dispatch: DispatchResult) -> None:
            async for item in engine.execute([dispatch]):
                await queue.put(item)

        tasks = [asyncio.create_task(_run(d)) for d in wave]
        pending = len(tasks)

        async def _drain() -> None:
            await asyncio.gather(*tasks)
            await queue.put(None)

        asyncio.create_task(_drain())

        while pending > 0:
            item = await queue.get()
            if item is None:
                break
            yield item

    async def _review_and_merge_task_to_main_if_ready(
        self,
        workspace_mgr,
        repo_path: str,
        task_id: str,
        task_results: list[TaskResult],
        merge_to_main: bool,
        current_state: dict,
    ) -> AsyncIterator[tuple[StreamEvent, TaskResult | None]]:
        if not merge_to_main or not workspace_mgr or not repo_path:
            return
        if not all(tr.success for tr in task_results):
            return

        orchestrator_cfg = current_state.get("orchestrator", {}) or {}
        session_id = str(orchestrator_cfg.get("session_id") or task_id)
        review_key = f"{session_id}:merge-main:{uuid.uuid4().hex}"
        diff_snapshot_id = str(uuid.uuid4())
        diff = await workspace_mgr.diff_task_to_main(repo_path, task_id)

        yield (
            StreamEvent.create(
                EventType.PLAN_REVIEW,
                session_id=session_id,
                task_id=task_id,
                review_key=review_key,
                plan={
                    "overview": "确认将 task 分支合并到 main",
                    "merge_to_main": True,
                    "tasks": [],
                },
                waves=[],
                review_type="merge_to_main",
                source_branch=f"task/{task_id}",
                target_branch="main",
                diff_snapshot_id=diff_snapshot_id,
                diff=diff,
            ),
            None,
        )

        review = await wait_for_external_review(session_id)
        action = review.get("action", "approve")
        if action != "approve":
            feedback = review.get("content", "")
            yield (
                StreamEvent.create(
                    EventType.TEXT,
                    text="已暂停合并到 main，等待调整规划。" + (f"\n\n反馈：{feedback}" if feedback else ""),
                    agent="Orchestrator",
                    agent_type="orchestrator",
                ),
                TaskResult(
                    task_id="merge-to-main",
                    agent="Orchestrator",
                    success=False,
                    content="",
                    error_type="review_rejected",
                    error_message=feedback or "User did not approve merge to main",
                ),
            )
            return

        result = await workspace_mgr.merge_task_to_main(repo_path, task_id)
        if result.success:
            yield (
                StreamEvent.create(
                    EventType.TEXT,
                    text=f"Merged {result.source_branch} into {result.target_branch}.",
                    agent="Orchestrator",
                    agent_type="orchestrator",
                ),
                TaskResult(
                    task_id="merge-to-main",
                    agent="Orchestrator",
                    success=True,
                    content=f"Merged {result.source_branch} into {result.target_branch}.",
                ),
            )
            return

        conflict_text = ", ".join(result.conflict_files) if result.conflict_files else "unknown files"
        yield (
            StreamEvent.create(
                EventType.TEXT,
                text=(
                    f"Merge {result.source_branch} -> {result.target_branch} failed; "
                    f"conflict_files=[{conflict_text}]; aborted={result.aborted}; error={result.error}"
                ),
                agent="Orchestrator",
                agent_type="orchestrator",
            ),
            TaskResult(
                task_id="merge-to-main",
                agent="Orchestrator",
                success=False,
                content="",
                error_type="merge_conflict" if result.conflict_files else "merge_error",
                error_message=(
                    f"Merge {result.source_branch} -> {result.target_branch} failed; "
                    f"conflict_files=[{conflict_text}]; aborted={result.aborted}; error={result.error}"
                ),
                conflict_files=result.conflict_files,
            ),
        )

    def _build_replan_reason(self, current_state: dict) -> str:
        task_results = current_state.get("task_results", [])
        failed = [tr for tr in task_results if not tr.get("success", True)]
        if not failed:
            return ""

        lines: list[str] = []
        for tr in failed:
            conflict_files = tr.get("conflict_files") or []
            details = tr.get("error_message") or tr.get("content", "")[:300] or "任务失败"
            line = f"- 任务 {tr.get('task_id', '?')} (agent: {tr.get('agent', '?')}): {details}"
            if conflict_files:
                line += "\n  冲突文件: " + ", ".join(conflict_files)
            lines.append(line)
        return "以下任务执行或合并失败，请重新规划：\n" + "\n".join(lines)

    def _build_done_event(self, current_state: dict) -> StreamEvent:
        output_type = current_state.get("output_type", "text")
        if output_type == "text":
            return StreamEvent.create(EventType.DONE, text=current_state.get("text", ""))
        return StreamEvent.create(EventType.DONE, text=current_state.get("summary", ""))

    async def interrupt(self, session_id: str) -> bool:
        return False

    async def destroy_session(self, session_id: str) -> None:
        pass
