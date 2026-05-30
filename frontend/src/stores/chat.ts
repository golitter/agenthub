import { create } from 'zustand'

import type { AgentType } from '@/generated/request'
import { reduceEventToBlocks } from '@/lib/block-reducer'
import type { CoordMessage, MessageBlock, PlanTask } from '@/lib/block-types'

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
  status?: string
}

export type ChatStatus = 'idle' | 'loading' | 'streaming' | 'tool_running' | 'done' | 'error'

export type NavTab = 'chat' | 'contacts' | 'admin' | 'settings'

export interface ActiveStream {
  messageId: string
  sessionId: string
}

interface SessionChatState {
  messages: ChatMessage[]
  streamingContent: string
  streamingAgentType?: AgentType
  streamingAgentName?: string
  streamingMessageId?: string
  status: ChatStatus
  error: Error | null
  toolName?: string
  activeStream: ActiveStream | null
  hasMore: boolean
  isLoadingMore: boolean
  runtimeBlocks: MessageBlock[]
}

interface ChatNavState {
  currentSessionId: string | null
  setCurrentSession: (sessionId: string) => void
  clearNavigation: () => void
}

interface ChatStoreState {
  nav: ChatNavState
  sessions: Record<string, SessionChatState>
  activeTab: NavTab

  // Nav actions
  setCurrentSession: (sessionId: string) => void
  clearNavigation: () => void
  setActiveTab: (tab: NavTab) => void

  // Session actions
  getSession: (sessionId: string) => SessionChatState
  loadHistory: (sessionId: string, messages: ChatMessage[], hasMore?: boolean) => void
  sendMessage: (sessionId: string, message: ChatMessage, activeStream: ActiveStream) => void
  streamStart: (sessionId: string, agentType: AgentType) => void
  streamText: (sessionId: string, text: string) => void
  streamToolCall: (sessionId: string, toolName: string) => void
  streamToolResult: (sessionId: string) => void
  streamDone: (sessionId: string) => void
  streamError: (sessionId: string, error: Error) => void
  resetSession: (sessionId: string) => void
  streamRuntimeEvent: (
    sessionId: string,
    event: { task_id: string; agent: string; status: string },
  ) => void
  streamRuntimeText: (
    sessionId: string,
    event: { task_id: string; agent: string; text: string },
  ) => void
  streamPlanEvent: (sessionId: string, tasks: PlanTask[], overview: string) => void
  streamCoordinationEvent: (sessionId: string, msg: CoordMessage) => void
  streamCoordinationDone: (sessionId: string, summary: string) => void
  streamAskCardStart: (
    sessionId: string,
    event: {
      question_id: string
      source_agent?: string
      source_agent_type?: string
      source_session_id?: string
      target_agent: string
      target_agent_type?: string
      target_session_id: string
      question: string
    },
  ) => void
  streamAskCardDone: (
    sessionId: string,
    event: {
      question_id: string
      source_agent?: string
      source_agent_type?: string
      source_session_id?: string
      target_agent?: string
      target_agent_type?: string
      target_session_id?: string
      question?: string
      summary?: string
      status?: string
    },
  ) => void
  streamAgentUpdate: (
    sessionId: string,
    agentType: AgentType,
    agentName: string,
    messageId?: string,
  ) => void

  // Pagination actions
  prependMessages: (sessionId: string, messages: ChatMessage[], hasMore: boolean) => void
  setLoadingMore: (sessionId: string, loading: boolean) => void
}

type ChatSet = (
  partial: Partial<ChatStoreState> | ((state: ChatStoreState) => Partial<ChatStoreState>),
) => void

const initialSessionState: SessionChatState = {
  messages: [],
  streamingContent: '',
  streamingAgentType: undefined,
  streamingAgentName: undefined,
  streamingMessageId: undefined,
  status: 'idle',
  error: null,
  toolName: undefined,
  activeStream: null,
  hasMore: true,
  isLoadingMore: false,
  runtimeBlocks: [],
}

