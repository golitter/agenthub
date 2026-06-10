/**
 * Message Store
 *
 * Manages message streaming, runtime blocks, block reducer logic,
 * rAF-based text batching, and announcements. Operates on the session
 * map provided by session-store via `useSessionStore`.
 *
 * TECH DEBT: Server State in Zustand
 * ───────────────────────────────────
 * messages / streamingContent / runtimeBlocks belong to Server State,
 * ideally managed through TanStack Query (caching, invalidation, optimistic updates),
 * but the current SSE streaming architecture requires rAF-batched token flushing,
 * and Zustand's synchronous set() is better suited for high-frequency updates than
 * TanStack Query's async cache.
 *
 * Migration path: extract streaming state into an independent useReducer (no Zustand dependency),
 * manage historical messages and pagination with TanStack Query, ultimately eliminating Server State from the store.
 * Priority: P2 (independent refactoring after features stabilize).
 */

import { create } from 'zustand'

import type { AgentType } from '@/generated/request'
import type { Announcement } from '@/lib/api'
import {
  createAnnouncement as apiCreateAnnouncement,
  deleteAnnouncement as apiDeleteAnnouncement,
  fetchAnnouncements as apiFetchAnnouncements,
} from '@/lib/api'
import { coalesceMessageBlocks, reduceEventToBlocks } from '@/lib/block-reducer'
import type { CoordMessage, MessageBlock, PlanReviewPayload, PlanTask } from '@/lib/block-types'

import type { ActiveStream, ChatMessage, ChatStatus, SessionChatState } from './session-store'
import { ensureSession, useSessionStore } from './session-store'

// Re-export for consumers that import these types via the barrel
export type { ActiveStream, ChatMessage, ChatStatus, SessionChatState } from './session-store'

// ── Helper types ───────────────────────────────────────────────────────

let _runtimeBlockId = 0
function nextRuntimeBlockId(): string {
  return `rtb-${++_runtimeBlockId}`
}

function askCardStatus(status?: string): 'answered' | 'failed' {
  return status === 'completed' || status === 'answered' ? 'answered' : 'failed'
}

function stripRuntimeStreamingText(blocks: MessageBlock[]): MessageBlock[] {
  return blocks.map((block) =>
    block.type === 'runtime_status' ? { ...block, streamingText: undefined } : block,
  )
}

function hasRuntimeStreamingText(blocks: MessageBlock[]): boolean {
  return blocks.some((block) => block.type === 'runtime_status' && Boolean(block.streamingText))
}

function buildAgentMessage(
  session: SessionChatState,
  sessionId: string,
  options: { keepRuntimeStreamingText?: boolean } = { keepRuntimeStreamingText: true },
): ChatMessage {
  const blocks = [
    ...(options.keepRuntimeStreamingText
      ? session.runtimeBlocks
      : stripRuntimeStreamingText(session.runtimeBlocks)),
    ...(session.streamingContent ? reduceEventToBlocks(session.streamingContent) : []),
  ]
  const baseTimestamp = session.messages[session.messages.length - 1]?.timestamp ?? 0
  const timestamp = Math.max(Date.now(), baseTimestamp + 1)
  return {
    id: session.streamingMessageId ?? `agent-${timestamp}`,
    role: 'agent',
    content: session.streamingContent,
    blocks: coalesceMessageBlocks(
      // 终态化消息：清除 html-render 的 streaming 标记。
      // streaming 是流式渲染时的 UI 派生状态（MessageList 通过 reduceEventToBlocks 直接产出），
      // 终态消息应始终渲染完整卡片，即使 SSE done 事件早于闭合 ``` 到达导致内容不完整
      blocks.map((b) =>
        b.type === 'html-render' && b.streaming ? { ...b, streaming: undefined } : b,
      ),
    ),
    agentType: session.streamingAgentType,
    agentName: session.streamingAgentName,
    sessionId,
    timestamp,
    messageId: session.streamingMessageId,
    groupId: session.streamingGroupId,
  }
}

