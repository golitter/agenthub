# Pi CLI Adapter 实现方案

## 文档状态

- 状态：已实现（自动化验证通过；真实 Pi CLI 多轮/取消验收见 `docs/testing/05-pi-adapter.md`）
- 目标版本：Pi CLI 0.82.1
- CLI 路径：由 `agentend/.env` 的 `PI_CLI_PATH` 注入，未配置时使用 `pi`
- 实现范围：AgentEnd Adapter 主体，以及让 `pi` 成为可选 Agent 所需的契约、Backend、Frontend 和 Skill 接入点

## 实现了什么

为 AgentHub 增加 `pi` Agent 类型，通过 AgentEnd 启动 Pi CLI，并将 Pi 的 JSONL 事件转换为统一的 `StreamEvent`。Pi 具备与 Claude Code、OpenCode、Codex 相同的基础运行能力：

- 在隔离 worktree 中执行用户任务；
- 通过 SSE 增量返回文本、工具调用、工具结果、完成和错误事件；
- 首轮创建 Pi 会话并把真实 session ID 写回 `SessionMappingStore`；
- 后续请求恢复同一个 Pi 会话；
- 接收规则引擎生成的系统提示追加内容；
- 使用 AgentHub 供给到 `.pi/skills/` 的内置及外部 Skill；
- 支持中断，并终止 Pi 及其派生的工具进程；
- 继续使用 RunSupervisor、事件日志、Langfuse trace 和出站负载净化等现有基础设施。

本方案不修改 `BaseAgentAdapter` 接口，不引入常驻 Pi RPC 进程，也不使用“最近一次会话”作为恢复依据。

## 怎么实现的

适配器主体位于 `src/adapters/pi.py`，复用现有 CLI 适配器的 subprocess / 进程组 / stderr drain / 会话映射基础设施。以下按 CLI 能力约定、总体结构、命令构建、事件转换、会话管理、进程生命周期与配置展开。

## 已确认的 Pi CLI 能力

本方案基于本机安装的 Pi CLI 0.82.1。相关参数如下：

| 能力 | Pi 参数或约定 |
|---|---|
| JSONL 事件流 | `--mode json` |
| 指定会话 | `--session <path\|id>` |
| 指定模型 | `--model <provider/model>` |
| 追加系统提示 | `--append-system-prompt <text>` |
| 工具白名单 | `--tools <comma-separated names>` |
| 信任项目资源 | `--approve` |
| 禁用扩展发现 | `--no-extensions` |
| 禁用提示模板发现 | `--no-prompt-templates` |
| 项目设置目录 | `.pi/settings.json` |
| 项目 Skill 目录 | `.pi/skills/`、`.agents/skills/` |
| 用户配置目录 | `~/.pi/agent/`，可由 `PI_CODING_AGENT_DIR` 覆盖 |
| 用户会话目录 | `~/.pi/agent/sessions/`，可由 `PI_CODING_AGENT_SESSION_DIR` 覆盖 |

Pi 的 JSON 模式会先输出 session header，再输出运行事件：

```jsonl
{"type":"session","version":3,"id":"<pi-session-id>","timestamp":"...","cwd":"/workspace"}
{"type":"agent_start"}
{"type":"turn_start"}
{"type":"message_update","message":{},"assistantMessageEvent":{"type":"text_delta","delta":"Hello"}}
{"type":"agent_end","messages":[]}
```

session header 中的 `id` 可作为 AgentHub 的 `cli_session_id`，不需要扫描 Pi 的 session 文件。

## 总体结构

```text
POST /v1/agent/stream
        │
        ▼
RunSupervisor + RuleEngine + WorkspaceManager
        │
        ▼
PiAdapter.stream_chat()
        │
        ├── asyncio subprocess
        │     cwd = session worktree
        │     command = pi --mode json ...
        │
        ├── stdout JSONL ──► StreamEvent
        ├── stderr drain  ──► bounded diagnostic
        └── process group ──► SIGTERM / SIGKILL
```

新增文件：

```text
agentend/src/adapters/pi.py
agentend/tests/test_pi_adapter.py
```

