import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

const SCROLL_BOTTOM_THRESHOLD = 60

interface UseMessageScrollOptions {
  hasMore: boolean
  isLoadingMore: boolean
  onLoadMore: () => Promise<void>
  streamingContent: string
  messagesLength: number
  resetKey?: string
}

export function useMessageScroll(
  parentRef: React.RefObject<HTMLDivElement | null>,
  options: UseMessageScrollOptions,
) {
  const { hasMore, isLoadingMore, onLoadMore, streamingContent, messagesLength, resetKey } = options
  const [autoScroll, setAutoScroll] = useState(true)
  const loadingRef = useRef(false)
  const scrollRafRef = useRef<number | null>(null)
  const prevMsgLenRef = useRef(messagesLength)
  const resetKeyRef = useRef(resetKey)

  const scrollToBottom = useCallback(() => {
    if (!parentRef.current) return
    parentRef.current.scrollTop = parentRef.current.scrollHeight
  }, [parentRef])

  const scheduleScrollToBottom = useCallback(() => {
    if (scrollRafRef.current !== null) {
      cancelAnimationFrame(scrollRafRef.current)
    }
    scrollRafRef.current = requestAnimationFrame(() => {
      scrollRafRef.current = null
      scrollToBottom()
      requestAnimationFrame(() => {
        scrollToBottom()
      })
    })
  }, [scrollToBottom])

  useLayoutEffect(() => {
    if (resetKeyRef.current === resetKey) return
    resetKeyRef.current = resetKey
    setAutoScroll(true)
    scheduleScrollToBottom()
  }, [resetKey, scheduleScrollToBottom])

  // 初始加载或历史记录加载完成：在 DOM 布局后同步滚动到底部
  useLayoutEffect(() => {
    if (autoScroll) {
      scrollToBottom()
      scheduleScrollToBottom()
    }
    prevMsgLenRef.current = messagesLength
  }, [autoScroll, scrollToBottom, scheduleScrollToBottom, messagesLength])

  // 流式内容更新：使用 rAF 节流以保证渲染流畅
  useEffect(() => {
    if (!autoScroll || !streamingContent) return
    // 如果是初始加载（消息数量已变化）则跳过，上面已处理
    if (messagesLength !== prevMsgLenRef.current) return
    scheduleScrollToBottom()
  }, [autoScroll, streamingContent, messagesLength, scheduleScrollToBottom])

  useEffect(() => {
    return () => {
      if (scrollRafRef.current !== null) {
        cancelAnimationFrame(scrollRafRef.current)
      }
    }
  }, [])

  const handleScroll = useCallback(() => {
    const el = parentRef.current
    if (!el) return

    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < SCROLL_BOTTOM_THRESHOLD
    setAutoScroll(atBottom)

    if (el.scrollTop === 0 && hasMore && !isLoadingMore && !loadingRef.current) {
      const oldScrollHeight = el.scrollHeight
      const loadResetKey = resetKeyRef.current
      loadingRef.current = true
      onLoadMore().finally(() => {
        loadingRef.current = false
        if (resetKeyRef.current !== loadResetKey) return
        requestAnimationFrame(() => {
          if (parentRef.current) {
            parentRef.current.scrollTop = parentRef.current.scrollHeight - oldScrollHeight
          }
        })
      })
    }
  }, [hasMore, isLoadingMore, onLoadMore, parentRef])

  const enableAutoScroll = useCallback(() => setAutoScroll(true), [])

  return { autoScroll, handleScroll, scrollToBottom, enableAutoScroll }
}
