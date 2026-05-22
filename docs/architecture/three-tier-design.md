# 三层架构设计：React + Go + AgentEnd Python

## 一、当前状态总览

```
bytedanceai/
├── agentend/      ✅ 已实现 (Python FastAPI, ~60个文件)
├── backend/       ❌ 空目录 (Go 后端未开始)
├── frontend/      ❌ 不存在 (React 前端未开始)
├── docs/          📄 架构文档已有
└── scripts/       空目录
```

只有 AgentEnd Python 端是真实代码，Go 后端和 React 前端都是空白。三层有极大的设计自由度，但要确保 API 契约一开始就对齐。

---

## 二、三层架构全景

```
┌─────────────────────────────────────────────────────────────────┐
│                    React Frontend (SPA)                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │ TextCard  │  │DiffCard  │  │ImageCard │  │ToolProg  │ ...   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
│         ▲              ▲              ▲              ▲          │
│         └──────────────┴──────────────┴──────────────┘          │
│                    MessageAggregator                             │
│                         ▲  SSE / WebSocket                      │
└─────────────────────────┼───────────────────────────────────────┘
                          │
┌─────────────────────────┼───────────────────────────────────────┐
│              Go Backend (API Gateway)                            │
│                         │                                       │
│  ┌──────────┐  ┌───────┴───┐  ┌──────────┐  ┌──────────┐      │
│  │ Auth     │  │SSE Proxy  │  │Session   │  │Workspace │      │
│  │ (JWT)    │  │/ WS Hub   │  │Manager   │  │Manager   │      │
│  └──────────┘  └───────┬───┘  └──────────┘  └──────────┘      │
│                        │ HTTP                                   │
└────────────────────────┼────────────────────────────────────────┘
                         │
┌────────────────────────┼────────────────────────────────────────┐
│          AgentEnd Runtime (FastAPI, Python)                      │
│                        │                                        │
│  ┌──────────┐  ┌──────┴────┐  ┌──────────┐  ┌──────────┐      │
│  │ Rule     │  │Adapter    │  │Orchestr.  │  │Workspace │      │
│  │ Engine   │  │Registry   │  │(LLM规划)  │  │Manager   │      │
│  └──────────┘  └──────┬────┘  └──────────┘  └──────────┘      │
│                       │                                         │
│         ┌─────────────┼──────────────┐                          │
│         ▼             ▼              ▼                          │
│   Claude CLI    OpenCode CLI   DeepSeek LLM                    │
└─────────────────────────────────────────────────────────────────┘
```

核心设计原则：**Runtime First · Event First · Adapter First**

所有 Agent 通过统一的 Adapter 模式接入，所有输出统一为 `StreamEvent`。前端、Agent、UI 三层完全解耦。

---

## 三、第一层：AgentEnd Python — 现有能力

### 3.1 项目结构

```
agentend/src/
├── api/v1/           # HTTP 端点
│   ├── agent.py      # POST /v1/agent/stream (SSE), POST /v1/agent/execute
│   ├── session.py    # GET/POST/DELETE /v1/session/*
│   ├── workspace.py  # POST/DELETE /v1/workspace/*
│   ├── health.py     # GET /health
│   └── pin.py        # POST /v1/pin/*
├── adapters/         # Agent 适配器
│   ├── base.py       # BaseAgentAdapter (5个抽象方法)
│   ├── claude.py     # Claude CLI 子进程管理
│   ├── opencode.py   # OpenCode CLI 子进程管理
│   └── orchestrator.py  # OrchestratorAdapter (规划+调度+聚合)
├── orchestrator/     # 编排引擎
│   ├── graph.py      # LangGraph: plan → write_shared
│   ├── dispatcher.py # 任务分发给 Agent
│   ├── aggregator.py # 结果聚合 (再调 LLM)
│   ├── evolution.py  # 经验记忆
│   ├── pin_memory.py # Pin 约束记忆
│   └── models.py     # TaskDef, PlanOutput, DispatchResult, TaskResult
├── session/          # 会话管理
├── workspace/        # Git worktree 隔离
├── rules/            # Rule Engine (安全校验)
└── schemas/          # StreamEvent, AgentRequest, AgentResponse
```

### 3.2 AgentEnd 暴露的 API Surface（Go 后端要调用的）

