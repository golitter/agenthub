import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'
import type { MessageBlock } from '@/lib/block-types'
import { AGENT_NAMES } from '@/lib/constants'
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

const LONG_MESSAGE_CHARS = 1600
const LONG_MESSAGE_LINES = 28
const MANY_STRUCTURED_BLOCKS = 6

function isLongText(content: string): boolean {
  return content.length > LONG_MESSAGE_CHARS || content.split('\n').length > LONG_MESSAGE_LINES
}

function isLongBlock(block: MessageBlock): boolean {
  switch (block.type) {
    case 'text':
    case 'tool_result':
      return isLongText(block.type === 'text' ? block.content : (block.output ?? ''))
    case 'html-render':
    case 'diff':
      return true
    case 'runtime_status':
      return isLongText(block.streamingText ?? '')
    case 'coordination':
      return (
        block.messages.length > MANY_STRUCTURED_BLOCKS ||
        block.messages.some((m) => isLongText(m.text))
      )
    case 'plan':
      return block.tasks.length > MANY_STRUCTURED_BLOCKS || isLongText(block.overview)
    case 'ask_agent':
      return false
    case 'tool_call':
      return isLongText(block.input ?? '')
    case 'image':
    case 'attachment':
    case 'preview':
      return false
  }
}

function isLongMessage(msg: ChatMessage, isStreaming: boolean): boolean {
  if (isStreaming) return false
  if (msg.blocks?.length) {
    return msg.blocks.some(isLongBlock)
  }
  return isLongText(msg.content)
}

function isStructuredMessage(msg: ChatMessage): boolean {
  return Boolean(msg.blocks?.some((block) => block.type !== 'text'))
}

function isTypeFallbackName(name: string | undefined, agentType: AgentType): boolean {
  if (!name) return false
  return name === agentType || name === AGENT_NAMES[agentType]
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
    const initialAgentName = isStreaming
      ? streamingAgentName || msg.agentName || agentName
      : msg.agentName || agentName

    const resolvedAgentType = msg.agentType ?? sessionAgentType ?? 'claude-code'

    const agentSession =
      agentSessionLookup?.get(initialAgentName ?? '') ??
      agentSessionLookup?.get(resolvedAgentType) ??
      agentSessionLookup?.get(AGENT_NAMES[resolvedAgentType] ?? resolvedAgentType) ??
      (msg.sessionId
        ? {
            sessionId: msg.sessionId,
            agentType: resolvedAgentType,
            agentName: initialAgentName ?? '',
          }
        : undefined)
    const displayAgentName =
      agentSession?.agentName ||
      (isTypeFallbackName(initialAgentName, resolvedAgentType)
        ? AGENT_NAMES[resolvedAgentType]
        : initialAgentName)
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
        isLong={isLongMessage(msg, isStreaming)}
        isStructured={isStructuredMessage(msg)}
        blocks={msg.blocks}
        sessionId={msgSessionId}
        agentSessionLookup={agentSessionLookup}
      >
        <MarkdownRenderer content={msg.content} />
      </MessageBubble>
    )
  }

  return <MessageBubble variant="system">{msg.content}</MessageBubble>
}
