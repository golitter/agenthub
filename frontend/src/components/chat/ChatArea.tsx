import { ArrowLeft, PanelRight } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'

import type { AgentType } from '@/generated/request'
import { useChatStream } from '@/hooks/use-chat-stream'
import { useConversations } from '@/hooks/use-conversations'
import { type AgentSessionInfo, getTaskMessages } from '@/lib/api'
import { ACTIVE_STATUSES, AGENT_NAMES, AGENT_TYPES } from '@/lib/constants'
import {
  UI_ACTIONS,
  UI_LABELS,
  UI_MESSAGES,
  UI_MISC,
  UI_PLACEHOLDERS,
  UI_STATUS,
} from '@/lib/ui-text'
import { type ChatMessage, useChatStore } from '@/stores/chat'

import { AgentAvatar } from './AgentAvatar'
import { GroupAvatar } from './GroupAvatar'
import { MessageInput } from './MessageInput'
import { MessageList } from './MessageList'

interface ChatAreaProps {
  taskId: string
  sessionId: string
  agentType?: AgentType
  agentName?: string
  avatarUrl?: string
  repoPath?: string
  isGroupChat?: boolean
  groupTitle?: string
  groupAgentTypes?: AgentType[]
  groupAgentNames?: string[]
  groupSessions?: AgentSessionInfo[]
  onBack?: () => void
  onOpenDetails?: () => void
}

