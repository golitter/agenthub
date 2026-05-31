import { useCallback, useEffect, useRef } from 'react'

import type { StreamEvent } from '@/generated/events'
import { EventTypeValues } from '@/generated/events'
import type { AgentType } from '@/generated/request'
import { getTaskMessages, submitMessage } from '@/lib/api'
import { connectSSE } from '@/lib/sse'
import { type ChatMessage, useChatStore } from '@/stores/chat'

// Re-export ChatMessage for consumers
export type { ChatMessage }

const INITIAL_MESSAGE_LIMIT = 60

export function useChatStream(
  taskId: string,
  sessionId: string,
  agentType: AgentType = 'claude-code',
) {
  const store = useChatStore()
  const abortRef = useRef<AbortController | null>(null)
  const session = store.getSession(sessionId)

  const connectToStream = useCallback(
    (messageId: string) => {
      abortRef.current?.abort()

      store.streamStart(sessionId, agentType)

      const controller = connectSSE({
        url: `/api/tasks/${taskId}/stream`,
        params: { session_id: sessionId, message_id: messageId },
        reconnect: true,
        onEvent: (event: StreamEvent) => {
          switch (event.type) {
            case EventTypeValues.Init:
              break
            case EventTypeValues.Text: {
              const textAgent = event.content?.agent as string | undefined
              const textAgentType = event.content?.agent_type as AgentType | undefined
              const textMessageId = event.content?.message_id as string | undefined
              if (textAgent && textAgentType) {
                store.streamAgentUpdate(sessionId, textAgentType, textAgent, textMessageId)
              }
              store.streamText(sessionId, (event.content?.text as string) ?? '')
              break
            }
            case EventTypeValues.ToolCall:
              store.streamToolCall(sessionId, (event.content?.name as string) ?? 'unknown')
              break
            case EventTypeValues.ToolResult:
              store.streamToolResult(sessionId)
              break
            case EventTypeValues.Done:
              store.streamDone(sessionId)
              // Close SSE connection to prevent auto-reconnect after stream ended
              abortRef.current?.abort()
              abortRef.current = null
              break
            case EventTypeValues.Error:
              store.streamError(
                sessionId,
                new Error(
                  (event.content?.error as string) ||
                    (event.content?.message as string) ||
                    'Unknown error',
                ),
              )
              abortRef.current?.abort()
              abortRef.current = null
              break
            case EventTypeValues.RuntimeExecuting:
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                agent: (event.content?.agent as string) ?? '',
                title: event.content?.title as string | undefined,
                status: 'running',
              })
              break
            case EventTypeValues.RuntimeCompleted: {
              const success = event.content?.success ?? false
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                agent: (event.content?.agent as string) ?? '',
                status: success ? 'completed' : 'failed',
              })
              break
            }
            case EventTypeValues.RuntimeText: {
              store.streamRuntimeText(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                agent: (event.content?.agent as string) ?? '',
                text: (event.content?.text as string) ?? '',
              })
              break
            }
            case EventTypeValues.Planning: {
              const node = event.content?.node as string
              if (node === 'dispatch') {
                const dispatch = event.content?.dispatch as
                  | { task_id?: string; agent?: string; content?: string }
                  | undefined
                if (dispatch) {
                  store.streamPlanEvent(
                    sessionId,
                    [
                      {
                        task_id: dispatch.task_id ?? '',
                        agent: dispatch.agent ?? '',
                        title: (dispatch.content ?? '').slice(0, 80),
                        status: 'pending',
                      },
                    ],
                    '',
                  )
                }
              }
              break
            }
            case EventTypeValues.CoordinationStart:
              // coordination channel opens — no action needed, messages will follow
              break
            case EventTypeValues.CoordinationMessage:
              store.streamCoordinationEvent(sessionId, {
                from: (event.content?.from as string) ?? '',
                to: (event.content?.to as string) ?? '',
                text: (event.content?.text as string) ?? '',
                round: (event.content?.round as number) ?? 1,
              })
              break
            case EventTypeValues.CoordinationDone: {
              const decisions = event.content?.decisions as string[] | undefined
              store.streamCoordinationDone(sessionId, decisions?.join('\n') ?? '')
              break
            }
            case EventTypeValues.AskCardStart:
              store.streamAskCardStart(sessionId, {
                question_id: (event.content?.question_id as string) ?? '',
                source_agent: event.content?.source_agent as string | undefined,
                source_agent_type: event.content?.source_agent_type as string | undefined,
                source_session_id: event.content?.source_session_id as string | undefined,
                target_agent: (event.content?.target_agent as string) ?? '',
                target_agent_type: event.content?.target_agent_type as string | undefined,
                target_session_id: (event.content?.target_session_id as string) ?? '',
                question: (event.content?.question as string) ?? '',
              })
              break
            case EventTypeValues.AskCardDone:
              store.streamAskCardDone(sessionId, {
                question_id: (event.content?.question_id as string) ?? '',
                source_agent: event.content?.source_agent as string | undefined,
                source_agent_type: event.content?.source_agent_type as string | undefined,
                source_session_id: event.content?.source_session_id as string | undefined,
                target_agent: event.content?.target_agent as string | undefined,
                target_agent_type: event.content?.target_agent_type as string | undefined,
                target_session_id: event.content?.target_session_id as string | undefined,
                question: event.content?.question as string | undefined,
                summary: event.content?.summary as string | undefined,
                status: event.content?.status as string | undefined,
              })
              break
            default:
              break
          }
        },
        onError: (error) => {
          // Don't overwrite done/idle state from connection close after stream ended
          const s = store.getSession(sessionId)
          if (s.status === 'done' || s.status === 'idle' || s.status === 'error') return
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

    getTaskMessages(taskId, { limit: INITIAL_MESSAGE_LIMIT, sessionId })
      .then((res) => {
        if (cancelled || res.data.length === 0) return
        const streaming = res.data.find((m) => m.role === 'agent' && m.status === 'streaming')
        const historyRows = streaming
          ? res.data.filter((m) => m.message_id !== streaming.message_id)
          : res.data
        const chatMessages: ChatMessage[] = historyRows.map((m) => ({
          id: `${m.role}-${m.id}`,
          dbId: m.id,
          role: m.role,
          content: m.content,
          agentType: m.agent_type as AgentType | undefined,
          agentName: m.agent_name || undefined,
          sessionId: m.session_id || undefined,
          timestamp: new Date(m.created_at).getTime(),
          messageId: m.message_id,
          status: m.status,
        }))
        store.loadHistory(sessionId, chatMessages, res.has_more)

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
