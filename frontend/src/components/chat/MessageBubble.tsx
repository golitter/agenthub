import type { ReactNode } from 'react'

import { AttachmentCard, DiffCard, HtmlCard, ImageCard, PreviewCard } from '@/components/cards'
import { MarkdownRenderer } from '@/components/markdown/MarkdownRenderer'
import type { AgentType } from '@/generated/request'
import type { MessageBlock } from '@/lib/block-types'
import { AGENT_COLORS } from '@/lib/constants'

import { AgentAvatar } from './AgentAvatar'

function BlockRenderer({ block, sessionId }: { block: MessageBlock; sessionId?: string }) {
  switch (block.type) {
    case 'text':
      return <MarkdownRenderer content={block.content} />
    case 'html-render':
      return <HtmlCard content={block.content} />
    case 'image':
      return <ImageCard path={block.path} sessionId={sessionId} />
    case 'attachment':
      return <AttachmentCard path={block.path} sessionId={sessionId} />
    case 'diff':
      return <DiffCard snapshotId={block.snapshotId} sessionId={sessionId} />
    case 'preview':
      return <PreviewCard url={block.url} />
  }
}

interface BaseProps {
  children?: ReactNode
  blocks?: MessageBlock[]
  sessionId?: string
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
        <div className="max-w-[80%] rounded-[10px] border border-primary-border bg-primary-soft px-4 py-3 text-sm">
          {props.children}
        </div>
      </div>
    )
  }

  if (props.variant === 'agent') {
    const hasBlocks = props.blocks && props.blocks.length > 0

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
        <div className="relative max-w-[80%] rounded-[10px] bg-card px-4 py-3 text-sm">
          <div
            className="absolute left-0 top-0 bottom-0 w-[3px] rounded-l-[10px]"
            style={{
              backgroundColor: AGENT_COLORS[props.agentType] ?? 'var(--primary)',
            }}
          />
          <div>
            {hasBlocks
              ? props.blocks!.map((block) => (
                  <BlockRenderer key={block.id} block={block} sessionId={props.sessionId} />
                ))
              : props.children}
            {props.isStreaming && (
              <span className="inline-block animate-pulse text-primary">▌</span>
            )}
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
