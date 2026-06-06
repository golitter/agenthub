# 三层架构设计：React + Go + AgentEnd Python

> 本文档定位：AI Runtime Platform 的三层架构设计，不是普通的 CRUD + Chat UI。
> 真正的核心不是 prompt，而是 **event lifecycle、runtime state、execution isolation、stream protocol**。
> Runtime 系统真正最难的从来不是 prompt engineering，而是 **state consistency、event ordering、stream durability、runtime lifecycle**。
>
> **核心纪律：Runtime for Agents，不是 Generic Distributed Runtime。**
> 永远优先服务 Agent 执行场景，不要把项目拖入"做基础设施 > 做 AI 产品"的陷阱。

---

## 〇、冻结边界（比功能更重要）

以下是不可违反的硬边界，比任何功能设计都重要：

### 冻结 1：Go NEVER executes, Python NEVER owns durability

```
Go    → 只 record / observe / request，永远不执行 Agent
Python → 只 produce execution transition，永远不拥有持久化权威
```

### 冻结 2：状态迁移 authority 归属

```
ONLY Python 能产生执行状态迁移:
  task.running / task.completed / task.failed / task.cancelled

Go 只能:
  record (写入 DB)
  observe (读取状态)
  request (请求 interrupt)

Go 不能 force transition。
如果 Go 和 Python 状态不一致 → Go 通过协议请求 Python 自行终结，不是强制改。
```

### 冻结 3：EventBus 只是 in-process ephemeral dispatcher

```
EventBus = 进程内临时分发器
不是: mini Kafka / message broker / durable queue

不做: ack / retry / durable queue / consumer group / exactly-once / persistence
```

### 冻结 4：Artifact 只是 Task Output Reference

```
Artifact = 任务产物的引用
不是: general asset system / ML platform

不做: artifact DAG / artifact dependency / artifact versioning / artifact lineage / artifact orchestration
```

### 冻结 5：LangGraph 只是 Planner Engine

```
LangGraph = 规划引擎 (plan / dispatch / aggregate / replan)
不是: Runtime Kernel

不接管: lifecycle / subscription / event ordering / runtime state
```

### 冻结 6：EventEnvelope v1 ABI

```
以下字段已冻结，不重命名，只能 deprecated + add new:
  version / event.type / category / sequence / terminal / render.kind

未来演进只能: version 递增 + 新增 type + deprecated 旧 type
```

## 一、当前状态总览

```
bytedanceai/
├── agentend/      ✅ 已实现 (Python FastAPI + LangGraph, 多 Agent 运行时)
├── backend/       ✅ 已实现 (Go Gin + GORM + MySQL + Redis Stream)
├── frontend/      ✅ 已实现 (React 19 + Vite 8 + TypeScript + Tailwind + shadcn/ui)
├── contracts/     ✅ 已实现 (YAML schemas + 三端代码生成)
├── docs/          📄 架构文档已有
└── scripts/       🔧 工程脚本 (run.sh, generate_contracts.py, test-clean.sh)
```

三端均已实现基础功能：前端 IM 聊天界面 + SSE 流式、后端 Task/Session CRUD + Redis Stream 透传、Agent 端多 Agent 适配器 + Workspace 隔离。

---

## 二、核心架构：从 Request-Driven 到 Event-Driven

当前 AgentEnd 的数据流本质上是 request-response streaming：

```
HTTP Request → Agent 执行 → SSE Response Stream
```

这会导致：
- HTTP 连接 = Agent 生命周期（前端断开、Go 重启、长时间运行全部无解）
- 无法 reconnect、replay
- 无法多前端观察同一个 task
- 无法 task resume

**目标架构：Event-Driven Runtime**

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     React Frontend (SPA)                                │
│                                                                         │
│   SSE Client ──▶ EventGraph ──▶ derived_messages = reducer(events)     │
│                      │                                                  │
│         ┌────────────┼────────────────────────┐                        │
│         ▼            ▼            ▼           ▼                        │
│     TextCard    DiffViewCard  ToolProgress  Timeline                   │
│     CodeBlock   ImageCard     PlanningCard  ...                        │
└────────┬───────────────────────────────────────────────────────────────┘
         │ SSE
         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│              Go Backend — Platform State Layer                          │
│                                                                         │
│   ┌──────────┐  ┌──────────────┐  ┌─────────────┐  ┌───────────┐      │
│   │ Auth     │  │ Subscription │  │ EventStore  │  │ TaskState │      │
│   │ (JWT)    │  │ Manager      │  │ (DB)        │  │ Machine   │      │
│   └──────────┘  └──────┬───────┘  └─────────────┘  └───────────┘      │
│                        │                                                │
│   SSE 订阅：Go 持有订阅关系，前端断开重连后恢复                            │
│   Event 持久化：从 AgentEnd SSE 流写入 DB，支持 replay                    │
│   Task 状态机：Go 是 task lifecycle 的 owner                             │
│                        │ HTTP                                           │
└────────────────────────┼────────────────────────────────────────────────┘
                         │
┌────────────────────────┼────────────────────────────────────────────────┐
│          AgentEnd Runtime — Execution Layer (FastAPI, Python)           │
│                        │                                                │
│   ┌──────────────────┐ │  ┌──────────┐  ┌──────────┐  ┌──────────┐    │
│   │  Runtime Core    │ │  │ Adapter  │  │ Orchestr │  │Workspace │    │
│   │  ┌────────────┐  │ │  │ Registry │  │ (LLM)    │  │ Ops      │    │
│   │  │ EventBus   │  │ │  └────┬─────┘  └──────────┘  └──────────┘    │
│   │  │ Dispatcher │◀─┼─┘       │                                       │
│   │  │ Lifecycle  │  │         ▼                                       │
│   │  │ Cancel     │  │   ┌──────────────┐                              │
│   │  └────────────┘  │   │ Claude CLI   │                              │
│   └──────────────────┘   │ OpenCode CLI │                              │
│                          └──────────────┘                              │
│                                                                         │
│   Adapter 不管理生命周期 — Runtime Core 负责 spawn/cancel/dispatch。     │
│   Adapter 只是 execute()。EventBus 是 in-process ephemeral dispatcher。  │
└─────────────────────────────────────────────────────────────────────────┘
```

### 2.1 两条核心边界

```
Go Backend = Platform State Layer（有状态）
├── task state (状态机)
├── event store (DB 持久化)
├── auth / session / subscription
├── replay / reconnect
├── workspace 元数据 (owner, project, status, ACL)
├── artifact 元数据
└── API 版本管理

