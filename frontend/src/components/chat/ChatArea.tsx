import { ArrowLeft } from 'lucide-react'
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
}: ChatAreaProps) {
  const { state, sendMessage, historyError, retryHistory } = useChatStream(taskId, sessionId, agentType, {
    includeTaskMessages: Boolean(isGroupChat),
  })
  const isStreaming = ACTIVE_STATUSES.has(state.status)
  const [loadError, setLoadError] = useState<string | null>(null)

  const { data: conversations } = useConversations()
  const getSession = useChatStore((s) => s.getSession)
  const prependMessages = useChatStore((s) => s.prependMessages)
  const setLoadingMore = useChatStore((s) => s.setLoadingMore)

  const loadMoreMessages = useCallback(async () => {
    const firstMsg = state.messages[0]
    if (!firstMsg?.dbId) return
    setLoadingMore(sessionId, true)
    setLoadError(null)
    try {
      const res = await getTaskMessages(taskId, {
        limit: 20,
        before: firstMsg.dbId,
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
  }, [taskId, sessionId, isGroupChat, state.messages, prependMessages, setLoadingMore])

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
      map.set(s.sessionId, s)
      map.set(s.routeId, s)
      map.set(s.mentionLabel, s)
      map.set(s.agentName, s)
      map.set(s.agentType, s)
      map.set(AGENT_NAMES[s.agentType] ?? s.agentType, s)
      for (const alias of s.aliases ?? []) {
        map.set(alias, s)
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
    <div className="flex h-full flex-col bg-background">
      {/* Header */}
      <header className="flex min-h-14 shrink-0 items-center gap-3 border-b border-border bg-background/95 px-6 backdrop-blur">
        {onBack && (
          <button
            type="button"
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[background,color,transform] hover:bg-hover hover:text-foreground active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring md:hidden"
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
      </header>

      {/* Load error banner */}
      {(historyError || loadError) && (
        <div
          className="flex shrink-0 items-center justify-between gap-3 border-b border-destructive/20 bg-danger-bg px-4 py-2 text-xs text-destructive"
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

      {/* Messages */}
      {state.messages.length === 0 && !isStreaming ? (
        <div className="chat-canvas flex flex-1 flex-col items-center justify-center px-6 py-10 text-center">
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

      {/* Input */}
      <MessageInput
        onSend={handleSend}
        sendDisabled={isStreaming}
        sendDisabledHint={sendDisabledHint}
        placeholder={`${UI_PLACEHOLDERS.MESSAGE_TO} ${displayName}...`}
        mentionSessions={isGroupChat ? groupSessions : undefined}
      />
    </div>
  )
}
