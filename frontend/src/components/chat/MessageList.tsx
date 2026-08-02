import { useVirtualizer } from '@tanstack/react-virtual'
import { ArrowDown, Loader2 } from 'lucide-react'
import { useEffect, useLayoutEffect, useMemo, useRef } from 'react'

import type { AgentType } from '@/generated/request'
import { useMessageScroll } from '@/hooks/use-message-scroll'
import type { AgentSessionInfo } from '@/lib/api'
import { coalesceMessageBlocks, reduceEventToBlocks } from '@/lib/block-reducer'
import type { MessageBlock } from '@/lib/block-types'
import { MESSAGE_ROLES } from '@/lib/constants'
import { UI_ACTIONS } from '@/lib/ui-text'
import type { ChatMessage } from '@/stores/chat'
import { shouldShowTimeSeparator } from '@/utils/time'

import { MessageRenderer } from './MessageRenderer'
import { TimeDivider } from './TimeDivider'

interface MessageListProps {
  messages: ChatMessage[]
  streamingContent: string
  streamingAgentType?: string
  streamingAgentName?: string
  runtimeBlocks: MessageBlock[]
  isStreaming: boolean
  avatarUrl?: string
  agentName?: string
  taskId?: string
  sessionId?: string
  sessionAgentType?: AgentType
  agentSessionLookup?: Map<string, AgentSessionInfo>
  hasMore: boolean
  isLoadingMore: boolean
  onLoadMore: () => Promise<void>
}

type DisplayItem =
  | { type: 'message'; msg: ChatMessage; isStreamingMsg: boolean }
  | { type: 'time-divider'; timestamp: number }

const VIRTUALIZE_THRESHOLD = 50
const MESSAGE_SCROLL_EVENT = 'agenthub:scroll-message'

function shouldRenderMessage(msg: ChatMessage): boolean {
  if (msg.role === MESSAGE_ROLES.USER) return true
  if (msg.blocks?.length) return true
  return Boolean(msg.content.trim())
}