`PiAdapter` 与现有 CLI Adapter 保持相同的方法结构：

```python
class PiAdapter(BaseAgentAdapter):
    async def create_session(self, session_id: str) -> None: ...
    async def chat(self, session_id: str, message: str, **kwargs) -> AgentResponse: ...
    async def stream_chat(self, session_id: str, message: str, **kwargs) -> AsyncIterator[StreamEvent]: ...
    async def interrupt(self, session_id: str) -> bool: ...
    async def destroy_session(self, session_id: str) -> None: ...
```

内部继续复用 `child_process_env()`、`drain_stderr()` 和 `terminate_process_group()`。

## 命令构建

### 新建会话

```bash
pi \
  --mode json \
  --approve \
  --no-extensions \
  --no-prompt-templates \
  [--append-system-prompt <rules>] \
  [--tools <tools>] \
  [--model <model>] \
  <message>
```

### 恢复会话

```bash
pi \
  --mode json \
  --approve \
  --no-extensions \
  --no-prompt-templates \
  --session <cli_session_id> \
  [--append-system-prompt <rules>] \
  [--tools <tools>] \
  [--model <model>] \
  <message>
```

### 参数映射

| AgentHub 参数 | Pi 处理 |
|---|---|
| `message` | 最后一个位置参数 |
| `cwd` | 传给 `asyncio.create_subprocess_exec(cwd=...)` |
| `cli_session_id` + `is_resume` | 恢复时追加 `--session <id>` |
| `system_prompt_append` | `--append-system-prompt <text>` |
| `allowed_tools` | 非空时规范化后传给 `--tools` |
| `model` | 非空时传给 `--model` |
| `max_turns` | Pi CLI 无对应参数，由 Run timeout 和 budget 约束 |
| `process_env` | 经 `child_process_env()` 白名单处理后传给子进程 |

Pi 内置工具名为小写，Adapter 应兼容 AgentHub 现有的 Claude 风格名称：

| 输入名称 | Pi 名称 |
|---|---|
| `Read` | `read` |
| `Bash` | `bash` |
| `Edit` | `edit` |
| `Write` | `write` |
| `Grep` | `grep` |
| `Find` | `find` |
| `LS` | `ls` |

未知工具名不应静默删除，应原样保留，以允许 Pi extension/custom tool 在显式配置下使用。只有 `allowed_tools` 非空时才传 `--tools`；为空时保留 Pi 默认工具集合。

### 项目信任与扩展策略

非交互模式不会弹出项目信任提示。若不显式传 `--approve`，Pi 默认可能忽略 `.pi/settings.json` 和 `.pi/skills/`，导致 AgentHub 供给的 `taskctl`、`render` 或外部 Skill 不可见。

因此使用以下组合：

```text
--approve --no-extensions --no-prompt-templates
```

- `--approve`：允许读取当前受管 worktree 中的项目设置和 Skill；
- `--no-extensions`：避免执行仓库内未经 AgentHub 管理的扩展代码；
- `--no-prompt-templates`：避免项目模板隐式改变输入；
- 不使用 `--no-skills`，否则 AgentHub Skills 无法加载；
- 不使用 `--no-context-files`，保留项目 `AGENTS.md`/`CLAUDE.md` 上下文发现。

## JSONL 事件转换

### 事件映射

| Pi 事件 | AgentHub 事件 | 内容 |
|---|---|---|
| `session` | `INIT` | `id → cli_session_id`，附加 `agent_type=pi` |
| `message_update` + `text_delta` | `TEXT` | `delta → text` |
| `message_update` + `thinking_end` | `TEXT` | `content → "[thinking] {content}"` |
| `tool_execution_start` | `TOOL_CALL` | `toolName → tool`，保留 `args` |
| `tool_execution_end` | `TOOL_RESULT` | 保留 `toolName`、`result`、`isError` |
| `message_update` + assistant `error` | `ERROR` | 提取 `errorMessage` |
| `agent_end` | `DONE` | 聚合本轮 assistant usage |
| `agent_start`、`turn_start`、`message_start` | 忽略 | 生命周期元事件 |
| `message_update` + `thinking_delta` | 忽略 | 避免重复添加 `[thinking]` 前缀 |
| `tool_execution_update` | 忽略 | 防止高频及超大部分结果进入 SSE |
| `queue_update`、`compaction_*`、`auto_retry_*` | 首期忽略 | 可记录 debug 日志，后续再扩展事件协议 |
| 非 JSON stdout 行 | 忽略 | 记录 debug 日志，不冒充 assistant 文本 |

