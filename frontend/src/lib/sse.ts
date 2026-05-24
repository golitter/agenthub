import type { StreamEvent } from '@/generated/events'

interface SSEOptions {
  url: string
  params?: Record<string, string>
  onEvent: (event: StreamEvent) => void
  onError?: (error: Error) => void
  /** Enable auto-reconnect (EventSource reconnects natively) */
  reconnect?: boolean
}

export function connectSSE({
  url,
  params,
  onEvent,
  onError,
  reconnect = false,
}: SSEOptions): AbortController {
  const controller = new AbortController()

  const qs = params ? '?' + new URLSearchParams(params).toString() : ''
  // Bypass Vite dev proxy in development — it buffers SSE responses
  const baseUrl = import.meta.env.DEV ? 'http://localhost:8080' : ''
  const fullUrl = `${baseUrl}${url}${qs}`

  const es = new EventSource(fullUrl)

  es.onmessage = (e: MessageEvent) => {
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
      if (!controller.signal.aborted) {
        onError?.(new Error('SSE connection closed'))
      }
      return
    }
    if (!reconnect) {
      es.close()
      if (!controller.signal.aborted) {
        onError?.(new Error('SSE connection error'))
      }
    }
    // If reconnect is true, EventSource reconnects automatically
  }

  controller.signal.addEventListener('abort', () => {
    es.close()
  })

  return controller
}