| Endpoint | Method | 用途 | 返回类型 |
|----------|--------|------|----------|
| `/health` | GET | 健康检查 | JSON |
| `/v1/agent/stream` | POST | 流式执行 Agent | **SSE** |
| `/v1/agent/execute` | POST | 同步执行 Agent | JSON |
| `/v1/session` | GET | 列出 sessions | JSON |
| `/v1/session/{id}` | GET/DELETE | 查询/销毁 session | JSON |
| `/v1/session/{id}/interrupt` | POST | 中断 session | JSON |
| `/v1/workspace/create` | POST | 创建 workspace | JSON |
| `/v1/workspace/{id}/commit` | POST | 提交 workspace | JSON |
| `/v1/workspace/{id}/merge` | POST | 合并 workspace | JSON |
| `/v1/workspace/{id}` | DELETE | 清理 workspace | JSON |
| `/v1/workspace` | GET | 列出 workspaces | JSON |
| `/v1/pin/add` | POST | 添加 Pin 约束 | JSON |
| `/v1/pin/remove` | POST | 移除 Pin 约束 | JSON |
| `/v1/pin/list` | GET | 列出 Pins | JSON |

### 3.3 核心数据流 — SSE Event

```
StreamEvent 当前格式:
{
  "type": "text" | "tool_call" | "tool_result" | "artifact" | "planning" | "done" | "error",
  "content": { ... },
  "timestamp": 1716345600.0
}
```

SSE 传输格式（在 `_execute_stream` 中）:
```
event: text
data: {"type":"text","content":{"text":"Hello"},"timestamp":...}

event: tool_call
data: {"type":"tool_call","content":{"tool":"Edit","args":{...}},"timestamp":...}

event: done
data: {"type":"done","content":{"text":"","usage":{}},"timestamp":...}
```

---

## 四、第二层：Go Backend — 设计方案

Go Backend 的核心角色是 **API Gateway**，不直接做 AI 推理，而是面向 React 提供连接，面向 AgentEnd 透传请求。

### 4.1 职责边界

```
✅ Go Backend 应该做的:              ❌ 不应该做的:
─────────────────────────            ─────────────────────
• 用户认证 (JWT/OAuth)               • AI 推理/编排
• WebSocket ↔ SSE 转换               • CLI 子进程管理
• 请求路由 & 负载均衡                • Git worktree 管理
• 多 AgentEnd 实例管理               • LLM 调用
• 数据库 (MySQL/PostgreSQL)          • Rule Engine
• 项目/任务 CRUD                     • Prompt 构建
• 权限 & 租户隔离                    • Artifact 文件管理
• Event 日志持久化
• API 版本管理 (v1, v2)
```

### 4.2 项目结构

```
backend/
├── cmd/
│   └── server/
│       └── main.go              # 入口
├── internal/
│   ├── config/                  # 配置 (YAML/ENV)
│   ├── middleware/               # JWT, CORS, Logging
│   ├── handler/                 # HTTP Handler (Gin)
│   │   ├── agent.go             # 代理到 AgentEnd 的 SSE/Execute
│   │   ├── session.go           # Session 管理
│   │   ├── workspace.go         # Workspace 管理
│   │   ├── project.go           # 项目 CRUD
│   │   ├── user.go              # 用户注册/登录
│   │   └── artifact.go          # 产物访问
│   ├── model/                   # GORM 模型
│   │   ├── user.go
│   │   ├── project.go
│   │   ├── task.go
│   │   └── event_log.go         # SSE Event 持久化
│   ├── service/                 # 业务逻辑层
│   │   ├── agent_service.go     # 调用 AgentEnd 的 client
│   │   └── project_service.go
│   ├── proxy/                   # SSE/HTTP 代理
│   │   ├── sse_proxy.go         # 透传 AgentEnd SSE → 前端
│   │   └── ws_hub.go            # WebSocket Hub (如果前端用 WS)
│   └── repository/              # 数据访问层
├── pkg/
│   └── agentend_client/         # AgentEnd HTTP Client
│       ├── client.go
│       ├── sse_reader.go        # SSE 流式读取器
│       └── types.go             # StreamEvent 等类型映射
├── migrations/                  # 数据库迁移
├── go.mod
└── config.yaml
```

### 4.3 GORM 数据模型

```go
type User struct {
    ID        uint   `gorm:"primaryKey"`
    Username  string `gorm:"uniqueIndex"`
    Email     string `gorm:"uniqueIndex"`
    Password  string // hashed
    CreatedAt time.Time
}

type Project struct {
    ID        uint   `gorm:"primaryKey"`
    Name      string
    RepoURL   string
    UserID    uint   `gorm:"index"`
    CreatedAt time.Time
}

type Task struct {
    ID          uint   `gorm:"primaryKey"`
    ProjectID   uint   `gorm:"index"`
    TaskID      string `gorm:"uniqueIndex"` // 对应 AgentEnd 的 task_id
    UserID      uint
    AgentType   string // claude-code, opencode, orchestrator
    Status      string // pending, running, completed, failed
    Message     string // 用户原始消息
    Result      string // 最终结果摘要
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type EventLog struct {
    ID        uint   `gorm:"primaryKey"`
    TaskID    string `gorm:"index"`
    EventType string // text, tool_call, tool_result, done...
    Source    string // claude-code, opencode, orchestrator
    Payload   JSON   // 完整的 event content
    Timestamp float64
}
```

