import type { Conversation } from '@/lib/api'
import { isActiveConversationStatus } from '@/lib/constants'

export const queryKeys = {
  conversations: ['conversations'] as const,
  skills: ['skills'] as const,
} as const

export interface ConversationStreamIdentity {
  taskId: string
  primarySessionId: string
  streamSessionId?: string
}

const conversationSort = (a: Conversation, b: Conversation): number => {
  const aPinned = a.pinnedAt ? 1 : 0
  const bPinned = b.pinnedAt ? 1 : 0
  if (aPinned !== bPinned) return bPinned - aPinned

  if (aPinned && bPinned && a.pinnedAt && b.pinnedAt) {
    const pinDiff = new Date(b.pinnedAt).getTime() - new Date(a.pinnedAt).getTime()
    if (pinDiff !== 0) return pinDiff
  }

  return new Date(b.lastActiveAt).getTime() - new Date(a.lastActiveAt).getTime()
}

/**
 * Patch one visible conversation without mutating the React Query cache.
 * Keeping the helper pure makes optimistic stream state easy to verify and roll back.
 */
export function patchConversation(
  current: Conversation[] | undefined,
  sessionId: string,
  patch: Partial<Conversation>,
): Conversation[] | undefined {
  if (!current) return current

  const index = current.findIndex((conversation) => conversation.sessionId === sessionId)
  if (index < 0) return current

  const next = current.slice()
  next[index] = { ...next[index], ...patch }
  return next.sort(conversationSort)
}

/** Add or replace a conversation while preserving the server list ordering. */
export function upsertConversation(
  current: Conversation[] | undefined,
  conversation: Conversation,
): Conversation[] {
  const next = (current ?? []).filter((item) => item.sessionId !== conversation.sessionId)
  next.push(conversation)
  return next.sort(conversationSort)
}

/**
 * A group task is rendered as one row keyed by its primary/orchestrator session.
 * Stream events can carry a worker session id, so never use taskId alone to patch
 * an arbitrary row or to mark another task's agent as running.
 */
export function patchConversationForStream(
  current: Conversation[] | undefined,
  identity: ConversationStreamIdentity,
  patch: Partial<Conversation>,
): Conversation[] | undefined {
  if (!current) return current

  const exact = current.find(
    (conversation) =>
      conversation.taskId === identity.taskId &&
      conversation.sessionId === identity.primarySessionId,
  )
  const streamSessionId = identity.streamSessionId
  const target =
    exact ??
    current.find(
      (conversation) =>
        conversation.taskId === identity.taskId &&
        Boolean(
          streamSessionId &&
          conversation.groupSessions?.some((session) => session.sessionId === streamSessionId),
        ),
    )
  if (!target) return current

  const patched = patchConversation(current, target.sessionId, patch)
  if (!patched || !streamSessionId || patch.status === undefined) return patched

  const targetIndex = patched.findIndex(
    (conversation) => conversation.sessionId === target.sessionId,
  )
  const groupSessions = patched[targetIndex]?.groupSessions
  if (!groupSessions) return patched

  let groupChanged = false
  const nextGroupSessions = groupSessions.map((session) => {
    if (session.sessionId !== streamSessionId || session.status === patch.status) return session
    groupChanged = true
    return { ...session, status: patch.status }
  })
  if (!groupChanged) return patched

  const next = patched.slice()
  next[targetIndex] = { ...next[targetIndex], groupSessions: nextGroupSessions }
  return next
}

/** Keep the server status that belongs to the stream being reconnected. */
export function getConversationStatusForStream(
  conversation: Conversation | undefined,
  streamSessionId?: string,
): string | undefined {
  if (!conversation) return undefined

  const groupStatus = streamSessionId
    ? conversation.groupSessions?.find((session) => session.sessionId === streamSessionId)?.status
    : undefined
  return [groupStatus, conversation.status].find(isActiveConversationStatus)
}

/** Prevent a done/error/cancel race from issuing duplicate list refetches. */
export function createConversationReconciler(invalidate: () => void) {
  let reconciled = false

  return {
    reset() {
      reconciled = false
    },
    invalidateOnce() {
      if (reconciled) return
      reconciled = true
      invalidate()
    },
  }
}

export function isAdminQueryKey(queryKey: readonly unknown[]): boolean {
  const root = queryKey[0]
  return typeof root === 'string' && root.startsWith('admin-')
}
