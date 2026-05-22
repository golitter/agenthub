from collections.abc import AsyncIterator

from src.adapters.base import BaseAgentAdapter
from src.orchestrator.aggregator import Aggregator
from src.orchestrator.dispatcher import Dispatcher
from src.orchestrator.evolution import EvolutionStore
from src.orchestrator.graph import build_graph
from src.orchestrator.models import TaskResult
from src.orchestrator.state import RuntimeState
from src.schemas.events import EventType, StreamEvent
from src.schemas.response import AgentResponse


class OrchestratorAdapter(BaseAgentAdapter):
    def __init__(self) -> None:
        self._graph = build_graph()

    async def create_session(self, session_id: str) -> None:
        pass

    async def chat(self, session_id: str, message: str, **kwargs) -> AgentResponse:
        chunks: list[str] = []
        async for event in self.stream_chat(session_id, message, **kwargs):
            if event.type == EventType.PLANNING.value:
                chunks.append(event.content.get("node", ""))
            elif event.type == EventType.DONE.value:
                text = event.content.get("text", "")
                if text:
                    chunks.append(text)
        return AgentResponse(session_id=session_id, content="\n".join(chunks), usage={})

    async def stream_chat(self, session_id: str, message: str, **kwargs) -> AsyncIterator[StreamEvent]:
        agents = kwargs["agents"]
        task_id = kwargs["task_id"]
        shared_dir = kwargs["shared_dir"]
        results_callback = kwargs.get("results_callback")

        # --- Phase 1: Planning ---
        yield StreamEvent.create(EventType.PLANNING, status="started")

        result = await self._graph.ainvoke(
            {
                "message": message,
                "agents": agents,
                "task_id": task_id,
                "shared_dir": shared_dir,
            }
        )

        yield StreamEvent.create(EventType.PLANNING, node="plan")
        yield StreamEvent.create(EventType.PLANNING, node="write_shared")

        plan = result.get("plan")
        if not plan:
            yield StreamEvent.create(EventType.ERROR, text="Plan generation failed")
            yield StreamEvent.create(EventType.DONE, text="")
            return

        overview = plan.overview

        # --- Phase 2: Dispatch ---
        runtime = RuntimeState()
        for task in plan.tasks:
            runtime.add_task(task.task_id)

        dispatcher = Dispatcher(agents)
        dispatch_results = dispatcher.dispatch(plan)

        for dr in dispatch_results:
            runtime.set_running(dr.task_id, dr.agent)
            yield StreamEvent.create(
                EventType.PLANNING,
                node="dispatch",
                dispatch=dr.model_dump(),
            )

        # --- Phase 3: Collect ---
        task_results: list[TaskResult] = []
        if results_callback:
            task_results = await results_callback(dispatch_results)
        else:
            for dr in dispatch_results:
                task_results.append(
                    TaskResult(
                        task_id=dr.task_id,
                        agent=dr.agent,
                        success=True,
                        content=f"(mock) Task dispatched to {dr.mention}",
                    )
                )

        for tr in task_results:
            if tr.success:
                runtime.set_completed(tr.task_id, tr.content)
            else:
                runtime.set_failed(tr.task_id)

        # --- Phase 4: Aggregate ---
        aggregator = Aggregator()
        aggregated = aggregator.aggregate(task_results, overview)

        yield StreamEvent.create(EventType.TEXT, text=overview)

        # --- Phase 5: Record experience ---
        evolution = EvolutionStore(shared_dir)
        all_success = all(tr.success for tr in task_results)
        evolution.record(
            message=message,
            plan_summary=overview[:200],
            results_summary=aggregated[:200] if aggregated else "",
            success=all_success,
            agent_performance=[
                {"agent_id": tr.agent, "success": tr.success, "duration": tr.duration} for tr in task_results
            ],
        )

        yield StreamEvent.create(
            EventType.DONE,
            text=aggregated or overview,
            files_written=[
                "config.yaml",
                "plans/overview.md",
                *[f"plans/{t.task_id}.md" for t in plan.tasks],
            ],
        )

    async def interrupt(self, session_id: str) -> bool:
        return False

    async def destroy_session(self, session_id: str) -> None:
        pass
