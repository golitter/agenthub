from __future__ import annotations

from src.orchestrator.agent_utils import dispatchable_agent_id
from src.orchestrator.models import DispatchResult, PlanOutput


class Dispatcher:
    def __init__(self, agents: list[dict]) -> None:
        self.agents = agents
        self._agent_map = {
            agent_id: agent
            for agent in agents
            if isinstance(agent, dict)
            for agent_id in [dispatchable_agent_id(agent)]
            if agent_id
        }

    def dispatch(self, plan: PlanOutput) -> list[DispatchResult]:
        results: list[DispatchResult] = []
        valid_ids = set(self._agent_map.keys())
        for task in plan.tasks:
            if task.session_id not in valid_ids:
                raise ValueError(f"Unknown agent id: {task.session_id}")

            agent_cfg = self._agent_map[task.session_id]
            workspace_path = agent_cfg.get("workspace_path", "")
            real_session_id = str(agent_cfg.get("session_id") or "").strip()
            if not real_session_id:
                raise ValueError(f"Agent '{task.session_id}' has no session_id")
            agent_type = str(agent_cfg.get("type") or task.session_id).strip()

            results.append(
                DispatchResult(
                    task_id=task.task_id,
                    agent=task.session_id,
                    agent_type=agent_type,
                    real_session_id=real_session_id,
                    mention=f"@{task.session_id}",
                    content=task.content,
                    depends_on=[],
                    workspace_path=workspace_path,
                )
            )
        return results


def topological_sort(dispatch_results: list[DispatchResult]) -> list[list[DispatchResult]]:
    """根据 depends_on 将 DispatchResults 排序为多个执行 wave。

    同一 wave 内的任务可以并行执行。
    wave 之间顺序执行。
    """
    if not dispatch_results:
        return []

    # 构建查找表和依赖图
    by_id: dict[str, DispatchResult] = {dr.task_id: dr for dr in dispatch_results}
    all_ids = set(by_id.keys())

    # 计算每个任务的入度（in-degree）
    in_degree: dict[str, int] = {tid: 0 for tid in all_ids}
    dependents: dict[str, list[str]] = {tid: [] for tid in all_ids}

    for dr in dispatch_results:
        for dep in dr.depends_on:
            if dep in all_ids:
                in_degree[dr.task_id] += 1
                dependents[dep].append(dr.task_id)

    # Kahn 算法
    waves: list[list[DispatchResult]] = []
    remaining = dict(in_degree)

    while remaining:
        # 查找入度为 0 的任务
        ready = [tid for tid, deg in remaining.items() if deg == 0]
        if not ready:
            # 检测到环 —— 将剩余任务放入同一个 wave
            waves.append([by_id[tid] for tid in remaining])
            break

        wave = [by_id[tid] for tid in ready]
        waves.append(wave)

        for tid in ready:
            del remaining[tid]
            for dep_tid in dependents[tid]:
                if dep_tid in remaining:
                    remaining[dep_tid] -= 1

    return waves
