import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'
import type { ChatMessage } from '@/stores/chat'

import { MarkdownRenderer } from '../markdown/MarkdownRenderer'
import { MessageBubble } from './MessageBubble'

interface MessageRendererProps {
  msg: ChatMessage
  isStreaming: boolean
  avatarUrl?: string
  agentName?: string
  sessionId?: string
  sessionAgentType?: AgentType
  agentSessionLookup?: Map<string, AgentSessionInfo>
  streamingAgentName?: string
}

export function MessageRenderer({
  msg,
  isStreaming,
  avatarUrl,
  agentName,
  sessionId,
  sessionAgentType,
  agentSessionLookup,
  streamingAgentName,
}: MessageRendererProps) {
  if (msg.role === 'user') {
    return <MessageBubble variant="user">{msg.content}</MessageBubble>
  }

  if (msg.role === 'agent') {
    const displayAgentName = isStreaming
      ? streamingAgentName || msg.agentName || agentName
      : msg.agentName || agentName

    const resolvedAgentType = msg.agentType ?? sessionAgentType ?? 'claude-code'

    const agentSession =
      agentSessionLookup?.get(displayAgentName ?? '') ??
      (msg.sessionId
        ? {
            sessionId: msg.sessionId,
            agentType: resolvedAgentType,
            agentName: displayAgentName ?? '',
          }
        : undefined)
    const msgSessionId = agentSession?.sessionId ?? sessionId ?? ''
    const msgAvatarUrl = agentSession?.avatarUrl ?? avatarUrl

    return (
      <MessageBubble
        variant="agent"
        agentType={resolvedAgentType}
        avatarUrl={msgAvatarUrl}
        agentName={displayAgentName}
        status={isStreaming ? 'running' : 'ready'}
        isStreaming={isStreaming}
        blocks={msg.blocks}
        sessionId={msgSessionId}
      >
        <MarkdownRenderer content={msg.content} />
      </MessageBubble>
    )
  }

  return <MessageBubble variant="system">{msg.content}</MessageBubble>
}