---

## 五、第三层：React Frontend — 设计方案

### 5.1 项目结构

```
frontend/
├── src/
│   ├── api/                    # API Client
│   │   ├── client.ts           # Axios/Fetch 封装
│   │   ├── sse.ts              # SSE 连接管理
│   │   └── types.ts            # 与后端对齐的类型
│   ├── hooks/
│   │   ├── useAgentStream.ts   # SSE 流式数据 hook
│   │   ├── useSession.ts
│   │   └── useWorkspace.ts
│   ├── components/
│   │   ├── chat/
│   │   │   ├── Conversation.tsx    # 主对话容器
│   │   │   ├── EventGroup.tsx      # 事件分组
│   │   │   ├── MessageInput.tsx    # 输入框
│   │   │   └── AgentTimeline.tsx   # Agent 时间线
│   │   └── cards/                  # UI 卡片
│   │       ├── TextCard.tsx
│   │       ├── CodeBlockCard.tsx
│   │       ├── DiffViewCard.tsx
│   │       ├── ToolProgressCard.tsx
│   │       └── ImageCard.tsx
│   ├── aggregator/
│   │   └── MessageAggregator.ts # SSE chunk 聚合层
│   ├── store/                  # 状态管理
│   │   └── agentStore.ts       # Zustand
│   └── pages/
│       ├── ChatPage.tsx
│       └── ProjectPage.tsx
```

### 5.2 MessageAggregator（核心设计）

SSE 直出会导致逐字符 chunk，React 疯狂 rerender。必须在 SSE 和 React 之间加一层聚合：

```
SSE Stream ──▶ EventBuffer ──▶ ChunkAggregator ──▶ Stable Messages ──▶ React Render
                  │                 │
                  │ 300ms debounce  │ flush on tool_call / artifact / done
                  │ for TEXT        │
                  ▼                 ▼
              累积 text chunk    产出完整的 Message 对象
```

聚合规则：

| Event Type | 聚合策略 |
|---|---|
| `TEXT`（连续 chunk） | 300ms debounce 合并 |
| `TOOL_CALL` | 立即 flush，开始新的聚合组 |
| `ARTIFACT` | 立即 flush 为独立卡片 |
| `DONE` | 强制 finalize 当前所有 buffer |

### 5.3 卡片组件体系

根据 `ui_type` 分发渲染：

```
Conversation
  └── EventGroup（按 tool_call / 思考 / 回复 自动分组）
        ├── TextCard           # ui_type: markdown
        ├── CodeBlockCard      # ui_type: code
        ├── DiffViewCard       # ui_type: diff
        ├── ImageCard          # ui_type: artifact.image
        ├── FileAttachmentCard # ui_type: artifact.file
        ├── DeployStatusCard   # ui_type: deploy.status
        ├── PlanningCard       # ui_type: planning.step
        ├── ToolProgressCard   # tool.start / tool.stdout / tool.stderr
        └── SystemCard         # init / error / done
```

---

## 六、三层联调的 API 契约

### 6.1 契约 1：Event 协议（Envelope）

建议 AgentEnd 直接输出 Envelope 格式，Go 只做透传：

```
AgentEnd 发出                    Go 透传                    React 消费
─────────────                  ─────────                  ──────────
{                              {                          {
  "type": "text",         ──▶    "event": {          ──▶    event.type === "text"
  "content": {...},              "type": "text",            → 渲染 TextCard
  "timestamp": 1716...           "source": "claude-code",
                                 "session_id": "...",
                                 "task_id": "...",
                               },
                               "payload": {...},
                               "meta": {"timestamp":...}
                             }
```

**不在 Go 层做 Envelope 转换**。理由：
1. 避免 Go 里 JSON 解析再重组——增加延迟和 bug 面
2. Go 的角色是透传，不是协议转换
3. Envelope 是 AgentEnd 的职责——它知道 `source`、`ui_type` 等元信息

### 6.2 契约 2：请求体对齐

```
React 发送                      Go 转发                 AgentEnd 接收
──────────                    ─────────               ─────────────
POST /api/tasks/run           POST /v1/agent/stream   AgentRequest
{                             {                       {
  task_id,           ──▶       task_id,        ──▶     task_id,
  message,                     message,                 message,
  agent_type,                  agent_type,              agent_type,
  session_id?,                 session_id?,             session_id,
  workspace_path?,             workspace_path?,         workspace_path,
  config?                      config?                  config?
}                             }                       }
```

Go 可以在这层增加：
- `user_id`（从 JWT 解析）
- `project_id`（关联项目）
- 把 `user_id` 映射为 AgentEnd 的 `session_id` 前缀（租户隔离）

### 6.3 契约 3：数据库 vs AgentEnd 状态

