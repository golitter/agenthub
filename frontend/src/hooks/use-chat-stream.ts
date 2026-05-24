import { useCallback, useEffect, useRef } from 'react'

import type { StreamEvent } from '@/generated/events'
import { EventTypeValues } from '@/generated/events'
import type { AgentType } from '@/generated/request'
import { getTaskMessages, submitMessage } from '@/lib/api'
import { connectSSE } from '@/lib/sse'
import { type ChatMessage, useChatStore } from '@/stores/chat'

// Re-export ChatMessage for consumers
export type { ChatMessage }

export function useChatStream(taskId: string, sessionId: string) {
  const store = useChatStore()
  const abortRef = useRef<AbortController | null>(null)
  const session = store.getSession(sessionId)

  const connectToStream = useCallback(
    (messageId: string) => {
      abortRef.current?.abort()

      store.streamStart(sessionId, 'claude-code')

      const controller = connectSSE({
        url: `/api/tasks/${taskId}/stream`,
        params: { session_id: sessionId, message_id: messageId },
        reconnect: true,
        onEvent: (event: StreamEvent) => {
          switch (event.type) {
            case EventTypeValues.Init:
              break
            case EventTypeValues.Text:
              store.streamText(sessionId, (event.content?.text as string) ?? '')
              break
            case EventTypeValues.ToolCall:
              store.streamToolCall(sessionId, (event.content?.name as string) ?? 'unknown')
              break
            case EventTypeValues.ToolResult:
              store.streamToolResult(sessionId)
              break
            case EventTypeValues.Done:
              store.streamDone(sessionId)
              break
            case EventTypeValues.Error:
              store.streamError(
                sessionId,
                new Error((event.content?.message as string) ?? 'Unknown error'),
              )
              break
          }
        },
        onError: (error) => {
          store.streamError(sessionId, error)
        },
      })

      abortRef.current = controller
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [taskId, sessionId],
  )

  const sendMessage = useCallback(
    async (message: string, agentType: AgentType = 'claude-code') => {
      const userMessage: ChatMessage = {
        id: `user-${Date.now()}`,
        role: 'user',
        content: message,
        timestamp: Date.now(),
      }

      store.sendMessage(sessionId, userMessage, {
        messageId: '',
        sessionId,
      })

      try {
        const result = await submitMessage(taskId, {
          message,
          session_id: sessionId,
          agent_type: agentType,
        })

        connectToStream(result.message_id)
      } catch (err) {
        store.streamError(sessionId, err instanceof Error ? err : new Error('发送失败'))
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [taskId, sessionId, connectToStream],
  )

  // Load history on mount; auto-reconnect if streaming message found
  useEffect(() => {
    let cancelled = false

    getTaskMessages(taskId)
      .then((msgs) => {
        if (cancelled || msgs.length === 0) return
        const chatMessages: ChatMessage[] = msgs.map((m) => ({
          id: `${m.role}-${m.id}`,
          role: m.role,
          content: m.content,
          agentType: m.agent_type as AgentType | undefined,
          timestamp: new Date(m.created_at).getTime(),
          messageId: m.message_id,
          status: m.status,
        }))
        store.loadHistory(sessionId, chatMessages)

        const streaming = msgs.find((m) => m.role === 'agent' && m.status === 'streaming')
        if (streaming && streaming.message_id) {
          connectToStream(streaming.message_id)
        }
      })
      .catch(() => {
        // silently ignore — empty state is fine
      })

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId, sessionId, connectToStream])

  const abort = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
  }, [])

  return { state: session, sendMessage, abort }
}
