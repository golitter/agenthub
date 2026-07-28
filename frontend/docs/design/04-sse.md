# SSE — 连接与数据流

## 实现了什么

基于 EventSource 的 SSE 客户端，配合两步式 API 调用（POST 提交消息 + GET 连接 SSE 流）实现实时流式通信。所有 REST API 调用集中在 `lib/api.ts`。

## 怎么实现的

### SSE 客户端 (`src/lib/sse.ts`)

封装 `EventSource` 连接，接收 SSE 事件并解析为 `StreamEvent` 类型。支持自动重连、连接建立超时、空闲超时和手动中断：

```typescript
interface SSEOptions {
  url: string
  params?: Record<string, string>
  onEvent: (event: StreamEvent) => void
  onError?: (error: Error) => void
  /** Enable auto-reconnect (EventSource reconnects natively) */
  reconnect?: boolean
  /** Max ms to wait for the first successful connection (default 30s) */
  openTimeoutMs?: number
  /** Max ms without any event before treating the stream as dead (default 5min) */
  staleTimeoutMs?: number
}

export function connectSSE(options: SSEOptions): AbortController
```

关键设计点：
- 默认使用同源 `/api/...` 建立 SSE 连接；如需直连后端，可通过 `VITE_SSE_BASE_URL` 显式覆盖
- `AbortController` 支持手动中断流，abort 时统一清理 timer 并关闭 EventSource
- `openTimeoutMs` 防止连接半开后 UI 长期停在 loading
- `staleTimeoutMs` 防止连接已失活但浏览器未触发错误；Backend 每 15 秒发送 heartbeat，正常流不会触发该超时
- `reconnect` 参数控制是否让 EventSource 自动重连；Backend 首包发送 `retry: 1000`，提示浏览器按 1 秒重连节奏恢复

### 两步式 SSE 流程

SSE 通信分为两步：先 POST 提交消息获取 `RunTaskResponse`，再用响应中的实际 `session_id + message_id` 连接 SSE 流：

```
客户端                              后端
  │                                  │
  │  POST /api/tasks/:id/run         │  提交消息
  │  { message, session_id,          │
  │    agent_type }                  │
  │ ────────────────────────────────►│
  │  ◄── { message_id }             │
  │                                  │
  │  GET /api/tasks/:id/stream       │  连接 SSE 流
  │  ?session_id=&message_id=        │
  │ ────────────────────────────────►│
  │                                  │
  │  retry: 1000 / : connected       │  建立连接 + 重连节奏提示
  │ ◄────────────────────────────────│
  │  SSE: {"type":"init"}            │
  │ ◄────────────────────────────────│
  │  SSE: {"type":"text","content":{"text":"..."}} │
  │ ◄────────────────────────────────│
  │  SSE: {"type":"tool_call","content":{"name":"..."}} │
  │ ◄────────────────────────────────│
  │  SSE: {"type":"done"}            │
  │ ◄────────────────────────────────│
```

### 重连与去重

浏览器自动重连会重新请求同一个 `session_id + message_id` 流。Backend 会先输出 MySQL 中该 agent message 已持久化的内容，再从 Redis Stream 追补 `last_seq` 后的事件，最后接入 RuntimeHub 实时事件。

前端 `message-store` 使用 `streamingMessageId + streamingReplay.offset` 处理 replay：同一个 `message_id` 下，如果新 text chunk 已经存在于当前 streaming 内容尾部，就只推进 offset 而不重复追加；如果 replay chunk 覆盖了已知尾部后还有新增文本，则只追加新增部分。

### API 层 (`src/lib/api.ts`)

所有 REST API 调用集中在 `lib/api.ts`，使用原生 `fetch` 封装。

**消息提交** — POST `/api/tasks/:id/run`：

```typescript
import type { RunTaskRequest, RunTaskResponse } from '@/generated/agent-routing'

export async function submitMessage(
  taskId: string,
  body: RunTaskRequest,
): Promise<RunTaskResponse> {
  const res = await fetch(`${API_BASE}/tasks/${taskId}/run`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new Error(json.msg || `HTTP ${res.status}`)
  }
  const json = await res.json()
  return json.data
}
```

`RunTaskResponse` 不只返回 `message_id`，还返回本次实际执行并产生 SSE 的 `session_id`、`agent_type`、`route_id`、`route_mode`。群聊路由后，前端必须使用响应里的实际 `session_id + message_id` 订阅 SSE。

**历史消息获取** — GET `/api/tasks/:id/messages`（支持 cursor 分页 + 群聊模式）：

```typescript
export interface TaskMessagesResponse {
  data: TaskMessage[]
  has_more: boolean
}

export async function getTaskMessages(
  taskId: string,
  params?: {
    limit?: number
    before?: number
    sessionId?: string
    mode?: 'group'
    primarySessionId?: string
  },
): Promise<TaskMessagesResponse> {
  const searchParams = new URLSearchParams()
  if (params?.limit) searchParams.set('limit', String(params.limit))
  if (params?.before) searchParams.set('before', String(params.before))
  if (params?.sessionId) searchParams.set('session_id', params.sessionId)
  if (params?.mode) searchParams.set('mode', params.mode)
  if (params?.primarySessionId) searchParams.set('primary_session_id', params.primarySessionId)
  const qs = searchParams.toString()
  const url = `${API_BASE}/tasks/${taskId}/messages${qs ? `?${qs}` : ''}`
  const res = await fetch(url)
  return handleResponse<TaskMessagesResponse>(res)
}
```

### 对话聚合

