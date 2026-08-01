# State — 状态管理

## 实现了什么

三层状态架构：Zustand 管理聊天导航（当前会话 ID）与各会话独立流式状态，TanStack React Query 管理服务端数据缓存，`useChatStream` hook 编排 SSE 连接与 store actions 的协作。页面级视图切换（chat / contacts / skills / admin）由 React Router 负责，不进入 Zustand。Agent 消息通过 `reduceEventToBlocks` 解析为 `MessageBlock[]` 结构化块（text / html-render / image / attachment / diff / preview / plan / plan_review / runtime_status / coordination / ask_agent / task_failure / final_summary / tool_call / tool_result）。

## 怎么实现的

### Zustand Store 架构

Chat 状态拆分为三个独立 Zustand Store，通过 `src/stores/chat.ts` barrel re-export 组合为向后兼容的 `useChatStore`：

| Store | 文件 | 职责 |
|-------|------|------|
| NavigationStore | `src/stores/navigation-store.ts` | 导航状态：仅 `currentSessionId`（页面级 Tab 由 React Router 负责） |
| SessionStore | `src/stores/session-store.ts` | 各会话独立数据 Map：messages/streaming/runtimeBlocks |
| MessageStore | `src/stores/message-store.ts` | 消息流式更新、runtime blocks、rAF 文本批处理、公告管理 |
| Chat (barrel) | `src/stores/chat.ts` | 组合上述三 Store，暴露 `useChatStore` 向后兼容 |

`chat.ts` 通过订阅三个 domain store 的变化，实时同步到一个组合的 Zustand store，使现有 `useChatStore(selector)` 调用无需修改。

### NavigationStore (`src/stores/navigation-store.ts`)

仅管理当前选中的会话 ID（页面级 Tab 切换由 React Router 的 `NavLink` + `<Routes>` 负责，不在此 store 中）。暴露 `useChatNav()` selector hook：

```typescript
interface NavigationState {
  currentSessionId: string | null
  setCurrentSession: (sessionId: string) => void
  clearNavigation: () => void
}
```

### SessionStore (`src/stores/session-store.ts`)

管理各会话独立的数据 Map，以 `sessions[sessionId]` 隔离。定义 `ChatMessage`、`ChatStatus`、`SessionChatState` 等核心类型：

```typescript
export interface ChatMessage {
  id: string
  dbId?: number
  role: 'user' | 'agent' | 'system'
  content: string
  blocks?: MessageBlock[]
  agentType?: AgentType
  agentName?: string
  sessionId?: string
  avatarUrl?: string
  timestamp: number
  messageId?: string
  groupId?: string
  status?: string
}

export type ChatStatus = 'idle' | 'loading' | 'streaming' | 'tool_running' | 'done' | 'error'

export interface ActiveStream {
  messageId: string
  sessionId: string
}

export interface SessionChatState {
  messages: ChatMessage[]
  streamingContent: string
  streamingReplay?: { messageId: string; offset: number }
  streamingAgentType?: AgentType
  streamingAgentName?: string
  streamingMessageId?: string
  streamingGroupId?: string
  status: ChatStatus
  error: Error | null
  toolName?: string
  activeStream: ActiveStream | null
  hasMore: boolean
  isLoadingMore: boolean
  runtimeBlocks: MessageBlock[]
  activePlanReviewKey?: string
}
```

SessionStore 暴露 `getSession`（含 `ensureSession` 兜底）和 `resetSession`。

### MessageStore (`src/stores/message-store.ts`)

管理消息流式更新、runtime blocks 和公告管理。操作 SessionStore 中的 sessions map，包含所有 stream actions 和 pagination actions：

`loadHistory` 对历史 agent 消息同样执行 `reduceEventToBlocks` + `coalesceMessageBlocks` 解析（合并相邻同类型块）：

```typescript
loadHistory: (sessionId, messages, hasMore) =>
  set((s) => ({
    sessions: {
      ...s.sessions,
      [sessionId]: {
        ...ensureSession(s, sessionId),
        status: 'done',
        messages: messages.map((msg) =>
          msg.role === 'agent' && msg.content
            ? { ...msg, blocks: coalesceMessageBlocks(reduceEventToBlocks(msg.content)) }
            : msg,
        ),
        hasMore: hasMore ?? false,
      },
    },
  })),
```

`prependMessages` 用于向上翻页加载更多历史消息，将新消息插入到列表头部：

```typescript
prependMessages: (sessionId, messages, hasMore) =>
  set((s) => {
    const session = ensureSession(s, sessionId)
    const mapped = messages.map((msg) =>
      msg.role === 'agent' && msg.content
        ? { ...msg, blocks: coalesceMessageBlocks(reduceEventToBlocks(msg.content)) }
        : msg,
    )
    return {
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...session,
          messages: [...mapped, ...session.messages],
          hasMore,
          isLoadingMore: false,
        },
      },
    }
  }),
```

`ensureSession` 工具函数（定义在 `session-store.ts`）确保访问不存在的 sessionId 时返回初始状态，而非 undefined：

