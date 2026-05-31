import { Maximize2 } from 'lucide-react'
import type { ReactNode } from 'react'
import { useState } from 'react'

import {
  AttachmentCard,
  CoordChannel,
  DiffCard,
  FinalSummaryCard,
  HtmlCard,
  ImageCard,
  PlanCard,
  PreviewCard,
  RuntimeStatus,
  TaskFailureCard,
  ToolCard,
} from '@/components/cards'
import { MarkdownRenderer } from '@/components/markdown/MarkdownRenderer'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'
import type { MessageBlock } from '@/lib/block-types'
import { AGENT_COLORS, AGENT_NAMES } from '@/lib/constants'
import { cn } from '@/lib/utils'
import { useAdminStore } from '@/stores/admin'

import { AgentHoverCard } from './AgentHoverCard'
import { AskAgentCard } from './AskAgentCard'

function BlockRenderer({
  block,
  sessionId,
  agentSessionLookup,
  expandedPreview,
}: {
  block: MessageBlock
  sessionId?: string
  agentSessionLookup?: Map<string, AgentSessionInfo>
  expandedPreview?: boolean
}) {
  switch (block.type) {
    case 'text':
      return <MarkdownRenderer content={block.content} />
    case 'html-render':
      return <HtmlCard content={block.content} expanded={expandedPreview} />
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
          title={block.title}
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
    case 'task_failure':
      return (
        <TaskFailureCard
          taskId={block.task_id}
          agent={block.agent}
          reason={block.reason}
          failureType={block.failureType}
        />
      )
    case 'final_summary':
      return (
        <FinalSummaryCard
          status={block.status}
          completed={block.completed}
          failed={block.failed}
          nextAction={block.nextAction}
          details={block.details}
        />
      )
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
  isLong?: boolean
  isStructured?: boolean
}

interface SystemBubbleProps extends BaseProps {
  variant: 'system'
}

type MessageBubbleProps = UserBubbleProps | AgentBubbleProps | SystemBubbleProps

const AGENT_TEXT_WIDTH = 'max-w-[min(68vw,38rem)]'
const AGENT_STRUCTURED_WIDTH = 'w-full max-w-[min(68vw,46rem)]'
const LONG_MESSAGE_PREVIEW_HEIGHT = 'h-[22rem]'

function AgentMessageContent({
  blocks,
  children,
  sessionId,
  agentSessionLookup,
  isStreaming,
  isLong,
  agentLabel,
  agentColor,
}: {
  blocks?: MessageBlock[]
  children?: ReactNode
  sessionId?: string
  agentSessionLookup?: Map<string, AgentSessionInfo>
  isStreaming?: boolean
  isLong?: boolean
  agentLabel?: string
  agentColor?: string
}) {
  const [zoomed, setZoomed] = useState(false)
  const hasBlocks = blocks && blocks.length > 0
  const showAgentLabel = Boolean(agentLabel && !hasBlocks)

  const renderContent = (expandedPreview = false) => (
    <div className="min-w-0 max-w-full space-y-3">
      {hasBlocks
        ? blocks!.map((block) => (
            <BlockRenderer
              key={block.id}
              block={block}
              sessionId={sessionId}
              agentSessionLookup={agentSessionLookup}
              expandedPreview={expandedPreview}
            />
          ))
        : children}
      {isStreaming && <span className="inline-block animate-pulse text-foreground">▌</span>}
    </div>
  )

  if (!isLong) {
    return (
      <>
        {showAgentLabel && (
          <div className="mb-2 flex min-w-0 items-center">
            <span
              className="max-w-full truncate rounded-full bg-accent px-2 py-0.5 text-[11px] font-medium"
              style={{ color: agentColor }}
            >
              @{agentLabel}
            </span>
          </div>
        )}
        {renderContent()}
      </>
    )
  }

  return (
    <>
      <div className="mb-2 flex min-w-0 items-center justify-between gap-2 border-b border-border/70 pb-2">
        {showAgentLabel ? (
          <span
            className="min-w-0 truncate rounded-full bg-accent px-2 py-0.5 text-[11px] font-medium"
            style={{ color: agentColor }}
          >
            @{agentLabel}
          </span>
        ) : (
          <span />
        )}
        <button
          type="button"
          className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          onClick={(event) => {
            event.stopPropagation()
            setZoomed(true)
          }}
          title="放大"
        >
          <Maximize2 className="h-4 w-4" strokeWidth={1.25} />
        </button>
      </div>

      <div
        role="button"
        tabIndex={0}
        className={cn(
          LONG_MESSAGE_PREVIEW_HEIGHT,
          'block w-full min-w-0 cursor-zoom-in overflow-y-auto overflow-x-hidden overscroll-contain rounded-md pr-2 text-left',
          'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
        )}
        onClick={() => setZoomed(true)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            setZoomed(true)
          }
        }}
        title="点击放大查看完整消息"
      >
        {renderContent()}
      </div>

      <Dialog open={zoomed} onOpenChange={setZoomed}>
        <DialogContent className="flex h-[min(86vh,900px)] max-h-[86vh] max-w-[min(92vw,1200px)] flex-col gap-0 overflow-hidden border-border bg-card p-0">
          <DialogTitle className="sr-only">消息详情</DialogTitle>
          <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-6 py-5 pr-8">
            {renderContent(true)}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

export function MessageBubble(props: MessageBubbleProps) {
  const adminAvatarUrl = useAdminStore((s) => s.adminAvatarUrl)

  if (props.variant === 'user') {
    return (
      <div className="flex max-w-full min-w-0 items-start justify-end gap-2.5">
        <div className="min-w-0 max-w-[min(80%,56rem)] overflow-hidden rounded-[10px] border border-primary-border bg-primary-soft px-4 py-3 text-sm [overflow-wrap:anywhere]">
          {props.children}
        </div>
        <img
          src={adminAvatarUrl}
          alt="我"
          className="mt-0.5 h-8 w-8 shrink-0 rounded-full object-cover"
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
        <div
          className={cn(
            bubbleWidth,
            'relative min-w-0 overflow-hidden rounded-[10px] bg-card px-4 py-3 text-sm [overflow-wrap:anywhere]',
          )}
        >
          <div
            className="absolute left-0 top-0 bottom-0 w-[3px] rounded-l-[10px]"
            style={{
              backgroundColor: AGENT_COLORS[props.agentType] ?? 'var(--primary)',
            }}
          />
          <AgentMessageContent
            blocks={props.blocks}
            sessionId={props.sessionId}
            agentSessionLookup={props.agentSessionLookup}
            isStreaming={props.isStreaming}
            isLong={props.isLong}
            agentLabel={agentLabel}
            agentColor={agentColor}
          >
            {props.children}
          </AgentMessageContent>
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