export function MessageList({
  messages,
  streamingContent,
  streamingAgentType,
  streamingAgentName,
  runtimeBlocks,
  isStreaming,
  avatarUrl,
  agentName,
  taskId,
  sessionId,
  sessionAgentType,
  agentSessionLookup,
  hasMore,
  isLoadingMore,
  onLoadMore,
}: MessageListProps) {
  const parentRef = useRef<HTMLDivElement>(null)

  const { autoScroll, handleScroll, scrollToBottom, enableAutoScroll } = useMessageScroll(
    parentRef,
    {
      hasMore,
      isLoadingMore,
      onLoadMore,
      streamingContent,
      messagesLength: messages.length,
      resetKey: sessionId,
    },
  )

  const displayItems = useMemo<DisplayItem[]>(() => {
    const streamingBlocks = coalesceMessageBlocks([
      ...runtimeBlocks,
      ...(streamingContent ? reduceEventToBlocks(streamingContent) : []),
    ])
    const allMsgs =
      isStreaming && (streamingContent || runtimeBlocks.length > 0)
        ? [
            ...messages,
            {
              id: 'streaming',
              role: 'agent' as const,
              content: streamingContent,
              blocks: streamingBlocks,
              agentType: streamingAgentType as AgentType | undefined,
              timestamp: Date.now(),
            },
          ]
        : messages
    const visibleMsgs = allMsgs.filter(shouldRenderMessage)
    const items: DisplayItem[] = []
    for (let i = 0; i < visibleMsgs.length; i++) {
      const msg = visibleMsgs[i]
      const prevMsg = i > 0 ? visibleMsgs[i - 1] : undefined
      if (shouldShowTimeSeparator(prevMsg?.timestamp, msg.timestamp)) {
        items.push({ type: 'time-divider', timestamp: msg.timestamp })
      }
      items.push({
        type: 'message',
        msg,
        isStreamingMsg: isStreaming && msg.id === 'streaming',
      })
    }
    return items
  }, [messages, isStreaming, streamingContent, runtimeBlocks, streamingAgentType])

  const useVirtual = displayItems.length > VIRTUALIZE_THRESHOLD

  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: displayItems.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => {
      const item = displayItems[index]
      if (!item) return 60
      if (item.type === 'time-divider') return 40
      return item.msg.content.length > 200 ? 200 : 80
    },
    overscan: 5,
    enabled: useVirtual,
  })

  useEffect(() => {
    const handleScrollRequest = (event: Event) => {
      const detail = (event as CustomEvent<{ sessionId?: string; messageId?: string }>).detail
      if (!detail || detail.sessionId !== sessionId || !detail.messageId) return

      const targetIndex = displayItems.findIndex(
        (item) => item.type === 'message' && item.msg.id === detail.messageId,
      )
      if (targetIndex < 0) return

      const highlight = () => {
        const elements = parentRef.current?.querySelectorAll<HTMLElement>('[data-message-id]')
        const target = elements
          ? Array.from(elements).find((element) => element.dataset.messageId === detail.messageId)
          : undefined
        if (!target) return
        target.scrollIntoView({ behavior: 'smooth', block: 'center' })
        target.classList.add('animate-search-highlight')
        window.setTimeout(() => target.classList.remove('animate-search-highlight'), 800)
      }

      if (useVirtual) {
        virtualizer.scrollToIndex(targetIndex, { align: 'center' })
        requestAnimationFrame(() => requestAnimationFrame(highlight))
      } else {
        highlight()
      }
    }

    window.addEventListener(MESSAGE_SCROLL_EVENT, handleScrollRequest)
    return () => window.removeEventListener(MESSAGE_SCROLL_EVENT, handleScrollRequest)
  }, [displayItems, sessionId, useVirtual, virtualizer])

  useLayoutEffect(() => {
    if (!autoScroll || displayItems.length === 0) return
    if (useVirtual) {
      virtualizer.scrollToIndex(displayItems.length - 1, { align: 'end' })
      requestAnimationFrame(() => {
        virtualizer.scrollToIndex(displayItems.length - 1, { align: 'end' })
      })
      return
    }
    scrollToBottom()
  }, [autoScroll, displayItems.length, scrollToBottom, useVirtual, virtualizer])

  const renderItem = (item: DisplayItem) => {
    if (item.type === 'time-divider') {
      return (
        <div className="mx-auto w-full max-w-[78rem] min-w-0 px-6 py-2">
          <TimeDivider timestamp={item.timestamp} />
        </div>
      )
    }
    return (
      <div className="mx-auto w-full max-w-[78rem] min-w-0 px-6 py-2">
        <MessageRenderer
          msg={item.msg}
          isStreaming={item.isStreamingMsg}
          avatarUrl={avatarUrl}
          agentName={agentName}
          taskId={taskId}
          sessionId={sessionId}
          sessionAgentType={sessionAgentType}
          agentSessionLookup={agentSessionLookup}
          streamingAgentName={streamingAgentName}
        />
      </div>
    )
  }

  return (
    <div className="chat-canvas relative flex-1 overflow-hidden">
      {isLoadingMore && (
        <div className="absolute left-0 right-0 top-0 z-10 flex justify-center py-2">
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" strokeWidth={1.25} />
        </div>
      )}
      <div
        ref={parentRef}
        className="h-full overflow-x-hidden overflow-y-auto overscroll-contain"
        onScroll={handleScroll}
      >
        {useVirtual ? (
          <div
            style={{
              height: `${virtualizer.getTotalSize()}px`,
              width: '100%',
              position: 'relative',
            }}
          >
            {virtualizer.getVirtualItems().map((virtualRow) => {
              const item = displayItems[virtualRow.index]
              if (!item) return null
              return (
                <div
                  key={item.type === 'time-divider' ? `divider-${virtualRow.index}` : item.msg.id}
                  data-message-id={item.type === 'message' ? item.msg.id : undefined}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    width: '100%',
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                  ref={virtualizer.measureElement}
                  data-index={virtualRow.index}
                >
                  {renderItem(item)}
                </div>
              )
            })}
          </div>
        ) : (
          <div className="py-6">
            {displayItems.map((item, i) => {
              const key = item.type === 'time-divider' ? `divider-${i}` : item.msg.id
              return (
                <div key={key} data-message-id={item.type === 'message' ? item.msg.id : undefined}>
                  {renderItem(item)}
                </div>
              )
            })}
          </div>
        )}
      </div>

      {!autoScroll && (
        <button
          type="button"
          className="absolute bottom-4 right-6 flex h-8 w-8 items-center justify-center rounded-lg border border-border/70 bg-card/95 shadow-[0_14px_32px_rgba(23,33,31,0.12)] transition-[background,transform,opacity] hover:bg-hover active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          aria-label={UI_ACTIONS.SCROLL_TO_BOTTOM}
          title={UI_ACTIONS.SCROLL_TO_BOTTOM}
          onClick={() => {
            scrollToBottom()
            enableAutoScroll()
          }}
        >
          <ArrowDown className="h-4 w-4 text-muted-foreground" strokeWidth={1.25} />
        </button>
      )}
    </div>
  )
}