AgentEnd Python = Runtime Execution Layer（无状态-ish）
├── LLM orchestration
├── tool execution
├── workspace 文件操作 (worktree git ops)
├── planning / agent runtime
├── event bus (内存，进程级)
├── event ring buffer (Go reconnect 前的缓冲)
└── artifact 文件存储
```

**不能让 Go 变成薄壳、Python 变成巨石。** 否则 session/task/event/replay/reconnect/multi-client 全绑死 Python，后期拆不动。

---

## 三、AgentEnd Python — Runtime Core

### 3.1 当前问题：Adapter = Runtime

当前 `OrchestratorAdapter` 和 `ClaudeCodeAdapter` 同时负责执行和生命周期管理（进程创建/销毁/中断），职责混乱。

### 3.2 新增 Runtime Core 层

```
agentend/src/
├── runtime/              ← Runtime Core（内核层，保持精简）
│   ├── eventbus.py       # 内存 Event Bus (进程内分发，不做 durable/retry/ack)
│   ├── task_runtime.py   # Task 生命周期 (spawn, cancel, wait)
│   ├── backpressure.py   # 背压策略 (drop merge / chunk merge)
│   ├── event_store.py    # Ring Buffer (Go reconnect 前的短期缓冲，5000 event)
│   └── lifecycle.py      # Agent 进程生命周期
├── artifacts/            ← Artifact Runtime (只做 Task Output Reference)
│   ├── manager.py        # 产物注册 / 解析 / 清理
│   └── resolver.py       # artifact_id → 真实路径
├── adapters/             # 简化：Adapter 只负责 execute
│   ├── base.py           # 精简为 execute + stream_execute
│   ├── claude.py
│   ├── codex.py
│   ├── opencode.py
│   └── orchestrator.py
├── orchestrator/         # 保留 LangGraph，只做 Planner Engine
│   ├── graph.py          # LangGraph 7-node StateGraph (skill_prepare → reason → dispatch → execute → review → evolve → save_mem)
│   ├── models.py
│   ├── prompts.py
│   └── ...
├── api/v1/               # HTTP 端点改为消费 Event Bus
├── workspace/            # 文件操作留在 Python
└── schemas/
```

### 3.3 Event Bus 架构

```
                    ┌─────────────┐
                    │  EventBus   │
                    │ (内存)      │
                    └──────┬──────┘
                           │ publish(event)
              ┌────────────┼────────────────┐
              ▼            ▼                ▼
        ┌──────────┐ ┌──────────┐   ┌───────────┐
        │ SSE      │ │ Logger   │   │ Metrics   │
        │ Consumer │ │ Consumer │   │ Consumer  │
        └──────────┘ └──────────┘   └───────────┘
```

Adapter 只做：

```python
class Adapter:
    async def execute(task_id, message, **kwargs) -> AsyncIterator[EventEnvelope]:
        ...
```

Runtime Core 负责：
- spawn (启动 adapter 执行)
- cancel (取消正在运行的 adapter)
- dispatch (分发 event 到 SSE consumer)
- buffer (Ring Buffer 短期暂存)

**Runtime Core 不做：** replay、subscription 管理、generic workflow engine、distributed scheduling、durable state、ack/retry/consumer group。

### 3.4 Runtime Core 边界纪律

```
✅ 做:                            ❌ 不做:
────────────────────              ────────────────────
• subprocess lifecycle            • generic DAG runtime
• event dispatch (内存，ephemeral) • distributed scheduling
• cancellation                    • durable persistence
• transient buffer (ring buffer)  • replay authority
                                  • ack / retry / consumer group
```

**Python 是 Ephemeral Execution Layer，不拥有 durable authority。**

### 3.5 保留 LangGraph，但只做 Planner Engine

LangGraph 职责范围：

```
✅ LangGraph 做:                  ❌ LangGraph 不做:
────────────────────              ────────────────────
• plan (任务拆解)                  • lifecycle management
• dispatch (分发给 Agent)           • subscription / event ordering
• aggregate (结果聚合)              • runtime state management
• replan (失败重规划，未来)          • durable checkpoint
```

演进路线：
1. 当前：7 节点状态机 `skill_prepare → reason → dispatch → execute → review → evolve → save_mem`，conditional routing + ask_agent 工具支持
2. 下一步：加入 `dispatch → collect → aggregate` 节点，形成闭环
3. 未来：条件分支（task 失败重规划）

---

## 四、Task Runtime State Machine

### 4.1 为什么需要完整的 State Machine

当前 Task 只有 `pending/running/completed` 三个状态，不够处理：

```
cancel race        — 用户取消时 task 正在 spawn，取消请求和 spawn 完成竞争
reconnect race     — Go 重连时 Python 的 task 可能已经完成或崩溃
duplicate spawn    — 同一个 task 被重复启动
retry              — 失败后重新执行
agent crash        — CLI 进程意外退出，状态需要同步
```

### 4.2 State Machine 定义

```
CREATED ──▶ QUEUED ──▶ SPAWNING ──▶ RUNNING
                                       │
                        ┌──────────────┤
                        ▼              ▼              ▼
                   COMPLETED       FAILED      INTERRUPTING
                                                   │
                                                   ▼
                                              CANCELLED
