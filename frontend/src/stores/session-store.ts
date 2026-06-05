/**
 * Session Store
 *
 * Manages the per-session data map (messages, streaming state, runtime blocks)
 * and basic session CRUD. Each session's state includes its messages, streaming
 * content, runtime blocks, and status.
 *
 * The message store (message-store.ts) operates on sessions through
 * the exported store's actions, keeping the session map as the single
 * source of truth for per-session data.
 */

import { create } from 'zustand'

import type { AgentType } from '@/generated/request'

// ── Re-export shared types that live here ──────────────────────────────
export interface ChatMessage {
  id: string
  dbId?: number
  role: 'user' | 'agent' | 'system'
  content: string
  blocks?: import('@/lib/block-types').MessageBlock[]
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

// ── Per-session state slice ────────────────────────────────────────────
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
  runtimeBlocks: import('@/lib/block-types').MessageBlock[]
  activePlanReviewKey?: string
}

export const initialSessionState: SessionChatState = {
  messages: [],
  streamingContent: '',
  streamingReplay: undefined,
  streamingAgentType: undefined,
  streamingAgentName: undefined,
  streamingMessageId: undefined,
  streamingGroupId: undefined,
  status: 'idle',
  error: null,
  toolName: undefined,
  activeStream: null,
  hasMore: true,
  isLoadingMore: false,
  runtimeBlocks: [],
  activePlanReviewKey: undefined,
}

// ── Session Store ──────────────────────────────────────────────────────

interface SessionStoreState {
  sessions: Record<string, SessionChatState>

  getSession: (sessionId: string) => SessionChatState
  resetSession: (sessionId: string) => void
}

export function ensureSession(
  state: { sessions: Record<string, SessionChatState> },
  sessionId: string,
): SessionChatState {
  return state.sessions[sessionId] ?? { ...initialSessionState }
}

export const useSessionStore = create<SessionStoreState>((set, get) => ({
  sessions: {},

  getSession: (sessionId) => get().sessions[sessionId] ?? { ...initialSessionState },

  resetSession: (sessionId) =>
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: { ...initialSessionState },
      },
    })),
}))
