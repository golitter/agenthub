# Adapter Layer — 适配器层

## 实现了什么

统一 Agent 适配器抽象，当前实现 `ClaudeCodeAdapter`、`OpenCodeAdapter`、`CodexAdapter` 和 `OrchestratorAdapter`，通过 CLI subprocess 驱动或 LLM 调用执行。

## 怎么实现的

### BaseAgentAdapter (`src/adapters/base.py`)

抽象基类，定义所有 Adapter 必须实现的接口：

```python
class BaseAgentAdapter(ABC):
    async def create_session(session_id) -> None
    async def chat(session_id, message, **kwargs) -> AgentResponse      # 同步调用
    async def stream_chat(session_id, message, **kwargs) -> AsyncIterator[StreamEvent]  # 流式调用
    async def interrupt(session_id) -> bool                              # 中断执行
    async def destroy_session(session_id) -> None                        # 销毁会话
```

### AdapterRegistry (`src/adapters/registry.py`)

通过 AgentType 枚举值（str）注册和查找 Adapter 类：

```python
registry.register(AgentType.CLAUDE_CODE, ClaudeCodeAdapter)  # key 实际存储为 str
registry.register(AgentType.OPENCODE, OpenCodeAdapter)
registry.register(AgentType.ORCHESTRATOR, OrchestratorAdapter)
registry.register(AgentType.CODEX, CodexAdapter)
adapter_cls = registry.get(AgentType.CLAUDE_CODE)  # 返回类，由调用方实例化
```

查找不存在的类型时抛出 `ValueError`。

### ClaudeCodeAdapter (`src/adapters/claude.py`)

核心实现，通过 `asyncio.create_subprocess_exec` 启动 Claude CLI 进程。

#### 命令构建 (`_build_command`)

将请求参数组装为 CLI 命令：

```python
claude -p "<message>" --output-format stream-json --verbose --include-partial-messages --dangerously-skip-permissions \
    [--resume <cli_session_id> | --session-id <cli_session_id>] \
    [--append-system-prompt "<text>"] \
    [--allowedTools Read,Write] \
    [--max-turns 20]
```

参数来源：
- `-p`：用户消息
- `--verbose`：输出 system init 事件（含 CLI session_id）
- `--include-partial-messages`：token 级流式输出（stream_event 类型）
- `--resume` / `--session-id`：复用或新建会话（对应 SessionMappingStore 映射）
- `--append-system-prompt`：Rule Engine 注入的约束文本
- `--allowedTools`：Rule Engine 限制的工具集
- `--max-turns`：执行轮数限制

#### 流式输出解析 (`_parse_stream_line`)

逐行读取 CLI 的 stdout，解析 JSON 并转换为 StreamEvent：

1. 空行 → 忽略（返回 None）
2. `stream_event` 类型 → 提取 `content_block_delta` 中的 text → TEXT 事件（token 级流式）
3. 合法 JSON → 按 `_TYPE_MAP` 映射为对应 StreamEvent（system→INIT, tool_use→TOOL_CALL 等）
4. 未映射的 JSON 类型 → 忽略（返回 None）
5. 非法 JSON → 包装为 TEXT 类型事件，不抛异常

### OpenCodeAdapter (`src/adapters/opencode.py`)

OpenCode CLI 适配器，结构与 ClaudeCodeAdapter 类似，通过 `asyncio.create_subprocess_exec` 启动 OpenCode CLI 进程。

#### 命令构建 (`_build_command`)

```python
opencode run <message> --format json [--dir <workspace>] [--model <model>] [--session <id> --fork]
```

参数来源：
- `run`：执行模式
- `--format json`：NDJSON 流式输出
- `--dir`：工作目录（用于 worktree 隔离）
- `--model`：模型覆盖
- `--session` / `--fork`：复用已有会话

#### 流式输出解析 (`_parse_ndjson_line`)

逐行读取 CLI 的 stdout（NDJSON 格式），解析并转换为 StreamEvent：