```

转换规则：

```
CREATED      → QUEUED        (入队等待调度)
QUEUED       → SPAWNING      (开始分配资源，启动 adapter)
SPAWNING     → RUNNING       (adapter 执行中)
SPAWNING     → FAILED        (spawn 失败，如 CLI 不存在)

RUNNING      → COMPLETED     (正常完成)
RUNNING      → FAILED        (执行失败)
RUNNING      → INTERRUPTING  (收到取消请求)

INTERRUPTING → CANCELLED     (取消完成)
INTERRUPTING → FAILED        (取消过程中崩溃)

FAILED       → QUEUED        (retry，可选)
```

### 4.3 双重真相关系：Execution Truth vs Durable Truth

```
关键原则：
  Execution State 不可外部强改。

  Go 是 "durable truth"  — DB 持久化，replay source
  Python 是 "execution truth" — 运行时实际状态

  这两种 truth 不能强制统一。
```

```
场景 1: CLI 崩溃
  Go DB:     running
  Python:    process dead
  → Python 发 task.failed event → Go 更新 DB

场景 2: 取消信号延迟
  Go DB:     cancelled
  Python:    tool still executing
  → Go 请求 interrupt → Python ACK → Python 发 task.cancelled event → Go 更新 DB

错误做法: Go 发现不一致 → 强制修改 Python state → runtime corruption
```

对账协议：

```
1. Python 通过 SSE event 实时上报状态变更 (task.started, task.completed, task.failed)
2. Go 收到 event 后更新 DB
3. Go 定期心跳检查 Python (GET /v1/session/{id})
4. 发现不一致时:
   → Go mark stale (标记 DB 状态为可疑)
   → Go request interrupt (请求 Python 停止执行)
   → Python ACK (确认停止)
   → Go 根据 ACK 结果更新 DB

不是 "Go 修正 Python"，而是 "Go 通过协议让 Python 自行终结"。
```

### 4.4 状态转换守卫

```python
# guards.py — 防止非法状态转换

VALID_TRANSITIONS = {
    "created":      {"queued"},
    "queued":       {"spawning", "failed"},
    "spawning":     {"running", "failed"},
    "running":      {"completed", "failed", "interrupting"},
    "interrupting": {"cancelled", "failed"},
    "cancelled":    set(),       # terminal
    "completed":    set(),       # terminal
    "failed":       {"queued"},  # retry only
}

def guard_transition(current: str, target: str) -> bool:
    if target not in VALID_TRANSITIONS.get(current, set()):
        raise InvalidTransition(current, target)
    return True
```

---

## 五、Event Backpressure

### 5.1 问题：高吞吐 event 可能导致系统崩溃

```
场景: npm install / pytest -vv / cargo build
→ 几万 event/s 的 tool.stdout

没有 backpressure → OOM / SSE 堵塞 / Go 写 DB 堵塞
```

### 5.2 Backpressure 策略

EventBus 使用有界 channel：

```python
asyncio.Queue(maxsize=1000)
```

不同 event type 有不同的丢弃/合并策略：

| Event Type | 策略 | 理由 |
|---|---|---|
| `text.delta` | **drop merge** — 新 chunk 追加到旧 chunk，不增加队列长度 | 文本是 append-only，丢弃中间 chunk 不影响最终结果 |
| `tool.stdout` | **chunk merge** — 批量合并，100ms 窗口 | 命令输出密集但可延迟 |
| `tool.stderr` | **chunk merge** — 同 stdout | 同上 |
| `task.completed` | **绝不丢弃** — 阻塞等待消费 | 终态 event 不能丢 |
| `task.failed` | **绝不丢弃** | 终态 event 不能丢 |
| `session.done` | **绝不丢弃** | 终态 event 不能丢 |
| `artifact.created` | **绝不丢弃** | 产物引用不能丢 |

### 5.3 Drop Merge 示例

```
text.delta: "H"
text.delta: "He"
text.delta: "Hel"
text.delta: "Hell"
text.delta: "Hello"

队列满时 drop merge → 只保留最新的累积:
text.delta: "Hello"
```

---

## 六、Event Persistence Boundary — Ring Buffer

### 6.1 问题：Go DB 挂了怎么办？

```
Go 负责持久化 EventLog，但如果 Go DB 写入失败/超时：
→ event 直接丢失
→ 前端重连后无法 replay
```

### 6.2 解决方案：Python Runtime Ring Buffer

```
Durability Owner:  Go DB (最终持久化)
短期缓冲:          Python Ring Buffer (Go reconnect 前的保险)
```

Python 端维护一个环形缓冲区：

```python
from collections import deque

class EventRingBuffer:
    def __init__(self, maxlen=5000):
        self._buffer = deque(maxlen=maxlen)
        self._lock = asyncio.Lock()

    async def append(self, event: EventEnvelope):
        async with self._lock:
            self._buffer.append(event)

    async def get_after(self, sequence: int) -> list[EventEnvelope]:
        """获取指定 sequence 之后的所有 event (供 Go reconnect)"""
        async with self._lock:
            return [e for e in self._buffer if e.sequence > sequence]
```

### 6.3 数据流

```
Adapter → EventBus → Consumer (SSE → Go)
                  → Consumer (Ring Buffer → 内存暂存)

