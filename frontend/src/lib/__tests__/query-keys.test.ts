import { describe, expect, it, vi } from 'vitest'

import type { Conversation } from '@/lib/api'
import { isActiveConversationStatus, toAgentDisplayStatus } from '@/lib/constants'

import {
  createConversationReconciler,
  getConversationStatusForStream,
  isAdminQueryKey,
  patchConversation,
  patchConversationForStream,
  queryKeys,
  upsertConversation,
} from '../query-keys'

const conversation = (overrides: Partial<Conversation> = {}): Conversation => ({
  taskId: 'task-1',
  sessionId: 'session-1',
  agentType: 'codex',
  agentName: '执行者',
  title: '执行者',
  lastActiveAt: '2026-08-27T10:00:00.000Z',
  taskTitle: '任务一',
  status: 'idle',
  ...overrides,
})

describe('isAdminQueryKey', () => {
  it('matches every namespaced admin query', () => {
    expect(isAdminQueryKey(['admin-sessions'])).toBe(true)
    expect(isAdminQueryKey(['admin-resources', { scope: 'all' }])).toBe(true)
    expect(isAdminQueryKey(['admin-avatar'])).toBe(true)
  })

  it('does not invalidate unrelated application data', () => {
    expect(isAdminQueryKey(['conversations'])).toBe(false)
    expect(isAdminQueryKey(['skills'])).toBe(false)
    expect(isAdminQueryKey([])).toBe(false)
  })
})

describe('conversation query cache helpers', () => {
  it('keeps server-side review and resolution states visibly active', () => {
    expect(isActiveConversationStatus('running')).toBe(true)
    expect(isActiveConversationStatus('resolving')).toBe(true)
    expect(isActiveConversationStatus('awaiting_review')).toBe(true)
    expect(isActiveConversationStatus('awaiting_resolution')).toBe(true)
    expect(isActiveConversationStatus('completed')).toBe(false)
  })

  it('projects every server terminal state to an intentional avatar state', () => {
    expect(toAgentDisplayStatus('idle')).toBe('ready')
    expect(toAgentDisplayStatus('completed')).toBe('ready')
    expect(toAgentDisplayStatus('running')).toBe('running')
    expect(toAgentDisplayStatus('awaiting_resolution')).toBe('running')
    expect(toAgentDisplayStatus('error')).toBe('error')
    expect(toAgentDisplayStatus('inactive')).toBe('offline')
    expect(toAgentDisplayStatus('interrupted')).toBe('offline')
  })

  it('patches immutably and moves the active conversation to the sorted position', () => {
    const first = conversation()
    const second = conversation({
      taskId: 'task-2',
      sessionId: 'session-2',
      lastActiveAt: '2026-08-27T11:00:00.000Z',
    })
    const current = [first, second]
    const next = patchConversation(current, 'session-1', {
      status: 'running',
      lastActiveAt: '2026-08-27T12:00:00.000Z',
    })

    expect(next).toEqual([
      { ...first, status: 'running', lastActiveAt: '2026-08-27T12:00:00.000Z' },
      second,
    ])
    expect(next).not.toBe(current)
    expect(current[0]).toBe(first)
  })

  it('returns the original reference when the target is missing', () => {
    const current = [conversation()]
    expect(patchConversation(current, 'missing', { status: 'running' })).toBe(current)
  })

  it('upserts a newly created conversation in server sort order', () => {
    const existing = conversation({ lastActiveAt: '2026-08-27T12:00:00.000Z' })
    const created = conversation({
      taskId: 'task-new',
      sessionId: 'session-new',
      lastActiveAt: '2026-08-27T13:00:00.000Z',
    })

    expect(upsertConversation([existing], created)).toEqual([created, existing])
    expect(upsertConversation(undefined, created)).toEqual([created])
  })

  it('patches the visible group row from a worker stream session', () => {
    const group = conversation({
      taskId: 'task-group',
      sessionId: 'orchestrator-session',
      isGroupChat: true,
      groupSessions: [
        {
          sessionId: 'orchestrator-session',
          agentType: 'orchestrator',
          agentName: '编排器',
          routeId: 'orchestrator',
          mentionLabel: '编排器',
        },
        {
          sessionId: 'worker-session',
          agentType: 'codex',
          agentName: '执行者',
          routeId: 'codex',
          mentionLabel: '执行者',
        },
      ],
    })
    const unrelated = conversation({ taskId: 'task-other', sessionId: 'other-session' })
    const next = patchConversationForStream(
      [group, unrelated],
      {
        taskId: 'task-group',
        primarySessionId: 'missing-primary',
        streamSessionId: 'worker-session',
      },
      { status: 'running' },
    )

    expect(next?.[0]).toMatchObject({
      sessionId: 'orchestrator-session',
      status: 'running',
      groupSessions: [
        { sessionId: 'orchestrator-session' },
        { sessionId: 'worker-session', status: 'running' },
      ],
    })
    expect(next?.[1]).toMatchObject({ sessionId: 'other-session', status: 'idle' })
  })

  it('preserves an active status from the stream member during group reconnect', () => {
    const group = conversation({
      taskId: 'task-group',
      sessionId: 'orchestrator-session',
      status: 'idle',
      isGroupChat: true,
      groupSessions: [
        {
          sessionId: 'orchestrator-session',
          agentType: 'orchestrator',
          agentName: '编排器',
          routeId: 'orchestrator',
          mentionLabel: '编排器',
          status: 'awaiting_review',
        },
        {
          sessionId: 'worker-session',
          agentType: 'codex',
          agentName: '执行者',
          routeId: 'codex',
          mentionLabel: '执行者',
          status: 'awaiting_resolution',
        },
      ],
    })

    expect(getConversationStatusForStream(group, 'worker-session')).toBe('awaiting_resolution')
    expect(getConversationStatusForStream(group, 'orchestrator-session')).toBe('awaiting_review')
  })

  it('does not patch a same-named primary session from another task', () => {
    const current = [conversation({ taskId: 'task-other', sessionId: 'shared-session' })]
    const next = patchConversationForStream(
      current,
      { taskId: 'task-target', primarySessionId: 'shared-session' },
      { status: 'running' },
    )

    expect(next).toBe(current)
    expect(next?.[0].status).toBe('idle')
  })

  it('invalidates only once across terminal event races', () => {
    const invalidate = vi.fn()
    const reconciler = createConversationReconciler(invalidate)

    reconciler.invalidateOnce()
    reconciler.invalidateOnce()
    expect(invalidate).toHaveBeenCalledTimes(1)

    reconciler.reset()
    reconciler.invalidateOnce()
    expect(invalidate).toHaveBeenCalledTimes(2)
  })

  it('keeps the conversations key centralized', () => {
    expect(queryKeys.conversations).toEqual(['conversations'])
    expect(queryKeys.skills).toEqual(['skills'])
  })
})
