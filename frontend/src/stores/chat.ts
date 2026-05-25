import { create } from 'zustand'

import type { AgentType } from '@/generated/request'
import { reduceEventToBlocks } from '@/lib/block-reducer'
import type { MessageBlock } from '@/lib/block-types'

export interface ChatMessage {
  id: string
  role: 'user' | 'agent' | 'system'
  content: string
  blocks?: MessageBlock[]
  agentType?: AgentType
  timestamp: number
  messageId?: string
  status?: string
}

export type ChatStatus = 'idle' | 'loading' | 'streaming' | 'tool_running' | 'done' | 'error'

export interface ActiveStream {
  messageId: string
  sessionId: string
}

interface SessionChatState {
  messages: ChatMessage[]
  streamingContent: string
  streamingAgentType?: AgentType
  status: ChatStatus
  error: Error | null
  toolName?: string
  activeStream: ActiveStream | null
}

interface ChatNavState {
  currentSessionId: string | null
  setCurrentSession: (sessionId: string) => void
  clearNavigation: () => void
}

interface ChatStoreState {
  nav: ChatNavState
  sessions: Record<string, SessionChatState>

  // Nav actions
  setCurrentSession: (sessionId: string) => void
  clearNavigation: () => void

  // Session actions
  getSession: (sessionId: string) => SessionChatState
  loadHistory: (sessionId: string, messages: ChatMessage[]) => void
  sendMessage: (sessionId: string, message: ChatMessage, activeStream: ActiveStream) => void
  streamStart: (sessionId: string, agentType: AgentType) => void
  streamText: (sessionId: string, text: string) => void
  streamToolCall: (sessionId: string, toolName: string) => void
  streamToolResult: (sessionId: string) => void
  streamDone: (sessionId: string) => void
  streamError: (sessionId: string, error: Error) => void
  resetSession: (sessionId: string) => void
}

const initialSessionState: SessionChatState = {
  messages: [],
  streamingContent: '',
  streamingAgentType: undefined,
  status: 'idle',
  error: null,
  toolName: undefined,
  activeStream: null,
}

function ensureSession(state: ChatStoreState, sessionId: string): SessionChatState {
  return state.sessions[sessionId] ?? { ...initialSessionState }
}

export const useChatStore = create<ChatStoreState>((set, get) => ({
  nav: { currentSessionId: null, setCurrentSession: () => {}, clearNavigation: () => {} },
  sessions: {},

  setCurrentSession: (sessionId) =>
    set((s) => ({ nav: { ...s.nav, currentSessionId: sessionId } })),
  clearNavigation: () => set((s) => ({ nav: { ...s.nav, currentSessionId: null } })),

  getSession: (sessionId) => get().sessions[sessionId] ?? { ...initialSessionState },

  loadHistory: (sessionId, messages) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'done',
          messages,
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
        },
      },
    })),

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

  streamToolCall: (sessionId, toolName) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'tool_running',
          toolName,
        },
      },
    })),

  streamToolResult: (sessionId) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'streaming',
          toolName: undefined,
        },
      },
    })),

  streamDone: (sessionId) =>
    set((s) => {
      const session = ensureSession(s, sessionId)
      const blocks = reduceEventToBlocks(session.streamingContent)
      const agentMessage: ChatMessage = {
        id: `agent-${Date.now()}`,
        role: 'agent',
        content: session.streamingContent,
        blocks,
        agentType: session.streamingAgentType,
        timestamp: Date.now(),
      }
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            status: 'done',
            messages: [...session.messages, agentMessage],
            streamingContent: '',
            streamingAgentType: undefined,
            activeStream: null,
          },
        },
      }
    }),

  streamError: (sessionId, error) =>
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
    })),

  resetSession: (sessionId) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: { ...initialSessionState },
      },
    })),
}))

export function useChatNav() {
  const currentSessionId = useChatStore((s) => s.nav.currentSessionId)
  const setCurrentSession = useChatStore((s) => s.setCurrentSession)
  const clearNavigation = useChatStore((s) => s.clearNavigation)
  return { currentSessionId, setCurrentSession, clearNavigation }
}