`fetchConversations()` 将 Task + Session 扁平化为 `Conversation` 视图。多 Agent task 聚合成一个群聊会话并优先选 Orchestrator session 作为主会话；单 Agent task 显示为单聊。排序规则与代码一致：置顶 task 优先（`pinnedAt` 倒序），然后按 `lastActiveAt` 倒序。

`createConversation()` 接收 agents 数组（支持多 Agent），自动注入 orchestrator 创建群聊 Task -> 取首个 Session -> 返回 Conversation。

### API 接口总览

| 函数 | 方法 | 路径 | 说明 |
|------|------|------|------|
| `fetchTasks` | GET | `/api/tasks` | 获取任务列表 |
| `fetchTask` | GET | `/api/tasks/:id` | 获取任务详情（含 sessions） |
| `createTask` | POST | `/api/tasks` | 创建任务 |
| `submitMessage` | POST | `/api/tasks/:id/run` | 提交消息，返回 RunTaskResponse（含实际 SSE session/message/route） |
| `submitPlanReview` | POST | `/api/tasks/:id/review` | 提交计划审查结果（approve/discuss/modify） |
| `getTaskMessages` | GET | `/api/tasks/:id/messages` | 获取任务消息列表（支持 cursor 分页 + 群聊 mode/primarySessionId） |
| `leaveTask` | DELETE | `/api/tasks/:id/leave` | 离开任务并清理 AgentEnd session/workspace/branch |
| `mergeTaskToMain` | POST | `/api/workspace/task/:id/merge-to-main` | 合并任务分支到默认分支（路径名保留 main 兼容旧接口） |
| `updateTaskPin` | PATCH | `/api/tasks/:id` | 更新置顶时间（置顶/取消置顶） |
| `updateSession` | PUT | `/api/sessions/:id` | 更新 session（agent_name / avatar_url） |
| `fetchAgentTypes` | GET | `/api/agent-types` | 获取可用 Agent 类型列表 |
| `uploadAvatar` | POST | `/api/agents/avatar` | 上传头像 |
| `validateRepoPath` | POST | `/api/validate-repo-path` | 校验仓库路径 |
| `initGitRepo` | POST | `/api/init-git-repo` | 初始化 Git 仓库（非 Git 目录自动初始化） |
| `fetchAgentProfile` | GET | `/api/sessions/:id/profile` | 获取 Agent 悬停卡片数据（名称 + 头像 + 技能） |
| `fetchAgentDetail` | GET | `/api/sessions/:id/detail` | 获取 Agent 详情页数据（元数据 + 技能 + 统计） |
| `fetchAgentSoul` | GET | `/api/sessions/:id/soul` | 获取 Agent Soul（人格描述 Markdown） |
| `updateAgentSoul` | PUT | `/api/sessions/:id/soul` | 更新 Agent Soul |
| `fetchAnnouncements` | GET | `/api/tasks/:id/announcements` | 获取群聊公告列表 |
| `createAnnouncement` | POST | `/api/tasks/:id/announcements` | 创建群聊公告 |
| `deleteAnnouncement` | DELETE | `/api/tasks/:id/announcements/:aid` | 删除群聊公告 |
| `fetchConversations` | GET | 多接口聚合 | Task+Session 扁平化对话列表 |
| `createConversation` | POST+GET | 多接口组合 | 创建 Task -> 取 Session -> 返回 Conversation |
| `fetchContactGroups` | GET | `/api/contact-groups` | 获取联系人分组列表 |
| `createContactGroup` | POST | `/api/contact-groups` | 创建联系人分组 |
| `updateContactGroup` | PUT | `/api/contact-groups/:id` | 更新联系人分组名称 |
| `deleteContactGroup` | DELETE | `/api/contact-groups/:id` | 删除联系人分组 |
| `addToContactGroup` | POST | `/api/contact-groups/:id/items` | 添加任务到联系人分组 |
| `removeFromContactGroup` | DELETE | `/api/contact-groups/:id/items/:taskID` | 从联系人分组移除任务 |
| `fetchSkills` | GET | `/api/skills` | 获取技能库列表 |
| `uploadSkill` | POST | `/api/skills/upload` | 上传技能文件 |
| `confirmSkill` | POST | `/api/skills/confirm` | 确认技能创建 |
| `deleteSkill` | DELETE | `/api/skills/:name` | 删除技能 |
| `importSkill` | POST | `/api/skills/:name/import` | 导入技能到指定 Session worktree |
| `removeSkill` | DELETE | `/api/skills/:name/sessions/:sessionId` | 从指定 Session 移除技能 |
| `adminAuth` | POST | `/api/admin/auth` | 管理员密码验证，返回 token |
| `getAdminResources` | GET | `/api/admin/resources` | 获取系统资源（磁盘/内存/Redis 用量） |
| `deleteAdminSessions` | DELETE | `/api/admin/sessions` | 批量删除会话 |
| `getAdminWorkspaces` | GET | `/api/admin/workspaces` | 获取工作区列表 |
| `deleteAdminWorkspace` | DELETE | `/api/admin/workspaces/:id` | 删除工作区 |
| `getAdminAgents` | GET | `/api/admin/agents` | 获取 Agent 列表 |
| `getAdminServices` | GET | `/api/admin/services` | 获取服务健康状态 |
| `getAdminStatistics` | GET | `/api/admin/statistics` | 获取统计数据 |
| `getAdminAvatar` | GET | `/api/admin/avatar` | 获取管理面板头像 |
| `updateAdminAvatar` | PUT | `/api/admin/avatar` | 更新管理面板头像 |
