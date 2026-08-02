import type { StreamEvent } from '@/generated/events'

interface SSEOptions {
  url: string
  params?: Record<string, string>
  onEvent: (event: StreamEvent) => void
  onError?: (error: Error) => void
  /** Enable auto-reconnect (EventSource reconnects natively) */
  reconnect?: boolean
  /** Max ms to wait for the first successful connection (default 30s) */
  openTimeoutMs?: number
  /** Max ms without any event before treating the stream as dead (default 5min) */
  staleTimeoutMs?: number
}

export function connectSSE({
  url,
  params,
  onEvent,
  onError,
  reconnect = false,
  openTimeoutMs = 30_000,
  staleTimeoutMs = 300_000,
}: SSEOptions): AbortController {
  const controller = new AbortController()

  const qs = params ? '?' + new URLSearchParams(params).toString() : ''
  const baseUrl = (import.meta.env.VITE_SSE_BASE_URL as string | undefined) ?? ''
  const fullUrl = `${baseUrl}${url}${qs}`

  const es = new EventSource(fullUrl)

  let settled = false
  let lastActivityTime = Date.now()

  const cleanup = () => {
    clearTimeout(openTimeout)
    clearInterval(staleCheck)
  }

  const fail = (error: Error) => {
    if (settled || controller.signal.aborted) return
    settled = true
    cleanup()
    es.close()
    onError?.(error)
  }

  const markActivity = () => {
    lastActivityTime = Date.now()
  }

  const openTimeout = globalThis.setTimeout(() => {
    fail(new Error('SSE connection timed out before opening'))
  }, openTimeoutMs)

  // Staleness check: close connection if no events received for staleTimeoutMs
  const staleCheck = globalThis.setInterval(() => {
    if (Date.now() - lastActivityTime > staleTimeoutMs) {
      fail(new Error('Stream timed out: no events received'))
    }
  }, 10_000)

  es.onopen = () => {
    clearTimeout(openTimeout)
    markActivity()
  }

  es.onmessage = (e: MessageEvent) => {
    clearTimeout(openTimeout)
    markActivity()
    const data = typeof e.data === 'string' ? e.data : ''
    if (!data.trim()) return
    try {
      const event: StreamEvent = JSON.parse(data)
      onEvent(event)
    } catch {
      console.warn('Failed to parse SSE event:', data)
    }
  }

  es.onerror = () => {
    // An error means the stream has not produced application data. Treating
    // reconnect attempts as activity would keep a dead stream alive forever
    // and bypass staleTimeoutMs.
    if (es.readyState === EventSource.CLOSED) {
      fail(new Error('SSE connection closed'))
      return
    }
    if (!reconnect) {
      fail(new Error('SSE connection error'))
    }
    // If reconnect is true, EventSource reconnects automatically
  }

  controller.signal.addEventListener('abort', () => {
    settled = true
    cleanup()
    es.close()
  })

  return controller
}
