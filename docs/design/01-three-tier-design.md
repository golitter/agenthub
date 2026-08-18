# 三层架构设计：React + Go + AgentEnd Python

本文只记录三端协作边界和跨端设计原则。具体实现细节不要在这里重复维护：

| 细节类型 | 权威文档 |
|----------|----------|
| Frontend 组件、状态、SSE、主题 | `frontend/docs/design/` |
| Backend 模型、Controller/Service/DAO、流式中转、配置、装配 | `backend/docs/design/` |
| AgentEnd 适配器、会话、规则、Workspace、Orchestrator | `agentend/docs/design/` |
| 跨端协议字段 | `contracts/schemas/` + `contracts/AGENTS.md` |
| 三端 SSE 时序 | `docs/design/sse-streaming-architecture.md` |

## 实现了什么

AgentHub 是三端协作的多 Agent 聊天系统：

- `frontend/`：React 单页应用，负责会话、消息流、卡片渲染、工作区与管理面板交互。
- `backend/`：Go HTTP API 与业务状态层，负责持久化、路由决策、SSE 中转、Workspace 代理和管理接口。
- `agentend/`：Python FastAPI Agent Runtime，负责运行 Claude Code / OpenCode CLI / Codex CLI / Pi CLI / Orchestrator，管理会话、规则、技能和 Git Worktree。
- `contracts/`：跨端协议单一来源，YAML schema 生成 TypeScript / Go / Python 类型。

核心目标是让 Agent 执行过程具备可观察、可恢复、可隔离的业务状态，而不是把任意运行时能力抽象成通用分布式平台。

## 怎么实现的

### 三端职责边界

```text
Frontend
  -> 展示与交互：会话列表、消息投影、卡片渲染、计划审查、工作区 UI

Backend
  -> 控制面：Task / Session / Message / Skill / ContactGroup / Admin 状态
  -> 中转层：消费 AgentEnd SSE，实时推送给前端，同时写入 Redis / MySQL
  -> 代理层：把 workspace 文件、diff、commit、preview 请求转交 AgentEnd

AgentEnd
  -> 执行层：运行 Agent CLI / Orchestrator
  -> 隔离层：按 task/session 管理 Git Worktree
  -> 规则层：Safety / Pin / Soul / GroupChat / Scope / Taskctl / Skill

Contracts
  -> 协议层：事件、请求、响应、路由、消息、会话状态的跨端类型来源
```

硬边界：

| 边界 | 含义 |
|------|------|
| Backend 不执行 Agent | Backend 只创建任务、保存状态、触发 AgentEnd，不直接启动 CLI |
| AgentEnd 不拥有业务持久化权威 | AgentEnd 产生执行输出，最终业务状态由 Backend 保存 |
| 契约先于代码 | 跨端字段先改 `contracts/schemas/*.yaml`，再 `make generate` |
| Workspace 操作在 AgentEnd | Go 只代理请求，不直接编辑 worktree |
| 前端只做投影 | 前端根据 API 与 SSE 事件更新 UI，不成为状态权威 |

### 请求到流式输出

```text
用户发送消息
  -> Frontend 调 Backend 创建 / 运行任务
  -> Backend 决定 direct / orchestrator / unchanged 路由
  -> Backend 调 AgentEnd /v1/agent/stream
  -> AgentEnd 运行目标 Agent 并输出 SSE
  -> Backend StreamWriter 消费 SSE
  -> Backend RuntimeHub 低延迟推前端
  -> Backend Redis Stream / MySQL 保存可恢复内容
  -> Frontend useChatStream + Zustand 合成消息和 runtime blocks
```

细节见：

- `docs/design/sse-streaming-architecture.md`
- `backend/docs/design/03-stream.md`
- `frontend/docs/design/04-sse.md`
- `agentend/docs/design/05-api.md`

### 状态归属

| 状态 | 权威来源 | 说明 |
|------|----------|------|
| Task / Session / Message | Backend MySQL | 刷新页面和服务重启后的恢复来源 |
| CLI session_id 映射 | AgentEnd 持久化文件 | 记录 API session 与 Claude/OpenCode/Codex/Pi CLI session 的对应关系 |
| Agent 路由协议 | `contracts/schemas/agent-routing.yaml` | Backend / Frontend / AgentEnd 共同使用 |
| StreamEvent 协议 | `contracts/schemas/event-types.yaml` | SSE 事件跨端类型来源 |
| UI runtime blocks | Frontend store | 仅服务当前渲染，不作为后端事实 |

### 代码阅读路径

```text
contracts/schemas/
  -> backend/internal/app/
  -> backend/internal/service/impl/
  -> backend/internal/stream/
  -> agentend/src/api/v1/agent.py
  -> agentend/src/adapters/
  -> frontend/src/hooks/use-chat-stream.ts
  -> frontend/src/stores/message-store.ts
  -> frontend/src/components/cards/
```

### 文档维护规则

1. 本文只维护跨端边界、状态归属和阅读路径。
2. 任一端的目录树、依赖版本、函数签名、模型字段，不在本文重复粘贴。
3. 发现跨端协议变化时，先更新 `contracts/schemas/`，再更新引用该契约的端内文档。
4. 三端实现细节以各端 `docs/design/` 为准，根 `docs/design/` 只承载跨端主题。
