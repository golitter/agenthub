# Agent 路由与 Orchestrator 自动分派

> 设计建议 v1 — 群聊中通过 `@` 显式指定 Agent，群聊未指定时由 Orchestrator 自动分派；单聊保持当前 Agent 直达

## Context

当前多 Agent 群聊已经具备两类能力：

1. `RunTask` 可以按 `session_id + agent_type` 调用任意单个 Agent。
2. Orchestrator 可以读取群成员配置，规划任务后通过 `Dispatcher` 和 `ExecutionEngine` 调用子 Agent。

现在缺的是一层清晰的「消息路由」语义：

- 用户输入 `@某Agent ...` 时，希望这个 Agent 直接回复。
- 用户在群聊中不指定 Agent，或需要多个 Agent 协作时，希望 Orchestrator 自动判断并分派。
- 用户在单 Agent 会话中不指定 Agent 时，仍应由当前 Agent 直接回复。

推荐把「单聊直达」「群聊显式直达」「群聊自动编排」三种路径明确区分，而不是所有消息都先交给 Orchestrator。

## 推荐结论

采用「显式直达优先，群聊自动分派兜底」：

| 用户输入 | 路由目标 | 理由 |
|----------|----------|------|
| `@前端 改一下按钮样式`，且只命中一个非 Orchestrator Agent | 直接调用该 Agent | 用户已经给出明确目标，绕过 Orchestrator 更快、更少歧义 |
| 群聊无 `@`，且 task 中存在 Orchestrator session | Orchestrator | 让 Orchestrator 判断是否直接回复、ask_agent 或 plan_and_dispatch |
| 单 Agent 会话无 `@`，或 task 中没有 Orchestrator session | 当前 `session_id + agent_type` | 保持单聊语义，避免把普通单聊误送到不存在的编排器 |
| `@orchestrator ...` | Orchestrator | 用户明确要编排器处理 |
| `@all ...` 或多个 `@Agent` | Orchestrator | 多目标协作由 Orchestrator 规划更稳 |
| `@未知Agent ...` | 前端提示 / 后端 400 | 避免静默发给错误 Agent |

核心原则：

1. `@` 是路由指令，不只是文本装饰。
2. 只有「单个、明确、非 Orchestrator 目标」才直达。
3. 群聊里的无明确目标、多目标、`@all`、`@orchestrator` 才交给 Orchestrator 保持系统一致性。
4. 单聊和无 Orchestrator 的 task 不做自动编排，保持当前请求目标。
5. Orchestrator 分派时继续复用现有 `plan_and_dispatch → Dispatcher → ExecutionEngine` 链路。

## 目标架构

```
用户输入
  │
  ▼
Message Router
  │
  ├─ 单个明确 @Agent / 单聊无 @ / 无 Orchestrator task
  │    ▼
  │  Direct Route
  │    Backend RunTask(target_session_id, target_agent_type)
  │    AgentEnd adapter.stream_chat()
  │
  └─ 群聊无 @ / @all / 多 @ / @orchestrator
       ▼
     Orchestrator Route
       Backend RunTask(orchestrator_session_id, orchestrator)
       OrchestratorAdapter.reason()
         ├─ text: 直接回复
         ├─ ask_agent: 规划前咨询子 Agent
         └─ plan_and_dispatch: 自动分派执行任务
```

## 为什么不要全部先给 Orchestrator

全部先给 Orchestrator 看起来统一，但会带来几个问题：

1. 用户已经写了 `@前端`，Orchestrator 仍要二次理解和转发，增加延迟。
2. 简单单 Agent 问答会被迫进入编排流程，SSE 事件和持久化消息更复杂。
3. Orchestrator 的职责会混在「智能分派」和「普通转发」之间。
4. 一旦 Orchestrator LLM 判断失误，明确 `@` 也可能被派错。

更好的边界是：

- 单聊直达：把当前会话 Agent 当作默认目标。
- `@` 直达：把用户的明确意图当作事实。
- 自动分派：只在群聊且用户没有明确指定单个目标时启用智能判断。

## 路由解析规则

### Mention 只在消息开头生效

建议只把消息开头的 `@` 当作路由指令：

```text
@前端 实现登录页       -> 路由指令
请检查 @前端 的结果    -> 普通文本，不触发直达
```

这样可以避免误伤正文中的引用、邮箱、代码片段。

### Agent 可匹配身份

