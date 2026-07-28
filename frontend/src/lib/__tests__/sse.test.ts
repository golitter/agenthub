import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { connectSSE } from '../sse'

type FakeEventSourceInstance = {
  url: string
  readyState: number
  onopen: (() => void) | null
  onmessage: ((event: MessageEvent) => void) | null
  onerror: (() => void) | null
  close: ReturnType<typeof vi.fn>
}

const eventSources: FakeEventSourceInstance[] = []

class FakeEventSource {
  static CLOSED = 2
  url: string
  readyState = 0
  onopen: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  close = vi.fn(() => {
    this.readyState = FakeEventSource.CLOSED
  })

  constructor(url: string) {
    this.url = url
    eventSources.push(this)
  }
}

describe('connectSSE', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    eventSources.length = 0
    vi.stubGlobal('EventSource', FakeEventSource)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('fails and closes when the stream never opens', () => {
    const onError = vi.fn()

    connectSSE({
      url: '/api/tasks/task-1/stream',
      params: { session_id: 'session-1', message_id: 'message-1' },
      onEvent: vi.fn(),
      onError,
      openTimeoutMs: 1_000,
    })

    vi.advanceTimersByTime(1_000)

    expect(eventSources[0].url).toBe(
      '/api/tasks/task-1/stream?session_id=session-1&message_id=message-1',
    )
    expect(eventSources[0].close).toHaveBeenCalledOnce()
    expect(onError).toHaveBeenCalledWith(new Error('SSE connection timed out before opening'))
  })

  it('clears the open timeout after the stream opens and delivers events', () => {
    const onEvent = vi.fn()
    const onError = vi.fn()

    const controller = connectSSE({
      url: '/api/tasks/task-1/stream',
      onEvent,
      onError,
      openTimeoutMs: 1_000,
    })

    eventSources[0].onopen?.()
    vi.advanceTimersByTime(1_000)
    eventSources[0].onmessage?.(
      new MessageEvent('message', { data: '{"type":"heartbeat"}' }),
    )

    expect(onError).not.toHaveBeenCalled()
    expect(onEvent).toHaveBeenCalledWith({ type: 'heartbeat' })

    controller.abort()
    expect(eventSources[0].close).toHaveBeenCalledOnce()
  })
})
