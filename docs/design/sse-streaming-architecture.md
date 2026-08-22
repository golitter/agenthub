# SSE 流式输出架构

本文只记录三端 SSE 全链路。端内实现细节分别维护在：

- `agentend/docs/design/05-api.md`
- `backend/docs/design/03-stream.md`
- `frontend/docs/design/04-sse.md`
- `frontend/docs/design/08-block-parser.md`

## 实现了什么

AgentHub 使用 Backend 作为 SSE 中转层，而不是让 Frontend 直连 AgentEnd。这样可以同时满足：

- 前端低延迟看到 Agent 输出。
- Backend 保存 Message，刷新页面后可恢复。
- AgentEnd 专注执行 Agent CLI，不承担业务持久化。
- Orchestrator / direct Agent / 子 Agent 镜像输出可以在 Backend 做路由和持久化控制。

## 怎么实现的

### 全局数据流

```text
Frontend
  POST /api/tasks/:taskId/run
  GET  /api/tasks/:taskId/stream?session_id=...&message_id=...
      |
      v
Backend
  TaskService.RunTask
  StreamService.ServeStream
  StreamWriter.Run
  RuntimeHub.Publish
  Redis Stream
  MySQL Message
      |
      v
AgentEnd
  POST /v1/agent/stream
  Adapter.stream_chat()
  StreamEvent
```

### AgentEnd：事件生产者

AgentEnd 的职责是把 Agent CLI 或 Orchestrator 的执行过程转成统一 SSE 事件：

| 层 | 代码 | 说明 |
|----|------|------|
| API | `agentend/src/api/v1/agent.py` | `/v1/agent/stream` 入口 |
| Adapter | `agentend/src/adapters/` | Claude / OpenCode / Codex / Pi / Orchestrator 输出适配 |
| Schema | `agentend/src/schemas/` + `agentend/src/generated/` | 请求、响应、事件模型 |
| Rules | `agentend/src/rules/` | 执行前注入 Safety / Scope / Pin / Soul / GroupChat / Taskctl / Skill 等规则 |

AgentEnd 不直接写 Backend 的业务数据库；它输出执行事件，由 Backend 消费。

### Backend：实时中转与持久化

Backend 接到运行请求后：

1. Service 校验任务、会话、路由和 Agent 配置。
2. 创建或更新用户消息与 agent streaming 消息。
3. 调 AgentEnd `/v1/agent/stream`。
4. `StreamWriter` 消费 AgentEnd SSE。
5. `RuntimeHub` 向在线前端推送低延迟事件。
6. Redis Stream 提供断线补偿。
7. MySQL Message 保存最终可恢复内容。
8. 前端订阅入口首包发送 `retry: 1000` 与 `: connected`，让浏览器 EventSource 使用更明确的重连节奏。

关键代码：

| 代码 | 说明 |
|------|------|
| `backend/internal/service/impl/task_service.go` | 创建与运行任务 |
| `backend/internal/service/impl/stream_helper.go` | 调 AgentEnd 并接入 StreamWriter |
| `backend/internal/stream/writer.go` | 消费 AgentEnd SSE 并刷写消息 |
| `backend/internal/stream/hub.go` | 内存发布订阅 |
| `backend/internal/controller/impl/stream_controller.go` | 前端 SSE 订阅入口 |

前端订阅同一个 `session_id + message_id` 时，Backend 按三段输出：

| 阶段 | 来源 | 作用 |
|------|------|------|
| 已落库内容 | MySQL Message content | 页面刷新或重连后立即恢复已生成文本 |
| 间隙补偿 | Redis Stream `last_seq` 之后的事件 | 补齐断线窗口内尚未进入 MySQL content 的事件 |
| 实时事件 | RuntimeHub | 低延迟推送当前 Agent 正在产生的输出 |

### Frontend：事件消费与消息投影

Frontend 不直接维护业务权威状态，只把历史消息和实时事件投影成当前 UI：

| 层 | 代码 | 说明 |
|----|------|------|
| SSE | `frontend/src/lib/sse.ts` | 建立 EventSource 连接 |
| Hook | `frontend/src/hooks/use-chat-stream.ts` | 启动任务、订阅流、处理生命周期 |
| Store | `frontend/src/stores/message-store.ts` | 合并 streaming 文本与 runtime blocks |
| Blocks | `frontend/src/lib/block-reducer.ts` | 解析结构化 aka_yhy 块 |
| UI | `frontend/src/components/cards/` | Diff / HTML / Image / Plan / Tool 等卡片 |

### 持久化与去重原则

| 场景 | 策略 |
|------|------|
| 在线实时输出 | RuntimeHub 先推给前端 |
| 前端断线重连 | Backend 从 Redis / MySQL 补偿历史内容 |
| 服务重启后遗留 streaming 消息 | Backend 启动时标记为 failed，并同步收敛 Session 状态 |
| Orchestrator 镜像子 Agent 输出 | 根据 group/session 规则决定是否持久化，避免重复写入当前 Session |
| 前端 replay 与 live 接缝 | 前端用 `message_id + streamingReplay.offset` / block 合并逻辑避免重复追加 |
| 连接半开 | Frontend `openTimeoutMs` 触发错误并关闭 EventSource |
| 长时间无事件 | Frontend `staleTimeoutMs` 触发错误；正常流由 Backend 15 秒 heartbeat 保活 |

### 关键设计决策

| 决策 | 原因 |
|------|------|
| Frontend 不直连 AgentEnd | Backend 需要保存业务状态、鉴权、路由和断线恢复 |
| 使用 SSE 而非 WebSocket | 当前是单向 token/event 流，SSE 更简单且浏览器支持重连语义 |
| RuntimeHub + Redis + MySQL | 分别覆盖低延迟、断线补偿、最终恢复 |
| StreamEvent 来源于 contracts | 避免三端手写字段漂移 |

### Orchestrator 子 Agent 的 IM 投影

Orchestrator 运行卡片与群成员发言使用不同事件语义：

| 事件 | UI 投影 | 身份要求 |
|------|---------|----------|
| `runtime_executing` / `runtime_completed` | Orchestrator 运行卡片 | `plan_task_id`、`run_id`、`attempt` |
| `runtime_text` | 卡片内运行日志（兼容旧流） | runtime identity |
| `text` + `group_id` + `message_id` | 群聊中的独立 Agent 消息气泡 | `agent`、`agent_type`、稳定的镜像 `message_id` |

AgentEnd 用子任务 Backend `run_task` 返回的 `message_id` 标识一次发言源；Backend 再按
`group_id + source_message_id` 创建或复用群聊镜像消息，并将镜像 `message_id` 发给前端。
因此同一个 Agent 的两次任务不会合并，而多个 Agent 的并行增量也不会争用同一条
streaming message。控制卡片标记始终写回 Orchestrator 根消息，不能跟随当前子消息游标。

### 维护规则

1. 本文只维护三端链路和设计决策。
2. 具体函数、字段、组件列表放端内 design 文档，不在本文重复。
3. SSE 事件字段变化先改 `contracts/schemas/event-types.yaml`，再运行 `make generate`。
