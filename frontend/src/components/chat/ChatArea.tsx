import { useCallback, useMemo, useState } from 'react'

import type { AgentType } from '@/generated/request'
import { useChatStream } from '@/hooks/use-chat-stream'
import { useConversations } from '@/hooks/use-conversations'
import { type AgentSessionInfo, getTaskMessages, type TaskMessage } from '@/lib/api'
import { ACTIVE_STATUSES, AGENT_NAMES, AGENT_TYPES, MESSAGE_ROLES } from '@/lib/constants'
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
}

function isVisibleGroupMessage(message: TaskMessage, primarySessionId: string): boolean {
  if (message.role === MESSAGE_ROLES.USER) return true
  if (message.role !== MESSAGE_ROLES.AGENT) return false
  return message.session_id === primarySessionId
}

export function ChatArea({
  taskId,
  sessionId,
  agentType = AGENT_TYPES.ClaudeCode,
  agentName,
  avatarUrl,
  repoPath: _repoPath,
  isGroupChat,
  groupTitle,
  groupAgentTypes,
  groupAgentNames,
  groupSessions,
}: ChatAreaProps) {
  const { state, sendMessage } = useChatStream(taskId, sessionId, agentType, {
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
      const visibleRows = isGroupChat
        ? res.data.filter((m) => isVisibleGroupMessage(m, sessionId))
        : res.data
      const chatMessages: ChatMessage[] = visibleRows.map((m) => ({
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
      setLoadError('加载历史消息失败')
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
    return `正在等待 ${streamingNames.join('、')} 回复中…`
  }, [isStreaming, conversations, taskId, getSession])

  const handleSend = async (message: string) => {
    sendMessage(message, agentType)
  }

  const displayName = isGroupChat
    ? (groupTitle ?? '群聊')
    : (agentName ?? AGENT_NAMES[agentType] ?? agentType)

  return (
    <div className="flex h-full flex-col bg-background">
      {/* Header */}
      <div className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-6">
        {isGroupChat && groupAgentTypes && groupAgentNames ? (
          <GroupAvatar agentTypes={groupAgentTypes} agentNames={groupAgentNames} size={24} />
        ) : null}
        <h2 className="text-sm font-medium text-foreground">{displayName}</h2>
        {isStreaming && <p className="text-[11px] text-tertiary">正在回复...</p>}
      </div>

      {/* Load error banner */}
      {loadError && (
        <div className="shrink-0 bg-danger-bg px-4 py-2 text-xs text-destructive">
          {loadError}
          <button className="ml-2 underline" onClick={() => setLoadError(null)}>
            关闭
          </button>
        </div>
      )}

      {/* Messages */}
      {state.messages.length === 0 && !isStreaming ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2">
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
          <p className="mt-2 text-sm font-medium text-foreground">{displayName}</p>
          <p className="text-xs text-tertiary">发送消息开始对话</p>
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
        placeholder={`发消息给 ${displayName}...`}
        mentionSessions={isGroupChat ? groupSessions : undefined}
      />
    </div>
  )
}
