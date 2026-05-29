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
  streamAgentUpdate: (sessionId: string, agentType: AgentType, agentName: string) => void

  // Pagination actions
  prependMessages: (sessionId: string, messages: ChatMessage[], hasMore: boolean) => void
  setLoadingMore: (sessionId: string, loading: boolean) => void
}

const initialSessionState: SessionChatState = {
  messages: [],
  streamingContent: '',
  streamingAgentType: undefined,
  streamingAgentName: undefined,
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

function ensureSession(state: ChatStoreState, sessionId: string): SessionChatState {
  return state.sessions[sessionId] ?? { ...initialSessionState }
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

function _scheduleFlush(set: Parameters<typeof useChatStore.setState>[0]) {
  if (_flushRafId !== null) return
  _flushRafId = requestAnimationFrame(() => {
    _flushRafId = null
    if (!_textBufs || _textBufs.size === 0) return
    const snapshot = new Map(_textBufs)
    _textBufs.clear()
    ;(set as (updater: (s: ChatStoreState) => Partial<ChatStoreState>) => void)(
      (s: ChatStoreState) => {
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
      },
    )
  })
}

function _flushTextBuf(set: Parameters<typeof useChatStore.setState>[0]) {
  if (_flushRafId !== null) {
    cancelAnimationFrame(_flushRafId)
    _flushRafId = null
  }
  if (!_textBufs || _textBufs.size === 0) return
  const snapshot = new Map(_textBufs)
  _textBufs.clear()
  ;(set as (updater: (s: ChatStoreState) => Partial<ChatStoreState>) => void)(
    (s: ChatStoreState) => {
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
    },
  )
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
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'done',
          messages: messages.map((msg) =>
            msg.role === 'agent' && msg.content
              ? { ...msg, blocks: reduceEventToBlocks(msg.content) }
              : msg,
          ),
          hasMore: hasMore ?? false,
        },
      },
    })),

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

  streamStart: (sessionId, agentType) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'streaming',
          streamingAgentType: agentType,
          streamingAgentName: undefined,
        },
      },
    })),

  streamText: (sessionId, text) => {
    _ensureBuf(sessionId).push(text)
    _scheduleFlush(set)
  },

  streamAgentUpdate: (sessionId, agentType, agentName) => {
    _flushTextBuf(set)
    set((s) => {
      const session = ensureSession(s, sessionId)
      const agentChanged =
        (session.streamingAgentType && session.streamingAgentType !== agentType) ||
        (session.streamingAgentName && session.streamingAgentName !== agentName)

      if (agentChanged && session.streamingContent.trim()) {
        // Agent switched mid-stream — finalize current content as a separate message
        const blocks = reduceEventToBlocks(session.streamingContent)
        const prevMessage: ChatMessage = {
          id: `agent-${Date.now()}`,
          role: 'agent',
          content: session.streamingContent,
          blocks,
          agentType: session.streamingAgentType,
          agentName: session.streamingAgentName,
          sessionId,
          timestamp: Date.now(),
        }
        return {
          sessions: {
            ...s.sessions,
            [sessionId]: {
              ...session,
              messages: [...session.messages, prevMessage],
              streamingContent: '',
              streamingAgentType: agentType,
              streamingAgentName: agentName,
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
      if (session.streamingContent.trim()) {
        const blocks = reduceEventToBlocks(session.streamingContent)
        if (import.meta.env.DEV) {
          console.group('[streamDone] block parse')
          console.log('raw content length:', session.streamingContent.length)
          console.log(
            'parsed blocks:',
            blocks.map((b) => b.type),
          )
          console.log('contains aka_yhy:', session.streamingContent.includes('aka_yhy'))
          if (session.streamingContent.includes('aka_yhy')) {
            console.log(
              'content around aka_yhy:',
              session.streamingContent.slice(
                Math.max(0, session.streamingContent.indexOf('aka_yhy') - 30),
                session.streamingContent.indexOf('aka_yhy') + 100,
              ),
            )
          }
          console.groupEnd()
        }
        newMessages.push({
          id: `agent-${Date.now()}`,
          role: 'agent',
          content: session.streamingContent,
          blocks,
          agentType: session.streamingAgentType,
          agentName: session.streamingAgentName,
          sessionId,
          timestamp: Date.now(),
        })
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
            activeStream: null,
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
        blocks[idx] = newBlock
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
        existing.tasks = tasks
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
