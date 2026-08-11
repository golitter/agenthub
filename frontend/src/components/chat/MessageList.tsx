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

// 各 block 类型的基准高度估算，供 virtualizer 在真实测量前使用。
// 数值近似于渲染后的卡片高度（px）。
function estimateBlockHeight(block: MessageBlock): number {
  switch (block.type) {
    case 'plan':
      return 120 + block.tasks.length * 36
    case 'plan_review':
      return 400
    case 'diff':
      return 400
    case 'final_summary':
      return 300
    case 'html-render':
      return 256
    case 'preview':
      return 256
    case 'image':
      return 220
    case 'attachment':
      return 80
    case 'tool_call':
      return (block.input?.length ?? 0) > 200 ? 200 : 80
    case 'tool_result':
      return (block.output?.length ?? 0) > 200 ? 200 : 80
    case 'runtime_status':
      return 60
    case 'coordination':
      return 60 + block.messages.length * 40
    case 'ask_agent':
      return 120
    case 'task_failure':
      return 100
    case 'text':
      return block.content.length > 200 ? 200 : 80
  }
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
  // 在每次流式会话期间锁定流式消息的时间戳，避免时间分隔逻辑（5 分钟窗口）
  // 随 token 到达而闪烁。
  const streamingStartedAtRef = useRef<number | null>(null)
  if (isStreaming) {
    if (streamingStartedAtRef.current === null) streamingStartedAtRef.current = Date.now()
  } else {
    streamingStartedAtRef.current = null
  }
  const streamingStartedAt = streamingStartedAtRef.current ?? Date.now()

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
              timestamp: streamingStartedAt,
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
    // streamingStartedAt 被刻意排除：它派生自 ref，在流式会话期间保持不变，
    // 若将其纳入依赖会在每次渲染时重建此 useMemo，违背其用途。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [messages, isStreaming, streamingContent, runtimeBlocks, streamingAgentType])

  const useVirtual = displayItems.length > VIRTUALIZE_THRESHOLD

  // 用 ref 持有最新的 displayItems，供 scroll-request 事件 handler 读取，
  // 使该 handler 的 effect 不必依赖 displayItems（流式时每个 token 都会让
  // displayItems 产生新引用），避免频繁 add/removeEventListener。
  const displayItemsRef = useRef(displayItems)
  displayItemsRef.current = displayItems

  const virtualizer = useVirtualizer({
    count: displayItems.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => {
      const item = displayItems[index]
      if (!item) return 60
      if (item.type === 'time-divider') return 40
      const msg = item.msg
      // 结构化 block 与纯文本高度差异很大；为每个已知 block 类型给出
      // 一个贴近实际的基准值，使 virtualizer 在 measureElement 运行前的
      // 初始布局更接近真实情况。
      const blocks = msg.blocks
      if (blocks && blocks.length > 0) {
        let total = 0
        for (const block of blocks) {
          total += estimateBlockHeight(block)
        }
        // 文本内容可能与 block 并存；也要将其计入。
        if (msg.content.trim()) total += msg.content.length > 200 ? 200 : 80
        return total
      }
      return msg.content.length > 200 ? 200 : 80
    },
    overscan: 5,
    enabled: useVirtual,
  })
  // virtualizer 同样用 ref 持有，避免 scroll-request effect 依赖它而重订阅。
  const virtualizerRef = useRef(virtualizer)
  virtualizerRef.current = virtualizer

  useEffect(() => {
    const handleScrollRequest = (event: Event) => {
      const detail = (event as CustomEvent<{ sessionId?: string; messageId?: string }>).detail
      if (!detail || detail.sessionId !== sessionId || !detail.messageId) return

      // 通过 ref 读取最新的 displayItems / virtualizer，避免把它们放进
      // 依赖数组导致流式期间频繁重订阅。
      const items = displayItemsRef.current
      const targetIndex = items.findIndex(
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

      const virt = virtualizerRef.current
      if (virt && items.length > VIRTUALIZE_THRESHOLD) {
        virt.scrollToIndex(targetIndex, { align: 'center' })
        requestAnimationFrame(() => requestAnimationFrame(highlight))
      } else {
        highlight()
      }
    }

    window.addEventListener(MESSAGE_SCROLL_EVENT, handleScrollRequest)
    return () => window.removeEventListener(MESSAGE_SCROLL_EVENT, handleScrollRequest)
  }, [sessionId])

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
        <div className="mx-auto w-full max-w-[78rem] min-w-0 px-3 py-2 sm:px-6">
          <TimeDivider timestamp={item.timestamp} />
        </div>
      )
    }
    return (
      <div className="mx-auto w-full max-w-[78rem] min-w-0 px-3 py-2 sm:px-6">
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
    <div className="chat-canvas relative min-h-0 flex-1 overflow-hidden">
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
          className="absolute bottom-3 right-3 flex h-8 w-8 items-center justify-center rounded-lg border border-border/70 bg-card/95 shadow-[0_14px_32px_rgba(23,33,31,0.12)] transition-[background,transform,opacity] hover:bg-bg-hover active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring sm:bottom-4 sm:right-6"
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