Go 正常时: Ring Buffer 的数据被 Go 持久化后可淘汰
Go 断连时: Ring Buffer 保持最近 5000 event，Go 重连后拉取补缺
```

---

## 七、EventEnvelope — Event 协议升级

### 7.1 EventEnvelope 格式

当前 `StreamEvent` 只有 `type + content + timestamp`，升级为完整 Envelope：

```json
{
  "version": 1,
  "event_id": "evt_abc123",
  "task_id": "task-001",
  "session_id": "sess-xyz",

  "source": {
    "type": "agent",
    "id": "claude-code",
    "instance": "agent-01"
  },

  "sequence": 15,

  "event": {
    "category": "text",
    "type": "text.delta",
    "ui_type": "markdown",
    "terminal": false,
    "payload": {}
  },

  "render": {
    "kind": "conversation",
    "group": "assistant-msg-3",
    "priority": "stream"
  },

  "meta": {
    "timestamp": 1716345600.0,
    "trace_id": "trace-xxx",
    "workspace_id": "ws-xxx",
    "parent_event_id": null
  }
}
```

### 7.2 Event Versioning

`version` 字段是协议演进的生命线。未来 `ui_type` 改名、`render.kind` 变化、`payload` schema 变化、`event.type` 细分都会发生。

规则：
- `version` 永远递增，不回退
- `event.type` 永不重命名，只能 deprecated + add new type
- 老 version 的 event 在 replay 时仍可被前端正确解析
- 前端按 `version` 分支处理不同版本的 payload

### 7.3 三层协议：event.type / ui_type / render

这三个字段解决不同层面的问题：

```
event.type  → Runtime 语义层 (告诉系统发生了什么)
ui_type     → Data Type 层   (告诉前端数据是什么格式)
render      → Rendering 层   (告诉前端怎么渲染)
```

**render 层用 semantic hint，不用组件名。**

```
❌ 危险: render.card = "TextCard"  ← 后端直接控制前端组件实现
✅ 正确: render.kind = "conversation"  ← 后端只输出语义，前端自己选组件
```

理由：前端重构时组件名会变（`TextCard` → `MarkdownMessageCard`），但语义不变。后端不应知道前端的组件实现。

示例：

```
render.kind: "conversation"  → 前端选 TextCard / MarkdownMessageCard
render.kind: "thinking"      → 前端选 ThinkingBlock (灰色/折叠)
render.kind: "tool-output"   → 前端选 ToolProgressCard
render.kind: "system-note"   → 前端选 SystemNoticeCard
render.kind: "planning"      → 前端选 PlanningCard
```

### 7.4 Event Category

`event.category` 将细粒度 event.type 分组，大幅降低前端 reducer complexity：

| category | 包含的 event.type | 前端用途 |
|---|---|---|
| `text` | text.delta, text.done | conversation 渲染 |
| `tool` | tool.start, tool.stdout, tool.stderr, tool.result, tool.error | timeline group |
| `planning` | planning.started, planning.step, planning.completed | planner UI |
| `artifact` | artifact.created | artifact system |
| `task` | task.started, task.completed, task.failed | lifecycle tracking |
| `system` | session.init, session.done, error, replay.complete | runtime control |

前端 reducer 可以按 category 先分流，再按 type 细处理。

### 7.5 Terminal Event

系统需要统一抽象"终态 event"——前端依赖它来 finalize / flush / stop spinner / close subscription。

```json
{
  "event": {
    "category": "task",
    "type": "task.completed",
    "terminal": true,
    ...
  }
}
```

Terminal event 列表：`task.completed`, `task.failed`, `task.cancelled`, `session.done`

规则：
- `terminal: true` 的 event 绝不能被 backpressure 丢弃
- 前端收到 `terminal: true` 后 finalize 当前所有 buffer，关闭 spinner

### 7.6 为什么每个字段都必须

| 字段 | 用途 |
|------|------|
| `version` | 协议演进，replay 兼容性 |
| `event_id` | 去重、乱序重组、幂等 |
| `task_id` / `session_id` | 多任务并发、过滤 |
| `source.type` / `source.id` / `source.instance` | 多 Agent Timeline 区分 |
| `sequence` | Replay 时按序恢复、断点续传 |
| `event.category` | 事件大类分流，降低 reducer complexity |
| `event.type` | Runtime 语义 (text.delta, tool.start, tool.stdout) |
| `event.ui_type` | 数据格式 (markdown, code, diff, tool.progress) |
| `event.terminal` | 终态标记，前端 finalize/spinner/subscription 生命周期 |
| `render.kind` | 语义渲染提示 (conversation, thinking, tool-output) |
| `render.group` | 事件分组 (同一组归为一个视觉单元) |
| `render.priority` | 渲染优先级 (stream 实时 / collapsed 折叠) |
| `meta.trace_id` | 分布式 tracing (OpenTelemetry / Langfuse / Helicone) |
| `meta.parent_event_id` | Event DAG (tool.start → tool.stdout → tool.result) |

### 7.7 细粒度 Event Type

```
当前:  text, tool_call, tool_result, done, error
升级为:

text.delta          # 文本增量
text.done           # 文本结束
tool.start          # 工具开始执行 → 显示 spinner
tool.stdout         # 标准输出增量 → 实时显示命令输出
tool.stderr         # 标准错误增量 → 实时显示错误
tool.result         # 最终结果
tool.error          # 执行失败
planning.started    # 规划开始
planning.step       # 规划步骤
planning.completed  # 规划完成
task.started        # 任务开始
task.completed      # 任务完成
task.failed         # 任务失败
artifact.created    # 产物生成
session.init        # 会话初始化
session.done        # 会话结束
error               # 错误
```

### 7.8 UI Type（UI Contract）

`ui_type` 是前后端之间的正式协议——后端声明用哪种卡片渲染，前端无需猜测。

| ui_type | 含义 | 对应卡片 |
|---|---|---|
| `markdown` | 自然语言文本 | TextCard |
| `code` | 代码块 | CodeBlockCard |
| `diff` | 代码差异 | DiffViewCard |
| `artifact.image` | 图片产物 | ImageCard |
| `artifact.file` | 文件产物 | FileAttachmentCard |
| `artifact.deploy` | 部署状态 (远期) | DeployStatusCard |
| `planning.step` | 规划步骤 | PlanningCard |
| `tool.progress` | 工具执行进度 | ToolProgressCard |

**Markdown 只负责自然语言；所有结构化 UI 必须是显式 Event。** 不要让前端从 Markdown 中解析卡片类型。

### 7.9 Envelope 升级策略

在 AgentEnd 端直接升级 `StreamEvent` schema。Go 不做协议转换。

---

## 八、Session 与 Task 分离

### 8.1 概念定义

```
Session = 长期上下文容器
├── 一个项目聊天窗口
├── 包含对话历史
├── 包含多个 Task 的运行记录
└── 生命周期：用户创建 → 用户关闭