```
方案 A（推荐）: Go 是状态的主人，AgentEnd 是无状态执行器
──────────────────────────────────────────────────────────
• Go DB 存储所有持久化状态 (session, task, event_log)
• AgentEnd 只管执行，不持久化
• Go 通过 AgentEnd 的 REST API 查询/管理运行时状态
• AgentEnd 重启后，Go 重新初始化 session
```

Go 从 SSE 事件流中实时提取状态变更写入 DB。

---

## 七、Go ↔ AgentEnd 的 SSE 透传交互

这是联调中最复杂的部分：

```
React                   Go Backend                    AgentEnd
  │                         │                             │
  │── POST /api/tasks/run ──▶                             │
  │                         │── POST /v1/agent/stream ──▶ │
  │                         │◀── SSE: event: init ────────│
  │◀── WS/SSE: init ───────│                             │
  │                         │◀── SSE: event: text ────────│
  │◀── WS/SSE: text ───────│                             │
  │                         │◀── SSE: event: tool_call ──│
  │◀── WS/SSE: tool_call ──│                             │
  │                         │◀── SSE: event: done ────────│
  │◀── WS/SSE: done ───────│                             │
```

Go 的 `sse_reader.go` 核心逻辑：

```go
func (c *AgentEndClient) StreamAgent(ctx context.Context, req AgentRequest) (<-chan StreamEvent, error) {
    // 1. POST 到 AgentEnd /v1/agent/stream
    // 2. 从 response body 逐行读取 SSE
    // 3. 解析 "event:" 和 "data:" 行
    // 4. 发送到 channel
}
```

---

## 八、共同开发策略

### 8.1 开发顺序

```
Week 1-2: 基础骨架 + API 契约
├── AgentEnd: Envelope 升级 (StreamEvent 加 id, source, ui_type)
├── Go: 项目骨架 + AgentEnd client + SSE proxy
└── React: 项目骨架 + SSE client + TextCard

Week 3-4: 核心链路打通
├── AgentEnd: 稳定 SSE 输出
├── Go: SSE 透传 + JWT auth + DB models
└── React: MessageAggregator + 基础卡片渲染

Week 5-6: 功能完善
├── AgentEnd: Orchestrator 改进, ArtifactManager
├── Go: WebSocket 支持, Event Log, 多实例管理
└── React: 完整卡片系统, Timeline, 工具执行可视化
```

### 8.2 Contract-First 开发模式

先定义三层之间的 API 契约，然后各端独立开发：

1. 在 `docs/` 中定义 OpenAPI spec (Go 对外 API)、AgentEnd API spec、Event Protocol spec
2. 前后端 mock：
   - React: MSW (Mock Service Worker) mock Go API
   - Go: 直接调用 AgentEnd（agentend 已可用）
3. 联调测试：先跑通 React → Go → AgentEnd 的 SSE 流，再逐步加功能

### 8.3 各端并行工作

```
┌─────────────────────────────────────────────────────────────┐
│ 可并行的开发任务                                              │
├──────────────┬──────────────────┬────────────────────────────┤
│  React       │  Go              │  AgentEnd                  │
├──────────────┼──────────────────┼────────────────────────────┤
│ 项目初始化    │ 项目初始化        │ Envelope 升级              │
│  (Vite+TS)   │  (Gin+GORM)      │ StreamEvent schema 扩展    │
│              │                  │                            │
│ UI 组件开发   │ DB schema 设计   │ ArtifactManager 实现       │
│  (Storybook) │  (migrations)    │ (产物注册/解析)            │
│              │                  │                            │
│ SSE client   │ AgentEnd client  │ ui_type 标注              │
│  封装        │  + SSE reader    │ (adapter 层补充)           │
│              │                  │                            │
│ Zustand      │ JWT auth         │ Orchestrator 改进          │
│  store 设计  │  middleware      │ (去 LangGraph, 简化)       │
└──────────────┴──────────────────┴────────────────────────────┘
```

---

## 九、待确认的设计决策

### 9.1 前端到 Go 的实时通信协议

| 方案 | 优点 | 缺点 |
|------|------|------|
| SSE (透传) | 简单，Go 只做 pipe | 前端无法向正在运行的 Agent 发消息 |
| WebSocket | 双向通信 | Go 需要维护 WS Hub，需要 SSE→WS 转换 |
| SSE + REST (推荐) | SSE 下行，REST 上行 | 最实用 |

### 9.2 是否保留 LangGraph

当前只有两个节点的线性管道，orchestrator-drawbacks.md 列了很多 LangGraph 的弊端。选项：
- 去 LangGraph，用普通 async 函数
- 保留 LangGraph 但升级为真正的 DAG

### 9.3 Event Envelope 升级策略

- 在 AgentEnd 端直接升级 `StreamEvent` schema
- 或在 Go 层做包装（不推荐）