let _runtimeBlockId = 0
function nextRuntimeBlockId(): string {
  return `rtb-${++_runtimeBlockId}`
}

function askCardStatus(status?: string): 'answered' | 'failed' {
  return status === 'completed' || status === 'answered' ? 'answered' : 'failed'
}

function ensureSession(state: ChatStoreState, sessionId: string): SessionChatState {
  return state.sessions[sessionId] ?? { ...initialSessionState }
}

function buildAgentMessage(session: SessionChatState, sessionId: string): ChatMessage {
  const blocks = [
    ...session.runtimeBlocks,
    ...(session.streamingContent ? reduceEventToBlocks(session.streamingContent) : []),
  ]
  const baseTimestamp = session.messages[session.messages.length - 1]?.timestamp ?? 0
  const timestamp = Math.max(Date.now(), baseTimestamp + 1)
  return {
    id: session.streamingMessageId ?? `agent-${timestamp}`,
    role: 'agent',
    content: session.streamingContent,
    blocks,
    agentType: session.streamingAgentType,
    agentName: session.streamingAgentName,
    sessionId,
    timestamp,
    messageId: session.streamingMessageId,
  }
}

function hydrateAgentBlocks(messages: ChatMessage[]): ChatMessage[] {
  return messages.map((msg) =>
    msg.role === 'agent' && msg.content
      ? { ...msg, blocks: reduceEventToBlocks(msg.content) }
      : msg,
  )
}

function isLiveStatus(status: ChatStatus): boolean {
  return status === 'loading' || status === 'streaming' || status === 'tool_running'
}

function isOptimisticUserMessage(msg: ChatMessage): boolean {
  return msg.role === 'user' && msg.dbId === undefined && !msg.messageId
}

function findMatchingOptimisticUser(merged: ChatMessage[], msg: ChatMessage): number {
  if (msg.role !== 'user') return -1
  return merged.findIndex(
    (existing) =>
      isOptimisticUserMessage(existing) &&
      existing.content === msg.content &&
      Math.abs(existing.timestamp - msg.timestamp) < 120_000,
  )
}

function mergeHistoryMessages(current: ChatMessage[], history: ChatMessage[]): ChatMessage[] {
  const merged = [...current]
  const seen = new Set<string>()

  for (const msg of merged) {
    if (msg.dbId !== undefined) {
      seen.add(`db:${msg.dbId}`)
    }
    if (msg.messageId) {
      seen.add(`mid:${msg.messageId}`)
    }
  }

  for (const msg of history) {
    const dbKey = msg.dbId !== undefined ? `db:${msg.dbId}` : undefined
    const messageKey = msg.messageId ? `mid:${msg.messageId}` : undefined
    if ((dbKey && seen.has(dbKey)) || (messageKey && seen.has(messageKey))) {
      continue
    }
    const optimisticIdx = findMatchingOptimisticUser(merged, msg)
    if (optimisticIdx >= 0) {
      merged[optimisticIdx] = msg
      if (dbKey) seen.add(dbKey)
      if (messageKey) seen.add(messageKey)
      continue
    }
    merged.push(msg)
    if (dbKey) seen.add(dbKey)
    if (messageKey) seen.add(messageKey)
  }

  return merged.sort((a, b) => {
    if (a.dbId !== undefined && b.dbId !== undefined) return a.dbId - b.dbId
    return a.timestamp - b.timestamp
  })
}

// --- rAF-based text batching ---
// Instead of calling Zustand.set() on every single SSE token (which
// triggers a full React re-render + useMemo + scroll), tokens are
// buffered and flushed once per animation frame via rAF.
let _textBufs: Map<string, string[]> | null = null
let _flushRafId: number | null = null

function _ensureBuf(sessionId: string): string[] {
  if (!_textBufs) _textBufs = new Map()
  let buf = _textBufs.get(sessionId)
  if (!buf) {
    buf = []
    _textBufs.set(sessionId, buf)
  }
  return buf
}

