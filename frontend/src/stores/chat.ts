/**
 * Chat Store — Barrel 重新导出
 *
 * 本文件从三个领域专用 store（navigation、session、message）重新导出全部内容，
 * 并提供一个向后兼容的 `useChatStore`，将它们组合成单个 Zustand store。
 *
 * 所有既有的从 `@/stores/chat` 导入的代码将继续保持不变地工作。
 */

// ── 重新导出 domain store 的全部公开类型与 hooks ────────────
export { useMessageStore } from './message-store'
export { useChatNav, useNavigationStore } from './navigation-store'
export type { ActiveStream, ChatMessage, ChatStatus, SessionChatState } from './session-store'
export { initialSessionState, useSessionStore } from './session-store'

// ── 向后兼容的组合式 Zustand store ─────────────────────────
// 消费方通过 `useChatStore(selector)` 将其当作单个 store 使用。我们创建了一个
// 真正的 Zustand store，它通过订阅从三个 domain store 同步状态，
// 因此 selector 与 getState() 的行为与最初的单体 store 完全一致。

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
  announcementsError: Record<string, boolean>
  setCurrentSession: (sessionId: string) => void
  clearNavigation: () => void
  getSession: (sessionId: string) => SessionChatState
  resetSession: (sessionId: string) => void
  loadHistory: (sessionId: string, messages: ChatMessage[], hasMore?: boolean) => void
  sendMessage: (sessionId: string, message: ChatMessage, activeStream: ActiveStream) => void
  streamStart: (sessionId: string, agentType: AgentType) => void
  clearActiveStream: (sessionId: string) => void
  streamText: (sessionId: string, text: string, messageId?: string) => void
  streamGroupedText: (
    sessionId: string,
    event: {
      text: string
      messageId: string
      groupId: string
      agentType?: AgentType
      agentName?: string
    },
  ) => void
  streamGroupedMessageStatus: (
    sessionId: string,
    messageId: string,
    status: 'completed' | 'failed',
  ) => void
  streamToolCall: (sessionId: string, toolName: string) => void
  streamToolResult: (sessionId: string) => void
  streamDone: (sessionId: string) => void
  streamError: (sessionId: string, error: Error) => void
  streamRuntimeEvent: (
    sessionId: string,
    event: {
      task_id: string
      plan_task_id?: string
      integration_operation_id?: string
      run_id?: string
      attempt?: number
      conflict_id?: string
      conflict_files?: string[]
      error_code?: string
      error_message?: string
      agent: string
      status: string
      title?: string
    },
  ) => void
  streamRuntimeText: (
    sessionId: string,
    event: {
      task_id: string
      plan_task_id?: string
      integration_operation_id?: string
      run_id?: string
      attempt?: number
      conflict_id?: string
      conflict_files?: string[]
      error_code?: string
      error_message?: string
      agent: string
      text: string
    },
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

/** 通过读取三个 domain store 来构建组合状态。 */
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
    announcementsError: message.announcementsError,
    loadHistory: message.loadHistory,
    sendMessage: message.sendMessage,
    streamStart: message.streamStart,
    clearActiveStream: message.clearActiveStream,
    streamText: message.streamText,
    streamGroupedText: message.streamGroupedText,
    streamGroupedMessageStatus: message.streamGroupedMessageStatus,
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
 * 一个真正的 Zustand store，镜像了最初单体 store 的结构。
 * 通过订阅三个 domain store 来保持同步。
 */
export const useChatStore = create<ComposedChatStoreState>(() => syncComposedState())

// ── 保持组合 store 同步 ────────────────────────────────────
useNavigationStore.subscribe(() => useChatStore.setState(syncComposedState()))
useSessionStore.subscribe(() => useChatStore.setState(syncComposedState()))
useMessageStore.subscribe(() => useChatStore.setState(syncComposedState()))