Pi 的 `text_delta` 是真实增量，不能从 `message.partial` 中重复提取完整文本，否则前端会出现重复内容。

思考内容首期在 `thinking_end` 时一次性输出，与 Codex 完成后输出 reasoning 的行为接近。后续若需要真正的思考流，应新增独立 `planning` 映射或维护 thinking block 状态，而不是为每个 delta 添加一次 `[thinking]`。

### 工具结果规范化

Pi 的 `result` 可能是字符串，也可能包含 `content` 数组和其他结构。Adapter 内部可以保留完整结果；对外发送前，现有 `sanitize_stream_event()` 会移除 `TOOL_RESULT.result` 并记录 `output_size`，避免大文件或命令输出进入 Run 事件日志和 SSE。

Adapter 应至少保留：

```python
StreamEvent.create(
    EventType.TOOL_RESULT,
    tool=data.get("toolName", ""),
    result=data.get("result"),
    is_error=bool(data.get("isError")),
    agent_type="pi",
)
```

### DONE 和错误兜底

Adapter 维护 `saw_done`：

- 收到 `agent_end` 时发送一次 `DONE`，并设置 `saw_done=True`；
- 进程以 0 退出且没有 `agent_end` 时补发一次 `DONE`；
- 非零退出时发送 `ERROR`，错误文本优先使用已限制大小的 stderr；
- 若 stderr 为空，使用稳定的 fallback：`Pi process failed`；
- Pi 已输出协议级 ERROR 后仍允许非零退出兜底，但实现应避免同一协议错误被重复转换。

`chat()` 只聚合 `TEXT.text`，并从 `DONE.usage` 获取 usage，与现有 OpenCode/Codex Adapter 一致。

## 会话管理

继续使用 AgentHub 的双层会话映射：

```text
AgentHub session_id
        │
        ▼
SessionMappingStore
        │
        ▼
Pi session UUID
```

首次请求：

1. `SessionMappingStore` 中没有 Pi session ID；
2. Adapter 不传 `--session`；
3. Pi 创建会话并输出 `session` header；
4. Adapter 转换为 `INIT(cli_session_id=<pi-id>)`；
5. `agent.py::_execute_stream()` 将 ID 写入 `SessionMappingStore`。

后续请求：

1. `_resolve_session()` 读出 Pi session ID；
2. Adapter 添加 `--session <pi-id>`；
3. Pi 加载同一 JSONL session，并继续对话。

不使用 `--continue`。它按 cwd 选择最近会话，在同一仓库存在多个 AgentHub session 时可能恢复错误上下文。

不使用 `--no-session`。该选项会让 session header 缺少可持久恢复的文件，无法满足多轮会话要求。

## 进程生命周期

子进程创建方式与其他 CLI Adapter 一致：

```python
process = await asyncio.create_subprocess_exec(
    *cmd,
    stdout=asyncio.subprocess.PIPE,
    stderr=asyncio.subprocess.PIPE,
    cwd=cwd,
    env=child_process_env(process_env),
    start_new_session=True,
    limit=10 * 1024 * 1024,
)
```

`start_new_session=True` 使 Pi 成为独立进程组组长。中断或 finally 清理时调用 `terminate_process_group()`：先向整个进程组发送 SIGTERM，等待 `execution.process_terminate_timeout`，仍未退出则发送 SIGKILL。这样可以同时清理 Pi 和它通过 `bash` 启动的子进程。

stderr 必须在读取 stdout 的同时由独立 task 持续 drain，不能等 stdout 结束后再读取，否则大量 stderr 可能填满 pipe 并造成死锁。

## 配置

### `agents.json`

实际配置和示例配置都增加：

```json
"pi": {
  "config_dir": ".pi",
  "event_type": "pi",
  "cli_path": "pi"
}
```

