from __future__ import annotations

from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI

from src.app.config import settings
from src.orchestrator.models import TaskResult

_AGGREGATE_PROMPT = """\
你是一个 AI 项目经理，需要汇总多个 Agent 的执行结果为一份简洁的报告。

## 规划概述

{overview}

## 各 Agent 执行结果

{results}

请用中文输出一份结构化的汇总报告，包含：
1. 整体完成情况
2. 各任务关键产出
3. 后续建议（如果有）
"""


class Aggregator:
    async def aggregate(self, results: list[TaskResult], overview: str) -> str:
        if not results:
            return ""

        results_text = "\n".join(
            f"### {r.task_id}（{r.agent}）{'✅' if r.success else '❌'}\n{r.content}" for r in results
        )

        llm = ChatOpenAI(
            model=settings.llm.model,
            base_url=settings.llm.base_url,
            api_key=settings.llm.api_key,
        )
        prompt = _AGGREGATE_PROMPT.format(overview=overview, results=results_text)
        response = await llm.ainvoke([HumanMessage(content=prompt)])
        return response.content.strip()
