import type { ReactNode } from 'react'

import type { AgentType } from '@/generated/request'

import { AgentAvatar } from './AgentAvatar'

interface BaseProps {
  children: ReactNode
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
}

interface SystemBubbleProps extends BaseProps {
  variant: 'system'
}

type MessageBubbleProps = UserBubbleProps | AgentBubbleProps | SystemBubbleProps

export function MessageBubble(props: MessageBubbleProps) {
  if (props.variant === 'user') {
    return (
      <div className="flex justify-end">
        <div
          className="max-w-[80%] rounded-[10px] border px-4 py-3 text-sm"
          style={{
            backgroundColor: 'rgba(99,102,241,0.08)',
            borderColor: 'rgba(99,102,241,0.15)',
          }}
        >
          {props.children}
        </div>
      </div>
    )
  }

  if (props.variant === 'agent') {
    return (
      <div className="flex gap-3">
        <div className="mt-1">
          <AgentAvatar
            agentType={props.agentType}
            status={props.status ?? 'offline'}
            avatarUrl={props.avatarUrl}
            agentName={props.agentName}
          />
        </div>
        <div
          className="relative max-w-[80%] rounded-[10px] px-4 py-3 text-sm"
          style={{ backgroundColor: 'var(--card)' }}
        >
          <div
            className="absolute left-0 top-0 bottom-0 w-[3px] rounded-l-[10px]"
            style={{
              backgroundColor: `var(--agent-${props.agentType === 'claude-code' ? 'claude' : props.agentType === 'opencode' ? 'opencode' : 'orchestrator'})`,
            }}
          />
          <div>
            {props.children}
            {props.isStreaming && <span className="inline-block animate-pulse text-brand">▌</span>}
          </div>
        </div>
      </div>
    )
  }

  // system
  return (
    <div className="flex justify-center">
      <p className="text-xs" style={{ color: 'var(--text-secondary)' }}>
        {props.children}
      </p>
    </div>
  )
}