1. 空行 → 忽略（返回 None）
2. 合法 JSON → 按事件类型映射（step_start→INIT, text→TEXT, reasoning→TEXT 加前缀, tool_use→TOOL_CALL/TOOL_RESULT, error→ERROR, step_finish→忽略）
3. 非 JSON → 包装为 TEXT 类型事件

#### 流式调用 (`stream_chat`)

```python
async def stream_chat(self, session_id, message, **kwargs) -> AsyncIterator[StreamEvent]:
    cmd = self._build_command(message, ...)
    process = await asyncio.create_subprocess_exec(*cmd, stdout=PIPE, stderr=PIPE, cwd=kwargs.get("cwd"))
    self._processes[session_id] = process  # 记录进程句柄

    async for line in process.stdout:
        event = self._parse_stream_line(line.decode())
        if event:
            yield event

    # 进程退出后清理
    await process.wait()
    if process.returncode != 0:
        yield StreamEvent.create(EventType.ERROR, error=stderr, returncode=process.returncode)
    self._processes.pop(session_id, None)
```

进程通过 `dict[str, Process]` 管理，key 为 session_id。支持 `cwd` kwarg 指定工作目录（用于 worktree 隔离）。

#### 同步调用 (`chat`)

内部调用 `stream_chat`，收集所有 text 事件拼接后返回 `AgentResponse`。

#### 中断机制 (`interrupt`)

```
SIGTERM → 等待超时 → SIGKILL
```

1. 查找 session 对应的进程
2. 发送 SIGTERM
3. `asyncio.wait_for(process.wait(), timeout=config.execution.process_terminate_timeout)`
4. 超时则发送 SIGKILL
5. 从 `_processes` 中移除

超时时长来自 `config.yaml` 的 `execution.process_terminate_timeout`。

#### NDJSON 事件映射

| NDJSON type | → EventType | 特殊处理 |
|---|---|---|
| `error` | `ERROR` | 提取嵌套 error.data.message |
| `step_start` | `INIT` | 提取 sessionID |
| `text` | `TEXT` | 提取 part.text |
| `reasoning` | `TEXT` | 加 `[thinking]` 前缀 |
| `tool_use` | `TOOL_CALL` / `TOOL_RESULT` | status=error 时转 TOOL_RESULT |
| `step_finish` | 忽略 | — |

### CodexAdapter (`src/adapters/codex.py`)

Codex CLI 适配器，结构与 ClaudeCodeAdapter 类似，通过 `asyncio.create_subprocess_exec` 启动 Codex CLI 进程。

#### 命令构建 (`_build_command`)

```python
# 新建会话
codex exec --json --dangerously-bypass-approvals-and-sandbox --disable apps --disable plugins -s danger-full-access <message> [-C <cwd>] [-m <model>]

# 恢复会话
codex exec resume <cli_session_id> --json --dangerously-bypass-approvals-and-sandbox --disable apps --disable plugins <message>
```

参数来源：
- `exec`：执行模式
- `--json`：JSON 流式输出
- `--dangerously-bypass-approvals-and-sandbox`：跳过审批和沙箱
- `--disable apps --disable plugins`：禁用 apps 和 plugins
- `-s danger-full-access`：允许完全访问工作区
- `-C`：工作目录（用于 worktree 隔离）
- `resume`：复用已有会话

#### 流式输出解析 (`_parse_stream_line`)

Codex CLI 输出 JSON Lines 格式，逐行解析：

| 事件类型 | 映射 |
|---------|------|
| `thread.started` | INIT（提取 `thread_id` 作为 `cli_session_id`） |
| `item.started`（type=command_execution） | TOOL_CALL |
| `item.completed`（type=reasoning） | TEXT（带 `[thinking]` 前缀） |
| `item.completed`（type=agent_message） | TEXT |
| `item.completed`（type=command_execution） | TOOL_RESULT |
| `turn.completed` | DONE（含 usage） |
| `turn.started` | 忽略 |

#### 中断机制

与 ClaudeCodeAdapter 相同：SIGTERM → 等待超时 → SIGKILL。
