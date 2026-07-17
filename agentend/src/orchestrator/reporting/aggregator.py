from __future__ import annotations

import json
from datetime import datetime

from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI

from src.app.config import settings
from src.orchestrator.models import TaskResult

_AGGREGATE_PROMPT = """\
你是一个 AI 项目经理，需要汇总多个 Agent 的执行结果为一份简洁的报告。

## 当前时间

{current_time}

如果报告中需要写“报告生成时间”“当前日期”或任何相对日期，必须使用上面的当前时间。
不要输出占位符，也不要根据模型知识猜测日期。

## 规划概述

{overview}

## 各 Agent 执行结果（已截断，失败原因单独标注）

{results}

请用中文输出一份结构化的汇总报告，包含：
1. 整体完成情况
2. 各任务关键产出
3. 后续建议（如果有）
要求：不要复制完整执行日志；不要输出原始 [Timeout] 或 [Error] 标记；失败任务请单独说明。
"""


def _current_time_context() -> str:
    now = datetime.now().astimezone()
    return "\n".join(
        [
            f"当前日期: {now:%Y-%m-%d}",
            f"当前时间: {now:%Y-%m-%d %H:%M:%S %Z}",
            f"UTC offset: {now:%z}",
        ]
    )


def _truncate_text(text: str, limit: int = 1200) -> str:
    stripped = text.replace("```", "`\u200b``").strip()
    if len(stripped) <= limit:
        return stripped
    return stripped[:limit].rstrip() + "\n...(details truncated)"


def build_final_summary_block(results: list[TaskResult]) -> str:
    completed = sum(1 for result in results if result.success)
    failed = len(results) - completed
    status = "success" if failed == 0 else "failed" if completed == 0 else "partial"
    next_action = "可以继续验收结果。" if failed == 0 else "请优先重试或人工检查失败任务，再合并最终结果。"
    details = [
        {
            "task_id": result.task_id,
            "agent": result.agent,
            "status": "completed" if result.success else "failed",
            "summary": (_truncate_text(result.content, 120) if result.success else result.error_message or "任务失败"),
        }
        for result in results
    ]
    payload = {
        "status": status,
        "completed": completed,
        "failed": failed,
        "nextAction": next_action,
        "details": details,
    }
    return "```aka_yhy\ntype: final_summary\njson: " + json.dumps(payload, ensure_ascii=False) + "\n```"


class Aggregator:
    async def aggregate(self, results: list[TaskResult], overview: str, config: dict | None = None) -> str:
        if not results:
            return ""

        results_text = "\n".join(
            "\n".join(
                [
                    f"### {r.task_id}（{r.agent}）{'完成' if r.success else '失败'}",
                    f"失败类型: {r.error_type or '-'}",
                    f"失败原因: {r.error_message or '-'}",
                    _truncate_text(r.content),
                ]
            )
            for r in results
        )

        llm = ChatOpenAI(
            model=settings.llm.model,
            base_url=settings.llm.base_url,
            api_key=settings.llm.api_key,
        )
        prompt = _AGGREGATE_PROMPT.format(
            current_time=_current_time_context(),
            overview=overview,
            results=results_text,
        )
        response = await llm.ainvoke([HumanMessage(content=prompt)], config=config)
        details = response.content.strip()
        summary = build_final_summary_block(results)
        return summary if not details else f"{summary}\n\n{details}"
