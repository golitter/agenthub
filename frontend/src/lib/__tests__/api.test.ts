import { afterEach, describe, expect, it, vi } from 'vitest'

import { fetchAdminSessions, fetchConversations, fetchTasks } from '../api'

function jsonResponse(data: unknown, init?: ResponseInit) {
  return new Response(JSON.stringify(data), {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
}

describe('fetchConversations', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns an empty list when there are no tasks', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ data: [] })))

    await expect(fetchConversations()).resolves.toEqual([])
  })

  it('surfaces an error when every existing task detail fails', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({
          data: [
            {
              task_id: 'task-1',
              title: 'First task',
              repo_path: '/workspace/first',
              status: 'active',
              created_at: '2026-08-24T00:00:00Z',
              updated_at: '2026-08-24T00:00:00Z',
            },
          ],
        }),
      )
      .mockResolvedValueOnce(jsonResponse({ msg: 'temporary failure' }, { status: 503 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchConversations()).rejects.toMatchObject({
      message: 'temporary failure',
      status: 503,
    })
  })

  it('does not expose a partial list after a transient detail failure', async () => {
    const task = {
      task_id: 'task-1',
      title: 'First task',
      repo_path: '/workspace/first',
      status: 'active',
      created_at: '2026-08-24T00:00:00Z',
      updated_at: '2026-08-24T00:00:00Z',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({ data: [task, { ...task, task_id: 'task-2', title: 'Second task' }] }),
      )
      .mockResolvedValueOnce(jsonResponse({ data: { task, sessions: [] } }))
      .mockResolvedValueOnce(jsonResponse({ msg: 'temporary failure' }, { status: 503 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchConversations()).rejects.toMatchObject({ status: 503 })
  })

  it('ignores a task detail that was deleted between list and detail requests', async () => {
    const task = {
      task_id: 'task-deleted',
      title: 'Deleted task',
      repo_path: '/workspace/deleted',
      status: 'active',
      created_at: '2026-08-24T00:00:00Z',
      updated_at: '2026-08-24T00:00:00Z',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: [task] }))
      .mockResolvedValueOnce(jsonResponse({ msg: 'not found' }, { status: 404 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchConversations()).resolves.toEqual([])
  })
})

describe('fetchTasks', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('aggregates cursor-paginated task responses', async () => {
    const firstTask = {
      task_id: 'task-2',
      title: 'Second task',
      repo_path: '/workspace/second',
      status: 'active',
      created_at: '2026-08-24T00:00:00Z',
      updated_at: '2026-08-24T00:00:00Z',
    }
    const secondTask = { ...firstTask, task_id: 'task-1', title: 'First task' }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse(
          { data: [firstTask] },
          { headers: { 'X-Has-More': 'true', 'X-Next-Cursor': 'task-2' } },
        ),
      )
      .mockResolvedValueOnce(
        jsonResponse({ data: [secondTask] }, { headers: { 'X-Has-More': 'false' } }),
      )
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchTasks()).resolves.toEqual([firstTask, secondTask])
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/tasks?limit=100')
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/tasks?limit=100&before=task-2')
  })
})

describe('fetchAdminSessions', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns every session from a multi-agent task', async () => {
    const task = {
      task_id: 'task-group',
      title: 'Review group',
      repo_path: '/workspace/group',
      status: 'active',
      created_at: '2026-08-24T00:00:00Z',
      updated_at: '2026-08-24T00:00:00Z',
    }
    const sessionBase = {
      id: 1,
      task_id: task.task_id,
      agent_name: 'Agent one',
      route_id: 'agent-one',
      mention_label: 'Agent one',
      status: 'idle',
      created_at: '2026-08-24T00:00:00Z',
      updated_at: '2026-08-24T00:00:00Z',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: [task] }))
      .mockResolvedValueOnce(
        jsonResponse({
          data: {
            task,
            sessions: [
              { ...sessionBase, session_id: 'session-1', agent_type: 'claude-code' },
              {
                ...sessionBase,
                id: 2,
                session_id: 'session-2',
                agent_type: 'codex',
                agent_name: 'Agent two',
              },
            ],
          },
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    const sessions = await fetchAdminSessions()
    expect(sessions.map((session) => session.sessionId)).toEqual(['session-1', 'session-2'])
    expect(sessions.every((session) => session.taskId === task.task_id)).toBe(true)
  })
})
