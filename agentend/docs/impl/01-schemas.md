# Schemas — 数据模型

## 实现了什么

定义了三个核心 Pydantic 数据模型，作为整个系统的统一消息协议。

## 怎么实现的

### AgentRequest (`src/schemas/request.py`)

请求模型，Go Backend 调用 Runtime 时传入：

```python
class AgentRequest(BaseModel):
    task_id: str                           # 任务 ID
    session_id: str                        # 会话 ID，复用已有会话
    message: str                           # 用户消息
    agent_type: str = "claude-code"        # Agent 类型，默认 claude-code
    stream: bool = True                    # 是否流式返回
    system_prompt: str | None = None       # 自定义系统提示词
    rules: list[str] = []                  # 规则名称列表
    workspace_path: str | None = None      # 工作空间路径
    config: dict | None = None             # 额外配置（如 allowed_tools）
```

### AgentResponse (`src/schemas/response.py`)

同步模式的响应模型：

```python
class AgentResponse(BaseModel):
    session_id: str              # 会话 ID
    content: str                 # Agent 输出的文本内容
    artifacts: list[dict] = []   # 产物列表（如生成的文件）
    usage: dict = {}             # Token 使用量
```

### StreamEvent (`src/schemas/events.py`)

流式模式的事件模型，对应 SSE 中的每个事件：

```python
class EventType(str, Enum):
    TEXT = "text"              # 文本输出
    TOOL_CALL = "tool_call"    # 工具调用
    TOOL_RESULT = "tool_result" # 工具执行结果
    ARTIFACT = "artifact"      # 产物
    DONE = "done"              # 执行完成
    ERROR = "error"            # 错误

class StreamEvent(BaseModel):
    type: str                  # EventType 枚举值
    content: dict = {}         # 事件内容
    timestamp: float           # 时间戳
```

SSE 输出格式：`event: <type>\ndata: <json>\n\n`

### CLI 输出类型映射

Claude Code CLI 的 stream-json 输出类型 → StreamEvent 类型：

| CLI 输出 type | StreamEvent type |
|---------------|-----------------|
| `assistant`   | `text`          |
| `tool_use`    | `tool_call`     |
| `tool_result` | `tool_result`   |
| `result`      | `done`          |
| 其他          | `text`（保留原始数据） |
