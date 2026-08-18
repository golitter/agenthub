# CLI Adapter 适配差异对比

## 实现了什么

四个 CLI Adapter（ClaudeCodeAdapter、OpenCodeAdapter、CodexAdapter、PiAdapter）共享相同的 subprocess 模式，但 CLI 接口差异导致命令构建、输出解析、会话恢复的实现各不相同。本文档对比四者的关键差异，OrchestratorAdapter 是 in-process 模式，不在本文档范围内。

## 怎么实现的

## 架构总览

```
┌─────────────────────────────────────────────────────────────────┐
│                      BaseAgentAdapter                           │
│  create_session / chat / stream_chat / interrupt / destroy     │
├─────────────────┬─────────────────┬─────────────────────────────┤
│ ClaudeCodeAdapter│ OpenCodeAdapter │ CodexAdapter │  PiAdapter  │
│ (claude -p)      │ (opencode run)  │ (codex exec) │ (pi --mode) │
│                 │                 │                             │
│ --output-format │ --format json   │ --json                      │
│ stream-json     │                 │                             │
│                 │                 │ --dangerously-bypass-       │
│                 │                 │ approvals-and-sandbox       │
│                 │                 │                             │
│                 │                 │ -s danger-full-access       │
├─────────────────┴─────────────────┴─────────────────────────────┤
│  共享: asyncio subprocess / _processes dict / SIGTERM→SIGKILL  │
└─────────────────────────────────────────────────────────────────┘
```

## 命令构建对比

### 基础命令

| Adapter | 命令模板 |
|---------|----------|
| Claude | `claude -p <MSG> --output-format stream-json --verbose --include-partial-messages --dangerously-skip-permissions` |
| OpenCode | `opencode run <MSG> --format json` |
| Codex | `codex exec --json --dangerously-bypass-approvals-and-sandbox --disable apps --disable plugins -s danger-full-access <MSG>` |
| Pi | `pi --mode json --approve --no-extensions --no-prompt-templates <MSG>` |

### 参数适配

| 能力 | Claude | OpenCode | Codex | Pi |
|------|--------|----------|-------|----|
| 工作目录 | `cwd=` 传入 subprocess | `--dir <path>` | `-C <path>` | `cwd=` 传入 subprocess |
| 模型覆盖 | 无 | `--model <model>` | `-m <model>` | `--model <model>` |
| 系统提示词追加 | `--append-system-prompt` | 拼入 prompt 前 `[系统约束: ...]` | 拼入 prompt 前 `[系统约束: ...]` | `--append-system-prompt` |
| 工具限制 | `--allowedTools` | 不支持 | 不支持 | `--tools <comma-separated names>` |
| 轮次限制 | `--max-turns` | 不支持 | 不支持 | Run timeout / budget |
| 沙箱 | 无内置 | 无内置 | `-s danger-full-access` | `--approve` 信任 worktree |
| 扩展/模板 | CLI 默认 | CLI 默认 | `--disable plugins` | `--no-extensions --no-prompt-templates` |

### 会话恢复

| Adapter | 首次 | 恢复 |
|---------|------|------|
| Claude | 不传 session 参数，CLI 自建 session（INIT 事件回写 mapping） | `--resume <ID>` |
| OpenCode | 不传 session 参数，CLI 自建 session（INIT 事件回写 mapping） | `--session <ID> --fork` |
| Codex | 无参数（从 thread.started 事件获取 thread_id） | `exec resume <ID>` |
| Pi | 不传 session 参数，CLI 从 `session` 事件返回 ID（INIT 事件回写 mapping） | `--session <ID>` |

Codex 恢复命令的特殊性：
- `resume` 是 `exec` 的子命令，不是 flag
- resume **不支持** `-C` 和 `-s` 参数
- 命令格式：`codex exec resume <SESSION_ID> --json --dangerously-bypass-approvals-and-sandbox --disable apps --disable plugins <PROMPT>`

## 输出解析对比

### 输出格式

四者均为 NDJSON/JSONL（每行一个 JSON 对象），但事件结构不同。

### 事件映射

#### Claude CLI (`stream-json`)

```jsonl
{"type":"system","session_id":"..."}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"text":"..."}}}
{"type":"tool_use","name":"Read","input":{...}}
{"type":"tool_result","tool_use_id":"...","content":"..."}
{"type":"result","result":"...","usage":{...}}
```

| CLI 事件 | → StreamEvent |
|----------|---------------|
| `type: system` | INIT（提取 session_id） |
| `stream_event → content_block_delta` | TEXT（token 级流式） |
| `type: tool_use` | TOOL_CALL（name + input） |
| `type: tool_result` | TOOL_RESULT（tool_use_id + content） |
| `type: result` | DONE（result + usage） |
| 非 JSON 行 | TEXT（原文包装） |

#### OpenCode CLI (`--format json`)

```jsonl
{"type":"step_start","sessionID":"..."}
{"type":"text","part":{"text":"..."}}
{"type":"reasoning","part":{"text":"..."}}
{"type":"tool_use","part":{"tool":"Read","state":{"input":{},"status":"","output":""}}}
{"type":"step_finish"}
{"type":"error","error":{...}}
```

| CLI 事件 | → StreamEvent |
|----------|---------------|
| `type: step_start` | INIT（提取 sessionID） |
| `type: text` | TEXT（part.text） |
| `type: reasoning` | TEXT（加 `[thinking]` 前缀） |
| `type: tool_use` | TOOL_CALL / TOOL_RESULT（按 state.status 判断） |
| `type: error` | ERROR |
| 非 JSON 行 | TEXT（原文包装） |