根据现有约定，环境变量覆盖名自动为：

```text
PI_CLI_PATH
```

本机如果不使用 PATH 中的 `pi`，在 `agentend/.env` 设置 `PI_CLI_PATH`，不要修改提交的 `agents.json`。

### `config.yaml`

为了让 `/v1/agents/configs` 展示 Pi 的用户级配置，可选增加：

```yaml
agents:
  pi:
    config_path: ""
```

本机配置路径通过 `agentend/.env` 的 `PI_CONFIG_PATH` 注入；该字段只用于管理界面读取配置，不决定 Adapter 的 CLI 路径。

### `config_dir` 选择

`config_dir` 使用 `.pi`，现有以下逻辑均可复用：

- SkillProvisioner 将内置 Skill 供给到 `.pi/skills/`；
- Skills API 在 `.pi/skills/` 扫描、安装和移除外部 Skill；
- SoulRule 从 `.pi/SOUL.md` 读取身份文档并注入系统提示；
- TaskctlRule 生成 `.pi/skills/taskctl/taskctl` 路径；
- WorkspaceManager 将 `/.pi` 加入 worktree 本地 excludes；
- workspace diff 排除受管 Skill 文件。

Pi 原生扫描 `.pi/skills/`，因此不需要为 Pi 单独拆分“配置目录”和“Skill 目录”。

## 接入改动清单

### 契约层

按照契约优先原则，先修改：

- `contracts/schemas/agent-request.yaml`：`AgentType` 增加 `pi`；
- `contracts/logs/YYYY-MM-DD-add-pi-agent.md`：记录三端影响；
- 运行 `make generate`，生成 Python、TypeScript、Go 三端类型。

不得手工修改三个端的 `generated/` 文件。

### AgentEnd

| 文件 | 修改 |
|---|---|
| `src/adapters/pi.py` | 新增 PiAdapter |
| `src/adapters/__init__.py` | 导出 PiAdapter |
| `src/app/dependencies.py` | 注册 `AgentType.PI → PiAdapter` |
| `src/api/v1/agents.py` | 增加 Pi 展示元数据 |
| `src/api/v1/skills.py` | `.pi` 目录映射及外部 Skill 白名单加入 `pi` |
| `src/skills/builtin/taskctl/main.go` | 从 `.pi` 路径识别 agent type `pi` |
| `agents.json` | 配置实际 Pi CLI 路径 |
| `agents.example.json` | 增加 Pi 示例配置 |
| `config.example.yaml` | 增加可选 Pi config_path |
| `tests/test_pi_adapter.py` | Adapter 命令、解析、错误和进程测试 |
| `tests/test_skills.py` | Pi Skill 目录和外部 Skill 边界测试 |
| `src/skills/builtin/taskctl/main_test.go` | `.pi` 路径解析测试 |

### Backend

| 文件 | 修改 |
|---|---|
| `internal/controller/impl/agent_controller.go` | Agent 类型列表加入 `pi` |
| `internal/service/impl/skill_service.go` | 外部 Skill 导入白名单加入 `pi` |
| 相关测试 | 覆盖 Pi 会话创建和 Skill 导入 |

Backend 的 session/model 字段使用字符串保存，不需要数据库 schema 迁移。

### Frontend

生成契约后补齐所有 `Record<AgentType, ...>` 和手写类型判断：

| 文件 | 修改 |
|---|---|
| `src/lib/constants.ts` | Pi 名称、描述、颜色 |
| `src/index.css` | 增加 `--agent-pi` 亮色/暗色 token |
| `src/components/im/NewChatDialog.tsx` | fallback Agent 类型加入 Pi |
| `src/components/chat/AskAgentCard.tsx` | `isAgentType()` 接受 Pi |
| `src/pages/AgentProfilePage.tsx` | Pi 视为外部 Adapter Agent |

建议展示：

```text
name: Pi
description: 支持多模型、Skills 和会话恢复的 AI 编程助手
```

## 测试设计

### Adapter 单元测试

`tests/test_pi_adapter.py` 至少覆盖：

