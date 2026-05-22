from pydantic import BaseModel, Field


class TaskDef(BaseModel):
    task_id: str = Field(description="任务唯一标识，如 task-001")
    session_id: str = Field(description="负责执行的 agent id，如 claude-code, opencode")
    title: str = Field(description="任务标题，简明扼要")
    content: str = Field(description="任务的详细描述和执行要求")


class PlanOutput(BaseModel):
    overview: str = Field(description="整体规划概述，描述如何分解用户需求")
    tasks: list[TaskDef] = Field(description="拆解后的任务列表，按执行顺序排列")


class TaskResult(BaseModel):
    task_id: str = Field(description="任务唯一标识")
    agent: str = Field(description="执行该任务的 agent id")
    success: bool = Field(description="任务是否成功完成")
    content: str = Field(description="任务执行结果内容")
    duration: float = Field(default=0.0, description="执行耗时（秒）")


class DispatchResult(BaseModel):
    task_id: str = Field(description="任务唯一标识")
    agent: str = Field(description="目标 agent id")
    mention: str = Field(description="@agent 群聊提及字符串")
    content: str = Field(description="任务详细描述")
    depends_on: list[str] = Field(default_factory=list, description="依赖的任务 ID 列表")
    workspace_path: str = Field(default="", description="agent 的 workspace 路径")