export function ChatArea({
  taskId,
  sessionId,
  agentType = AGENT_TYPES.ClaudeCode,
  agentName,
  avatarUrl,
  repoPath,
  isGroupChat,
  groupTitle,
  groupAgentTypes,
  groupAgentNames,
  groupSessions,
  onBack,
  onOpenDetails,
}: ChatAreaProps) {
  const { state, sendMessage, historyError, retryHistory } = useChatStream(
    taskId,
    sessionId,
    agentType,
    {
      includeTaskMessages: Boolean(isGroupChat),
    },
  )
  const isStreaming = ACTIVE_STATUSES.has(state.status)
  const [loadError, setLoadError] = useState<string | null>(null)

  const { data: conversations } = useConversations()
  const getSession = useChatStore((s) => s.getSession)
  const prependMessages = useChatStore((s) => s.prependMessages)
  const setLoadingMore = useChatStore((s) => s.setLoadingMore)

  // 只需要第一条消息的 dbId 作为分页游标；单独提取它可以避免在每个
  // 流式 token 追加到 state.messages 时重建 loadMoreMessages
  // （及其滚动监听器）。
  const firstDbId = state.messages[0]?.dbId
  const loadMoreMessages = useCallback(async () => {
    if (!firstDbId) return
    setLoadingMore(sessionId, true)
    setLoadError(null)
    try {
      const res = await getTaskMessages(taskId, {
        limit: 20,
        before: firstDbId,
        sessionId: isGroupChat ? undefined : sessionId,
        mode: isGroupChat ? 'group' : undefined,
        primarySessionId: isGroupChat ? sessionId : undefined,
      })
      const chatMessages: ChatMessage[] = res.data.map((m) => ({
        id: `${m.role}-${m.id}`,
        dbId: m.id,
        role: m.role as 'user' | 'agent',
        content: m.content,
        agentType: m.agent_type as AgentType | undefined,
        agentName: m.agent_name || undefined,
        sessionId: m.session_id || undefined,
        timestamp: new Date(m.created_at).getTime(),
        messageId: m.message_id,
        groupId: m.group_id,
        status: m.status,
      }))
      prependMessages(sessionId, chatMessages, res.has_more)
    } catch {
      setLoadingMore(sessionId, false)
      setLoadError(UI_MESSAGES.LOAD_HISTORY_FAILED)
    }
  }, [taskId, sessionId, isGroupChat, firstDbId, prependMessages, setLoadingMore])

  const agentSessionLookup = useMemo(() => {
    const sessions = groupSessions?.length
      ? groupSessions
      : [
          {
            sessionId,
            agentType,
            agentName: agentName ?? AGENT_NAMES[agentType] ?? agentType,
            routeId: agentName ?? AGENT_NAMES[agentType] ?? agentType,
            mentionLabel: agentName ?? AGENT_NAMES[agentType] ?? agentType,
            avatarUrl,
          },
        ]
    const map = new Map<string, AgentSessionInfo>()
    for (const s of sessions) {
      // 仅以非空字符串建立查找索引，避免空 key（''）污染 Map，
      // 导致后续用空串查找时误命中错误的 session。
      const keys = [
        s.sessionId,
        s.routeId,
        s.mentionLabel,
        s.agentName,
        s.agentType,
        AGENT_NAMES[s.agentType] ?? s.agentType,
        ...(s.aliases ?? []),
      ]
      for (const k of keys) {
        if (k) map.set(k, s)
      }
    }
    return map
  }, [groupSessions, sessionId, agentType, agentName, avatarUrl])

  const sendDisabledHint = useMemo(() => {
    if (!isStreaming) return undefined
    const taskSessions = conversations?.filter((c) => c.taskId === taskId) ?? []
    const streamingNames = taskSessions
      .filter((c) => {
        const s = getSession(c.sessionId)
        return ACTIVE_STATUSES.has(s.status)
      })
      .map((c) => c.agentName ?? AGENT_NAMES[c.agentType] ?? c.agentType)
    if (streamingNames.length === 0) return undefined
    return `${UI_MISC.WAITING_REPLY} ${streamingNames.join('、')} ${UI_MISC.REPLYING}`
  }, [isStreaming, conversations, taskId, getSession])

  const handleSend = async (message: string) => {
    sendMessage(message, agentType)
  }

  const displayName = isGroupChat
    ? (groupTitle ?? UI_LABELS.GROUP_CHAT)
    : (agentName ?? AGENT_NAMES[agentType] ?? agentType)
  const memberCount = groupSessions?.length ?? groupAgentTypes?.length ?? 0
  const contextLabel = isGroupChat
    ? memberCount > 0
      ? `${memberCount} ${UI_MISC.AGENT_COUNT_SUFFIX}`
      : UI_LABELS.GROUP_CHAT
    : (AGENT_NAMES[agentType] ?? agentType)

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-background">
      {/* 头部 */}
      <header className="flex min-h-14 shrink-0 items-center gap-2 border-b border-border bg-background/95 px-3 backdrop-blur sm:gap-3 sm:px-6">
        {onBack && (
          <button
            type="button"
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring md:hidden"
            onClick={onBack}
            aria-label={UI_LABELS.BACK_TO_CONVERSATIONS}
            title={UI_LABELS.BACK_TO_CONVERSATIONS}
          >
            <ArrowLeft className="h-4 w-4" strokeWidth={1.5} aria-hidden="true" />
          </button>
        )}
        <div className="flex min-w-0 flex-1 items-center gap-3">
          {isGroupChat && groupAgentTypes && groupAgentNames ? (
            <GroupAvatar agentTypes={groupAgentTypes} agentNames={groupAgentNames} size={28} />
          ) : (
            <AgentAvatar
              agentType={agentType}
              status={isStreaming ? 'running' : 'ready'}
              size={28}
              avatarUrl={avatarUrl}
              agentName={agentName}
              sessionId={sessionId}
            />
          )}
          <div className="min-w-0">
            <h2 className="truncate text-sm font-semibold text-foreground">{displayName}</h2>
            <p className="truncate text-[11px] text-tertiary">
              {repoPath ? `${contextLabel} · ${repoPath}` : contextLabel}
            </p>
          </div>
        </div>
        {isStreaming && (
          <p
            className="inline-flex shrink-0 items-center gap-1.5 rounded-[7px] border border-success/25 bg-success/10 px-2 py-1 text-[11px] font-medium text-success"
            aria-live="polite"
          >
            <span className="h-1.5 w-1.5 rounded-full bg-success animate-pulse" />
            {UI_STATUS.STREAMING}
          </p>
        )}
        {onOpenDetails && (
          <button
            type="button"
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring xl:hidden"
            onClick={onOpenDetails}
            aria-label="打开会话详情"
            title="打开会话详情"
          >
            <PanelRight className="h-4 w-4" strokeWidth={1.5} aria-hidden="true" />
          </button>
        )}
      </header>

      {/* 加载错误提示条 */}
      {(historyError || loadError) && (
        <div
          className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-destructive/20 bg-danger-bg px-3 py-2 text-xs text-destructive sm:px-4"
          role="alert"
        >
          <span>{historyError ? UI_MESSAGES.LOAD_HISTORY_FAILED : loadError}</span>
          <div className="flex shrink-0 items-center gap-2">
            {historyError && (
              <button
                type="button"
                className="rounded-[5px] px-2 py-1 font-medium underline-offset-4 transition-colors hover:bg-destructive/10 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={retryHistory}
              >
                {UI_ACTIONS.RETRY}
              </button>
            )}
            {loadError && (
              <button
                type="button"
                className="rounded-[5px] px-2 py-1 underline-offset-4 transition-colors hover:bg-destructive/10 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => setLoadError(null)}
              >
                {UI_ACTIONS.CLOSE}
              </button>
            )}
          </div>
        </div>
      )}

      {/* 消息列表 */}
      {state.messages.length === 0 && !isStreaming ? (
        <div className="chat-canvas flex min-h-0 flex-1 flex-col items-center justify-center overflow-y-auto px-6 py-10 text-center">
          <div className="mb-4 flex h-16 w-16 items-center justify-center rounded-[16px] border border-border/70 bg-card/85 shadow-[0_18px_48px_rgba(23,33,31,0.10)]">
            {isGroupChat && groupAgentTypes && groupAgentNames ? (
              <GroupAvatar agentTypes={groupAgentTypes} agentNames={groupAgentNames} size={48} />
            ) : (
              <AgentAvatar
                agentType={agentType}
                status="ready"
                size={48}
                avatarUrl={avatarUrl}
                agentName={agentName}
                sessionId={sessionId}
              />
            )}
          </div>
          <p className="text-sm font-semibold text-foreground">{displayName}</p>
          <h3 className="mt-4 text-base font-semibold text-foreground text-balance">
            {UI_MESSAGES.CHAT_EMPTY_TITLE}
          </h3>
          <p className="mt-2 max-w-[26rem] text-sm leading-6 text-tertiary text-pretty">
            {UI_MESSAGES.CHAT_EMPTY_DESC}
          </p>
        </div>
      ) : (
        <MessageList
          messages={state.messages}
          streamingContent={state.streamingContent}
          streamingAgentType={state.streamingAgentType}
          streamingAgentName={state.streamingAgentName}
          runtimeBlocks={state.runtimeBlocks}
          isStreaming={isStreaming}
          avatarUrl={avatarUrl}
          agentName={agentName}
          taskId={taskId}
          sessionId={sessionId}
          sessionAgentType={agentType}
          agentSessionLookup={agentSessionLookup}
          hasMore={state.hasMore}
          isLoadingMore={state.isLoadingMore}
          onLoadMore={loadMoreMessages}
        />
      )}

      {/* 输入框 */}
      <div className="shrink-0">
        <MessageInput
          onSend={handleSend}
          sendDisabled={isStreaming}
          sendDisabledHint={sendDisabledHint}
          placeholder={`${UI_PLACEHOLDERS.MESSAGE_TO} ${displayName}...`}
          mentionSessions={isGroupChat ? groupSessions : undefined}
        />
      </div>
    </div>
  )
}
