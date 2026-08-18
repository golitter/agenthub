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

const AGENT_TEXT_WIDTH = 'max-w-[min(100%,38rem)]'
const AGENT_STRUCTURED_WIDTH = 'w-full max-w-[min(100%,46rem)]'

export function MessageBubble(props: MessageBubbleProps) {
  const adminAvatarUrl = useAdminStore((s) => s.adminAvatarUrl)

  if (props.variant === 'user') {
    return (
      <div className="group flex max-w-full min-w-0 items-end justify-end gap-2 pr-0.5 sm:gap-2.5">
        <div className="min-w-0 max-w-[min(86%,36rem)] overflow-hidden rounded-[15px] rounded-br-[4px] border border-primary/25 bg-primary/15 px-4 py-2.5 text-[13px] leading-5.5 text-foreground shadow-[0_8px_24px_rgba(15,118,110,0.09),inset_0_1px_0_rgba(255,255,255,0.04)] transition-[background-color,border-color] [overflow-wrap:anywhere] group-hover:border-primary/40 group-hover:bg-primary/18 sm:max-w-[min(74%,36rem)] [&_a]:text-primary [&_a:hover]:text-primary/80 [&_blockquote]:my-2 [&_blockquote]:border-l-primary-border [&_blockquote]:bg-primary-soft [&_code]:bg-primary/10 [&_ol]:my-1.5 [&_p]:m-0 [&_p+p]:mt-2 [&_pre]:my-2 [&_pre]:border-primary-border [&_pre]:bg-code-bg [&_ul]:my-1.5">
          {props.children}
        </div>
        <img
          src={adminAvatarUrl}
          alt={UI_MISC.ME}
          className="h-7 w-7 shrink-0 rounded-[10px] border-2 border-background bg-card object-cover shadow-[0_6px_18px_rgba(0,0,0,0.16)]"
          onError={(event) => {
            event.currentTarget.src = '/favicon.svg'
          }}
        />
      </div>
    )
  }

  if (props.variant === 'agent') {
    const bubbleWidth =
      props.isStructured || props.isLong ? AGENT_STRUCTURED_WIDTH : AGENT_TEXT_WIDTH
    const contentClassName = props.isStructured
      ? 'rounded-[12px] border border-border/60 bg-card/80 px-3 py-3 shadow-[0_14px_34px_rgba(0,0,0,0.10)] sm:px-4 sm:py-3.5'
      : 'rounded-r-[10px] border-l-2 border-primary/30 bg-card/25 py-1.5 pl-4 pr-3 transition-[background-color,border-color] hover:border-primary/45 hover:bg-card/40 [&_blockquote]:my-3 [&_li]:my-1 [&_ol]:my-2.5 [&_p]:my-2.5 [&_p:first-child]:mt-0 [&_p:last-child]:mb-0 [&_pre]:my-3 [&_ul]:my-2.5'
    const agentColor = AGENT_COLORS[props.agentType] ?? 'var(--primary)'
    const agentLabel = props.agentName || AGENT_NAMES[props.agentType] || props.agentType

    return (
      <div className="flex max-w-full min-w-0 gap-2 sm:gap-3">
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
          <div
            className={cn(
              'min-w-0 overflow-hidden text-sm leading-relaxed [overflow-wrap:anywhere]',
              contentClassName,
            )}
          >
            <AgentMessageContent
              blocks={props.blocks}
              taskId={props.taskId}
              sessionId={props.sessionId}
              agentSessionLookup={props.agentSessionLookup}
              isStreaming={props.isStreaming}
              isLong={props.isLong}
              // 规划审查在 SSE `done` 事件之后以及历史记录水合（hydration）之后
              // 仍然可操作。Store 层的 review key 是判断该卡片
              // 是否仍然有效的唯一来源。
              interactive
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
