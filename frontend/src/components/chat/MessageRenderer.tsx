import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'
import type { MessageBlock } from '@/lib/block-types'
import { AGENT_NAMES, AGENT_TYPES, MESSAGE_ROLES } from '@/lib/constants'
import type { ChatMessage } from '@/stores/chat'

import { MarkdownRenderer } from '../markdown/MarkdownRenderer'
import { MessageBubble } from './MessageBubble'

interface MessageRendererProps {
  msg: ChatMessage
  isStreaming: boolean
  avatarUrl?: string
  agentName?: string
  taskId?: string
  sessionId?: string
  sessionAgentType?: AgentType
  agentSessionLookup?: Map<string, AgentSessionInfo>
  streamingAgentName?: string
}

const LONG_MESSAGE_CHARS = 4000
const LONG_MESSAGE_LINES = 80
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
      return false
    case 'coordination':
      return (
        block.messages.length > MANY_STRUCTURED_BLOCKS ||
        block.messages.some((m) => isLongText(m.text))
      )
    case 'plan':
      return block.tasks.length > MANY_STRUCTURED_BLOCKS + 2 || isLongText(block.overview)
    case 'plan_review':
      return false
    case 'ask_agent':
    case 'task_failure':
      return false
    case 'final_summary':
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

function isCompatibleSession(
  session: AgentSessionInfo | undefined,
  resolvedAgentType: AgentType,
): session is AgentSessionInfo {
  return Boolean(session && session.agentType === resolvedAgentType)
}

export function MessageRenderer({
  msg,
  isStreaming,
  avatarUrl,
  agentName,
  taskId,
  sessionId,
  sessionAgentType,
  agentSessionLookup,
  streamingAgentName,
}: MessageRendererProps) {
  if (msg.role === MESSAGE_ROLES.USER) {
    return (
      <MessageBubble variant="user">
        <MarkdownRenderer content={msg.content} />
      </MessageBubble>
    )
  }

  if (msg.role === MESSAGE_ROLES.AGENT) {
    const initialAgentName = isStreaming
      ? streamingAgentName || msg.agentName || agentName
      : msg.agentName || agentName

    const resolvedAgentType = msg.agentType ?? sessionAgentType ?? AGENT_TYPES.ClaudeCode

    const sessionById = msg.sessionId ? agentSessionLookup?.get(msg.sessionId) : undefined
    const sessionByName = initialAgentName ? agentSessionLookup?.get(initialAgentName) : undefined
    const sessionByType = agentSessionLookup?.get(resolvedAgentType)
    const sessionByDefaultName = agentSessionLookup?.get(
      AGENT_NAMES[resolvedAgentType] ?? resolvedAgentType,
    )

    const agentSession =
      (isCompatibleSession(sessionById, resolvedAgentType)
        ? sessionById
        : isCompatibleSession(sessionByName, resolvedAgentType)
          ? sessionByName
          : isCompatibleSession(sessionByType, resolvedAgentType)
            ? sessionByType
            : isCompatibleSession(sessionByDefaultName, resolvedAgentType)
              ? sessionByDefaultName
              : undefined) ??
      (msg.sessionId
        ? {
            sessionId: msg.sessionId,
            agentType: resolvedAgentType,
            agentName: initialAgentName ?? '',
            routeId: initialAgentName ?? resolvedAgentType,
            mentionLabel: initialAgentName ?? resolvedAgentType,
            avatarUrl: undefined,
          }
        : undefined)
    const displayAgentName =
      agentSession?.agentName ||
      (isTypeFallbackName(initialAgentName, resolvedAgentType)
        ? AGENT_NAMES[resolvedAgentType]
        : initialAgentName)
    const msgSessionId = agentSession?.sessionId ?? sessionId ?? ''
    const msgAvatarUrl =
      agentSession?.avatarUrl ?? (resolvedAgentType === sessionAgentType ? avatarUrl : undefined)

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
        taskId={taskId}
        sessionId={msgSessionId}
        agentSessionLookup={agentSessionLookup}
      >
        <MarkdownRenderer content={msg.content} />
      </MessageBubble>
    )
  }

  return <MessageBubble variant="system">{msg.content}</MessageBubble>
}