Task = 一次 Agent 运行
├── 一次 agent execution (从用户消息到最终结果)
├── 拥有独立的 Task State Machine
├── 拥有独立的 Event 序列
└── 生命周期：created → queued → ... → completed/failed/cancelled
```

### 8.2 关系模型

```
Session
 ├── Task 1 (claude-code, completed)
 │    ├── Event 1..N (text.delta, tool.start, tool.result, ...)
 │    └── Artifacts (diff.patch, report.md)
 ├── Task 2 (orchestrator, running)
 │    ├── Sub-Task 2a (claude-code, running)
 │    └── Sub-Task 2b (opencode, queued)
 └── Task 3 (opencode, pending)
```

### 8.3 为什么必须分离

不分离会导致：

```
resume context       — 恢复哪个上下文？session 还是 task？
conversation memory  — 对话历史是 session 级别还是 task 级别？
multi-run history    — 用户怎么查看之前运行过的 task？
retry                — 重试是重跑整个 session 还是单个 task？
cost tracking        — token 用量按 session 还是 task 统计？
```

### 8.4 Go DB 数据模型更新

```go
type Session struct {
    ID          uint   `gorm:"primaryKey"`
    SessionID   string `gorm:"uniqueIndex"`
    ProjectID   uint   `gorm:"index"`
    UserID      uint   `gorm:"index"`
    Title       string
    Status      string // active, archived
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Task struct {
    ID          uint   `gorm:"primaryKey"`
    TaskID      string `gorm:"uniqueIndex"`
    SessionID   string `gorm:"index"`  // 属于哪个 Session
    ProjectID   uint   `gorm:"index"`
    UserID      uint
    ParentTaskID string `gorm:"index"` // Orchestrator 子任务的父任务
    AgentType   string
    Status      string // created/queued/spawning/running/completed/failed/interrupting/cancelled
    Message     string
    Result      string
    RetryCount  int
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

---

## 九、Replay + Live Catch-up

### 9.1 经典问题：历史 replay 与实时 stream 之间的 gap

```
客户端 replay 历史读到 seq=15
此时新 event 已产生到 seq=20
客户端 attach realtime

→ seq 16~20 可能丢失
```

### 9.2 解决方案：Subscription Cursor

```
前端重连流程:

1. 前端发送 GET /api/tasks/{id}/events?cursor=15
2. Go 同时做两件事:
   a. 从 EventLog DB 读取 seq > 15 的历史 event (replay)
   b. attach 实时 SSE subscription
3. Go 先发完历史 event，再无缝切换到实时 event
4. 整个过程中 event 是严格递增的，不会丢也不会重

关键: 先 attach subscription，再 replay 历史
→ 避免订阅期间产生的新 event 被漏掉
```

```
时序:

前端 ──▶ GET /events?cursor=15
Go:   attach subscription (此时实时 event 持续流入)
Go:   replay seq 16, 17, 18, 19, 20 (从 DB)
Go:   切换到 live mode
Go:   seq 21, 22, 23... (实时推送)
```

### 9.3 Go 端实现要点

```go
func (s *SubscriptionManager) Subscribe(ctx context.Context, taskID string, cursor int) (<-chan EventEnvelope, error) {
    ch := make(chan EventEnvelope, 100)

    // 1. 先注册 subscription (实时 event 开始缓冲)
    sub := s.addSubscription(taskID, ch)
    defer s.removeSubscription(sub)

    // 2. 再 replay 历史 (cursor 之后)
    historical, _ := s.eventStore.GetAfterSequence(taskID, cursor)
    for _, evt := range historical {
        ch <- evt
    }

    // 3. 发送 catch-up-complete 标记
    ch <- EventEnvelope{Event: Event{Type: "replay.complete"}}

    // 4. 之后 ch 由实时 subscription 填充
    return ch, nil
}
```

---

## 十、Artifact Runtime（Task Output Reference）

### 10.1 Artifact 是任务产物引用，不是通用资产系统

```
Artifact = Task Output Reference
├── diff.patch, report.md     (文件)
├── architecture.png          (图片)
└── { "todos": [...] }        (结构化输出)

不做: artifact DAG / dependency / versioning / lineage / orchestration
```

### 10.2 Artifact Manager（精简）

### 10.3 Artifact 生命周期

```
Agent 执行 → 产生产物
    ↓
ArtifactManager.register(task_id, path, mime_type, metadata)
    ↓
返回 artifact_id
    ↓
发出 EventEnvelope: artifact.created (携带 artifact_id)
    ↓
前端通过 GET /v1/artifacts/{artifact_id} 访问
    ↓
Go 持有 artifact 元数据 (owner, ACL, expiry)
Python 持有 artifact 文件 (worktree 内)
```

### 10.4 前端访问方式

```
前端只拿 artifact_id → GET /api/artifacts/{artifact_id} → Go 解析 → 代理到 Python 或 CDN
```

不在 SSE payload 中暴露任何 workspace 绝对路径。

---

## 十一、Go Backend — Platform State Layer

Go 不是薄壳 API Gateway，而是平台的**状态主人**。

### 11.1 职责边界

```
✅ Go 应该做的:                       ❌ 不应该做的:
─────────────────────────             ─────────────────────
• 用户认证 (JWT/OAuth)                • AI 推理/编排
• SSE 订阅管理 (断开重连恢复)          • CLI 子进程管理
• Event Store (DB 持久化)             • LLM 调用
• Task 状态机 (lifecycle owner)       • Rule Engine
• Session 生命周期                     • Prompt 构建
• Workspace 元数据 (owner, ACL, quota) • worktree git 操作
• Artifact 元数据 (owner, type, URL)  • Artifact 文件存储
• 项目/任务/Session CRUD              • Backpressure (Python 负责)
• 权限 & 租户隔离
• Replay (从 DB 读历史 event)
• Subscription Cursor (无 gap catch-up)
• 多 AgentEnd 实例管理
```

### 11.2 项目结构

> 以下为设计目标结构。当前实际结构已演进为 Controller → Service → DAO 三层架构，参见 backend/AGENTS.md。

```
backend/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── conf/                        # 配置加载（YAML + env override）
│   │   └── conf.go
│   ├── generated/                   # 契约生成的类型文件
│   ├── controller/                  # Controller 层（参数绑定 + vo 响应）
│   │   ├── controller.go            # 接口定义（统一 RegisterRoutes）
│   │   └── impl/                    # 14 组实现
│   │       ├── task_controller.go
│   │       ├── session_controller.go
│   │       ├── message_controller.go
│   │       ├── stream_controller.go
│   │       ├── agent_profile_controller.go
│   │       ├── avatar_controller.go
│   │       ├── diff_snapshot_controller.go
│   │       ├── workspace_controller.go
│   │       ├── announcement_controller.go
│   │       ├── contact_group_controller.go
│   │       ├── skill_controller.go
│   │       ├── admin_controller.go
│   │       ├── agent_controller.go
│   │       └── errors.go
│   ├── service/                     # Service 层（纯业务逻辑，无 Gin 依赖）
│   │   ├── service.go               # 接口定义 + DTO
│   │   ├── bizerr.go                # 统一业务错误
│   │   └── impl/                    # 实现组 + task_route.go + group_chat_window.go
│   ├── dao/                         # DAO 层（接口可 Mock 替换）
│   │   ├── dao.go                   # 8 组接口
│   │   ├── gorm/                    # GORM 实现 + cascade.go
│   │   └── mock/                    # 测试替身
│   ├── middleware/                   # auth, admin_auth, cors, logger, rate_limit
│   │   ├── auth.go
│   │   ├── admin_auth.go
│   │   ├── cors.go
│   │   ├── logger.go
│   │   └── rate_limit.go
│   ├── model/                       # GORM 模型（11 个）
│   ├── stream/                      # Redis Stream 写入
│   │   ├── hub.go
│   │   └── writer.go
│   └── vo/                          # View Object（API 响应结构）
│       └── response.go
├── pkg/
│   ├── agentend_client/
│   │   └── client.go
│   ├── db/                          # MySQL 连接（单例）
│   ├── qiniu/                       # 七牛云上传
│   └── redis/                       # Redis 连接
├── configs/
│   └── config.yaml
├── go.mod
└── go.sum
```

### 11.3 GORM 数据模型

> 当前已实现的模型仅包含 Session、Task、Message 三张表。以下为设计目标模型，部分尚未实现。

```go
// 已实现 — backend/internal/model/session.go
type Session struct {
    ID          uint   `gorm:"primaryKey"`
    SessionID   string `gorm:"uniqueIndex"`
    Title       string
    Status      string // active, inactive, completed
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// 已实现 — backend/internal/model/task.go
type Task struct {
    ID         uint   `gorm:"primaryKey"`
    TaskID     string `gorm:"uniqueIndex"`
    SessionID  string `gorm:"index"`
    AgentType  string
    Status     string // pending, running, completed, failed
    Message    string
    Result     string
    CreatedAt  time.Time
    UpdatedAt  time.Time
}

// 已实现 — backend/internal/model/message.go
type Message struct {
    ID          uint   `gorm:"primaryKey"`
    MessageID   string `gorm:"uniqueIndex"`
    SessionID   string `gorm:"index"`
    TaskID      string `gorm:"index"`
    Role        string // user, agent
    Content     string
    Status      string // streaming, completed, failed
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

设计目标（尚未实现）：

```go
// 规划中
type User struct { ... }
type Project struct { ... }
type WorkspaceMeta struct { ... }
type ArtifactMeta struct { ... }
type EventLog struct { ... }
```

### 11.4 Workspace 元数据归属

```
Workspace 文件操作 → Python (Agent 直接操作文件，强依赖 worktree)
Workspace 元数据   → Go (owner, project_id, branch, status, ACL, quota)
```

Go 负责 workspace list / dashboard / quota / cleanup / ACL。Python 只做 git worktree 的 CRUD。

---

## 十二、React Frontend — Event Graph Based

### 12.1 核心设计：Incremental Projection，不是全量 reduce

```
❌ 不要: derived_messages = reduce(allEvents)
→ replay 大 session 时 10万 events 全量重跑，前端卡死

✅ 而是: applyEvent(prevState, evt) — 增量投影
→ 每个 event 只做一次 state patch，O(1) 不是 O(n)
```

```
Raw Event Log
      ↓
Incremental Projection Engine (applyEvent)
      ↓
Derived View Model
```

events[] 仍然是原始层（支持 replay / time travel / filtering），但 derived view 是增量计算的。

### 12.2 项目结构

> 以下为设计目标结构。当前实际结构参见 frontend/AGENTS.md。

```
frontend/
├── src/
│   ├── api/
│   │   ├── client.ts
│   │   ├── sse.ts                # SSE 连接 + 断线重连 + cursor 恢复
│   │   └── types.ts              # EventEnvelope TypeScript 类型
│   ├── hooks/
│   │   ├── useAgentStream.ts     # SSE 流式数据 hook
│   │   └── useReplay.ts          # 历史回放 hook (cursor-based)
│   ├── store/
│   │   ├── eventStore.ts         # events[] 原始层 (Zustand)
│   │   └── projection.ts        # applyEvent(prevState, evt) 增量投影
│   ├── components/
│   │   ├── chat/
│   │   │   ├── Conversation.tsx
│   │   │   ├── EventGroup.tsx    # 按 render.group 分组
│   │   │   ├── MessageInput.tsx
│   │   │   └── AgentTimeline.tsx
│   │   └── cards/
│   │       ├── TextCard.tsx
│   │       ├── CodeBlockCard.tsx
│   │       ├── DiffViewCard.tsx
│   │       ├── ToolProgressCard.tsx
│   │       ├── ImageCard.tsx
│   │       ├── SandboxCard.tsx
│   │       └── StructuredCard.tsx
│   └── pages/
│       ├── ChatPage.tsx
│       └── ProjectPage.tsx
```

### 12.3 Event Aggregator

SSE 直出会导致逐字符 chunk，React 疯狂 rerender。加聚合层：

```
SSE Stream ──▶ EventBuffer ──▶ ChunkAggregator ──▶ events[] ──▶ React Render
                  │                 │
                  │ 300ms debounce  │ flush on tool.start / artifact / done
                  │ for text.delta  │
                  ▼                 ▼
              累积 text.delta    追加到 events[]
```

| Event Type | 聚合策略 |
|---|---|
| `text.delta`（连续 chunk） | 300ms debounce 合并 |
| `tool.start` | 立即 flush，开始新的聚合组 |
| `artifact.created` | 立即 flush 为独立卡片 |
| `terminal: true` (任何) | 强制 finalize 当前所有 buffer |
| `replay.complete` | 标记 replay 阶段结束，切换到 live 模式 |

### 12.4 卡片组件体系

根据 `render.kind`（语义提示）+ `ui_type` 分发渲染，`render.group` 决定分组。**前端自行决定用哪个组件**，后端不指定组件名：

```
render.kind → 前端组件映射 (前端内部决策，后端不知道):

"conversation" → TextCard / MarkdownMessageCard
"thinking"     → ThinkingBlock (灰色/折叠)
"tool-output"  → ToolProgressCard (同 group 内组合)
"system-note"  → SystemNoticeCard
"planning"     → PlanningCard
"artifact"     → ImageCard / FileAttachmentCard / StructuredCard
"sandbox"      → SandboxCard
```

### 12.5 断线重连流程

```
SSE 断开
  ↓
自动重连 (exponential backoff)
  ↓
GET /api/tasks/{id}/events?cursor={lastSeq}
  ↓
replay 历史 event → 追加到 events[]
  ↓
收到 replay.complete → 切换到 live 模式
  ↓
继续接收实时 event
```

---

## 十三、三层联调 API 契约

### 13.1 请求体对齐

```
React 发送                      Go 转发                 AgentEnd 接收
──────────                    ─────────               ─────────────
POST /api/sessions/{sid}      POST /v1/agent/stream   AgentRequest
       /tasks/run             {
{                             {                       {
  task_id,           ──▶       task_id,        ──▶     task_id,
  message,                     message,                 message,
  agent_type,                  agent_type,              agent_type,
  workspace_path?,             workspace_path?,         workspace_path,
  config?                      config?                  config?
}                             }                       }
```

Go 在这层增加 `user_id`（JWT）、`session_id`（URL path）、`project_id`（关联项目）。

### 13.2 EventEnvelope 对齐

```
AgentEnd 发出 EventEnvelope → Go 透传 EventEnvelope → React 消费 EventEnvelope
```

不在 Go 层做 Envelope 转换。AgentEnd 直接输出完整格式。

### 13.3 状态归属

| 状态 | 归属 | 理由 |
|------|------|------|
| Session 元数据 | Go DB | 用户维度管理，长期上下文 |
| Task 状态机 | Go DB | 平台级状态，需持久化、可查询 |
| Event 历史 | Go DB (EventLog) | 支持 replay、断线重连 |
| Agent 运行时进程 | Python 内存 | CLI 子进程必须在内存 |
| Workspace 文件操作 | Python | Agent 直接操作 worktree |
| Workspace 元数据 | Go DB | owner、ACL、quota |
| Artifact 文件 | Python | worktree 内文件 |
| Artifact 元数据 | Go DB | owner、type、expiry、ACL |
| Event Bus | Python 内存 | 进程级 event 分发 |
| Event Ring Buffer | Python 内存 | Go 断连前的保险缓冲 |
| Backpressure 策略 | Python | Runtime 级流量控制 |
| SSE 订阅关系 | Go 内存 | 多前端订阅管理 |
| Subscription Cursor | Go 内存 | 无 gap catch-up |

---

## 十四、SSE 透传交互时序

```
React                   Go Backend                    AgentEnd
  │                         │                             │
  │── POST /api/sessions/  │                             │
  │   {sid}/tasks/run ─────▶                             │
  │   Authorization: Bearer │── POST /v1/agent/stream ──▶ │
  │                         │                             │
  │                         │◀── SSE: session.init ───────│
  │◀── SSE: session.init ──│   → EventLog DB INSERT     │
  │                         │                             │
  │                         │◀── SSE: text.delta ─────────│
  │◀── SSE: text.delta ────│   → EventLog DB INSERT     │
  │                         │                             │
  │                         │◀── SSE: tool.start ─────────│
  │◀── SSE: tool.start ────│   → EventLog DB INSERT     │
  │                         │                             │
  │                         │◀── SSE: tool.stdout ────────│
  │◀── SSE: tool.stdout ───│   → EventLog DB INSERT     │
  │                         │                             │
  │                         │◀── SSE: tool.result ────────│
  │◀── SSE: tool.result ───│   → EventLog DB INSERT     │
  │                         │                             │
  │                         │◀── SSE: artifact.created ───│
  │◀── SSE: artifact ──────│   → ArtifactMeta DB INSERT │
  │                         │                             │
  │                         │◀── SSE: session.done ───────│
  │◀── SSE: session.done ──│   → Task status = completed│
```

---

## 十五、通信协议选择

### 15.1 前端到 Go：坚持 SSE，不要过早 WebSocket

当前 LLM 输出是 append-only stream，本质上是 server push only。SSE 完全够。

| 协议 | 适用场景 | 当前是否需要 |
|------|---------|-------------|
| SSE | append-only stream (当前场景) | ✅ 立即 |
| SSE + REST | SSE 下行 + REST 上行 (中断/输入) | ✅ 立即 |
| WebSocket | 协同编辑、实时光标、双向 tool IO | ❌ 后期 |

WebSocket 真正需要的场景：collaborative editing、agent interrupt push、multi-user presence、live cursor、duplex tool IO。在那之前，WS Hub 会让 Go 复杂度暴涨。

### 15.2 Go 到 AgentEnd：HTTP SSE

Go 作为 AgentEnd 的 SSE 消费者，用标准 HTTP POST + SSE response 读取。

---

## 十六、共同开发策略

### 16.1 开发路线

```
Phase 1: 基础骨架 + Event 协议 + State Machine
├── AgentEnd: EventEnvelope 升级 + Runtime Core (eventbus + state_machine)
├── Go: 项目骨架 + AgentEnd client + SSE proxy + DB
└── React: 项目骨架 + SSE client + eventStore + TextCard

Phase 2: 核心链路打通
├── AgentEnd: Adapter 层改写 (execute only) + Backpressure
├── Go: SSE 透传 + EventLog 持久化 + JWT auth + Task State Machine
└── React: EventAggregator + derived_messages + 基础卡片

Phase 3: 可靠性与恢复
├── AgentEnd: LangGraph 升级 (闭环 DAG) + Ring Buffer + Backpressure 策略
├── Go: Subscription Cursor + 无 gap catch-up + 状态对账
└── React: AgentTimeline + 断线重连 + cursor 恢复

Phase 4: 产物体系
├── AgentEnd: Artifact Runtime (registry + resolver + serializers)
├── Go: Artifact 元数据管理 + 代理访问 + 多 AgentEnd 实例管理
└── React: 完整卡片 (DiffView + Image + Sandbox + Structured)

Phase 5: 高级特性
├── AgentEnd: Orchestrator 改进 (多 Agent 并行调度)
├── Go: Workspace 元数据管理 + ACL + quota
└── React: 多 Agent Timeline + Event collapse + Time travel
```

### 16.2 Contract-First 开发模式

先定义三层之间的 API 契约，然后各端独立开发：

1. 在 `docs/` 中定义 Event Protocol spec、OpenAPI spec
2. 前后端 mock：
   - React: MSW (Mock Service Worker) mock Go API
   - Go: 直接调用 AgentEnd（agentend 已可用）
3. 联调测试：先跑通 React → Go → AgentEnd 的 SSE 流，再逐步加功能

### 16.3 各端并行工作

```
┌──────────────────────────────────────────────────────────────────┐
│ 可并行的开发任务                                                   │
├──────────────┬───────────────────┬────────────────────────────────┤
│  React       │  Go               │  AgentEnd                      │
├──────────────┼───────────────────┼────────────────────────────────┤
│ 项目初始化    │ 项目初始化         │ EventEnvelope schema 升级      │
│  (Vite+TS)   │  (Gin+GORM)       │ Runtime Core (EventBus + SM)   │
│              │                   │                                │
│ UI 组件开发   │ DB schema 设计    │ Adapter 层精简                  │
│  (Storybook) │  (migrations)     │ (execute only)                 │
│              │                   │                                │
│ SSE client   │ AgentEnd client   │ LangGraph DAG 升级              │
│  + cursor    │  + SSE reader     │ (闭环编排)                      │
│  + 重连      │  + State Machine  │                                │
│              │                   │                                │
│ eventStore   │ JWT auth          │ Backpressure 策略              │
│  (Zustand)   │  middleware       │ Ring Buffer                    │
└──────────────┴───────────────────┴────────────────────────────────┘
```

---

## 十七、架构演进路线

当前系统正在从 "AI Chat App" 往 "AI Runtime Platform" 演化。类似系统：
Claude Code Runtime、Cursor Agent Runtime、Devin Runtime、OpenHands Runtime、E2B Runtime、Temporal-like AI Runtime。

### 17.1 P0 — 立即做（冻结边界 + 最小可用内核）

- [ ] **冻结六大边界** (冻结 1-6，见第〇章)
- [ ] **冻结 EventEnvelope v1** (含 version, category, terminal, semantic render.kind)
- [ ] Event Versioning 规则 (version 递增，event.type 永不重命名)
- [ ] Runtime Core: EventBus + Task Runtime + Cancellation (精简，不做 replay/workflow/subscription)
- [ ] Event Ring Buffer (Python 只做短期缓冲，5000 event)
- [ ] Session 与 Task 分离
- [ ] Go 持有平台状态 (Go 只 record/observe/request，不 force transition)
- [ ] LangGraph 只做 Planner Engine (plan/dispatch/aggregate)
- [ ] 坚持 SSE

### 17.2 P1 — 下一阶段（可靠性 + 产物）

- [ ] Incremental Projection (applyEvent，不是 reduce(all))
- [ ] Terminal Event abstraction (统一 finalize/spinner/subscription lifecycle)
- [ ] Subscription Cursor + 无 gap catch-up
- [ ] Go ↔ Python 双重 truth 对账 (mark stale + request interrupt，不强制修改)
- [ ] Artifact Runtime (registry + resolver + serializers)

### 17.3 P2 — 未来（等 Agent 产品验证后再考虑）

- [ ] 多 AgentEnd 实例
- [ ] WebSocket (仅协同编辑场景)
- [ ] Capability-based isolation (sandbox / multi-tenant)

### 17.4 明确不做

```
❌ 不要让 Go 变第二 Runtime (Go 不执行、不 force transition)
❌ 不要让 EventBus 变 Kafka (不做 ack/retry/durable queue/consumer group)
❌ 不要让 Artifact 变 ML Platform (不做 DAG/dependency/versioning/lineage)
❌ 不要让 LangGraph 变 Temporal (不接管 lifecycle/subscription/state)
❌ 不要让 Python 拥有 durable authority (不做 replay authority/durable state)
```
