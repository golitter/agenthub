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

class AgentRequest(BaseModel):         # generated 基类
    task_id: str                           # 任务 ID
    session_id: str                        # 会话 ID，复用已有会话
    message: str                           # 用户消息
    agent_type: AgentType = CLAUDE_CODE    # Agent 类型（枚举）
    stream: bool = True                    # 是否流式返回
    system_prompt: str | None = None       # 自定义系统提示词
    rules: list[str] = Field(default_factory=list)   # 规则名称列表（schemas 层扩展）
    workspace_path: str | None = None      # 工作空间路径
    repo_path: str | None = None           # Git 仓库路径（自动创建 worktree）
    config: dict | None = None             # 额外配置（如 allowed_tools）
    group_chat_messages: list[Any] = []    # 跨 Agent 上下文消息（Orchestrator 场景）
```

### AgentResponse (`src/schemas/response.py`)

同步模式的响应模型：

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
    DONE = "done"              # 执行完成
    ERROR = "error"            # 错误
    PLAN_REVIEW = "plan_review"   # 规划审查
    RUNTIME_EXECUTING = "runtime_executing"   # Runtime 正在执行 Agent
    RUNTIME_TEXT = "runtime_text"             # Runtime 产生的文本事件
    RUNTIME_COMPLETED = "runtime_completed"   # Runtime 执行完成
    COORDINATION_START = "coordination_start"   # 协调开始
    COORDINATION_MESSAGE = "coordination_message" # 协调消息
    COORDINATION_DONE = "coordination_done"     # 协调完成
    ASK_CARD_START = "ask_card_start"           # 跨 Agent 提问开始
    ASK_CARD_DONE = "ask_card_done"             # 跨 Agent 提问完成

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
