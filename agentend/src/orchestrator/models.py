from typing import Literal

from pydantic import BaseModel, Field, model_validator


ExecutionStatus = Literal["pending", "running", "completed", "failed", "timeout", "cancelled", "blocked"]
IntegrationStatus = Literal[
    "pending",
    "merged",
    "conflict",
    "failed",
    "not_required",
    "awaiting_user",
    "partial",
]
ConflictStatus = Literal[
    "detected",
    "preparing",
    "resolving",
    "verifying",
    "resolved",
    "retryable",
    "awaiting_user",
    "cancelled",
]


class TaskDef(BaseModel):
    task_id: str = Field(description="任务唯一标识，如 task-001")
    session_id: str = Field(description="负责执行的 Agent 公共 id（不是名称、类型或真实 session_id）")
    title: str = Field(description="任务标题，简明扼要")
    content: str = Field(description="任务的详细描述和执行要求")
    depends_on: list[str] = Field(default_factory=list, description="依赖的任务 ID 列表")
    requires_integrated_dependencies: bool = Field(
        default=True,
        description="是否必须等待依赖任务成功集成后才能执行",
    )


class PlanOutput(BaseModel):
    overview: str = Field(description="整体规划概述，描述如何分解用户需求")
    tasks: list[TaskDef] = Field(description="拆解后的任务列表，按执行顺序排列")
    merge_to_main: bool = Field(
        default=False,
        description="任务成功后是否由 orchestrator 请求合并 task 分支到 main",
    )


class TaskResult(BaseModel):
    task_id: str = Field(description="任务唯一标识")
    root_task_id: str = Field(default="", description="根任务 ID，用于恢复和审计")
    agent: str = Field(description="执行该任务的 agent id")
    attempt: int = Field(default=0, ge=0, description="该任务的执行尝试次数")
    execution_status: ExecutionStatus = Field(default="pending", description="Agent 执行状态")
    integration_status: IntegrationStatus = Field(default="pending", description="Git 集成状态")
    # 迁移期保留 success。新代码必须根据 execution_status/integration_status 路由。
    success: bool | None = Field(default=None, description="兼容字段：执行且集成成功")
    content: str = Field(description="任务执行结果内容")
    message_id: str = Field(default="", description="Backend 持久化的 Agent 回复 message_id")
    run_id: str = Field(default="", description="对应 child Run ID")
    plan_task_id: str = Field(default="", description="逻辑计划任务 ID；兼容时等于 task_id")
    integration_operation_id: str = Field(default="", description="关联的集成操作 ID")
    integration_scope_id: str = Field(default="", description="Git 集成范围 ID")
    workspace_id: str = Field(default="", description="具体 WorkspaceManager 记录 ID")
    resolved_from_conflict: bool = Field(default=False, description="是否由 Resolver 从冲突恢复")
    duration: float = Field(default=0.0, description="执行耗时（秒）")
    error_type: str = Field(default="", description="失败类型，如 timeout 或 error")
    error_code: str = Field(default="", description="机器可读错误码")
    error_message: str = Field(default="", description="结构化失败原因")
    conflict_files: list[str] = Field(default_factory=list, description="merge 冲突文件列表")
    source_branch: str = Field(default="", description="产物源分支")
    source_commit: str = Field(default="", description="产物源提交")
    target_branch: str = Field(default="", description="集成目标分支")
    target_commit: str = Field(default="", description="集成前目标提交")
    merge_base: str = Field(default="", description="源/目标共同祖先")
    legacy_conflict_detection: bool = Field(default=False, description="是否使用了文本兼容兜底")

    @model_validator(mode="after")
    def normalize_compatibility_fields(self) -> "TaskResult":
        if not self.root_task_id:
            self.root_task_id = self.task_id
        if not self.plan_task_id:
            self.plan_task_id = self.task_id

        # 旧调用方只提供 success/error_type；将其映射到新的双状态模型。
        if self.execution_status == "pending" and self.success is not None:
            if self.success:
                self.execution_status = "completed"
                if self.integration_status == "pending":
                    self.integration_status = "not_required"
            else:
                self.execution_status = "timeout" if self.error_type == "timeout" else "failed"

        self.success = self.execution_status == "completed" and self.integration_status in {
            "merged",
            "not_required",
        }
        return self

    @property
    def execution_completed(self) -> bool:
        return self.execution_status == "completed"

    @property
    def integration_conflict(self) -> bool:
        return self.integration_status == "conflict"


