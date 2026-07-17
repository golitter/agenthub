/**
 * Chat Store — Barrel Re-export
 *
 * This file re-exports everything from the three domain-specific stores
 * (navigation, session, message) and provides a backward-compatible
 * `useChatStore` that composes them into a single Zustand store.
 *
 * All existing imports from `@/stores/chat` continue to work unchanged.
 */

// ── Re-export all public types and hooks from domain stores ────────────
export { useMessageStore } from './message-store'
export { useChatNav, useNavigationStore } from './navigation-store'
export type { ActiveStream, ChatMessage, ChatStatus, SessionChatState } from './session-store'
export { initialSessionState, useSessionStore } from './session-store'

// ── Backward-compatible composed Zustand store ─────────────────────────
// Consumers use `useChatStore(selector)` as a single store. We create a
// real Zustand store that syncs from the three domain stores via
// subscriptions, so selectors and getState() work identically to the
// original monolithic store.

import { create } from 'zustand'

import type { AgentType } from '@/generated/request'
import type { Announcement } from '@/lib/api'
import type { CoordMessage, PlanReviewPayload, PlanTask } from '@/lib/block-types'

import { useMessageStore } from './message-store'
import { useNavigationStore } from './navigation-store'
import type { ActiveStream, ChatMessage, SessionChatState } from './session-store'
import { useSessionStore } from './session-store'

interface ComposedChatStoreState {
  nav: {
    currentSessionId: string | null
    setCurrentSession: (id: string) => void
    clearNavigation: () => void
  }
  sessions: Record<string, SessionChatState>
  announcements: Record<string, Announcement[]>
  announcementsLoading: Record<string, boolean>
  setCurrentSession: (sessionId: string) => void
  clearNavigation: () => void
  getSession: (sessionId: string) => SessionChatState
  resetSession: (sessionId: string) => void
  loadHistory: (sessionId: string, messages: ChatMessage[], hasMore?: boolean) => void
  sendMessage: (sessionId: string, message: ChatMessage, activeStream: ActiveStream) => void
  streamStart: (sessionId: string, agentType: AgentType) => void
  streamText: (sessionId: string, text: string, messageId?: string) => void
  streamToolCall: (sessionId: string, toolName: string) => void
  streamToolResult: (sessionId: string) => void
  streamDone: (sessionId: string) => void
  streamError: (sessionId: string, error: Error) => void
  streamRuntimeEvent: (
    sessionId: string,
    event: { task_id: string; agent: string; status: string; title?: string },
  ) => void
  streamRuntimeText: (
    sessionId: string,
    event: { task_id: string; agent: string; text: string },
  ) => void
  streamPlanEvent: (sessionId: string, tasks: PlanTask[], overview: string) => void
  streamPlanReviewEvent: (sessionId: string, event: PlanReviewPayload) => void
  setPlanReviewStatus: (
    sessionId: string,
    reviewKey: string | undefined,
    status: 'pending' | 'submitted' | 'approved' | 'stale',
  ) => void
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
      group_id?: string
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
      group_id?: string
    },
  ) => void
  streamAgentUpdate: (
    sessionId: string,
    agentType: AgentType,
    agentName: string,
    messageId?: string,
    groupId?: string,
  ) => void
  prependMessages: (sessionId: string, messages: ChatMessage[], hasMore: boolean) => void
  setLoadingMore: (sessionId: string, loading: boolean) => void
  loadAnnouncements: (taskId: string) => Promise<void>
  addAnnouncement: (
    taskId: string,
    data: { sender_id: string; sender_name: string; content: string; pinned?: boolean },
  ) => Promise<void>
  removeAnnouncement: (taskId: string, id: number) => Promise<void>
}

/** Build the composed state by reading the three domain stores. */
function syncComposedState(): ComposedChatStoreState {
  const nav = useNavigationStore.getState()
  const session = useSessionStore.getState()
  const message = useMessageStore.getState()
  return {
    nav: {
      currentSessionId: nav.currentSessionId,
      setCurrentSession: nav.setCurrentSession,
      clearNavigation: nav.clearNavigation,
    },
    setCurrentSession: nav.setCurrentSession,
    clearNavigation: nav.clearNavigation,
    sessions: session.sessions,
    getSession: session.getSession,
    resetSession: session.resetSession,
    announcements: message.announcements,
    announcementsLoading: message.announcementsLoading,
    loadHistory: message.loadHistory,
    sendMessage: message.sendMessage,
    streamStart: message.streamStart,
    streamText: message.streamText,
    streamToolCall: message.streamToolCall,
    streamToolResult: message.streamToolResult,
    streamDone: message.streamDone,
    streamError: message.streamError,
    streamRuntimeEvent: message.streamRuntimeEvent,
    streamRuntimeText: message.streamRuntimeText,
    streamPlanEvent: message.streamPlanEvent,
    streamPlanReviewEvent: message.streamPlanReviewEvent,
    setPlanReviewStatus: message.setPlanReviewStatus,
    streamCoordinationEvent: message.streamCoordinationEvent,
    streamCoordinationDone: message.streamCoordinationDone,
    streamAskCardStart: message.streamAskCardStart,
    streamAskCardDone: message.streamAskCardDone,
    streamAgentUpdate: message.streamAgentUpdate,
    prependMessages: message.prependMessages,
    setLoadingMore: message.setLoadingMore,
    loadAnnouncements: message.loadAnnouncements,
    addAnnouncement: message.addAnnouncement,
    removeAnnouncement: message.removeAnnouncement,
  }
}

/**
 * A real Zustand store that mirrors the original monolithic shape.
 * It is kept in sync via subscriptions to the three domain stores.
 */
export const useChatStore = create<ComposedChatStoreState>(() => syncComposedState())

// ── Keep the composed store in sync ────────────────────────────────────
useNavigationStore.subscribe(() => useChatStore.setState(syncComposedState()))
useSessionStore.subscribe(() => useChatStore.setState(syncComposedState()))
useMessageStore.subscribe(() => useChatStore.setState(syncComposedState()))
