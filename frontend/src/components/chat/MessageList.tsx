import { useVirtualizer } from '@tanstack/react-virtual'
import { ArrowDown } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'

import type { AgentType } from '@/generated/request'
import type { ChatMessage } from '@/hooks/use-chat-stream'

import { MarkdownRenderer } from '../markdown/MarkdownRenderer'
import { MessageBubble } from './MessageBubble'

interface MessageListProps {
  messages: ChatMessage[]
  streamingContent: string
  streamingAgentType?: string
  isStreaming: boolean
  avatarUrl?: string
  agentName?: string
}

const VIRTUALIZE_THRESHOLD = 50

export function MessageList({
  messages,
  streamingContent,
  streamingAgentType,
  isStreaming,
  avatarUrl,
  agentName,
}: MessageListProps) {
  const parentRef = useRef<HTMLDivElement>(null)
  const [autoScroll, setAutoScroll] = useState(true)

  const allMessages =
    isStreaming && streamingContent
      ? [
          ...messages,
          {
            id: 'streaming',
            role: 'agent' as const,
            content: streamingContent,
            agentType: streamingAgentType as AgentType | undefined,
            timestamp: Date.now(),
          },
        ]
      : messages

  const useVirtual = allMessages.length > VIRTUALIZE_THRESHOLD

  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: allMessages.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => {
      const msg = allMessages[index]
      if (!msg) return 60
      return msg.content.length > 200 ? 200 : 80
    },
    overscan: 5,
    enabled: useVirtual,
  })

  const scrollToBottom = useCallback(() => {
    if (!parentRef.current) return
    parentRef.current.scrollTop = parentRef.current.scrollHeight
  }, [])

  useEffect(() => {
    if (autoScroll) {
      scrollToBottom()
    }
  }, [autoScroll, scrollToBottom, streamingContent, messages.length])

  const handleScroll = useCallback(() => {
    const el = parentRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60
    setAutoScroll(atBottom)
  }, [])

  return (
    <div className="relative flex-1 overflow-hidden">
      <div ref={parentRef} className="h-full overflow-y-auto" onScroll={handleScroll}>
        {useVirtual ? (
          <div
            style={{
              height: `${virtualizer.getTotalSize()}px`,
              width: '100%',
              position: 'relative',
            }}
          >
            {virtualizer.getVirtualItems().map((virtualRow) => {
              const msg = allMessages[virtualRow.index]
              if (!msg) return null
              return (
                <div
                  key={msg.id}
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
                  <div className="px-6 py-2">
                    <MessageRenderer
                      msg={msg}
                      isStreaming={isStreaming && msg.id === 'streaming'}
                      avatarUrl={avatarUrl}
                      agentName={agentName}
                    />
                  </div>
                </div>
              )
            })}
          </div>
        ) : (
          <div className="py-4">
            {allMessages.map((msg) => (
              <div key={msg.id} className="px-6 py-2">
                <MessageRenderer
                  msg={msg}
                  isStreaming={isStreaming && msg.id === 'streaming'}
                  avatarUrl={avatarUrl}
                  agentName={agentName}
                />
              </div>
            ))}
          </div>
        )}
      </div>

      {!autoScroll && (
        <button
          className="absolute bottom-4 right-6 flex h-8 w-8 items-center justify-center rounded-xl transition-colors"
          style={{ backgroundColor: 'var(--bg-hover)' }}
          onClick={() => {
            scrollToBottom()
            setAutoScroll(true)
          }}
        >
          <ArrowDown
            className="h-4 w-4"
            style={{ color: 'var(--text-secondary)' }}
            strokeWidth={1.25}
          />
        </button>
      )}
    </div>
  )
}

function MessageRenderer({
  msg,
  isStreaming,
  avatarUrl,
  agentName,
}: {
  msg: ChatMessage
  isStreaming: boolean
  avatarUrl?: string
  agentName?: string
}) {
  if (msg.role === 'user') {
    return <MessageBubble variant="user">{msg.content}</MessageBubble>
  }

  if (msg.role === 'agent') {
    return (
      <MessageBubble
        variant="agent"
        agentType={msg.agentType ?? 'claude-code'}
        avatarUrl={avatarUrl}
        agentName={agentName}
        status={isStreaming ? 'running' : 'ready'}
        isStreaming={isStreaming}
      >
        <MarkdownRenderer content={msg.content} />
      </MessageBubble>
    )
  }

  return <MessageBubble variant="system">{msg.content}</MessageBubble>
}