function _scheduleFlush(set: ChatSet) {
  if (_flushRafId !== null) return
  _flushRafId = requestAnimationFrame(() => {
    _flushRafId = null
    if (!_textBufs || _textBufs.size === 0) return
    const snapshot = new Map(_textBufs)
    _textBufs.clear()
    set((s) => {
      const nextSessions = { ...s.sessions }
      for (const [sid, pieces] of snapshot) {
        if (pieces.length === 0) continue
        const session = ensureSession(s, sid)
        nextSessions[sid] = {
          ...session,
          status: session.status === 'tool_running' ? 'streaming' : session.status,
          streamingContent: session.streamingContent + pieces.join(''),
        }
      }
      return { sessions: nextSessions }
    })
  })
}

function _flushTextBuf(set: ChatSet) {
  if (_flushRafId !== null) {
    cancelAnimationFrame(_flushRafId)
    _flushRafId = null
  }
  if (!_textBufs || _textBufs.size === 0) return
  const snapshot = new Map(_textBufs)
  _textBufs.clear()
  set((s) => {
    const nextSessions = { ...s.sessions }
    for (const [sid, pieces] of snapshot) {
      if (pieces.length === 0) continue
      const session = ensureSession(s, sid)
      nextSessions[sid] = {
        ...session,
        status: session.status === 'tool_running' ? 'streaming' : session.status,
        streamingContent: session.streamingContent + pieces.join(''),
      }
    }
    return { sessions: nextSessions }
  })
}

