/**
 * Session Store
 *
 * 管理按会话划分的数据 map（messages、streaming state、runtime blocks）
 * 以及基础的 session CRUD。每个会话的状态包含其 messages、streaming
 * content、runtime blocks 以及 status。
 *
 * message store（message-store.ts）通过导出 store 的 actions 操作会话，
 * 将 session map 作为按会话数据的唯一数据来源。
 */

import { create } from 'zustand'

import type { AgentType } from '@/generated/request'

// ── 重新导出存放于此的共享类型 ──────────────────────────────
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

// ── 单会话状态切片 ────────────────────────────────────────────
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