每个群成员应有一个后端生成的稳定路由身份 `route_id`，并支持少量别名。当前数据模型没有独立的 `agent_id` 字段，第一版可以不新增 DB 列，而是在读取 task sessions 时派生：

```text
route_id:      前端
mention_label: 前端
aliases:       前端, claude-code, Claude Code
session_id:    DB 真实 session_id
agent_type:    claude-code
```

`route_id` 应同时用于：

- Backend Message Router 的 mention 解析。
- Frontend `groupSessions` / Mention 菜单展示与插入。
- Orchestrator config 中 `agents[].id`。

匹配优先级：

1. `route_id`
2. `agent_name`
3. `agent_type`
4. 前端展示名，如 `AGENT_NAMES[agent_type]`

如果一个 mention 命中多个 Agent，不能直达，应交给 Orchestrator 或要求用户选一个。为了减少歧义，只有唯一命中的值才应该作为别名暴露给前端自动补全；重名、同类型重复的 Agent 仍以唯一 `route_id` 为准。

### Orchestrator 配置里的 Agent ID 必须唯一

当前后端给 Orchestrator 注入 agents 时，`id` 主要来自 `AgentName`，为空时回退到 `AgentType`。如果群里有两个同类型 Agent，`Dispatcher` 的 `_agent_map` 会发生覆盖。

建议在 `injectOrchestratorConfig` 里复用同一套 `route_id` 生成逻辑：

```text
base = agent_name 非空 ? agent_name : agent_type
id = base
若重复，则追加序号：base-2, base-3
```

例如：

```json
[
  { "id": "实现者", "type": "claude-code", "session_id": "..." },
  { "id": "claude-code", "type": "claude-code", "session_id": "..." },
  { "id": "claude-code-2", "type": "claude-code", "session_id": "..." }
]
```

这样 Orchestrator prompt 里的「可用 Agents」和 `Dispatcher` 的映射才是稳定的。

## 推荐实现方式

### 1. Backend 增加 Message Router

把路由解析放在 Backend 更稳，因为 Backend 持有 task 下所有 sessions，且所有客户端都会经过它。

建议在 `RunTask` 一开始、保存用户消息之前解析，但只处理外部用户请求。Orchestrator 内部调用子 Agent 时会带 `skip_user_message=true`，这类请求已经有明确的 `session_id + agent_type`，不能再进入 mention 路由，否则可能把子任务内容里的 `@xxx` 当成新路由指令。

```
RunTask
  │
  ├─ 查询 task sessions
  ├─ req.skip_user_message == true?
  │    └─ yes: trust request session_id / agent_type
  ├─ resolveMessageRoute(taskID, req)
  │    ├─ direct: 改写 req.SessionID / agentType / agentMessage
  │    ├─ orchestrator: 使用 Orchestrator session
  │    └─ unchanged: 单聊或无 Orchestrator task，保持原请求目标
  ├─ createUserMessage(displayMessage)
  ├─ ensureSession(targetSessionID)
  ├─ createAgentMessage(targetSessionID)
  ├─ buildAgentRequest(agentMessage)
  └─ runStream(...)
```

路由结果建议包含：

```go
type MessageRoute struct {
    Mode          string // "direct" | "orchestrator" | "unchanged"
    SessionID     string
    AgentType     string
    AgentName     string
    RouteID       string
    AgentMessage  string // 发给 Agent 的内容，可去掉开头 @xxx
    DisplayMessage string // 存进消息表的原始内容
}
```

直达时，用户消息建议存到目标 Agent 的 `session_id` 下；`unchanged` 模式沿用请求里的 `session_id`：

- 目标 Agent 收到的是去掉路由前缀后的正文。
- UI 展示的是原始用户消息，比如 `@前端 改一下按钮样式`。
- 群聊历史需要显示所有 user 消息，避免刷新后丢失直达消息。

### 2. RunTask 响应返回实际路由结果

前端需要知道应该订阅哪个 session 的 SSE。

现有响应：

```json
{
  "message_id": "...",
  "status": "streaming"
}
```

建议向后兼容地增加字段：

```json
{
  "message_id": "...",
  "status": "streaming",
  "session_id": "实际执行的 session_id",
  "agent_type": "实际执行的 agent_type",
  "agent_name": "实际执行的 agent_name",
  "route_id": "实际命中的 route_id",
  "route_mode": "direct"
}
```

