import type { ReactNode } from 'react'

import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'
import type { MessageBlock } from '@/lib/block-types'
import { AGENT_COLORS, AGENT_NAMES } from '@/lib/constants'
import { UI_MISC } from '@/lib/ui-text'
import { cn } from '@/lib/utils'
import { useAdminStore } from '@/stores/admin'

import { AgentHoverCard } from './AgentHoverCard'
import { AgentMessageContent } from './AgentMessageContent'

interface BaseProps {
  children?: ReactNode
  blocks?: MessageBlock[]
  taskId?: string
  sessionId?: string
  agentSessionLookup?: Map<string, AgentSessionInfo>
}

interface UserBubbleProps extends BaseProps {
  variant: 'user'
}

interface AgentBubbleProps extends BaseProps {
  variant: 'agent'
  agentType: AgentType
  avatarUrl?: string
  agentName?: string
  status?: 'ready' | 'running' | 'offline' | 'error'
  isStreaming?: boolean
  isLong?: boolean
  isStructured?: boolean
}

interface SystemBubbleProps extends BaseProps {
  variant: 'system'
}

type MessageBubbleProps = UserBubbleProps | AgentBubbleProps | SystemBubbleProps

const AGENT_TEXT_WIDTH = 'max-w-[min(62vw,38rem)]'
const AGENT_STRUCTURED_WIDTH = 'w-full max-w-[min(68vw,46rem)]'

export function MessageBubble(props: MessageBubbleProps) {
  const adminAvatarUrl = useAdminStore((s) => s.adminAvatarUrl)

  if (props.variant === 'user') {
    return (
      <div className="flex max-w-full min-w-0 items-end justify-end gap-2.5">
        <div className="min-w-0 max-w-[min(72%,38rem)] overflow-hidden rounded-[16px] rounded-br-[5px] border border-primary-border bg-primary-soft px-3.5 py-2 text-sm leading-relaxed text-foreground shadow-[0_10px_28px_rgba(15,118,110,0.08)] [overflow-wrap:anywhere] [&_a]:text-primary [&_a:hover]:text-primary/80 [&_blockquote]:border-l-primary-border [&_blockquote]:bg-primary-soft [&_code]:bg-primary/10 [&_pre]:border-primary-border [&_pre]:bg-code-bg">
          {props.children}
        </div>
        <img
          src={adminAvatarUrl}
          alt={UI_MISC.ME}
          className="h-8 w-8 shrink-0 rounded-[10px] border border-border object-cover"
        />
      </div>
    )
  }

  if (props.variant === 'agent') {
    const bubbleWidth =
      props.isStructured || props.isLong ? AGENT_STRUCTURED_WIDTH : AGENT_TEXT_WIDTH
    const agentColor = AGENT_COLORS[props.agentType] ?? 'var(--primary)'
    const agentLabel = props.agentName || AGENT_NAMES[props.agentType] || props.agentType

    return (
      <div className="flex max-w-full min-w-0 gap-3">
        <div className="mt-1 shrink-0">
          <AgentHoverCard
            sessionId={props.sessionId ?? ''}
            agentType={props.agentType}
            agentName={props.agentName}
            avatarUrl={props.avatarUrl}
            status={props.status}
          />
        </div>
        <div className={cn(bubbleWidth, 'min-w-0')}>
          <div className="mb-2 flex min-w-0 items-center gap-2">
            <span className="truncate text-[13px] font-semibold text-foreground">{agentLabel}</span>
            <span
              className="shrink-0 rounded-[4px] px-1.5 py-0.5 font-mono text-[10px] font-medium"
              style={{
                color: agentColor,
                backgroundColor: `${agentColor}1A`,
              }}
            >
              {props.agentType}
            </span>
          </div>
          <div className="min-w-0 overflow-hidden rounded-[12px] border border-border/60 bg-card/80 px-4 py-3.5 text-sm leading-relaxed shadow-[0_14px_34px_rgba(0,0,0,0.10)] [overflow-wrap:anywhere]">
            <AgentMessageContent
              blocks={props.blocks}
              taskId={props.taskId}
              sessionId={props.sessionId}
              agentSessionLookup={props.agentSessionLookup}
              isStreaming={props.isStreaming}
              isLong={props.isLong}
              interactive={props.isStreaming}
              agentLabel={undefined}
              agentColor={agentColor}
            >
              {props.children}
            </AgentMessageContent>
          </div>
        </div>
      </div>
    )
  }

  // system
  return (
    <div className="flex justify-center">
      <p className="text-xs text-muted-foreground">{props.children}</p>
    </div>
  )
}
