import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'

import type { StreamEvent } from '@/generated/events'
import { EventTypeValues } from '@/generated/events'
import type { AgentType } from '@/generated/request'
import { cancelAgentRun, type Conversation, getTaskMessages, submitMessage } from '@/lib/api'
import { MESSAGE_ROLES } from '@/lib/constants'
import {
  createConversationReconciler,
  getConversationStatusForStream,
  patchConversationForStream,
  queryKeys,
} from '@/lib/query-keys'
import { connectSSE } from '@/lib/sse'
import { UI_MESSAGES } from '@/lib/ui-text'
import { type ChatMessage, useChatStore } from '@/stores/chat'

// 重新导出 ChatMessage 供消费方使用
export type { ChatMessage }

const INITIAL_MESSAGE_LIMIT = 60

function isActiveChatStatus(status: string): boolean {
  return status === 'loading' || status === 'streaming' || status === 'tool_running'
}

function runtimeIdentity(content: Record<string, unknown> | undefined) {
  const conflictFiles = Array.isArray(content?.conflict_files)
    ? (content?.conflict_files as string[])
    : undefined
  return {
    plan_task_id: content?.plan_task_id as string | undefined,
    integration_operation_id: content?.integration_operation_id as string | undefined,
    run_id: content?.run_id as string | undefined,
    attempt: typeof content?.attempt === 'number' ? content.attempt : undefined,
    conflict_id: content?.conflict_id as string | undefined,
    conflict_files: conflictFiles,
    error_code: content?.error_code as string | undefined,
    error_message: content?.error_message as string | undefined,
  }
}

