import { AgentAvatar } from '@/components/chat/AgentAvatar'
import { GroupAvatar } from '@/components/chat/GroupAvatar'
import { useHoverStyle } from '@/hooks/use-hover-style'
import type { Conversation } from '@/lib/api'
import { AGENT_NAMES } from '@/lib/constants'

interface ConversationItemProps {
  conversation: Conversation
  isActive: boolean
  onClick: () => void
}

function relativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return '刚刚'
  if (mins < 60) return `${mins}分钟前`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}小时前`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days}天前`
  return new Date(dateStr).toLocaleDateString()
}

export function ConversationItem({ conversation, isActive, onClick }: ConversationItemProps) {
  const isGroup = !!conversation.isGroupChat
  const singleName =
    conversation.agentName || AGENT_NAMES[conversation.agentType] || conversation.agentType
  const displayName = isGroup ? conversation.title : singleName
  const hoverStyle = useHoverStyle()

  return (
    <button
      className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left"
      style={{
        backgroundColor: isActive ? 'var(--accent)' : 'transparent',
        borderLeft: isActive ? '2px solid var(--primary)' : '2px solid transparent',
      }}
      onClick={onClick}
      {...(!isActive && hoverStyle)}
    >
      {isGroup && conversation.groupAgentTypes && conversation.groupAgentNames ? (
        <GroupAvatar
          agentTypes={conversation.groupAgentTypes}
          agentNames={conversation.groupAgentNames}
        />
      ) : (
        <AgentAvatar
          agentType={conversation.agentType}
          status={conversation.status === 'running' ? 'running' : 'ready'}
          avatarUrl={conversation.avatarUrl}
          agentName={conversation.agentName || undefined}
          sessionId={conversation.sessionId}
        />
      )}
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between">
          <span
            className="truncate text-sm font-medium"
            style={{ color: isActive ? 'var(--foreground)' : 'var(--muted-foreground)' }}
          >
            {displayName}
          </span>
          <span className="shrink-0 text-[11px] text-tertiary">
            {relativeTime(conversation.lastActiveAt)}
          </span>
        </div>
        <p className="mt-0.5 truncate text-xs text-tertiary">{conversation.taskTitle}</p>
      </div>
    </button>
  )
}
