import { useCallback, useEffect, useRef, useState } from 'react'

import type { StreamEvent } from '@/generated/events'
import { EventTypeValues } from '@/generated/events'
import type { AgentType } from '@/generated/request'
import { getTaskMessages, submitMessage } from '@/lib/api'
import { MESSAGE_ROLES } from '@/lib/constants'
import { connectSSE } from '@/lib/sse'
import { UI_MESSAGES } from '@/lib/ui-text'
import { type ChatMessage, useChatStore } from '@/stores/chat'

// 重新导出 ChatMessage 供消费方使用
export type { ChatMessage }

const INITIAL_MESSAGE_LIMIT = 60

function isActiveChatStatus(status: string): boolean {
  return status === 'loading' || status === 'streaming' || status === 'tool_running'
}

export function useChatStream(
  taskId: string,
  sessionId: string,
  agentType: AgentType = 'claude-code',
  options: { includeTaskMessages?: boolean } = {},
) {
  const store = useChatStore()
  const abortRef = useRef<AbortController | null>(null)
  const mountedRef = useRef(true)
  const sendRequestRef = useRef(0)
  const [historyRetryKey, setHistoryRetryKey] = useState(0)
  const [historyErrorState, setHistoryError] = useState<{
    key: string
    error: Error
  } | null>(null)
  const session = store.getSession(sessionId)
  const historyRequestKey = `${taskId}:${sessionId}:${options.includeTaskMessages ? 'group' : 'session'}:${historyRetryKey}`

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      sendRequestRef.current += 1
      abortRef.current?.abort()
      abortRef.current = null
      // 中断路径不会经过 streamError，因此需要清除残留的 activeStream，
      // 否则它会阻止下次挂载时的历史记录重连。
      store.clearActiveStream(sessionId)
    }
    // 仅挂载时执行：清理逻辑在卸载时以初始 sessionId 运行；session 切换
    // 由下方的 effect 处理。store 是稳定的引用。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 同一个 ChatArea 实例在用户切换会话时可能接收到不同的 session。
  // 在新 session 启动其历史记录/重连 effect 之前，停止旧的流并使进行中的
  // submit 失效。
  useEffect(() => {
    return () => {
      sendRequestRef.current += 1
      abortRef.current?.abort()
      abortRef.current = null
      store.clearActiveStream(sessionId)
    }
    // store 是稳定的 Zustand store 引用。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId, sessionId, agentType])

  const connectToStream = useCallback(
    (
      messageId: string,
      streamSessionId: string = sessionId,
      streamAgentType: AgentType = agentType,
    ) => {
      if (!mountedRef.current) return
      abortRef.current?.abort()

      store.streamStart(sessionId, streamAgentType)

      let streamController: AbortController | null = null
      const isCurrentStream = () =>
        streamController !== null && abortRef.current === streamController
      const closeCurrentStream = () => {
        if (!isCurrentStream()) return
        streamController?.abort()
        abortRef.current = null
      }

      streamController = connectSSE({
        url: `/api/tasks/${encodeURIComponent(taskId)}/stream`,
        params: { session_id: streamSessionId, message_id: messageId },
        reconnect: true,
        onEvent: (event: StreamEvent) => {
          // EventSource 可能在 close() 之后投递已排队的事件。
          // 绝不让已过期的流修改当前 session 的状态。
          if (!isCurrentStream()) return
          switch (event.type) {
            case EventTypeValues.Init:
              break
            case EventTypeValues.Text: {
              const textAgent = event.content?.agent as string | undefined
              const textAgentType = event.content?.agent_type as AgentType | undefined
              const textMessageId = event.content?.message_id as string | undefined
              const groupId = event.content?.group_id as string | undefined
              if (textAgent && textAgentType) {
                store.streamAgentUpdate(sessionId, textAgentType, textAgent, textMessageId, groupId)
              }
              store.streamText(sessionId, (event.content?.text as string) ?? '', textMessageId)
              break
            }
            case EventTypeValues.ToolCall:
              store.streamToolCall(
                sessionId,
                (event.content?.tool as string | undefined) ??
                  (event.content?.name as string | undefined) ??
                  'unknown',
              )
              break
            case EventTypeValues.ToolResult:
              store.streamToolResult(sessionId)
              break
            case EventTypeValues.Done:
              store.streamDone(sessionId)
              // 关闭 SSE 连接，防止流结束后自动重连
              closeCurrentStream()
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
              closeCurrentStream()
              break
            case EventTypeValues.Heartbeat:
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
            case EventTypeValues.PlanReview: {
              const plan = (event.content?.plan ?? {}) as {
                overview?: string
                tasks?: Array<{
                  task_id?: string
                  session_id?: string
                  title?: string
                  content?: string
                }>
              }
              const rawWaves = event.content?.waves
              const waves = Array.isArray(rawWaves)
                ? rawWaves.map((wave) =>
                    Array.isArray(wave)
                      ? wave.map((task) => {
                          const item = task as {
                            task_id?: string
                            session_id?: string
                            agent?: string
                            title?: string
                            content?: string
                          }
                          return {
                            task_id: item.task_id ?? '',
                            session_id: item.session_id,
                            agent: item.agent ?? item.session_id ?? '',
                            title: item.title || (item.content ?? '').slice(0, 80),
                            content: item.content,
                            status: 'pending' as const,
                          }
                        })
                      : [],
                  )
                : []
              store.streamPlanReviewEvent(sessionId, {
                review_key:
                  (event.content?.review_key as string | undefined) ??
                  `${taskId}:${(event.content?.session_id as string | undefined) ?? sessionId}`,
                session_id: (event.content?.session_id as string | undefined) ?? sessionId,
                task_id: (event.content?.task_id as string | undefined) ?? taskId,
                review_type: event.content?.review_type as 'plan' | 'merge_to_main' | undefined,
                source_branch: event.content?.source_branch as string | undefined,
                target_branch: event.content?.target_branch as string | undefined,
                diff_snapshot_id: event.content?.diff_snapshot_id as string | undefined,
                overview: plan.overview ?? '',
                tasks: (plan.tasks ?? []).map((task) => ({
                  task_id: task.task_id ?? '',
                  session_id: task.session_id,
                  agent: task.session_id ?? '',
                  title: task.title || (task.content ?? '').slice(0, 80),
                  content: task.content,
                  status: 'pending',
                })),
                waves,
                status: 'pending',
              })
              break
            }
            case EventTypeValues.CoordinationStart:
              // coordination 通道开启 — 无需操作，消息会随后到来
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
                group_id: event.content?.group_id as string | undefined,
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
                group_id: event.content?.group_id as string | undefined,
              })
              break
            default:
              break
          }
        },
        onError: (error) => {
          if (!isCurrentStream()) return
          // 不要用流结束后连接关闭产生的状态覆盖 done/idle 状态
          const s = store.getSession(sessionId)
          if (s.status === 'done' || s.status === 'idle' || s.status === 'error') return
          store.streamError(sessionId, error)
        },
      })

      abortRef.current = streamController
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [taskId, sessionId, agentType],
  )

  const sendMessage = useCallback(
    async (message: string, agentType: AgentType = 'claude-code') => {
      const requestId = ++sendRequestRef.current
      const userMessage: ChatMessage = {
        id: `user-${Date.now()}`,
        role: MESSAGE_ROLES.USER,
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

        if (!mountedRef.current || requestId !== sendRequestRef.current) return
        connectToStream(
          result.message_id,
          result.session_id ?? sessionId,
          result.agent_type as AgentType,
        )
      } catch (err) {
        if (!mountedRef.current || requestId !== sendRequestRef.current) return
        store.streamError(
          sessionId,
          err instanceof Error ? err : new Error(UI_MESSAGES.SEND_FAILED),
        )
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [taskId, sessionId, connectToStream],
  )

  // 挂载时加载历史记录；若发现 streaming 消息则自动重连
  useEffect(() => {
    let cancelled = false

    getTaskMessages(taskId, {
      limit: INITIAL_MESSAGE_LIMIT,
      sessionId: options.includeTaskMessages ? undefined : sessionId,
      mode: options.includeTaskMessages ? 'group' : undefined,
      primarySessionId: options.includeTaskMessages ? sessionId : undefined,
    })
      .then((res) => {
        if (cancelled || res.data.length === 0) return
        const visibleRows = res.data
        const streaming = res.data.find(
          (m) =>
            m.role === 'agent' &&
            m.status === 'streaming' &&
            visibleRows.some((row) => row.message_id === m.message_id),
        )
        const historyRows = streaming
          ? visibleRows.filter((m) => m.message_id !== streaming.message_id)
          : visibleRows
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
          groupId: m.group_id,
          status: m.status,
        }))
        store.loadHistory(sessionId, chatMessages, res.has_more)

        const currentSession = store.getSession(sessionId)
        const hasCurrentWork =
          isActiveChatStatus(currentSession.status) || currentSession.activeStream !== null

        if (streaming && streaming.message_id && !hasCurrentWork) {
          connectToStream(
            streaming.message_id,
            streaming.session_id || sessionId,
            (streaming.agent_type as AgentType | undefined) ?? agentType,
          )
        }
      })
      .catch((error) => {
        if (cancelled) return
        setHistoryError({
          key: historyRequestKey,
          error: error instanceof Error ? error : new Error(UI_MESSAGES.LOAD_HISTORY_FAILED),
        })
      })

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    taskId,
    sessionId,
    agentType,
    options.includeTaskMessages,
    connectToStream,
    historyRequestKey,
  ])

  const abort = useCallback(() => {
    sendRequestRef.current += 1
    abortRef.current?.abort()
    abortRef.current = null
  }, [])

  const retryHistory = useCallback(() => {
    setHistoryError(null)
    setHistoryRetryKey((key) => key + 1)
  }, [])

  return {
    state: session,
    sendMessage,
    abort,
    historyError:
      historyErrorState?.key === historyRequestKey ? historyErrorState.error : null,
    retryHistory,
  }
}