export const useChatStore = create<ChatStoreState>((set, get) => ({
  nav: { currentSessionId: null, setCurrentSession: () => {}, clearNavigation: () => {} },
  sessions: {},
  activeTab: 'chat',

  setCurrentSession: (sessionId) =>
    set((s) => ({ nav: { ...s.nav, currentSessionId: sessionId } })),
  clearNavigation: () => set((s) => ({ nav: { ...s.nav, currentSessionId: null } })),
  setActiveTab: (tab) => set({ activeTab: tab }),

  getSession: (sessionId) => get().sessions[sessionId] ?? { ...initialSessionState },

  loadHistory: (sessionId, messages, hasMore) =>
    set((s) => {
      const session = ensureSession(s, sessionId)
      const historyMessages = hydrateAgentBlocks(messages)
      const nextMessages =
        session.messages.length > 0
          ? mergeHistoryMessages(session.messages, historyMessages)
          : historyMessages
      const nextStatus =
        isLiveStatus(session.status) || session.status === 'error' ? session.status : 'done'
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            status: nextStatus,
            messages: nextMessages,
            hasMore: hasMore ?? false,
          },
        },
      }
    }),

  sendMessage: (sessionId, message, activeStream) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'loading',
          messages: [...(s.sessions[sessionId]?.messages ?? []), message],
          streamingContent: '',
          runtimeBlocks: [],
          error: null,
          activeStream,
        },
      },
    })),

  streamStart: (sessionId, agentType) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'streaming',
          streamingAgentType: agentType,
          streamingAgentName: undefined,
          streamingMessageId: undefined,
        },
      },
    })),

  streamText: (sessionId, text) => {
    _ensureBuf(sessionId).push(text)
    _scheduleFlush(set)
  },

  streamAgentUpdate: (sessionId, agentType, agentName, messageId) => {
    _flushTextBuf(set)
    set((s) => {
      const session = ensureSession(s, sessionId)
      const agentChanged =
        (session.streamingAgentType && session.streamingAgentType !== agentType) ||
        (session.streamingAgentName && session.streamingAgentName !== agentName)
      const messageChanged =
        !!session.streamingMessageId && !!messageId && session.streamingMessageId !== messageId

      if (
        (agentChanged || messageChanged) &&
        (session.streamingContent.trim() || session.runtimeBlocks.length > 0)
      ) {
        const prevMessage = buildAgentMessage(session, sessionId)
        return {
          sessions: {
            ...s.sessions,
            [sessionId]: {
              ...session,
              messages: [...session.messages, prevMessage],
              streamingContent: '',
              streamingAgentType: agentType,
              streamingAgentName: agentName,
              streamingMessageId: messageId,
              runtimeBlocks: [],
            },
          },
        }
      }

      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            streamingAgentType: agentType,
            streamingAgentName: agentName,
            streamingMessageId: messageId ?? session.streamingMessageId,
          },
        },
      }
    })
  },

  streamToolCall: (sessionId, toolName) => {
    _flushTextBuf(set)
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'tool_running',
          toolName,
        },
      },
    }))
  },

  streamToolResult: (sessionId) => {
    _flushTextBuf(set)
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'streaming',
          toolName: undefined,
        },
      },
    }))
  },

  streamDone: (sessionId) => {
    _flushTextBuf(set)
    set((s) => {
      const session = ensureSession(s, sessionId)
      const newMessages = [...session.messages]
      if (session.streamingContent.trim() || session.runtimeBlocks.length > 0) {
        newMessages.push(buildAgentMessage(session, sessionId))
      }
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            status: 'done',
            messages: newMessages,
            streamingContent: '',
            streamingAgentType: undefined,
            streamingAgentName: undefined,
            streamingMessageId: undefined,
            activeStream: null,
            runtimeBlocks: [],
          },
        },
      }
    })
  },

  streamError: (sessionId, error) => {
    _flushTextBuf(set)
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'error',
          error,
          streamingContent: '',
          streamingMessageId: undefined,
          runtimeBlocks: [],
          activeStream: null,
        },
      },
    }))
  },

  streamRuntimeEvent: (sessionId, event) =>
    set((s) => {
      const session = ensureSession(s, sessionId)
      const blocks = [...session.runtimeBlocks]
      const idx = blocks.findIndex(
        (b) => b.type === 'runtime_status' && b.task_id === event.task_id,
      )
      const newBlock: MessageBlock = { type: 'runtime_status', id: nextRuntimeBlockId(), ...event }
      if (idx >= 0) {
        const existing = blocks[idx]
        blocks[idx] =
          existing.type === 'runtime_status'
            ? {
                ...existing,
                ...event,
                id: existing.id,
                streamingText: existing.streamingText,
              }
            : newBlock
      } else {
        blocks.push(newBlock)
      }
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...session, runtimeBlocks: blocks },
        },
      }
    }),

  streamRuntimeText: (sessionId, event) =>
    set((s) => {
      const session = ensureSession(s, sessionId)
      const blocks = session.runtimeBlocks.map((b) => {
        if (b.type === 'runtime_status' && b.task_id === event.task_id) {
          return { ...b, streamingText: (b.streamingText ?? '') + event.text }
        }
        return b
      })
      // If no runtime_status block exists yet, create one
      if (!blocks.some((b) => b.type === 'runtime_status' && b.task_id === event.task_id)) {
        blocks.push({
          type: 'runtime_status',
          id: nextRuntimeBlockId(),
          task_id: event.task_id,
          agent: event.agent,
          status: 'running',
          streamingText: event.text,
        })
      }
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...session, runtimeBlocks: blocks },
        },
      }
    }),

  streamPlanEvent: (sessionId, tasks, overview) =>
    set((s) => {
      const session = ensureSession(s, sessionId)
      const blocks = [...session.runtimeBlocks]
      const existing = blocks.find((b) => b.type === 'plan')
      if (existing && existing.type === 'plan') {
        const taskMap = new Map(existing.tasks.map((task) => [task.task_id, task]))
        for (const task of tasks) {
          taskMap.set(task.task_id, task)
        }
        existing.tasks = [...taskMap.values()]
      } else {
        blocks.push({ type: 'plan', id: nextRuntimeBlockId(), overview, tasks })
      }
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...session, runtimeBlocks: blocks },
        },
      }
    }),

  streamCoordinationEvent: (sessionId, msg) =>
    set((s) => {
      const session = ensureSession(s, sessionId)
      const blocks = [...session.runtimeBlocks]
      let coord = blocks.find((b) => b.type === 'coordination')
      if (coord && coord.type === 'coordination') {
        coord = { ...coord, messages: [...coord.messages, msg] }
        const ci = blocks.findIndex((b) => b.type === 'coordination')
        blocks[ci] = coord
      } else {
        blocks.push({
          type: 'coordination',
          id: nextRuntimeBlockId(),
          messages: [msg],
          closed: false,
        })
      }
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...session, runtimeBlocks: blocks },
        },
      }
    }),

  streamCoordinationDone: (sessionId, summary) =>
    set((s) => {
      const session = ensureSession(s, sessionId)
      const blocks = session.runtimeBlocks.map((b) =>
        b.type === 'coordination' ? { ...b, closed: true, summary } : b,
      )
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...session, runtimeBlocks: blocks },
        },
      }
    }),

  streamAskCardStart: (sessionId, event) =>
    set((s) => {
      const session = ensureSession(s, sessionId)
      const blocks = [...session.runtimeBlocks]
      const existingIdx = blocks.findIndex(
        (b) => b.type === 'ask_agent' && b.question_id === event.question_id,
      )
      const block: MessageBlock = {
        type: 'ask_agent',
        id: nextRuntimeBlockId(),
        question_id: event.question_id,
        source_agent: event.source_agent,
        source_agent_type: event.source_agent_type,
        source_session_id: event.source_session_id,
        target_agent: event.target_agent,
        target_agent_type: event.target_agent_type,
        target_session_id: event.target_session_id,
        question: event.question,
        status: 'pending',
        collapsed: false,
      }
      if (existingIdx >= 0) {
        blocks[existingIdx] = { ...block, id: blocks[existingIdx].id }
      } else {
        blocks.push(block)
      }
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...session, status: 'streaming', runtimeBlocks: blocks },
        },
      }
    }),

  streamAskCardDone: (sessionId, event) =>
    set((s) => {
      const session = ensureSession(s, sessionId)
      const status = askCardStatus(event.status)
      const blocks = session.runtimeBlocks.map((block) => {
        if (block.type !== 'ask_agent' || block.question_id !== event.question_id) return block
        return {
          ...block,
          source_agent: event.source_agent ?? block.source_agent,
          source_agent_type: event.source_agent_type ?? block.source_agent_type,
          source_session_id: event.source_session_id ?? block.source_session_id,
          target_agent: event.target_agent ?? block.target_agent,
          target_agent_type: event.target_agent_type ?? block.target_agent_type,
          target_session_id: event.target_session_id ?? block.target_session_id,
          question: event.question ?? block.question,
          status,
          collapsed: status === 'answered',
          summary: event.summary || block.summary,
        }
      })
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...session, runtimeBlocks: blocks },
        },
      }
    }),

  resetSession: (sessionId) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: { ...initialSessionState },
      },
    })),

  prependMessages: (sessionId, messages, hasMore) =>
    set((s) => {
      const session = ensureSession(s, sessionId)
      const mapped = messages.map((msg) =>
        msg.role === 'agent' && msg.content
          ? { ...msg, blocks: reduceEventToBlocks(msg.content) }
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

  setLoadingMore: (sessionId, loading) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          isLoadingMore: loading,
        },
      },
    })),
}))

export function useChatNav() {
  const currentSessionId = useChatStore((s) => s.nav.currentSessionId)
  const setCurrentSession = useChatStore((s) => s.setCurrentSession)
  const clearNavigation = useChatStore((s) => s.clearNavigation)
  return { currentSessionId, setCurrentSession, clearNavigation }
}

export function useActiveTab() {
  const activeTab = useChatStore((s) => s.activeTab)
  const setActiveTab = useChatStore((s) => s.setActiveTab)
  return { activeTab, setActiveTab }
}
