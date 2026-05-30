import { useCallback, useMemo, useState } from 'react'

import type { AgentType } from '@/generated/request'
import { useChatStream } from '@/hooks/use-chat-stream'
import { useConversations } from '@/hooks/use-conversations'
import { type AgentSessionInfo, getTaskMessages, validateRepoPath } from '@/lib/api'
import { AGENT_NAMES } from '@/lib/constants'
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

export function ChatArea({
  taskId,
  sessionId,
  agentType = 'claude-code',
  agentName,
  avatarUrl,
  repoPath,
  isGroupChat,
  groupTitle,
  groupAgentTypes,
  groupAgentNames,
  groupSessions,
}: ChatAreaProps) {
  const { state, sendMessage } = useChatStream(taskId, sessionId, agentType)
  const isStreaming = ['loading', 'streaming', 'tool_running'].includes(state.status)
  const [validationError, setValidationError] = useState<string | null>(null)
  const [validating, setValidating] = useState(false)
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
      const res = await getTaskMessages(taskId, { limit: 20, before: firstMsg.dbId, sessionId })
      const chatMessages: ChatMessage[] = res.data.map((m) => ({
        id: `${m.role}-${m.id}`,
        dbId: m.id,
        role: m.role as 'user' | 'agent',
        content: m.content,
        agentType: m.agent_type as AgentType | undefined,
        sessionId: m.session_id || undefined,
        timestamp: new Date(m.created_at).getTime(),
        messageId: m.message_id,
        status: m.status,
      }))
      prependMessages(sessionId, chatMessages, res.has_more)
    } catch {
      setLoadingMore(sessionId, false)
      setLoadError('加载历史消息失败')
    }
  }, [taskId, sessionId, state.messages, prependMessages, setLoadingMore])

  const agentSessionLookup = useMemo(() => {
    if (!groupSessions) return undefined
    const map = new Map<string, AgentSessionInfo>()
    for (const s of groupSessions) {
      map.set(s.agentName, s)
    }
    return map
  }, [groupSessions])

  const sendDisabledHint = useMemo(() => {
    if (!isStreaming) return undefined
    const taskSessions = conversations?.filter((c) => c.taskId === taskId) ?? []
    const streamingNames = taskSessions
      .filter((c) => {
        const s = getSession(c.sessionId)
        return ['loading', 'streaming', 'tool_running'].includes(s.status)
      })
      .map((c) => c.agentName ?? AGENT_NAMES[c.agentType] ?? c.agentType)
    if (streamingNames.length === 0) return undefined
    return `正在等待 ${streamingNames.join('、')} 回复中…`
  }, [isStreaming, conversations, taskId, getSession])

  const handleSend = async (message: string) => {
    setValidationError(null)

    if (repoPath) {
      setValidating(true)
      try {
        const result = await validateRepoPath(repoPath)
        if (!result.valid) {
          setValidationError(result.errors.join('; '))
          setValidating(false)
          return
        }
      } catch {
        setValidationError('路径校验失败，请检查 Agent 服务是否可用')
        setValidating(false)
        return
      }
      setValidating(false)
    }

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

      {/* Validation error banner */}
      {validationError && (
        <div className="shrink-0 bg-danger-bg px-4 py-2 text-xs text-destructive">
          {validationError}
          <button className="ml-2 underline" onClick={() => setValidationError(null)}>
            关闭
          </button>
        </div>
      )}

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
        disabled={validating}
        sendDisabled={isStreaming}
        sendDisabledHint={sendDisabledHint}
        placeholder={validating ? '校验路径中...' : `发消息给 ${displayName}...`}
      />
    </div>
  )
}