旧前端可以忽略新字段；新前端用 `session_id` 连接 SSE，并用 `route_mode` 判断是否需要把流式内容写回当前群聊窗口。

当前 `POST /tasks/:taskId/run` 是 Backend 手写 API 类型，不属于 `agent-response.yaml`。如果后续要严格契约化，可新增 `contracts/schemas/run-task.yaml` 或把 Backend chat API 纳入已有契约生成流程。

### 3. Frontend 做 Mention 体验，但不作为唯一真相

前端负责体验：

- 输入 `@` 时弹出群成员列表。
- 选择成员后插入 `@mention_label `。
- 发送前可做本地预判，用于提示未知 Agent。

但最终路由以后端返回为准：

```
submitMessage(...)
  ▼
response.session_id
  ▼
connectSSE({ session_id: response.session_id, message_id })
```

`useChatStream` 需要拆开两个概念：

| 概念 | 含义 |
|------|------|
| `displaySessionId` | 当前打开的群聊主 session，通常是 Orchestrator |
| `streamSessionId` | 本次实际执行并订阅 SSE 的 session，可能是子 Agent |

前端 store 仍把流式内容写入 `displaySessionId` 对应的聊天窗口，但 SSE 参数使用 `streamSessionId`。当前 `ServeStream` 会根据 `message_id` 对应的数据库记录取真实 session 来订阅 Redis key，但前端仍应传后端返回的实际 `session_id`，避免以后后端增加参数校验时语义不一致。

`groupSessions` 建议补充后端生成的路由字段：

```ts
interface AgentSessionInfo {
  sessionId: string
  agentType: AgentType
  agentName: string
  routeId: string
  mentionLabel: string
  aliases?: string[]
}
```

### 4. 群聊历史加载规则调整

当前群聊历史会加载 task 下多 session 的 agent 消息，但对 user 消息更偏向当前 session。直达 `@Agent` 后，用户消息会保存在目标 Agent session 下，因此历史过滤要调整。

推荐由 Backend 增加 `GET /tasks/:taskId/messages?mode=group&primary_session_id=...` 统一返回群聊可见消息；前端可保留相同规则作为兼容兜底，但不要长期只依赖前端过滤，因为分页是在过滤前发生的。

群聊可见消息规则：

```text
user 消息：
  task 下全部显示

agent 消息：
  - session_id != groupPrimarySessionId: 显示
  - session_id == groupPrimarySessionId:
      只显示 agent_type 为空或 agent_type == orchestrator 的消息
      避免 Orchestrator stream 中镜像出来的子 Agent 内容与子 session 内容重复
```

这保持现有「避免重复子 Agent 消息」的意图，同时让 `@` 直达的用户消息刷新后不丢。

### 5. Orchestrator 自动分派保持现有链路

群聊无 `@` 且存在 Orchestrator session 时，继续走 Orchestrator：

```
Backend RunTask(orchestrator)
  │
  ▼
OrchestratorAdapter.stream_chat()
  │
  ├─ reason_node
  │    ├─ 直接 text 回复
  │    ├─ ask_agent(agent, question)
  │    └─ plan_and_dispatch(...)
  │
  ▼
Dispatcher.dispatch(plan)
  │
  ▼
ExecutionEngine._execute_task()
  │
  ▼
BackendClient.run_task(target session, skip_user_message=true)
```

需要加强的是 Orchestrator 的 agents 配置唯一性，而不是重写编排链路。

## 需要修改的文件

### Backend

| 文件 | 建议改动 |
|------|----------|
| `backend/internal/controller/impl/task_controller.go` | `RunTask` 前置 `resolveMessageRoute`，但跳过 `skip_user_message=true`；响应增加实际路由字段 |
| `backend/internal/service/impl/task_service.go` | `injectOrchestratorConfig` 复用唯一 `route_id` |
| `backend/internal/service/impl/task_route.go` | 新增 `resolveMessageRoute` 路由解析逻辑 |
| `backend/internal/controller/impl/message_controller.go` | 推荐增加 `mode=group` / `primary_session_id`，由后端统一执行群聊可见消息过滤 |
| `backend/internal/model/message.go` | 第一版不必改；如后续要表达来源/目标，可新增 `source_session_id`、`target_session_id` |

### Frontend