```typescript
export function ensureSession(
  state: { sessions: Record<string, SessionChatState> },
  sessionId: string,
): SessionChatState {
  return state.sessions[sessionId] ?? { ...initialSessionState }
}
```

### 聊天状态机流转

Store actions 驱动聊天流的全生命周期：

```
         sendMessage()
  idle ──────────────► loading
                          │
                    SSE init event → streamStart()
                          │
                          ▼
                      streaming ◄──── streamText()
                       │  ▲
          streamToolCall()  streamToolResult()
                       │  ▲
                       ▼  │
                    tool_running
                       │
            ┌──────────┼──────────┐
    streamDone()  streamError()  abort()
            │          │          │
            ▼          ▼          ▼
          done       error      (idle)
```

### Store Action 实现

`sendMessage` 追加用户消息，设置状态为 `loading`，记录 `activeStream`：

```typescript
sendMessage: (sessionId, message, activeStream) =>
  set((s) => ({
    sessions: {
      ...s.sessions,
      [sessionId]: {
        ...ensureSession(s, sessionId),
        status: 'loading',
        messages: [...(s.sessions[sessionId]?.messages ?? []), message],
        streamingContent: '',
        error: null,
        activeStream,
      },
    },
  })),
```

`streamText` 追加流式文本，若当前处于 `tool_running` 状态则回退到 `streaming`：

```typescript
streamText: (sessionId, text) =>
  set((s) => {
    const session = ensureSession(s, sessionId)
    return {
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...session,
          status: session.status === 'tool_running' ? 'streaming' : session.status,
          streamingContent: session.streamingContent + text,
        },
      },
    }
  }),
```

`streamDone` 通过 `buildAgentMessage` 将 `streamingContent`（连同 `runtimeBlocks`）解析为 `MessageBlock[]`，合并相邻同类型块（`coalesceMessageBlocks`），并清除 `html-render` 块的 `streaming` 标记（确保终态消息始终渲染完整卡片），最后转为 agent 消息追加到 `messages`：

```typescript
function buildAgentMessage(session: SessionChatState, sessionId: string, options): ChatMessage {
  const blocks = [
    ...(options.keepRuntimeStreamingText ? session.runtimeBlocks : stripRuntimeStreamingText(session.runtimeBlocks)),
    ...(session.streamingContent ? reduceEventToBlocks(session.streamingContent) : []),
  ]
  const timestamp = Math.max(Date.now(), (session.messages.at(-1)?.timestamp ?? 0) + 1)
  return {
    id: session.streamingMessageId ?? `agent-${timestamp}`,
    role: 'agent',
    content: session.streamingContent,
    blocks: coalesceMessageBlocks(
      // 终态化：清除 html-render 的 streaming 标记
      blocks.map((b) => (b.type === 'html-render' && b.streaming ? { ...b, streaming: undefined } : b)),
    ),
    agentType: session.streamingAgentType,
    agentName: session.streamingAgentName,
    sessionId,
    timestamp,
    messageId: session.streamingMessageId,
    groupId: session.streamingGroupId,
  }
}

// streamDone 调用 buildAgentMessage({ keepRuntimeStreamingText: false }) 追加到 messages，
// 并清空 streamingContent / streamingAgentType / activeStream 等流式状态。
```

### useChatNav 选择器 (`src/stores/navigation-store.ts`)

暴露导航状态的选择器 hook，组件通过它订阅 `currentSessionId`，避免订阅整个 store 导致不必要的 re-render：

```typescript
export function useChatNav() {
  const currentSessionId = useNavigationStore((state) => state.currentSessionId)
  const setCurrentSession = useNavigationStore((state) => state.setCurrentSession)
  const clearNavigation = useNavigationStore((state) => state.clearNavigation)
  return { currentSessionId, setCurrentSession, clearNavigation }
}
```

`useChatNav` 从 `useNavigationStore` 直接订阅；调用方通常从 barrel `@/stores/chat` 导入。不存在 `useActiveTab` —— Tab 切换由 React Router 的 URL 驱动。

### React Query Hooks (`src/hooks/use-conversations.ts`)

对话列表使用 `useQuery` 查询，新建对话使用 `useMutation`，成功后自动 invalidate 缓存：

```typescript
export function useConversations() {
  return useQuery({
    queryKey: ['conversations'],
    queryFn: fetchConversations,
  })
}

export function useCreateConversation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      agents: { type: AgentType; name: string }[]
      repoPath?: string
      title?: string
    }) => createConversation(params.agents, params.repoPath, params.title),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['conversations'] })
    },
  })
}
```

### useChatStream Hook (`src/hooks/use-chat-stream.ts`)

核心编排 hook，连接 Zustand store 与 SSE 客户端。挂载时加载最近 20 条历史消息（cursor 分页），若发现 `status === 'streaming'` 的 agent 消息则自动重连 SSE。返回 `{ state, sendMessage, abort }`：

