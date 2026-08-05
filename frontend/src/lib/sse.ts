import type { StreamEvent } from '@/generated/events'

interface SSEOptions {
  url: string
  params?: Record<string, string>
  onEvent: (event: StreamEvent) => void
  onError?: (error: Error) => void
  /** 启用自动重连（EventSource 原生支持重连） */
  reconnect?: boolean
  /** 等待首次成功连接的最长时间（毫秒，默认 30s） */
  openTimeoutMs?: number
  /** 无任何事件的最长时间（毫秒），超过即视为流已死（默认 5min） */
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

  // 过时检查：若在 staleTimeoutMs 内未收到任何事件则关闭连接
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
    // 出错意味着流没有产出应用数据。如果把重连尝试也算作活动，
    // 会让已死的流永远存活并绕过 staleTimeoutMs。
    if (es.readyState === EventSource.CLOSED) {
      fail(new Error('SSE connection closed'))
      return
    }
    if (!reconnect) {
      fail(new Error('SSE connection error'))
    }
    // 如果 reconnect 为 true，EventSource 会自动重连
  }

  controller.signal.addEventListener('abort', () => {
    settled = true
    cleanup()
    es.close()
  })

  return controller
}
