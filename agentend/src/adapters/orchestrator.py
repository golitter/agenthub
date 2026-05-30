from __future__ import annotations

import asyncio
import logging
from collections.abc import AsyncIterator
from pathlib import Path

from src.adapters.base import BaseAgentAdapter
from src.adapters.registry import AdapterRegistry
from src.clients.backend_client import BackendClient
from src.orchestrator.execution.engine import ExecutionEngine
from src.orchestrator.memory.evolution import EvolutionStore
from src.orchestrator.models import DispatchResult, TaskResult
from src.orchestrator.planning.graph import (
    build_graph,
    reset_reason_runtime_context,
    set_reason_runtime_context,
)
from src.orchestrator.reporting.aggregator import Aggregator
from src.schemas.events import EventType, StreamEvent
from src.schemas.response import AgentResponse
from src.skills.provisioner import SkillProvisioner

logger = logging.getLogger(__name__)


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

        # Orchestrator is a coordinator, not a code worker. Keep its planning
        # tools scoped to shared/.agent; sub-agents read/edit code in their own worktrees.
        allowed_read_dirs = [str(Path(shared_dir).resolve())]

        SkillProvisioner().provision(shared_dir, "orchestrator")

        config = {"configurable": {"thread_id": session_id}}
        ask_event_queue: asyncio.Queue[StreamEvent] = asyncio.Queue()

        initial_state = {
            "message": message,
            "agents": agents,
            "orchestrator": orchestrator,
            "task_id": task_id,
            "shared_dir": shared_dir,
            "allowed_read_dirs": allowed_read_dirs,
            "output_type": "",
            "text": "",
            "plan": None,
            "dispatch_results": [],
            "execution_waves": [],
            "task_results": [],
            "task_status": {},
            "needs_replan": False,
            "replan_reason": "",
            "summary": "",
            "iteration": 0,
            "max_iterations": 3,
            "memory_messages": [],
        }

        current_state: dict = dict(initial_state)

        try:
            update_queue: asyncio.Queue[dict | Exception | None] = asyncio.Queue()

            async def _produce_graph_updates() -> None:
                tokens = set_reason_runtime_context(
                    ask_event_queue=ask_event_queue,
                    backend_client=backend_client,
                    cwd=cwd,
                )
                try:
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
                    ):
                        yield event

                elif node_name == "review":
                    if node_output.get("needs_replan"):
                        yield StreamEvent.create(
                            EventType.PLANNING,
                            node="review",
                            status="replan",
                            reason=node_output.get("replan_reason", ""),
                        )

                elif node_name == "evolve":
                    pass

                elif node_name == "save_mem":
                    yield self._build_done_event(current_state)

            await producer

            while not ask_event_queue.empty():
                yield ask_event_queue.get_nowait()

        except Exception:
            logger.exception("Orchestrator stream_chat failed")
            yield StreamEvent.create(EventType.ERROR, error="Orchestrator internal error")
            yield StreamEvent.create(EventType.DONE, text="")

    async def _handle_reason(self, node_output: dict) -> list[StreamEvent]:
        """Convert reason node output to SSE events."""
        events: list[StreamEvent] = []
        output_type = node_output.get("output_type", "")

        if output_type == "text":
            events.append(
                StreamEvent.create(
                    EventType.TEXT,
                    text=node_output.get("text", ""),
                    agent="Orchestrator",
                    agent_type="orchestrator",
                )
            )
        elif output_type == "plan":
            plan = node_output.get("plan")
            if plan:
                events.append(StreamEvent.create(EventType.PLANNING, node="reason", status="plan_generated"))
                events.append(
                    StreamEvent.create(
                        EventType.TEXT,
                        text=plan.overview,
                        agent="Orchestrator",
                        agent_type="orchestrator",
                    )
                )
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
    ) -> AsyncIterator[StreamEvent]:
        """Execute tasks wave-by-wave, yielding SSE events in real-time."""
        execution_waves = current_state.get("execution_waves", [])
        dispatch_results = current_state.get("dispatch_results", [])
        plan = current_state.get("plan")
        overview = plan.overview if plan else ""

        task_results: list[TaskResult] = []
        dispatch_map = {dr.task_id: dr for dr in dispatch_results}

        if backend_client:
            engine = ExecutionEngine(
                backend_client=backend_client,
                workspace_mgr=workspace_mgr,
                repo_path=repo_path,
                task_id=task_id,
                shared_dir=shared_dir,
                cwd=cwd,
                adapter_registry=self._registry,
            )

            for wave in execution_waves:
                async for event, result in self._stream_wave(engine, wave):
                    yield event
                    if result is not None:
                        task_results.append(result)
                        dr = dispatch_map.get(result.task_id)
                        yield StreamEvent.create(
                            EventType.TEXT,
                            text=result.content or "(no output)",
                            agent=result.agent,
                            agent_type=dr.agent_type if dr else "unknown",
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
                yield StreamEvent.create(
                    EventType.TEXT,
                    text=tr.content,
                    agent=tr.agent,
                    agent_type=dr.agent_type,
                )

        # Aggregate
        aggregator = Aggregator()
        aggregated = await aggregator.aggregate(task_results, overview)

        if aggregated:
            yield StreamEvent.create(
                EventType.TEXT,
                text=aggregated,
                agent="Orchestrator",
                agent_type="orchestrator",
            )

        # Update local state for downstream nodes
        current_state["task_results"] = [
            {
                "task_id": tr.task_id,
                "agent": tr.agent,
                "success": tr.success,
                "content": tr.content,
                "duration": tr.duration,
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

    def _build_done_event(self, current_state: dict) -> StreamEvent:
        output_type = current_state.get("output_type", "text")
        if output_type == "text":
            return StreamEvent.create(EventType.DONE, text=current_state.get("text", ""))
        return StreamEvent.create(EventType.DONE, text=current_state.get("summary", ""))

    async def interrupt(self, session_id: str) -> bool:
        return False

    async def destroy_session(self, session_id: str) -> None:
        pass
