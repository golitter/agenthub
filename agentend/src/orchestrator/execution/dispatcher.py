from __future__ import annotations

from src.orchestrator.models import DispatchResult, PlanOutput


class Dispatcher:
    def __init__(self, agents: list[dict]) -> None:
        self.agents = agents
        self._agent_map = {a["id"]: a for a in agents}

    def dispatch(self, plan: PlanOutput) -> list[DispatchResult]:
        results: list[DispatchResult] = []
        valid_ids = set(self._agent_map.keys())
        for task in plan.tasks:
            if task.session_id not in valid_ids:
                # Fallback: assign to first available agent if session_id is invalid (e.g. a skill name)
                fallback = next(iter(self._agent_map), None)
                if fallback:
                    task.session_id = fallback
            agent_cfg = self._agent_map.get(task.session_id, {})
            workspace_path = agent_cfg.get("workspace_path", "")
            real_session_id = agent_cfg.get("session_id", "")
            agent_type = agent_cfg.get("type", task.session_id)

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
