from __future__ import annotations

import logging
from typing import Annotated, TypedDict

from langgraph.graph import StateGraph

from src.orchestrator.models import DispatchResult

logger = logging.getLogger(__name__)


def _add_results(left: list, right: list) -> list:
    return left + right


class ExecuteState(TypedDict):
    execution_waves: list[list[DispatchResult]]
    task_results: Annotated[list, _add_results]
    shared_dir: str
    task_id: str
    cwd: str
    repo_path: str


def build_execute_subgraph() -> StateGraph:
    """构建一个逐 wave 执行任务的子图。

    每个 wave 内的任务并行执行；wave 之间顺序执行。
    """
    graph = StateGraph(ExecuteState)
    graph.add_node("wave_execute", wave_execute_node)
    graph.set_entry_point("wave_execute")
    graph.set_finish_point("wave_execute")
    return graph.compile()


def wave_execute_node(state: ExecuteState) -> dict:
    """占位节点：wave 的执行由 OrchestratorAdapter 驱动。

    该适配器迭代 execution_waves，并直接使用 ExecutionEngine
    进行异步流式处理。本节点仅将 wave 结构记录到 state 中。
    """
    return {"task_results": []}
