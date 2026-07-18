import type { StreamEvent } from '@/generated/events'

interface SSEOptions {
  url: string
  params?: Record<string, string>
  onEvent: (event: StreamEvent) => void
  onError?: (error: Error) => void
  /** Enable auto-reconnect (EventSource reconnects natively) */
  reconnect?: boolean
  /** Max ms without any event before treating the stream as dead (default 5min) */
  staleTimeoutMs?: number
}

export function connectSSE({
  url,
  params,
  onEvent,
  onError,
  reconnect = false,
  staleTimeoutMs = 300_000,
}: SSEOptions): AbortController {
  const controller = new AbortController()

  const qs = params ? '?' + new URLSearchParams(params).toString() : ''
  const baseUrl = (import.meta.env.VITE_SSE_BASE_URL as string | undefined) ?? ''
  const fullUrl = `${baseUrl}${url}${qs}`

  const es = new EventSource(fullUrl)

  let lastEventTime = Date.now()

  // Staleness check: close connection if no events received for staleTimeoutMs
  const staleCheck = setInterval(() => {
    if (Date.now() - lastEventTime > staleTimeoutMs) {
      clearInterval(staleCheck)
      es.close()
      if (!controller.signal.aborted) {
        onError?.(new Error('Stream timed out: no events received'))
      }
    }
  }, 10_000)

  es.onmessage = (e: MessageEvent) => {
    lastEventTime = Date.now()
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
    if (es.readyState === EventSource.CLOSED) {
      clearInterval(staleCheck)
      if (!controller.signal.aborted) {
        onError?.(new Error('SSE connection closed'))
      }
      return
    }
    if (!reconnect) {
      clearInterval(staleCheck)
      es.close()
      if (!controller.signal.aborted) {
        onError?.(new Error('SSE connection error'))
      }
    }
    // If reconnect is true, EventSource reconnects automatically
  }

  controller.signal.addEventListener('abort', () => {
    clearInterval(staleCheck)
    es.close()
  })

  return controller
}
