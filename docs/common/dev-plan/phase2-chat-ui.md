# Phase 2: 最小聊天界面 — 发消息 + 流式回复

> 目标: 浏览器打开页面，输入消息，看到 Agent 流式回复文字。
> 预估: 3 天
> 前置: Phase 1 完成 (Go SSE proxy 可用)

## 交付标准

1. 浏览器打开 `http://localhost:5173`
2. 看到聊天页面 — 左侧空会话列表 + 右侧聊天区
3. 输入框输入消息，点击发送
4. 右侧实时出现 Agent 流式回复文字
5. 消息结束后停止 streaming 状态

## 页面结构

```
┌──────────────────────────────────────────────────┐
│  AgentHub                            [theme btn] │
├───────────┬──────────────────────────────────────┤
│           │                                      │
│ Sessions  │  Chat Area                           │
│           │                                      │
│ [+ New]   │  ┌──────────────────────────────┐   │
│           │  │ Welcome! 开始一段对话吧        │   │
│ (empty)   │  └──────────────────────────────┘   │
│           │                                      │
│           │                                      │
│           │                                      │
│           │──────────────────────────────────────│
│           │ ┌─────────────────────┐  [Send]      │
│           │ │ 输入消息...          │              │
│           │ └─────────────────────┘              │
└───────────┴──────────────────────────────────────┘
```

发消息后:

```
┌──────────────────────────────────────────────────┐
│  AgentHub                                        │
├───────────┬──────────────────────────────────────┤
│           │                                      │
│ Sessions  │  👤 你                                │
│           │  帮我写一个 hello world                │
│ > New     │                                      │
│   Chat    │  🤖 Claude Code                      │
│           │  好的，这是一个 hello world 程序▌      │
│           │  ```python                            │
│           │  print("Hello, World!")               │
│           │  ```                                  │
│           │                                      │
│           │──────────────────────────────────────│
│           │ ┌─────────────────────┐  [Send]      │
│           │ │ 输入消息...          │              │
│           │ └─────────────────────┘              │
└───────────┴──────────────────────────────────────┘
```

## 要写的文件

### 1. API 层

**文件**: `frontend/src/api/client.ts`

```
职责: 封装 fetch 调用
方法:
  - createSession(title: string): Promise<Session>
  - listSessions(): Promise<Session[]>
  - getSession(id: string): Promise<Session>
  - deleteSession(id: string): Promise<void>
  - listAgents(): Promise<string[]>
```

**文件**: `frontend/src/api/types.ts`

```
类型:
  - Session { id, session_id, title, status, created_at, updated_at }
  - Task { id, task_id, session_id, agent_type, status, message, result }
  - StreamEvent { type: string, content: string, timestamp: number }
  - Agent { type: string }
```

### 2. SSE 连接层

**文件**: `frontend/src/api/sse.ts`

```
职责: 封装 SSE 连接，处理流式数据

核心函数:
  streamTask(params: {
    sessionId: string
    message: string
    agentType: string
    onEvent: (event: StreamEvent) => void
    onError: (error: Error) => void
    onDone: () => void
  }): AbortController

实现方式:
  用 fetch + ReadableStream (不用 EventSource)
  因为需要 POST body

  const resp = await fetch('/api/sessions/${sid}/tasks/run', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message, agent_type })
  })
  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  // 逐块读, 按 \n\n 分割 SSE event
  // 解析 data: {...} 行
  // 调用 onEvent 回调
```

### 3. Store 层

**文件**: `frontend/src/stores/chat.ts`

```
职责: 管理当前聊天状态

State:
  - currentSessionId: string | null
  - messages: Message[]
  - isStreaming: boolean
  - streamingContent: string  // 当前正在流式输出的内容

Actions:
  - sendMessage(message: string, agentType: string)
      1. 添加用户消息到 messages[]
      2. 调用 api/sse.streamTask()
      3. onEvent 时更新 streamingContent
      4. onDone 时把 streamingContent 固化为一条 assistant 消息

  - setCurrentSession(sessionId: string)
  - clearMessages()

类型:
  Message {
    id: string
    role: 'user' | 'assistant'
    content: string
    agentType?: string
    timestamp: number
  }
```

**文件**: `frontend/src/stores/session.ts`