class IntegrationResult(BaseModel):
    """taskctl merge 写入 shared/.agent/integration-results 的事实结果。"""

    version: int = 1
    run_id: str
    root_run_id: str = ""
    parent_run_id: str = ""
    task_id: str = ""
    integration_operation_id: str = ""
    plan_task_id: str = ""
    integration_scope_id: str = ""
    workspace_id: str = ""
    workspace_handle: str = ""
    session_id: str
    attempt: int = Field(default=0, ge=0)
    status: Literal["merged", "conflict", "failed", "partial"]
    source_branch: str = ""
    source_commit: str = ""
    target_branch: str = ""
    target_commit: str = ""
    merge_base: str = ""
    conflict_files: list[str] = Field(default_factory=list)
    aborted: bool = False
    error_code: str = ""
    error_message: str = ""
    started_at: str = ""
    finished_at: str = ""
    result_digest: str = ""
    target_commit_after: str = ""


class IntegrationResultV1(IntegrationResult):
    """Strict compatibility reader for the legacy taskctl writer.

    In V1, task_id is not a planner task ID. It is the Git integration scope
    extracted from the worktree path.
    """

    version: Literal[1] = 1
    task_id: str = Field(min_length=1)


class IntegrationResultV2(IntegrationResult):
    """Strict reader for the operation-addressed result contract."""

    version: Literal[2] = 2
    run_id: str = Field(min_length=1)
    root_run_id: str = Field(min_length=1)
    parent_run_id: str = Field(min_length=1)
    session_id: str = Field(min_length=1)
    integration_operation_id: str = Field(min_length=1)
    plan_task_id: str = Field(min_length=1)
    integration_scope_id: str = Field(min_length=1)

    @model_validator(mode="after")
    def require_workspace_reference(self) -> "IntegrationResultV2":
        if not self.workspace_id and not self.workspace_handle:
            raise ValueError("V2 result requires workspace_id or workspace_handle")
        return self


class ConflictRecord(BaseModel):
    """持久化的冲突恢复事实，供重连、审计和人工接管使用。"""

    conflict_id: str
    root_run_id: str = ""
    task_id: str
    failed_task_id: str
    original_operation_id: str = ""
    plan_task_id: str = ""
    integration_scope_id: str = ""
    workspace_id: str = ""
    failed_agent: str
    attempt: int = Field(default=0, ge=0)
    source_branch: str = ""
    source_commit: str = ""
    target_branch: str = ""
    target_commit: str = ""
    merge_base: str = ""
    conflict_files: list[str] = Field(default_factory=list)
    resolver_agent: str = ""
    resolver_session_id: str = ""
    resolver_branch: str = ""
    resolver_run_id: str = ""
    status: ConflictStatus
    last_error_code: str = ""
    last_error_message: str = ""


class DispatchResult(BaseModel):
    task_id: str = Field(description="任务唯一标识")
    attempt: int = Field(default=0, ge=0, description="该任务执行尝试次数")
    agent: str = Field(description="目标 agent 名称/id")
    agent_type: str = Field(default="", description="目标 agent 类型，如 claude-code, opencode")
    real_session_id: str = Field(default="", description="DB 分配的真实 session_id")
    mention: str = Field(description="@agent 群聊提及字符串")
    content: str = Field(description="任务详细描述")
    depends_on: list[str] = Field(default_factory=list, description="依赖的任务 ID 列表")
    requires_integrated_dependencies: bool = Field(default=True, description="是否要求依赖已集成")
    workspace_path: str = Field(default="", description="agent 的 workspace 路径")
    plan_task_id: str = Field(default="", description="逻辑计划任务 ID；与 task_id 兼容")
    integration_operation_id: str = Field(default="", description="预登记的集成操作 ID")
    workspace_handle: str = Field(default="", description="不透明 Workspace 引用")
    integration_scope_id: str = Field(default="", description="Git 集成范围 ID")
