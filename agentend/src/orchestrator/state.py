from enum import Enum


class TaskState(str, Enum):
    PENDING = "pending"
    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"


class RuntimeState:
    def __init__(self) -> None:
        self.tasks: dict[str, TaskState] = {}
        self.results: dict[str, str] = {}
        self.running_agents: dict[str, str] = {}

    def add_task(self, task_id: str) -> None:
        self.tasks[task_id] = TaskState.PENDING

    def set_running(self, task_id: str, agent_id: str = "") -> None:
        self.tasks[task_id] = TaskState.RUNNING
        if agent_id:
            self.running_agents[agent_id] = task_id

    def set_completed(self, task_id: str, result: str = "") -> None:
        self.tasks[task_id] = TaskState.COMPLETED
        if result:
            self.results[task_id] = result

    def set_failed(self, task_id: str) -> None:
        self.tasks[task_id] = TaskState.FAILED
