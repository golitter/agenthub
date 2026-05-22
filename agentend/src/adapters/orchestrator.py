from collections.abc import AsyncIterator

from src.adapters.base import BaseAgentAdapter
from src.orchestrator.graph import build_graph
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
        overview = plan.overview if plan else ""

        yield StreamEvent.create(EventType.TEXT, text=overview)

        yield StreamEvent.create(
            EventType.DONE,
            text=overview,
            files_written=[
                "config.yaml",
                "plans/overview.md",
                *[f"plans/{t.task_id}.md" for t in plan.tasks],
            ]
            if plan
            else [],
        )

    async def interrupt(self, session_id: str) -> bool:
        return False

    async def destroy_session(self, session_id: str) -> None:
        pass