#### Codex CLI (`--json`)

```jsonl
{"type":"thread.started","thread_id":"..."}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"ls -la","status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"ls -la","aggregated_output":"...","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"..."}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"..."}}
{"type":"turn.completed","usage":{"input_tokens":...,"output_tokens":...}}
```

| CLI 事件 | → StreamEvent |
|----------|---------------|
| `thread.started` | INIT（提取 thread_id） |
| `turn.started` | 忽略 |
| `item.started` (command_execution) | TOOL_CALL（command） |
| `item.completed` (reasoning) | TEXT（加 `[thinking]` 前缀） |
| `item.completed` (agent_message) | TEXT（正文） |
| `item.completed` (command_execution) | TOOL_RESULT（aggregated_output + exit_code） |
| `turn.completed` | DONE（usage） |
| 未知事件 | 忽略 |

#### Pi CLI (`--mode json`)

```jsonl
{"type":"session","version":3,"id":"...","cwd":"/workspace"}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"..."}}
{"type":"tool_execution_start","toolName":"read","args":{"path":"a.py"}}
{"type":"tool_execution_end","toolName":"read","result":"...","isError":false}
{"type":"agent_end","messages":[]}
```

| CLI 事件 | → StreamEvent |
|----------|---------------|
| `session` | INIT（提取 `id`） |
| `message_update → text_delta` | TEXT（真实增量） |
| `message_update → thinking_end` | TEXT（加 `[thinking]` 前缀） |
| `tool_execution_start` | TOOL_CALL（`toolName` + `args`） |
| `tool_execution_end` | TOOL_RESULT（`toolName` + `result` + `isError`） |
| `message_update → error` | ERROR（提取 `errorMessage`） |
| `agent_end` | DONE（聚合 assistant usage） |
| 生命周期、`thinking_delta`、`tool_execution_update` | 忽略 |

### 关键差异

**1. 工具执行进度**

Codex 独有 `item.started` 事件，可以在工具执行**过程中**通知前端，Claude 和 OpenCode 只有完成事件。

**2. 思考过程**

| Adapter | 思考事件 | 处理方式 |
|---------|----------|----------|
| Claude | `stream_event → thinking` delta | 忽略（不输出） |
| OpenCode | `type: reasoning` | 加 `[thinking]` 前缀 |
| Codex | `item.completed → reasoning` | 加 `[thinking]` 前缀 |
| Pi | `thinking_end` | 加 `[thinking]` 前缀 |

**3. 工具类型**

| Adapter | 工具标识 | 输出 |
|---------|----------|------|
| Claude | `name`（如 Read、Write） | `content` 字段 |
| OpenCode | `part.tool` | `state.output` |
| Codex | 固定为 `command_execution` | `aggregated_output` + `exit_code` |
| Pi | `toolName`（如 `read`、`bash`） | `result` 字段 |

Codex 的工具调用全部通过 shell 执行，工具名统一是 `command_execution`，实际命令在 `command` 字段中。

**4. 错误处理**

四者 stderr 处理一致：进程异常退出时读取有界 stderr 生成 ERROR 事件。Pi 若已经输出相同协议级 ERROR，不会重复转换同一错误。

## 进程管理

四个 CLI Adapter 共享相同的进程管理模式：

```python
# 启动
process = await asyncio.create_subprocess_exec(*cmd, stdout=PIPE, stderr=PIPE, cwd=...)
self._processes[session_id] = process

# 读取
async for line in process.stdout:
    event = self._parse_stream_line(line.decode())
    if event:
        yield event

# 中断（src/adapters/base.py: terminate_process_group，独立进程组整组终止）
os.killpg(process.pid, signal.SIGTERM)          # SIGTERM 整组（含 CLI 子孙进程）
while 轮询 os.killpg(pid, 0) 至超时:            # timeout=settings.execution.process_terminate_timeout
os.killpg(process.pid, signal.SIGKILL)          # 超时后 SIGKILL 整组
```

配置来自 `config.yaml` 的 `execution.process_terminate_timeout`。

## 配置差异

| 配置项 | Claude | OpenCode | Codex | Pi |
|--------|--------|----------|-------|----|
| CLI 路径 | `agents.json` → `claude-code.cli_path` | `agents.json` → `opencode.cli_path` | `agents.json` → `codex.cli_path` | `agents.json` → `pi.cli_path` |
| 配置目录（workspace 内，`agents.json` 的 `config_dir`） | `.claude/` | `.opencode/` | `.codex/` | `.pi/` |

配置目录用于 workspace 隔离时排除（通过 `git_ops.setup_worktree_excludes` 写入 worktree 本地 excludes 文件，并经 `git config --worktree core.excludesFile` 注册），防止 agent 的本地配置被提交到仓库。

> **CLI 路径优先级**（见 `src/app/agent_config.py:get_agent_cli_path()`）：环境变量 `<AGENT_TYPE>_CLI_PATH`（如 `CODEX_CLI_PATH`、`CLAUDE_CODE_CLI_PATH`、`PI_CLI_PATH`）优先于 `agents.json` 的 `cli_path`。仅在环境变量未设置时才回退到 `agents.json`。系统配置文件路径同样使用 `<AGENT_TYPE>_CONFIG_PATH` 覆盖 `config.yaml`。
