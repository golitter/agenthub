import { Pin } from 'lucide-react'

import { AgentAvatar } from '@/components/chat/AgentAvatar'
import { GroupAvatar } from '@/components/chat/GroupAvatar'
import type { Conversation } from '@/lib/api'
import { AGENT_NAMES } from '@/lib/constants'
import { UI_TIME } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

interface ConversationItemProps {
  conversation: Conversation
  isActive: boolean
  onClick: () => void
}

function relativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return UI_TIME.JUST_NOW
  if (mins < 60) return `${mins}${UI_TIME.MINUTES_AGO}`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}${UI_TIME.HOURS_AGO}`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days}${UI_TIME.DAYS_AGO}`
  return new Date(dateStr).toLocaleDateString()
}

export function ConversationItem({ conversation, isActive, onClick }: ConversationItemProps) {
  const isGroup = !!conversation.isGroupChat
  const singleName =
    conversation.agentName || AGENT_NAMES[conversation.agentType] || conversation.agentType
  const displayName = isGroup ? conversation.title : singleName

  return (
    <button
      type="button"
      className={cn(
        'flex w-full items-center gap-3 rounded-xl border px-3 py-2.5 text-left transition-[background,border-color,transform] active:scale-[0.99]',
        isActive
          ? 'border-primary-border bg-primary-soft'
          : 'border-transparent hover:border-border/70 hover:bg-accent',
      )}
      onClick={onClick}
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
            className={cn(
              'truncate text-sm font-medium',
              isActive ? 'text-foreground' : 'text-text-secondary',
            )}
          >
            {displayName}
          </span>
          {conversation.pinnedAt && (
            <Pin className="h-3 w-3 shrink-0 -rotate-45 text-primary" strokeWidth={1.25} />
          )}
          <span className="shrink-0 text-[11px] text-tertiary">
            {relativeTime(conversation.lastActiveAt)}
          </span>
        </div>
        <p className="mt-0.5 truncate text-xs text-tertiary">{conversation.taskTitle}</p>
      </div>
    </button>
  )
}
