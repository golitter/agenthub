from collections.abc import AsyncIterator
from pathlib import Path

from src.adapters.base import BaseAgentAdapter
from src.adapters.registry import AdapterRegistry
from src.app.config import settings
from src.clients.backend_client import BackendClient
from src.orchestrator.execution.coordination import CoordinationChannel
from src.orchestrator.execution.dispatcher import Dispatcher
from src.orchestrator.execution.engine import ExecutionEngine
from src.orchestrator.execution.state import RuntimeState
from src.orchestrator.memory.evolution import EvolutionStore
from src.orchestrator.models import TaskResult
from src.orchestrator.planning.graph import build_graph
from src.orchestrator.reporting.aggregator import Aggregator
from src.schemas.events import EventType, StreamEvent
from src.schemas.response import AgentResponse
from src.skills.provisioner import SkillProvisioner


class OrchestratorAdapter(BaseAgentAdapter):
    def __init__(self, registry: AdapterRegistry | None = None) -> None:
        self._graph = build_graph()
        self._registry = registry

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
        cwd = kwargs.get("cwd", "")
        repo_path = kwargs.get("repo_path", "")
        workspace_mgr = kwargs.get("workspace_mgr")
        results_callback = kwargs.get("results_callback")
        backend_client: BackendClient | None = kwargs.get("backend_client")

        # --- Phase 1: Planning ---
        yield StreamEvent.create(EventType.PLANNING, status="started")

        # Build allowed read directories: shared_dir + orchestrator's own session workspace
        allowed_read_dirs = [str(Path(shared_dir).resolve())]
        if cwd:
            allowed_read_dirs.append(str(Path(cwd).resolve()))

        # Provision skills to shared_dir for orchestrator's progressive disclosure
        SkillProvisioner().provision(shared_dir, "orchestrator")

        # Stream graph execution to yield progress events for each node
        result: dict = {}
        async for chunk in self._graph.astream(
            {
                "message": message,
                "agents": agents,
                "task_id": task_id,
                "shared_dir": shared_dir,
                "allowed_read_dirs": allowed_read_dirs,
            },
            stream_mode="updates",
        ):
            # chunk is {node_name: node_output} in "updates" mode
            node_name = next(iter(chunk))
            node_output = chunk[node_name]
            if isinstance(node_output, dict):
                result.update(node_output)
            yield StreamEvent.create(EventType.PLANNING, node=node_name)

        plan = result.get("plan")
        if not plan:
            yield StreamEvent.create(EventType.ERROR, text="Plan generation failed")
            yield StreamEvent.create(EventType.DONE, text="")
            return

        overview = plan.overview

        # Yield orchestrator plan as a chat message (triggers agent switch in frontend)
        yield StreamEvent.create(EventType.TEXT, text=overview, agent="Orchestrator", agent_type="orchestrator")

        # --- Phase 1.5: Coordination ---
        if self._registry:
            channel = CoordinationChannel(
                self._registry,
                model=settings.llm.model,
                base_url=settings.llm.base_url,
                api_key=settings.llm.api_key,
            )
            async for event in channel.coordinate(plan, agents):
                yield event
            _coord_context = channel.summary()

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

        # --- Phase 3: Execute + Collect ---
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
            async for event, result in engine.execute(dispatch_results):
                yield event
                if result is not None:
                    task_results.append(result)
                    if result.success:
                        runtime.set_completed(result.task_id, result.content)
                    else:
                        runtime.set_failed(result.task_id)
                    # Yield sub-agent response as a chat message
                    dr = dispatch_map.get(result.task_id)
                    yield StreamEvent.create(
                        EventType.TEXT,
                        text=result.content or "(no output)",
                        agent=result.agent,
                        agent_type=dr.agent_type if dr else "unknown",
                    )
        elif results_callback:
            task_results = await results_callback(dispatch_results)
            for tr in task_results:
                if tr.success:
                    runtime.set_completed(tr.task_id, tr.content)
                else:
                    runtime.set_failed(tr.task_id)
                dr = dispatch_map.get(tr.task_id)
                yield StreamEvent.create(
                    EventType.TEXT,
                    text=tr.content or "(no output)",
                    agent=tr.agent,
                    agent_type=dr.agent_type if dr else "unknown",
                )
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
                runtime.set_completed(tr.task_id, tr.content)
                dr = dispatch_map.get(tr.task_id)
                yield StreamEvent.create(
                    EventType.TEXT,
                    text=tr.content,
                    agent=tr.agent,
                    agent_type=dr.agent_type if dr else "unknown",
                )

        # --- Phase 4: Aggregate ---
        aggregator = Aggregator()
        aggregated = await aggregator.aggregate(task_results, overview)

        if aggregated:
            yield StreamEvent.create(
                EventType.TEXT,
                text=aggregated,
                agent="Orchestrator",
                agent_type="orchestrator",
            )

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
