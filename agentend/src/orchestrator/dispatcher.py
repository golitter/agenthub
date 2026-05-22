from __future__ import annotations

from src.orchestrator.models import DispatchResult, PlanOutput


class Dispatcher:
    def __init__(self, agents: list[dict]) -> None:
        self.agents = agents
        self._agent_map = {a["id"]: a for a in agents}

    def dispatch(self, plan: PlanOutput) -> list[DispatchResult]:
        results: list[DispatchResult] = []
        for task in plan.tasks:
            agent_cfg = self._agent_map.get(task.session_id, {})
            workspace_path = agent_cfg.get("workspace_path", "")

            results.append(
                DispatchResult(
                    task_id=task.task_id,
                    agent=task.session_id,
                    mention=f"@{task.session_id}",
                    content=task.content,
                    depends_on=[],
                    workspace_path=workspace_path,
                )
            )
        return results
