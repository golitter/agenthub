# Schemas — 数据模型

## 实现了什么

定义了三个核心 Pydantic 数据模型，作为整个系统的统一消息协议。

## 怎么实现的

### AgentRequest (`src/schemas/request.py`)

请求模型（继承自 `src/generated/request.py`），Go Backend 调用 Runtime 时传入：

```python
class AgentType(str, Enum):
    CLAUDE_CODE = "claude-code"
    OPENCODE = "opencode"
    ORCHESTRATOR = "orchestrator"
    CODEX = "codex"
    PI = "pi"

class AgentRequest(BaseModel):         # generated 基类
    task_id: str                           # 任务 ID（schemas 层加正则与长度约束）
    session_id: str                        # 会话 ID，复用已有会话（schemas 层加正则与长度约束）
    message: str                           # 用户消息
    agent_type: AgentType = CLAUDE_CODE    # Agent 类型（枚举）
    stream: bool = True                    # 是否流式返回
    system_prompt: str | None = None       # 自定义系统提示词
    rules: list[str] = Field(default_factory=list)   # 规则名称列表（schemas 层覆盖 generated 默认值）
    workspace_path: str | None = None      # 工作空间路径
    repo_path: str | None = None           # Git 仓库路径（自动创建 worktree）
    config: dict | None = None             # 额外配置（如 allowed_tools，schemas 层收窄类型）
    group_chat_messages: list[dict[str, Any]] = []    # 跨 Agent 上下文消息（Orchestrator 场景）
    # Run 生命周期相关字段（generated 基类提供，由 RunSupervisor 消费）
    message_id: str | None = None          # 关联的 Backend 消息 ID
    artifact_upload_token: str | None = None  # 产物上传凭证
    run_id: str | None = None              # 指定 Run ID（缺省自动生成 UUID）
    root_run_id: str | None = None         # 根 Run ID（子任务继承）
    parent_run_id: str | None = None       # 父 Run ID（Orchestrator 分发）
    workspace_id: str | None = None        # 关联 workspace ID
    budget: dict | None = None             # 资源预算（wall_time/max_turns/max_output_bytes 等，见 AgentRunBudget）
    # 内部子 Run 身份字段（schemas 层加长度/正则约束，供集成与冲突恢复追溯）
    current_run_id: str | None = None      # 当前子 Run ID（UUID 校验）
    plan_task_id: str | None = None        # 关联的规划任务 ID
    integration_operation_id: str | None = None  # 集成操作 ID（UUID 校验）
    workspace_handle: str | None = None    # 工作区句柄（integrate/resolve 场景）
    integration_capability: str | None = None   # 集成能力声明
    integration_attempt: int = 0           # 集成重试计数（0..100）
```

`schemas` 层还有一个 `validate_internal_lineage` 模型校验器：当请求携带 `integration_operation_id`（内部子 Run）时，强制要求 `run_id`/`root_run_id`/`parent_run_id`/`current_run_id`/`plan_task_id`/`workspace_id`/`workspace_handle` 全部非空，且四个血缘 ID 必须是合法 UUID；`integration_attempt` 必须依附于内部规划任务。

### AgentResponse (`src/schemas/response.py`)

同步模式的响应模型（继承自 `src/generated/response.py`，schemas 层为 `artifacts`/`usage` 补充 `Field` 默认值）：

```python
class AgentResponse(BaseModel):
    session_id: str              # 会话 ID
    content: str                 # Agent 输出的文本内容
    artifacts: list[dict] = Field(default_factory=list)   # 产物列表（如生成的文件）
    usage: dict = Field(default_factory=dict)             # Token 使用量
```

### StreamEvent (`src/schemas/events.py`)

流式模式的事件模型（继承自 `src/generated/events.py`），对应 SSE 中的每个事件：

```python
class EventType(str, Enum):
    INIT = "init"              # CLI 会话初始化
    TEXT = "text"              # 文本输出
    TOOL_CALL = "tool_call"    # 工具调用
    TOOL_RESULT = "tool_result" # 工具执行结果
    ARTIFACT = "artifact"      # 产物
    PLANNING = "planning"      # Orchestrator 规划阶段
    PLAN_REVIEW = "plan_review"   # 规划审查
    DONE = "done"              # 执行完成
    ERROR = "error"            # 错误
    HEARTBEAT = "heartbeat"    # SSE 心跳（保活）
    RUNTIME_EXECUTING = "runtime_executing"   # Runtime 正在执行 Agent
    RUNTIME_TEXT = "runtime_text"             # Runtime 产生的文本事件
    RUNTIME_COMPLETED = "runtime_completed"   # Runtime 执行完成
    COORDINATION_START = "coordination_start"   # 协调开始
    COORDINATION_MESSAGE = "coordination_message" # 协调消息
    COORDINATION_DONE = "coordination_done"     # 协调完成
    ASK_CARD_START = "ask_card_start"           # 跨 Agent 提问开始
    ASK_CARD_DONE = "ask_card_done"             # 跨 Agent 提问完成
    INTEGRATION_STARTED = "integration_started"       # 产物集成开始
    INTEGRATION_COMPLETED = "integration_completed"   # 产物集成完成
    INTEGRATION_CONFLICT = "integration_conflict"     # 产物集成冲突
    RESOLUTION_STARTED = "resolution_started"         # 冲突解决开始
    RESOLUTION_PROGRESS = "resolution_progress"       # 冲突解决进度
    RESOLUTION_COMPLETED = "resolution_completed"     # 冲突解决完成
    RESOLUTION_FAILED = "resolution_failed"           # 冲突解决失败
    ORCHESTRATOR_PAUSED = "orchestrator_paused"       # 编排等待用户确认

class StreamEvent(_StreamEvent):   # generated 基类中 type: EventType，schemas 层覆盖为 str
    type: str                  # EventType 枚举值（字符串形式）
    content: dict = Field(default_factory=dict)  # 事件内容
    timestamp: float = Field(default_factory=time.time)  # 时间戳

    @staticmethod
    def create(event_type, agent_type=None, **kwargs) -> StreamEvent  # 工厂方法
```

SSE 输出格式：`event: <type>\ndata: <json>\n\n`

### CLI 输出类型映射

Claude Code CLI 的 stream-json 输出类型 → StreamEvent 类型：

| CLI 输出 type | StreamEvent type | 说明 |
|---------------|-----------------|------|
| `system`      | `init`          | 包含 `session_id` |
| `stream_event`| `text`          | token 级流式（`--include-partial-messages`），提取 `content_block_delta` |
| `assistant`   | 忽略            | 完整 assistant 消息，已被 stream_event 覆盖 |
| `tool_use`    | `tool_call`     | 工具名 + 参数 |
| `tool_result` | `tool_result`   | 工具执行结果 |
| `result`      | `done`          | 最终文本 + usage |
| 非 JSON 行    | `text`          | 原文包装为 TEXT 事件 |
| 其他          | 忽略            | 返回 None |