export function useChatStream(
  taskId: string,
  sessionId: string,
  agentType: AgentType = 'claude-code',
  options: { includeTaskMessages?: boolean } = {},
) {
  const store = useChatStore()
  const queryClient = useQueryClient()
  const abortRef = useRef<AbortController | null>(null)
  const mountedRef = useRef(true)
  const sendRequestRef = useRef(0)
  const activeMessageIdRef = useRef<string | null>(null)
  const lifecycleGenerationRef = useRef(0)
  const conversationActivityGenerationRef = useRef(0)
  const conversationReconcilerRef = useRef<ReturnType<typeof createConversationReconciler> | null>(
    null,
  )
  const [isCancelling, setIsCancelling] = useState(false)
  const [hasActiveRun, setHasActiveRun] = useState(false)
  const [historyRetryKey, setHistoryRetryKey] = useState(0)
  const [historyErrorState, setHistoryError] = useState<{
    key: string
    error: Error
  } | null>(null)
  const session = store.getSession(sessionId)
  const historyRequestKey = `${taskId}:${sessionId}:${options.includeTaskMessages ? 'group' : 'session'}:${historyRetryKey}`

  const reconcileConversationsOnce = useCallback(() => {
    conversationReconcilerRef.current?.invalidateOnce()
  }, [])

  const markConversationActive = useCallback(
    async (
      streamSessionId = sessionId,
      activityGeneration = conversationActivityGenerationRef.current,
      conversationStatus = 'running',
    ) => {
      const lifecycleGeneration = lifecycleGenerationRef.current
      // A terminal reconciliation may still be fetching when the user starts
      // the next run. Cancel that stale response before projecting running,
      // otherwise it can overwrite the optimistic state for the new run. Do
      // not cancel an initial fetch or a fetch without this cached row: the
      // sidebar still needs that request to populate its conversation list.
      const conversationQuery = queryClient.getQueryState<Conversation[]>(queryKeys.conversations)
      const hasCachedTarget = conversationQuery?.data?.some(
        (conversation) =>
          conversation.taskId === taskId &&
          (conversation.sessionId === sessionId ||
            Boolean(
              streamSessionId &&
              conversation.groupSessions?.some(
                (groupSession) => groupSession.sessionId === streamSessionId,
              ),
            )),
      )
      if (conversationQuery?.fetchStatus === 'fetching' && hasCachedTarget) {
        // cancelQueries 默认会在 promise 完成时恢复取消前快照；必须等待它
        // 完成后再写入 running，否则恢复动作可能覆盖下面的乐观状态。
        await queryClient.cancelQueries({ queryKey: queryKeys.conversations })
      }
      if (
        !mountedRef.current ||
        lifecycleGenerationRef.current !== lifecycleGeneration ||
        conversationActivityGenerationRef.current !== activityGeneration
      ) {
        return
      }
      queryClient.setQueryData<Conversation[]>(queryKeys.conversations, (current) =>
        patchConversationForStream(
          current,
          { taskId, primarySessionId: sessionId, streamSessionId },
          { status: conversationStatus, lastActiveAt: new Date().toISOString() },
        ),
      )
    },
    [queryClient, sessionId, taskId],
  )

  const resetConversationReconciler = useCallback(() => {
    conversationReconcilerRef.current = createConversationReconciler(() => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.conversations })
    })
  }, [queryClient])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  // 同一个 ChatArea 实例在用户切换会话时可能接收到不同的 session。
  // 在新 session 启动其历史记录/重连 effect 之前，停止旧的流并使进行中的
  // submit 失效。
  //
  // 该 effect 的 cleanup 在每次 deps 变化（含真正的组件卸载）时都会用
  // **上一次渲染的 sessionId 闭包**执行，因此会话切换路径上（A→B→C）
  // 每一个中间会话都会被正确清理。这里不再额外保留 mount-only 的清理
  // effect，避免卸载时与 deps effect 重复调用 clearActiveStream。
  useEffect(() => {
    return () => {
      lifecycleGenerationRef.current += 1
      conversationActivityGenerationRef.current += 1
      sendRequestRef.current += 1
      abortRef.current?.abort()
      abortRef.current = null
      activeMessageIdRef.current = null
      setIsCancelling(false)
      setHasActiveRun(false)
      // 中断路径不会经过 streamError，因此需要清除残留的本地流式执行态，
      // 否则它会阻止下次挂载时按服务端历史重连。
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
      conversationStatus = 'running',
    ) => {
      if (!mountedRef.current) return
      const activityGeneration = ++conversationActivityGenerationRef.current
      activeMessageIdRef.current = messageId
      abortRef.current?.abort()
      setIsCancelling(false)
      resetConversationReconciler()
      void markConversationActive(streamSessionId, activityGeneration, conversationStatus)

      store.streamStart(sessionId, streamAgentType)

      let streamController: AbortController | null = null
      const isCurrentStream = () =>
        streamController !== null && abortRef.current === streamController
      const closeCurrentStream = () => {
        if (!isCurrentStream()) return
        conversationActivityGenerationRef.current += 1
        streamController?.abort()
        abortRef.current = null
        activeMessageIdRef.current = null
        setIsCancelling(false)
        setHasActiveRun(false)
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
              const text = (event.content?.text as string) ?? ''
              if (groupId && textMessageId) {
                store.streamGroupedText(sessionId, {
                  text,
                  messageId: textMessageId,
                  groupId,
                  agentType: textAgentType,
                  agentName: textAgent,
                })
                break
              }
              if (textAgent && textAgentType) {
                store.streamAgentUpdate(sessionId, textAgentType, textAgent, textMessageId, groupId)
              }
              store.streamText(sessionId, text, textMessageId)
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
              reconcileConversationsOnce()
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
              reconcileConversationsOnce()
              closeCurrentStream()
              break
            case EventTypeValues.Heartbeat:
              break
            case EventTypeValues.RuntimeExecuting:
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                ...runtimeIdentity(event.content),
                agent: (event.content?.agent as string) ?? '',
                title: event.content?.title as string | undefined,
                status: 'running',
              })
              break
            case EventTypeValues.RuntimeCompleted: {
              const success = event.content?.success ?? false
              const runtimeMessageId = event.content?.message_id as string | undefined
              const runtimeGroupId = event.content?.group_id as string | undefined
              if (runtimeMessageId && runtimeGroupId) {
                store.streamGroupedMessageStatus(
                  sessionId,
                  runtimeMessageId,
                  success ? 'completed' : 'failed',
                )
              }
              const reportedStatus = event.content?.status as string | undefined
              const status = success
                ? 'completed'
                : reportedStatus === 'awaiting_user'
                  ? 'awaiting_user'
                  : reportedStatus === 'conflict'
                    ? 'conflict'
                    : reportedStatus === 'partial'
                      ? 'partial'
                      : reportedStatus === 'merged'
                        ? 'completed'
                        : reportedStatus === 'resolving' || reportedStatus === 'verifying'
                          ? reportedStatus
                          : 'failed'
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                ...runtimeIdentity(event.content),
                agent: (event.content?.agent as string) ?? '',
                status,
              })
              break
            }
            case EventTypeValues.RuntimeText: {
              store.streamRuntimeText(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                ...runtimeIdentity(event.content),
                agent: (event.content?.agent as string) ?? '',
                text: (event.content?.text as string) ?? '',
              })
              break
            }
            case EventTypeValues.IntegrationStarted:
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                ...runtimeIdentity(event.content),
                agent: (event.content?.agent as string) ?? '',
                status: 'integrating',
              })
              break
            case EventTypeValues.IntegrationCompleted: {
              const reportedStatus = event.content?.status as string | undefined
              const success = event.content?.success !== false && reportedStatus !== 'failed'
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                ...runtimeIdentity(event.content),
                agent: (event.content?.agent as string) ?? '',
                status: reportedStatus === 'partial' ? 'partial' : success ? 'completed' : 'failed',
              })
              break
            }
            case EventTypeValues.IntegrationConflict:
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                ...runtimeIdentity(event.content),
                agent: (event.content?.agent as string) ?? '',
                status: 'conflict',
                title: Array.isArray(event.content?.conflict_files)
                  ? `冲突：${(event.content.conflict_files as string[]).join(', ')}`
                  : undefined,
              })
              break
            case EventTypeValues.ResolutionStarted:
            case EventTypeValues.ResolutionProgress:
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                ...runtimeIdentity(event.content),
                agent: (event.content?.resolver_agent as string) ?? '',
                status: event.content?.status === 'verifying' ? 'verifying' : 'resolving',
              })
              break
            case EventTypeValues.ResolutionCompleted:
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                ...runtimeIdentity(event.content),
                agent: (event.content?.resolver_agent as string) ?? '',
                status: event.content?.status === 'partial' ? 'partial' : 'completed',
              })
              break
            case EventTypeValues.ResolutionFailed:
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? '',
                ...runtimeIdentity(event.content),
                agent: (event.content?.resolver_agent as string) ?? '',
                status: event.content?.status === 'awaiting_user' ? 'awaiting_user' : 'resolving',
                title: event.content?.error_message as string | undefined,
              })
              break
            case EventTypeValues.OrchestratorPaused:
              store.streamRuntimeEvent(sessionId, {
                task_id: (event.content?.task_id as string) ?? taskId,
                ...runtimeIdentity(event.content),
                agent: 'Orchestrator',
                status: 'awaiting_user',
                title: '自动冲突恢复已暂停，等待人工处理',
              })
              break
            case EventTypeValues.Planning: {
              const node = event.content?.node as string
              if (node === 'dispatch') {
                const dispatch = event.content?.dispatch as
                  { task_id?: string; agent?: string; content?: string } | undefined
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
          if (s.status !== 'done' && s.status !== 'idle' && s.status !== 'error') {
            store.streamError(sessionId, error)
            reconcileConversationsOnce()
          }
          // SSE 自身已经关闭，但 AbortController 仍需同步清理，尤其要解除
          // 取消按钮的 isCancelling 锁；否则下一轮流可能无法再次停止。
          closeCurrentStream()
        },
      })

      abortRef.current = streamController
      setHasActiveRun(true)
    },
    // 组合 store 的 action 引用稳定；getSession 通过 Zustand 的 get() 读取最新状态。
    // 不把每个 token 都会变化的组合 state 放进依赖，避免重建 connect 回调并重跑历史 effect。
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [
      agentType,
      markConversationActive,
      reconcileConversationsOnce,
      resetConversationReconciler,
      sessionId,
      taskId,
    ],
  )

  const sendMessage = useCallback(
    async (message: string, agentType: AgentType = 'claude-code') => {
      const requestId = ++sendRequestRef.current
      const activityGeneration = ++conversationActivityGenerationRef.current
      resetConversationReconciler()
      void markConversationActive(sessionId, activityGeneration)
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
        conversationActivityGenerationRef.current += 1
        setHasActiveRun(false)
        store.streamError(
          sessionId,
          err instanceof Error ? err : new Error(UI_MESSAGES.SEND_FAILED),
        )
        reconcileConversationsOnce()
      }
    },
    // store 是组合 Zustand store，引用会随 domain 状态镜像变化；这里使用其动作而不是
    // 将整份状态作为依赖，避免每个流式 token 重建发送回调。
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [
      connectToStream,
      markConversationActive,
      reconcileConversationsOnce,
      resetConversationReconciler,
      sessionId,
      taskId,
    ],
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
        if (cancelled) return
        // 组件可能在运行期间被卸载，离开后不会再收到 SSE 终态事件；
        // 历史加载完成时顺带对账一次列表，避免返回会话后仍显示旧的 running。
        void queryClient.invalidateQueries({ queryKey: queryKeys.conversations })
        if (res.data.length === 0) return
        const visibleRows = res.data
        const streaming = res.data.find(
          (m) =>
            m.role === 'agent' &&
            m.status === 'streaming' &&
            Boolean(m.message_id),
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
          const cachedConversation = queryClient
            .getQueryData<Conversation[]>(queryKeys.conversations)
            ?.find(
              (conversation) =>
                conversation.taskId === taskId &&
                (conversation.sessionId === sessionId ||
                  Boolean(
                    streaming.session_id &&
                      conversation.groupSessions?.some(
                        (groupSession) => groupSession.sessionId === streaming.session_id,
                      ),
                  )),
            )
          const reconnectStatus = getConversationStatusForStream(
            cachedConversation,
            streaming.session_id,
          )
          connectToStream(
            streaming.message_id,
            streaming.session_id || sessionId,
            (streaming.agent_type as AgentType | undefined) ?? agentType,
            reconnectStatus,
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
    queryClient,
  ])

  const abort = useCallback(() => {
    sendRequestRef.current += 1
    conversationActivityGenerationRef.current += 1
    abortRef.current?.abort()
    abortRef.current = null
    activeMessageIdRef.current = null
    setIsCancelling(false)
    setHasActiveRun(false)
    // 公开 abort() 也可能绕过 SSE 的终态回调；清除本地执行态后，下一次挂载
    // 才能依据服务端仍处于 streaming 的历史消息重新建立订阅。
    store.clearActiveStream(sessionId)
    // store 是组合 Zustand store，动作引用稳定；避免 token 更新导致回调重建。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId])

  const stopRun = useCallback(async () => {
    const messageId = activeMessageIdRef.current
    if (!messageId || isCancelling) return
    setIsCancelling(true)
    try {
      await cancelAgentRun(taskId, messageId)
      // Keep the SSE consumer connected until AgentEnd publishes the
      // structured cancellation terminal event. That event performs the
      // single reconciliation after the backend has persisted the terminal
      // session state; reconciling this 202 response can race that write.
    } catch (error) {
      // A late cancellation failure must not turn a newer run in the same
      // session into an error after this message has already been superseded.
      if (activeMessageIdRef.current !== messageId) return
      const currentStatus = store.getSession(sessionId).status
      conversationActivityGenerationRef.current += 1
      abortRef.current?.abort()
      abortRef.current = null
      activeMessageIdRef.current = null
      setIsCancelling(false)
      setHasActiveRun(false)
      if (isActiveChatStatus(currentStatus)) {
        store.streamError(sessionId, error instanceof Error ? error : new Error('停止任务失败'))
        reconcileConversationsOnce()
      }
    }
    // 组合 store 的 action 引用稳定；避免 token 更新导致停止回调重建。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isCancelling, reconcileConversationsOnce, sessionId, taskId])

  const retryHistory = useCallback(() => {
    setHistoryError(null)
    setHistoryRetryKey((key) => key + 1)
  }, [])

  return {
    state: session,
    sendMessage,
    abort,
    stopRun,
    isCancelling,
    canStop: hasActiveRun,
    historyError: historyErrorState?.key === historyRequestKey ? historyErrorState.error : null,
    retryHistory,
  }
}