```
职责: 管理会话列表

State:
  - sessions: Session[]
  - loading: boolean

Actions:
  - fetchSessions()
  - createSession(title: string)
  - deleteSession(id: string)
```

### 4. 页面和组件

**文件**: `frontend/src/App.tsx` (重写)

```
路由:
  / → ChatPage (主聊天页面，默认)

简化: Phase 2 只有一个页面，不需要 React Router。
直接在 App.tsx 渲染 ChatPage。
```

**文件**: `frontend/src/pages/ChatPage.tsx`

```
布局:
  <div className="flex h-screen">
    <Sidebar />
    <ChatArea />
  </div>
```

**文件**: `frontend/src/components/chat/Sidebar.tsx`

```
左侧面板:
  - Logo / 标题
  - [+ New Chat] 按钮
  - Session 列表 (点击切换)
  - 每个 session 显示 title + 时间

Phase 2 简化:
  - 没有搜索、没有置顶、没有归档
  - 点击 session 切换当前会话
```

**文件**: `frontend/src/components/chat/ChatArea.tsx`

```
右侧聊天区:
  - 顶部: 当前 session 标题
  - 中部: MessageList
  - 底部: MessageInput

空状态: "开始一段新对话吧" 提示
```

**文件**: `frontend/src/components/chat/MessageList.tsx`

```
消息列表:
  - 渲染 messages[] 为 MessageBubble 列表
  - 底部追加 streaming 状态的 "typing" 气泡
  - 自动滚动到底部
```

**文件**: `frontend/src/components/chat/MessageBubble.tsx`

```
单条消息:
  - role === 'user' → 右侧蓝色气泡
  - role === 'assistant' → 左侧灰色气泡，显示 agent 名称
  - content 先用纯文本渲染 (后续升级 Markdown)
  - streaming 状态显示闪烁光标 ▌
```

**文件**: `frontend/src/components/chat/MessageInput.tsx`

```
输入区域:
  - textarea 自适应高度
  - Enter 发送, Shift+Enter 换行
  - 发送按钮
  - streaming 中 disabled 输入
```

### 5. Vite 代理配置

**文件**: `frontend/vite.config.ts` (修改)

```
新增 proxy:
  '/api': {
    target: 'http://localhost:8080',
    changeOrigin: true
  }

这样前端请求 /api/* 自动转发到 Go 后端。
```

## 文件清单

```
frontend/
├── src/
│   ├── App.tsx                          # 重写: 渲染 ChatPage
│   ├── api/
│   │   ├── client.ts                    # 新增 ~60 行
│   │   ├── types.ts                     # 新增 ~40 行
│   │   └── sse.ts                       # 新增 ~80 行
│   ├── stores/
│   │   ├── chat.ts                      # 新增 ~100 行
│   │   └── session.ts                   # 新增 ~50 行
│   ├── pages/
│   │   └── ChatPage.tsx                 # 新增 ~30 行
│   └── components/chat/
│       ├── Sidebar.tsx                  # 新增 ~80 行
│       ├── ChatArea.tsx                 # 新增 ~50 行
│       ├── MessageList.tsx              # 新增 ~40 行
│       ├── MessageBubble.tsx            # 新增 ~60 行
│       └── MessageInput.tsx             # 新增 ~60 行
└── vite.config.ts                       # 修改: 加 proxy
```

**新增代码量: ~650 行，修改 ~30 行**

## 技术决策

| 决策 | 选择 | 理由 |
|------|------|------|
| SSE 实现 | fetch + ReadableStream | 需要 POST body，EventSource 只支持 GET |
| 状态管理 | Zustand | 已装好，轻量 |
| 数据请求 | 直接 fetch | Session CRUD 很简单，不需要 React Query 的复杂缓存 |
| Markdown 渲染 | Phase 2 先不做 | 纯文本先跑通，Phase 3 加 |
| 路由 | Phase 2 不用 | 单页面，Phase 3 再加 |
| 样式 | Tailwind | 已配好，直接写 |

## 注意事项

- SSE 解析注意处理跨 chunk 的 data 行（`\n\n` 分割可能在两个 chunk 之间）
- streaming 结束判断: AgentEnd 发 `done` 类型的 event
- 先不做 Markdown 渲染，`content` 原样显示
- 自动滚动用 `scrollIntoView({ behavior: 'smooth' })`
- session 列表为空时自动创建一个 "New Chat" session
