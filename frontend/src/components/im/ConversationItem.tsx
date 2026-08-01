import { Pin } from 'lucide-react'

import { AgentAvatar } from '@/components/chat/AgentAvatar'
import { GroupAvatar } from '@/components/chat/GroupAvatar'
import type { Conversation } from '@/lib/api'
import { ACTIVE_STATUSES, AGENT_NAMES } from '@/lib/constants'
import { UI_CARD_STATUS, UI_LABELS, UI_MISC, UI_TIME } from '@/lib/ui-text'
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
  const isRunning = ACTIVE_STATUSES.has(conversation.status) || conversation.status === 'running'
  const memberCount = conversation.memberCount ?? conversation.groupAgentTypes?.length ?? 0
  const detailLabel = isGroup
    ? memberCount > 0
      ? `${memberCount} ${UI_MISC.AGENT_COUNT_SUFFIX}`
      : UI_LABELS.GROUP_CHAT
    : AGENT_NAMES[conversation.agentType] || conversation.agentType

  return (
    <button
      type="button"
      aria-current={isActive ? 'true' : undefined}
      className={cn(
        'group relative flex w-full items-center gap-3 rounded-xl border px-3 py-2.5 text-left transition-[background,border-color,box-shadow,transform] active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
        isActive
          ? 'border-primary-border bg-primary-soft shadow-[0_10px_24px_rgba(15,118,110,0.08)]'
          : 'border-transparent hover:border-border/70 hover:bg-accent',
      )}
      onClick={onClick}
    >
      {isActive && (
        <span
          className="absolute left-0 top-1/2 h-8 w-0.5 -translate-y-1/2 rounded-full bg-primary"
          aria-hidden="true"
        />
      )}
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
        <div className="flex items-center justify-between gap-2">
          <span
            className={cn(
              'truncate text-sm font-medium',
              isActive ? 'text-foreground' : 'text-text-secondary',
            )}
            title={displayName}
          >
            {displayName}
          </span>
          <span className="flex shrink-0 items-center gap-1.5">
            {isRunning && (
              <span className="inline-flex items-center gap-1 rounded-[5px] bg-warning/10 px-1.5 py-0.5 text-[10px] font-medium text-warning">
                <span className="h-1.5 w-1.5 rounded-full bg-warning animate-pulse" />
                {UI_CARD_STATUS.RUNNING}
              </span>
            )}
            {conversation.pinnedAt && (
              <Pin
                className="h-3 w-3 shrink-0 -rotate-45 text-primary"
                strokeWidth={1.25}
                aria-label={UI_LABELS.PIN_CHAT}
              />
            )}
            <time className="text-[11px] text-tertiary" dateTime={conversation.lastActiveAt}>
              {relativeTime(conversation.lastActiveAt)}
            </time>
          </span>
        </div>
        <p className="mt-0.5 truncate text-xs text-tertiary" title={conversation.taskTitle}>
          {conversation.taskTitle}
        </p>
        <p className="mt-1 truncate font-mono text-[10px] text-tertiary/80">{detailLabel}</p>
      </div>
    </button>
  )
}
