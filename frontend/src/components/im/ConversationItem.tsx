import { AgentAvatar } from '@/components/chat/AgentAvatar'
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
  const name =
    conversation.agentName || AGENT_NAMES[conversation.agentType] || conversation.agentType
  const hoverStyle = useHoverStyle()

  return (
    <button
      className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors duration-120 ease-out"
      style={{
        backgroundColor: isActive ? 'var(--bg-hover)' : 'transparent',
        borderLeft: isActive ? '2px solid var(--color-brand)' : '2px solid transparent',
      }}
      onClick={onClick}
      {...(!isActive && hoverStyle)}
    >
      <AgentAvatar
        agentType={conversation.agentType}
        status={conversation.status === 'running' ? 'running' : 'ready'}
        avatarUrl={conversation.avatarUrl}
        agentName={conversation.agentName || undefined}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between">
          <span
            className="truncate text-sm font-medium"
            style={{ color: isActive ? 'var(--text-primary)' : 'var(--text-secondary)' }}
          >
            {name}
          </span>
          <span className="shrink-0 text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
            {relativeTime(conversation.lastActiveAt)}
          </span>
        </div>
        <p className="mt-0.5 truncate text-xs" style={{ color: 'var(--text-tertiary)' }}>
          {conversation.taskTitle}
        </p>
      </div>
    </button>
  )
}