| 文件 | 建议改动 |
|------|----------|
| `frontend/src/components/chat/MessageInput.tsx` | 增加 `@` 成员选择和插入能力，展示 `mentionLabel`，提交原始显示文本 |
| `frontend/src/components/chat/ChatArea.tsx` | 传入 `groupSessions` 给输入框；调整群聊历史过滤规则 |
| `frontend/src/hooks/use-chat-stream.ts` | `sendMessage` 使用后端返回的实际 `session_id` 订阅 SSE；拆分 display/stream session |
| `frontend/src/lib/api.ts` | `submitMessage` 返回类型增加路由字段；`AgentSessionInfo` 增加 `routeId` / `mentionLabel` / `aliases` |

### AgentEnd

| 文件 | 建议改动 |
|------|----------|
| `agentend/src/orchestrator/execution/dispatcher.py` | 保持现状；依赖唯一 `route_id` 后映射更可靠 |
| `agentend/src/orchestrator/planning/prompts.py` | 可补一句：用户显式指定单 Agent 的场景通常已由 Backend 直达，进入 Orchestrator 的 `@all` / 多目标请求需要按协作任务处理 |

## 测试清单

### Backend 单元测试

1. `@前端 xxx` 命中唯一非 Orchestrator，返回 direct route。
2. 群聊无 `@` 且存在 Orchestrator，返回 Orchestrator route。
3. 单 Agent 会话无 `@`，返回 unchanged/current route。
4. task 中没有 Orchestrator 时无 `@`，返回 unchanged/current route。
5. `skip_user_message=true` 的内部 dispatch 不解析 mention，保持请求中的 `session_id + agent_type`。
6. `@orchestrator xxx`，返回 Orchestrator route。
7. `@all xxx`，返回 Orchestrator route。
8. `@未知 xxx`，返回 400 或 unresolved route。
9. 两个同名/同类型 Agent 时，生成唯一 `route_id`。
10. 群聊 `mode=group` 历史分页返回可见消息，直达 user 消息刷新后仍可见。

### Frontend 测试

1. 输入 `@` 弹出群成员列表。
2. 选择成员后插入 `@mentionLabel `。
3. `submitMessage` 收到 direct route 后，用返回的 `session_id` 建 SSE。
4. 群聊刷新后仍显示直达用户消息和目标 Agent 回复。
5. Orchestrator session 中镜像的子 Agent 消息不重复显示。

### 手动验收

1. 创建包含 Orchestrator、Claude Code、Codex 的群聊。
2. 发送 `@Codex 总结这个项目结构`。
3. 预期：Codex 直接回复，Orchestrator 不产生规划卡。
4. 发送 `帮我分析并安排谁来修复这个问题`。
5. 预期：Orchestrator 回复，必要时出现 plan / ask-card / runtime 事件。
6. 刷新页面。
7. 预期：两轮消息均保留，用户消息、Codex 回复、Orchestrator 回复归属正确且无重复。

## 分阶段落地

### Phase 1: 低风险直达

- Backend 增加路由解析。
- RunTask 响应增加实际 `session_id`。
- Frontend 按响应 session 订阅 SSE。
- 群聊历史通过 `mode=group` 或兼容过滤显示所有 user 消息。
- 内部 `skip_user_message=true` 请求保持原样，不参与路由。

### Phase 2: 体验完善

- MessageInput 增加 mention 菜单。
- 未知 mention 前端即时提示。
- 群成员展示唯一 `route_id`、`mentionLabel` 和别名。

### Phase 3: 契约化与可观测性

- 将 RunTask 请求/响应纳入 contracts。
- Message 增加可选路由元数据。
- 后端日志记录 `route_mode`、`source_session_id`、`target_session_id`，便于排查派错 Agent。

## 最小可接受实现

如果要尽快上线，最小实现可以只做六件事：

1. Backend 在 `RunTask` 开头识别消息开头的 `@Agent`。
2. 仅对 `skip_user_message=false` 的外部用户请求解析路由；内部 dispatch 保持原样。
3. 命中唯一非 Orchestrator 时，把请求改写到目标 `session_id + agent_type`；单聊或无 Orchestrator task 保持当前请求目标。
4. 群聊无 `@` 且存在 Orchestrator 时，把请求改写到 Orchestrator session。
5. RunTask 响应返回实际 `session_id`。
6. Frontend SSE 使用响应里的 `session_id`，群聊历史显示所有 user 消息。

这六步完成后，`@指定 Agent 回复`、`群聊无 @ 由 Orchestrator 自动分派`、`单聊无 @ 保持当前 Agent 回复` 就能形成清晰闭环。
