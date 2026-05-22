from pydantic import BaseModel, Field


class TaskDef(BaseModel):
    task_id: str = Field(description="任务唯一标识，如 task-001")
    session_id: str = Field(description="负责执行的 agent id，如 claude-code, opencode")
    title: str = Field(description="任务标题，简明扼要")
    content: str = Field(description="任务的详细描述和执行要求")


class PlanOutput(BaseModel):
    overview: str = Field(description="整体规划概述，描述如何分解用户需求")
    tasks: list[TaskDef] = Field(description="拆解后的任务列表，按执行顺序排列")
