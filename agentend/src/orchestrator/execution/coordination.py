from __future__ import annotations

from collections.abc import AsyncIterator

from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI

from src.adapters.registry import AdapterRegistry
from src.generated.events import EventType
from src.orchestrator.models import PlanOutput
from src.schemas.events import StreamEvent

_QUESTION_PROMPT = """\
你是项目经理，正在协调一个多 Agent 团队完成以下任务。

## 任务概述
{overview}

## 当前 Agent
- ID: {agent_id}
- 名称: {agent_name}

## 你的任务
针对该 Agent 即将执行的工作，提出一个关键确认问题。问题应该简短（1-2 句话），
聚焦于执行细节、边界条件或潜在的实现选择。

只输出问题本身，不要加前缀或额外说明。
"""


class CoordinationChannel:
    def __init__(self, registry: AdapterRegistry, model: str, base_url: str, api_key: str) -> None:
        self._registry = registry
        self._llm = ChatOpenAI(model=model, base_url=base_url, api_key=api_key)
        self._decisions: list[dict] = []
        self._agent_map: dict[str, dict] = {}

    async def coordinate(
        self,
        plan: PlanOutput,
        agents: list[dict],
        config: dict | None = None,
    ) -> AsyncIterator[StreamEvent]:
        self._agent_map = {a["id"]: a for a in agents}
        unique_agents = list({t.session_id for t in plan.tasks})

        yield StreamEvent.create(
            EventType.COORDINATION_START,
            round=1,
            agents=unique_agents,
        )

        for agent_id in unique_agents:
            agent_info = self._agent_map.get(agent_id, {})
            agent_name = agent_info.get("name", agent_id)

            question = await self._generate_question(plan.overview, agent_id, agent_name, config=config)
            yield StreamEvent.create(
                EventType.COORDINATION_MESSAGE,
                **{"from": "orchestrator", "to": agent_id, "text": question, "round": 1},
            )

            answer = await self._ask_agent(agent_id, question)
            yield StreamEvent.create(
                EventType.COORDINATION_MESSAGE,
                **{"from": agent_id, "to": "orchestrator", "text": answer, "round": 1},
            )

            self._decisions.append(
                {
                    "agent": agent_id,
                    "question": question,
                    "answer": answer,
                }
            )

        yield StreamEvent.create(
            EventType.COORDINATION_DONE,
            rounds=1,
            decisions=[f"{d['agent']}: {d['answer']}" for d in self._decisions],
        )

    def summary(self) -> str:
        if not self._decisions:
            return ""
        lines = ["协调结论："]
        for d in self._decisions:
            lines.append(f"- {d['agent']}: {d['answer']}")
        return "\n".join(lines)

    async def _generate_question(
        self,
        overview: str,
        agent_id: str,
        agent_name: str,
        config: dict | None = None,
    ) -> str:
        prompt = _QUESTION_PROMPT.format(
            overview=overview,
            agent_id=agent_id,
            agent_name=agent_name,
        )
        response = await self._llm.ainvoke([HumanMessage(content=prompt)], config=config)
        return response.content.strip()

    async def _ask_agent(self, agent_id: str, question: str) -> str:
        try:
            adapter_cls = self._registry.get(agent_id)
            adapter = adapter_cls()
            session_id = f"coord-{agent_id}"
            await adapter.create_session(session_id)
            try:
                response = await adapter.chat(session_id, question)
                return response.content
            finally:
                await adapter.destroy_session(session_id)
        except Exception as exc:
            return f"(协调失败: {exc})"