function hydrateAgentBlocks(messages: ChatMessage[]): ChatMessage[] {
  return messages.map((msg) =>
    msg.role === 'agent' && msg.content
      ? { ...msg, blocks: coalesceMessageBlocks(reduceEventToBlocks(msg.content)) }
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

function patchAskAgentBlock(
  block: MessageBlock,
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
) {
  if (block.type !== 'ask_agent' || block.question_id !== event.question_id) return block
  const status = askCardStatus(event.status)
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
}

function planReviewKey(
  sessionId: string,
  block: Extract<MessageBlock, { type: 'plan_review' }>,
): string {
  return block.review_key || `${block.task_id ?? ''}:${block.session_id ?? sessionId}`
}

function patchPlanReviewStatus(
  blocks: MessageBlock[] | undefined,
  sessionId: string,
  reviewKey: string | undefined,
  status: 'pending' | 'submitted' | 'approved' | 'stale',
): MessageBlock[] | undefined {
  if (!blocks) return blocks
  return blocks.map((block) => {
    if (block.type !== 'plan_review') return block
    if (reviewKey && planReviewKey(sessionId, block) !== reviewKey) return block
    return {
      ...block,
      review_key: planReviewKey(sessionId, block),
      status: status === 'stale' ? 'submitted' : status,
    }
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

type SessionSet = (
  partial:
    | Partial<{ sessions: Record<string, SessionChatState> }>
    | ((state: {
        sessions: Record<string, SessionChatState>
      }) => Partial<{ sessions: Record<string, SessionChatState> }>),
) => void

function _scheduleFlush(set: SessionSet) {
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

function _flushTextBuf(set: SessionSet) {
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

// ── Message Store ──────────────────────────────────────────────────────

interface MessageStoreState {
  // Announcement state
  announcements: Record<string, Announcement[]>
  announcementsLoading: Record<string, boolean>

  // Session message/streaming actions
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

  // Pagination actions
  prependMessages: (sessionId: string, messages: ChatMessage[], hasMore: boolean) => void
  setLoadingMore: (sessionId: string, loading: boolean) => void

  // Announcement actions
  loadAnnouncements: (taskId: string) => Promise<void>
  addAnnouncement: (
    taskId: string,
    data: { sender_id: string; sender_name: string; content: string; pinned?: boolean },
  ) => Promise<void>
  removeAnnouncement: (taskId: string, id: number) => Promise<void>
}

export const useMessageStore = create<MessageStoreState>((set) => ({
  announcements: {},
  announcementsLoading: {},

  loadHistory: (sessionId, messages, hasMore) =>
    useSessionStore.setState((s) => {
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
    useSessionStore.setState((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'loading',
          messages: [...(s.sessions[sessionId]?.messages ?? []), message],
          streamingContent: '',
          streamingReplay: undefined,
          runtimeBlocks: [],
          activePlanReviewKey: undefined,
          streamingGroupId: undefined,
          error: null,
          activeStream,
        },
      },
    })),

  streamStart: (sessionId, agentType) =>
    useSessionStore.setState((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          status: 'streaming',
          streamingContent: '',
          streamingReplay: undefined,
          streamingAgentType: agentType,
          streamingAgentName: undefined,
          streamingMessageId: undefined,
          streamingGroupId: undefined,
          runtimeBlocks: [],
          activePlanReviewKey: undefined,
        },
      },
    })),

  streamText: (sessionId, text, messageId) => {
    if (text.trim()) {
      useSessionStore.setState((s) => {
        const session = ensureSession(s, sessionId)
        if (!hasRuntimeStreamingText(session.runtimeBlocks)) return {}
        return {
          sessions: {
            ...s.sessions,
            [sessionId]: {
              ...session,
              runtimeBlocks: stripRuntimeStreamingText(session.runtimeBlocks),
            },
          },
        }
      })
    }
    if (messageId) {
      let textToAppend = text
      let shouldDrop = false
      useSessionStore.setState((s) => {
        const session = ensureSession(s, sessionId)
        if (session.streamingMessageId && session.streamingMessageId !== messageId) {
          shouldDrop = true
          return {}
        }

        const pending = _textBufs?.get(sessionId)?.join('') ?? ''
        const current = session.streamingContent + pending
        const replay =
          session.streamingReplay?.messageId === messageId
            ? session.streamingReplay
            : { messageId, offset: 0 }
        let nextOffset = replay.offset

        if (current && nextOffset < current.length) {
          const knownTail = current.slice(nextOffset)
          if (knownTail.startsWith(text)) {
            nextOffset += text.length
            textToAppend = ''
          } else if (text.startsWith(knownTail)) {
            textToAppend = text.slice(knownTail.length)
            nextOffset = current.length
          }
        }

        return {
          sessions: {
            ...s.sessions,
            [sessionId]: {
              ...session,
              streamingMessageId: messageId,
              streamingReplay: { messageId, offset: nextOffset },
            },
          },
        }
      })
      if (shouldDrop) return
      if (!textToAppend) return
      text = textToAppend
    }
    _ensureBuf(sessionId).push(text)
    _scheduleFlush(useSessionStore.setState as SessionSet)
  },

  streamAgentUpdate: (sessionId, agentType, agentName, messageId, groupId) => {
    _flushTextBuf(useSessionStore.setState as SessionSet)
    useSessionStore.setState((s) => {
      const session = ensureSession(s, sessionId)
      const shouldCarryGroupedAskCard =
        Boolean(groupId) &&
        !session.streamingContent.trim() &&
        session.runtimeBlocks.length > 0 &&
        session.runtimeBlocks.every((block) => block.type === 'ask_agent')
      const agentChanged =
        (session.streamingAgentType && session.streamingAgentType !== agentType) ||
        (session.streamingAgentName && session.streamingAgentName !== agentName)
      const messageChanged =
        !!session.streamingMessageId && !!messageId && session.streamingMessageId !== messageId

      if (
        (agentChanged || messageChanged) &&
        (session.streamingContent.trim() || session.runtimeBlocks.length > 0) &&
        !shouldCarryGroupedAskCard
      ) {
        const prevMessage = buildAgentMessage(session, sessionId, {
          keepRuntimeStreamingText: false,
        })
        return {
          sessions: {
            ...s.sessions,
            [sessionId]: {
              ...session,
              messages: [...session.messages, prevMessage],
              streamingContent: '',
              streamingReplay: undefined,
              streamingAgentType: agentType,
              streamingAgentName: agentName,
              streamingMessageId: messageId,
              streamingGroupId: groupId ?? session.streamingGroupId,
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
            streamingGroupId: groupId ?? session.streamingGroupId,
            streamingReplay:
              messageId && messageId !== session.streamingReplay?.messageId
                ? { messageId, offset: 0 }
                : session.streamingReplay,
          },
        },
      }
    })
  },

  streamToolCall: (sessionId, toolName) => {
    _flushTextBuf(useSessionStore.setState as SessionSet)
    useSessionStore.setState((s) => ({
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
    _flushTextBuf(useSessionStore.setState as SessionSet)
    useSessionStore.setState((s) => ({
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
    _flushTextBuf(useSessionStore.setState as SessionSet)
    useSessionStore.setState((s) => {
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
            streamingReplay: undefined,
            streamingAgentType: undefined,
            streamingAgentName: undefined,
            streamingMessageId: undefined,
            streamingGroupId: undefined,
            activeStream: null,
            runtimeBlocks: [],
            activePlanReviewKey: undefined,
          },
        },
      }
    })
  },

  streamError: (sessionId, error) => {
    _flushTextBuf(useSessionStore.setState as SessionSet)
    useSessionStore.setState((s) => {
      const session = ensureSession(s, sessionId)
      const messages = [...session.messages]
      const errorText = error.message || 'Unknown error'
      if (session.streamingContent.trim() || session.runtimeBlocks.length > 0) {
        messages.push({ ...buildAgentMessage(session, sessionId), status: 'failed' })
      } else {
        messages.push({
          ...buildAgentMessage({ ...session, streamingContent: errorText }, sessionId),
          status: 'failed',
        })
      }
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            status: 'error',
            error,
            messages,
            streamingContent: '',
            streamingReplay: undefined,
            streamingMessageId: undefined,
            streamingGroupId: undefined,
            runtimeBlocks: [],
            activePlanReviewKey: undefined,
            activeStream: null,
          },
        },
      }
    })
  },

  streamRuntimeEvent: (sessionId, event) =>
    useSessionStore.setState((s) => {
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
                title: event.title ?? existing.title,
              }
            : newBlock
      } else {
        blocks.push(newBlock)
      }
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...session, runtimeBlocks: blocks, activePlanReviewKey: undefined },
        },
      }
    }),

  streamRuntimeText: (sessionId, event) =>
    useSessionStore.setState((s) => {
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
          [sessionId]: { ...session, runtimeBlocks: blocks, activePlanReviewKey: undefined },
        },
      }
    }),

  streamPlanEvent: (sessionId, tasks, overview) =>
    useSessionStore.setState((s) => {
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

  streamPlanReviewEvent: (sessionId, event) =>
    useSessionStore.setState((s) => {
      const session = ensureSession(s, sessionId)
      const reviewKey =
        event.review_key || `${event.task_id ?? ''}:${event.session_id ?? sessionId}`
      const blocks = session.runtimeBlocks.filter((b) => b.type !== 'runtime_status')
      const existingIdx = blocks.findIndex(
        (b) => b.type === 'plan_review' && planReviewKey(sessionId, b) === reviewKey,
      )
      const block: MessageBlock = {
        type: 'plan_review',
        id: existingIdx >= 0 ? blocks[existingIdx].id : nextRuntimeBlockId(),
        ...event,
        review_key: reviewKey,
        status: 'pending',
      }
      if (existingIdx >= 0) {
        blocks[existingIdx] = block
      } else {
        blocks.push(block)
      }
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            status: 'streaming',
            runtimeBlocks: blocks,
            activePlanReviewKey: reviewKey,
          },
        },
      }
    }),

  setPlanReviewStatus: (sessionId, reviewKey, status) =>
    useSessionStore.setState((s) => {
      const session = ensureSession(s, sessionId)
      const activePlanReviewKey =
        status === 'pending'
          ? reviewKey
          : session.activePlanReviewKey === reviewKey || !reviewKey
            ? undefined
            : session.activePlanReviewKey
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...session,
            activePlanReviewKey,
            runtimeBlocks:
              patchPlanReviewStatus(session.runtimeBlocks, sessionId, reviewKey, status) ?? [],
            messages: session.messages.map((message) => ({
              ...message,
              blocks: patchPlanReviewStatus(message.blocks, sessionId, reviewKey, status),
            })),
          },
        },
      }
    }),

  streamCoordinationEvent: (sessionId, msg) =>
    useSessionStore.setState((s) => {
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
    useSessionStore.setState((s) => {
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

  streamAskCardStart: (sessionId, event) => {
    _flushTextBuf(useSessionStore.setState as SessionSet)
    useSessionStore.setState((s) => {
      const session = ensureSession(s, sessionId)
      const sourceAgentType = event.source_agent_type as AgentType | undefined
      const sourceAgentName = event.source_agent
      const targetAgentType = event.target_agent_type as AgentType | undefined
      const targetAgentName = event.target_agent
      const groupId = event.group_id
      const isGroupedFlow = Boolean(groupId)
      const speakerChanged =
        (sourceAgentType &&
          session.streamingAgentType &&
          session.streamingAgentType !== sourceAgentType) ||
        (sourceAgentName &&
          session.streamingAgentName &&
          session.streamingAgentName !== sourceAgentName)
      const shouldCloseCurrent =
        speakerChanged && (session.streamingContent.trim() || session.runtimeBlocks.length > 0)
      const baseSession = shouldCloseCurrent
        ? {
            ...session,
            messages: [
              ...session.messages,
              buildAgentMessage(session, sessionId, { keepRuntimeStreamingText: false }),
            ],
            streamingContent: '',
            streamingReplay: undefined,
            runtimeBlocks: [],
            activePlanReviewKey: undefined,
          }
        : session

      const blocks = [...baseSession.runtimeBlocks]
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
          [sessionId]: {
            ...baseSession,
            status: 'streaming',
            streamingAgentType:
              shouldCloseCurrent && isGroupedFlow
                ? (targetAgentType ?? baseSession.streamingAgentType)
                : (sourceAgentType ?? baseSession.streamingAgentType),
            streamingAgentName:
              shouldCloseCurrent && isGroupedFlow
                ? (targetAgentName ?? baseSession.streamingAgentName)
                : (sourceAgentName ?? baseSession.streamingAgentName),
            streamingMessageId: undefined,
            streamingGroupId: groupId ?? baseSession.streamingGroupId,
            runtimeBlocks: blocks,
            activePlanReviewKey: undefined,
          },
        },
      }
    })
  },

  streamAskCardDone: (sessionId, event) =>
    useSessionStore.setState((s) => {
      const session = ensureSession(s, sessionId)
      const blocks = session.runtimeBlocks.map((block) => patchAskAgentBlock(block, event))
      const messages = session.messages.map((message) =>
        message.blocks?.some(
          (block) => block.type === 'ask_agent' && block.question_id === event.question_id,
        )
          ? { ...message, blocks: message.blocks.map((block) => patchAskAgentBlock(block, event)) }
          : message,
      )
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...session, messages, runtimeBlocks: blocks },
        },
      }
    }),

  prependMessages: (sessionId, messages, hasMore) =>
    useSessionStore.setState((s) => {
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

  setLoadingMore: (sessionId, loading) =>
    useSessionStore.setState((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...ensureSession(s, sessionId),
          isLoadingMore: loading,
        },
      },
    })),

  loadAnnouncements: async (taskId) => {
    set((s) => ({ announcementsLoading: { ...s.announcementsLoading, [taskId]: true } }))
    try {
      const announcements = await apiFetchAnnouncements(taskId)
      set((s) => ({
        announcements: { ...s.announcements, [taskId]: announcements },
        announcementsLoading: { ...s.announcementsLoading, [taskId]: false },
      }))
    } catch {
      set((s) => ({ announcementsLoading: { ...s.announcementsLoading, [taskId]: false } }))
    }
  },

  addAnnouncement: async (taskId, data) => {
    const announcement = await apiCreateAnnouncement(taskId, data)
    set((s) => {
      const existing = s.announcements[taskId] ?? []
      return {
        announcements: {
          ...s.announcements,
          [taskId]: [announcement, ...existing],
        },
      }
    })
  },

  removeAnnouncement: async (taskId, id) => {
    await apiDeleteAnnouncement(taskId, id)
    set((s) => {
      const existing = s.announcements[taskId] ?? []
      return {
        announcements: {
          ...s.announcements,
          [taskId]: existing.filter((a) => a.id !== id),
        },
      }
    })
  },
}))
