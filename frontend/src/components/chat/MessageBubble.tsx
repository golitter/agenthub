import type { ReactNode } from 'react'

import {
  AttachmentCard,
  CoordChannel,
  DiffCard,
  HtmlCard,
  ImageCard,
  PlanCard,
  PreviewCard,
  RuntimeStatus,
  ToolCard,
} from '@/components/cards'
import { MarkdownRenderer } from '@/components/markdown/MarkdownRenderer'
import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'
import type { MessageBlock } from '@/lib/block-types'
import { AGENT_COLORS } from '@/lib/constants'

import { AgentHoverCard } from './AgentHoverCard'
import { AskAgentCard } from './AskAgentCard'

function BlockRenderer({
  block,
  sessionId,
  agentSessionLookup,
}: {
  block: MessageBlock
  sessionId?: string
  agentSessionLookup?: Map<string, AgentSessionInfo>
}) {
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
    case 'plan':
      return <PlanCard overview={block.overview} tasks={block.tasks} />
    case 'runtime_status':
      return (
        <RuntimeStatus
          task_id={block.task_id}
          agent={block.agent}
          status={block.status}
          streamingText={block.streamingText}
        />
      )
    case 'coordination':
      return (
        <CoordChannel messages={block.messages} closed={block.closed} summary={block.summary} />
      )
    case 'ask_agent': {
      const sourceSession = block.source_agent
        ? agentSessionLookup?.get(block.source_agent)
        : undefined
      const targetSession = agentSessionLookup?.get(block.target_agent)
      return (
        <AskAgentCard
          questionId={block.question_id}
          sourceAgent={block.source_agent}
          sourceAgentType={sourceSession?.agentType ?? block.source_agent_type}
          sourceSessionId={sourceSession?.sessionId ?? block.source_session_id}
          sourceAvatarUrl={sourceSession?.avatarUrl}
          targetAgent={block.target_agent}
          targetAgentType={targetSession?.agentType ?? block.target_agent_type}
          targetSessionId={targetSession?.sessionId ?? block.target_session_id}
          targetAvatarUrl={targetSession?.avatarUrl}
          question={block.question}
          status={block.status}
          collapsed={block.collapsed}
          summary={block.summary}
        />
      )
    }
    case 'tool_call':
      return <ToolCard name={block.name} input={block.input} />
    case 'tool_result':
      return <ToolCard output={block.output} />
  }
}

interface BaseProps {
  children?: ReactNode
  blocks?: MessageBlock[]
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
}

interface SystemBubbleProps extends BaseProps {
  variant: 'system'
}

type MessageBubbleProps = UserBubbleProps | AgentBubbleProps | SystemBubbleProps

export function MessageBubble(props: MessageBubbleProps) {
  if (props.variant === 'user') {
    return (
      <div className="flex max-w-full min-w-0 items-start justify-end gap-2.5">
        <div className="min-w-0 max-w-[min(80%,56rem)] overflow-hidden rounded-[10px] border border-primary-border bg-primary-soft px-4 py-3 text-sm [overflow-wrap:anywhere]">
          {props.children}
        </div>
        <img
          src="https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede"
          alt="我"
          className="mt-0.5 h-8 w-8 shrink-0 rounded-full object-cover"
        />
      </div>
    )
  }

  if (props.variant === 'agent') {
    const hasBlocks = props.blocks && props.blocks.length > 0

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
        <div className="relative min-w-0 max-w-[min(80%,56rem)] overflow-hidden rounded-[10px] bg-card px-4 py-3 text-sm [overflow-wrap:anywhere]">
          <div
            className="absolute left-0 top-0 bottom-0 w-[3px] rounded-l-[10px]"
            style={{
              backgroundColor: AGENT_COLORS[props.agentType] ?? 'var(--primary)',
            }}
          />
          <div className="min-w-0 max-w-full space-y-3">
            {hasBlocks
              ? props.blocks!.map((block) => (
                  <BlockRenderer
                    key={block.id}
                    block={block}
                    sessionId={props.sessionId}
                    agentSessionLookup={props.agentSessionLookup}
                  />
                ))
              : props.children}
            {props.isStreaming && (
              <span className="inline-block animate-pulse text-foreground">▌</span>
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
