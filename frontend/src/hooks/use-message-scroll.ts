import { useCallback, useEffect, useRef, useState } from 'react'

const SCROLL_BOTTOM_THRESHOLD = 60

interface UseMessageScrollOptions {
  hasMore: boolean
  isLoadingMore: boolean
  onLoadMore: () => Promise<void>
  streamingContent: string
  messagesLength: number
}

export function useMessageScroll(
  parentRef: React.RefObject<HTMLDivElement | null>,
  options: UseMessageScrollOptions,
) {
  const { hasMore, isLoadingMore, onLoadMore, streamingContent, messagesLength } = options
  const [autoScroll, setAutoScroll] = useState(true)
  const loadingRef = useRef(false)
  const scrollRafRef = useRef<number | null>(null)

  const scrollToBottom = useCallback(() => {
    if (!parentRef.current) return
    parentRef.current.scrollTop = parentRef.current.scrollHeight
  }, [parentRef])

  const scheduleScrollToBottom = useCallback(() => {
    if (scrollRafRef.current !== null) return
    scrollRafRef.current = requestAnimationFrame(() => {
      scrollRafRef.current = null
      scrollToBottom()
    })
  }, [scrollToBottom])

  useEffect(() => {
    if (autoScroll) {
      scheduleScrollToBottom()
    }
  }, [autoScroll, scheduleScrollToBottom, streamingContent, messagesLength])

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
      loadingRef.current = true
      onLoadMore().finally(() => {
        loadingRef.current = false
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