1. 新会话命令不包含 `--session`；
2. 恢复命令包含 `--session <id>`；
3. `cwd` 只通过 subprocess 参数传递；
4. system prompt、model 和 allowed tools 参数正确；
5. Claude 风格工具名转换为 Pi 小写名称；
6. `session → INIT` 并提取真实 Pi session ID；
7. `text_delta → TEXT` 且无重复内容；
8. `thinking_end → [thinking]` 文本；
9. tool start/end 正确映射；
10. 协议 ERROR 和非零退出错误；
11. 正常退出缺少 `agent_end` 时补发 DONE；
12. 已收到 `agent_end` 时不重复 DONE；
13. 大量 stderr 被并发 drain，不发生死锁；
14. interrupt 终止整个进程组并清理 `_processes`。

### 集成验证

```bash
make generate
cd agentend && uv run pytest tests/test_pi_adapter.py tests/test_skills.py
cd agentend && uv run ruff check src tests
cd backend && go test ./...
cd frontend && pnpm lint && pnpm build
make skills build
```

### 手动验收

1. 使用 Pi 创建单 Agent 会话，发送第一条消息；
2. SSE 首条有效事件包含 `init.content.cli_session_id`；
3. 发送第二条消息，确认恢复相同 Pi session；
4. 要求 Pi 读取、编辑文件，确认 tool_call/tool_result 和文本事件正常；
5. 调用 `.pi/skills/taskctl` 或 `.pi/skills/render`，确认 Skill 可发现；
6. 中途取消运行，确认 Pi 和它启动的 bash 子进程都退出；
7. 并行创建同一仓库的两个 Pi 会话，确认上下文不串线；
8. 检查 git diff，确认 `.pi` 受管文件不会进入用户提交；
9. 检查 SSE/Run 事件，确认工具大结果已由 transport sanitizer 移除。

## 实施顺序

```text
AgentType 契约增加 pi
  → make generate
  → PiAdapter 与单元测试
  → Adapter Registry 和 AgentEnd 元数据
  → SkillProvisioner / Skills API / taskctl 支持
  → Backend Agent 列表和 Skill 白名单
  → Frontend 展示和类型穷举
  → 三端自动测试
  → 手动多轮会话与中断验收
  → 同步 Adapter 对比和 reference 文档
```

## 风险与边界

### Pi 用户配置可写性

Pi 启动时可能在 `~/.pi/agent/` 创建 lock、settings 或 trust 文件。AgentEnd 服务账户必须对该目录具有读写权限。若部署环境将 home 设为只读，应通过 `PI_CODING_AGENT_DIR` 指向持久化可写目录，不能通过禁用 session 来规避。

### 模型和凭据

`child_process_env()` 会剥离 AgentEnd 自身的数据库、JWT、MinIO、Langfuse 等敏感变量，同时保留常见 Agent CLI 模型凭据。Pi 的模型配置和 API Key 应来自它自己的用户配置或受支持的模型环境变量，不应把 AgentEnd 服务密钥注入 Pi。

### 大事件

Pi 工具结果和 `agent_end.messages` 可能很大。Adapter 不应把完整 `agent_end.messages` 放入 `DONE`，只提取 usage；工具结果则依赖现有出站 sanitizer 在进入 Run 事件日志前裁剪。

### Pi CLI 版本变化

事件解析基于 0.82.1 的 `session`、`message_update`、`tool_execution_*` 和 `agent_end` 协议。升级 Pi 时必须先运行 Adapter fixture 测试，确认事件字段和 session header 没有不兼容变化。

## 完成标准

满足以下条件后，Pi Adapter 才视为完成：

- `pi` 已进入契约生成的三端 AgentType；
- 前端可以创建 Pi 单聊和包含 Pi 的群聊；
- 首轮和恢复轮均能正确输出 SSE；
- Pi session ID 能持久写回并稳定恢复；
- `.pi/skills/` 的内置和外部 Skill 可以加载；
- 取消运行能清理完整进程组；
- Pi 不会读取到 AgentEnd 的控制面密钥；
- 三端自动测试与手动多轮验收通过；
- Adapter 对比、配置和测试文档已同步更新。