```typescript
export function useChatStream(taskId: string, sessionId: string, agentType: AgentType = 'claude-code') {
  const store = useChatStore()
  const abortRef = useRef<AbortController | null>(null)

  const connectToStream = useCallback((messageId: string) => {
    abortRef.current?.abort()
    store.streamStart(sessionId, agentType)

    const controller = connectSSE({
      url: `/api/tasks/${taskId}/stream`,
      params: { session_id: sessionId, message_id: messageId },
      reconnect: true,
      onEvent: (event: StreamEvent) => {
        switch (event.type) {
          case EventTypeValues.Init:
            break
          case EventTypeValues.Text: {
            const textAgent = event.content?.agent as string | undefined
            const textAgentType = event.content?.agent_type as AgentType | undefined
            if (textAgent && textAgentType) {
              store.streamAgentUpdate(sessionId, textAgentType, textAgent)
            }
            store.streamText(sessionId, (event.content?.text as string) ?? '')
            break
          }
          case EventTypeValues.ToolCall:
            store.streamToolCall(sessionId, (event.content?.name as string) ?? 'unknown')
            break
          case EventTypeValues.ToolResult:
            store.streamToolResult(sessionId)
            break
          case EventTypeValues.Done:
            store.streamDone(sessionId)
            abortRef.current?.abort()
            abortRef.current = null
            break
          case EventTypeValues.Error:
            store.streamError(sessionId, new Error(...))
            abortRef.current?.abort()
            abortRef.current = null
            break
          case EventTypeValues.RuntimeExecuting:
            store.streamRuntimeEvent(sessionId, {
              task_id: (event.content?.task_id as string) ?? '',
              agent: (event.content?.agent as string) ?? '',
              status: 'running',
            })
            break
          case EventTypeValues.RuntimeCompleted: {
            const success = event.content?.success ?? false
            store.streamRuntimeEvent(sessionId, {
              task_id: (event.content?.task_id as string) ?? '',
              agent: (event.content?.agent as string) ?? '',
              status: success ? 'completed' : 'failed',
            })
            break
          }
          case EventTypeValues.RuntimeText:
            store.streamRuntimeText(sessionId, {
              task_id: (event.content?.task_id as string) ?? '',
              agent: (event.content?.agent as string) ?? '',
              text: (event.content?.text as string) ?? '',
            })
            break
          case EventTypeValues.Planning:
            // dispatch plan tasks
            break
          case EventTypeValues.CoordinationMessage:
            store.streamCoordinationEvent(sessionId, {
              from: (event.content?.from as string) ?? '',
              to: (event.content?.to as string) ?? '',
              text: (event.content?.text as string) ?? '',
              round: (event.content?.round as number) ?? 1,
            })
            break
          case EventTypeValues.CoordinationDone:
            store.streamCoordinationDone(sessionId, ...)
            break
        }
      },
      onError: (error) => {
        const s = store.getSession(sessionId)
        if (s.status === 'done' || s.status === 'idle' || s.status === 'error') return
        store.streamError(sessionId, error)
      },
    })
    abortRef.current = controller
  }, [taskId, sessionId])
```

发送消息时先追加 user 消息到 store，再调用 `submitMessage` API 获取 `RunTaskResponse`，然后用响应中的实际 `message_id + session_id + agent_type` 连接 SSE 流：

```typescript
const sendMessage = useCallback(async (message: string, agentType: AgentType = 'claude-code') => {
  const userMessage: ChatMessage = {
    id: `user-${Date.now()}`, role: 'user', content: message, timestamp: Date.now(),
  }
  store.sendMessage(sessionId, userMessage, { messageId: '', sessionId })

  const result = await submitMessage(taskId, {
    message, session_id: sessionId, agent_type: agentType,
  })
  connectToStream(
    result.message_id,
    result.session_id ?? sessionId,
    result.agent_type as AgentType,
  )
}, [taskId, sessionId, connectToStream])
```

历史消息加载（含 cursor 分页）和自动重连逻辑：

```typescript
useEffect(() => {
  let cancelled = false

  getTaskMessages(taskId, { limit: 20, sessionId })
    .then((res) => {
      if (cancelled || res.data.length === 0) return
      const chatMessages: ChatMessage[] = res.data.map((m) => ({
            id: `${m.role}-${m.id}`,
            dbId: m.id,
            role: m.role,
            content: m.content,
            agentType: m.agent_type as AgentType | undefined,
            agentName: m.agent_name || undefined,
            sessionId: m.session_id || undefined,
            timestamp: new Date(m.created_at).getTime(),
            messageId: m.message_id,
            status: m.status,
          }))
      store.loadHistory(sessionId, chatMessages, res.has_more)

      // 自动重连：如果发现 streaming 中的消息
      const streaming = res.data.find((m) => m.role === 'agent' && m.status === 'streaming')
      if (streaming && streaming.message_id) {
        connectToStream(streaming.message_id)
      }
    })
    .catch(() => { /* silently ignore */ })

  return () => { cancelled = true }
}, [taskId, sessionId, connectToStream])
```

返回 `{ state, sendMessage, abort }`：

```typescript
return {
  state: useChatStore((s) => s.getSession(sessionId)),
  sendMessage,
  abort: () => abortRef.current?.abort(),
}
```
