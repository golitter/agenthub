# CLI Adapter 适配差异对比

## 概述

三个 CLI Adapter（ClaudeCodeAdapter、OpenCodeAdapter、CodexAdapter）共享相同的 subprocess 模式，但 CLI 接口差异导致命令构建、输出解析、会话恢复的实现各不相同。OrchestratorAdapter 是 in-process 模式，不在本文档范围内。

## 架构总览

```
┌─────────────────────────────────────────────────────────────────┐
│                      BaseAgentAdapter                           │
│  create_session / chat / stream_chat / interrupt / destroy     │
├─────────────────┬─────────────────┬─────────────────────────────┤
│ ClaudeCodeAdapter│ OpenCodeAdapter │      CodexAdapter          │
│ (claude -p)     │ (opencode run)  │   (codex exec)             │
│                 │                 │                             │
│ --output-format │ --format json   │ --json                      │
│ stream-json     │                 │                             │
│                 │                 │ --dangerously-bypass-       │
│                 │                 │ approvals-and-sandbox       │
│                 │                 │                             │
│                 │                 │ -s workspace-write          │
├─────────────────┴─────────────────┴─────────────────────────────┤
│  共享: asyncio subprocess / _processes dict / SIGTERM→SIGKILL  │
└─────────────────────────────────────────────────────────────────┘
```

## 命令构建对比

### 基础命令

| Adapter | 命令模板 |
|---------|----------|
| Claude | `claude -p <MSG> --output-format stream-json --verbose --include-partial-messages` |
| OpenCode | `opencode run <MSG> --format json` |
| Codex | `codex exec --json --dangerously-bypass-approvals-and-sandbox -s workspace-write <MSG>` |

### 参数适配

| 能力 | Claude | OpenCode | Codex |
|------|--------|----------|-------|
| 工作目录 | `cwd=` 传入 subprocess | `--dir <path>` | `-C <path>` |
| 模型覆盖 | 无 | `--model <model>` | `-m <model>` |
| 系统提示词追加 | `--append-system-prompt` | 拼入 prompt 前 `[系统约束: ...]` | 不支持 |
| 工具限制 | `--allowedTools` | 不支持 | 不支持 |
| 轮次限制 | `--max-turns` | 不支持 | 不支持 |
| 沙箱 | 无内置 | 无内置 | `-s workspace-write` |
| 审批跳过 | 无内置 | 无内置 | `--dangerously-bypass-approvals-and-sandbox` |

### 会话恢复

| Adapter | 首次 | 恢复 |
|---------|------|------|
| Claude | `--session-id <ID>` | `--resume <ID>` |
| OpenCode | `--session <ID>` | `--session <ID> --fork` |
| Codex | 无参数（从 thread.started 事件获取 thread_id） | `exec resume <ID>` |

Codex 恢复命令的特殊性：
- `resume` 是 `exec` 的子命令，不是 flag
- resume **不支持** `-C` 和 `-s` 参数
- 命令格式：`codex exec resume <SESSION_ID> --json --dangerously-bypass-approvals-and-sandbox <PROMPT>`

## 输出解析对比

### 输出格式

三者均为 NDJSON（每行一个 JSON 对象），但事件结构不同。

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

### 关键差异

**1. 工具执行进度**

Codex 独有 `item.started` 事件，可以在工具执行**过程中**通知前端，Claude 和 OpenCode 只有完成事件。

**2. 思考过程**

| Adapter | 思考事件 | 处理方式 |
|---------|----------|----------|
| Claude | `stream_event → thinking` delta | 直接作为 TEXT 输出 |
| OpenCode | `type: reasoning` | 加 `[thinking]` 前缀 |
| Codex | `item.completed → reasoning` | 加 `[thinking]` 前缀 |

**3. 工具类型**

| Adapter | 工具标识 | 输出 |
|---------|----------|------|
| Claude | `name`（如 Read、Write） | `content` 字段 |
| OpenCode | `part.tool` | `state.output` |
| Codex | 固定为 `command_execution` | `aggregated_output` + `exit_code` |

Codex 的工具调用全部通过 shell 执行，工具名统一是 `command_execution`，实际命令在 `command` 字段中。

**4. 错误处理**

三者 stderr 处理一致：进程异常退出时读取 stderr 生成 ERROR 事件。Codex 的模型列表刷新 ERROR 日志也输出到 stderr，但不影响 stdout 的事件流解析。

## 进程管理

三个 CLI Adapter 共享相同的进程管理模式：

```python
# 启动
process = await asyncio.create_subprocess_exec(*cmd, stdout=PIPE, stderr=PIPE, cwd=...)
self._processes[session_id] = process

# 读取
async for line in process.stdout:
    event = self._parse_stream_line(line.decode())
    if event:
        yield event

# 中断
process.terminate()  # SIGTERM
await asyncio.wait_for(process.wait(), timeout=settings.execution.process_terminate_timeout)
# 超时后
process.kill()  # SIGKILL
```

配置来自 `config.yaml` 的 `execution.process_terminate_timeout`。

## 配置差异

| 配置项 | Claude | OpenCode | Codex |
|--------|--------|----------|-------|
| CLI 路径 | `cli.claude_path` | `cli.opencode_path` | `cli.codex_path` |
| 配置目录 | `~/.claude/` | `~/.opencode/` | `~/.codex/` |

配置目录用于 workspace 隔离时排除（写入 `.git/info/exclude`），防止 agent 的本地配置被提交到仓库。
