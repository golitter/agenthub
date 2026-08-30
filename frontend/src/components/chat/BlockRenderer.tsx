import { lazy, type ReactNode, Suspense } from 'react'

import { AttachmentCard } from '@/components/cards/AttachmentCard'
import { CoordChannel } from '@/components/cards/CoordChannel'
import { FinalSummaryCard } from '@/components/cards/FinalSummaryCard'
import { ImageCard } from '@/components/cards/ImageCard'
import { PlanCard } from '@/components/cards/PlanCard'
import { RuntimeStatus } from '@/components/cards/RuntimeStatus'
import { TaskFailureCard } from '@/components/cards/TaskFailureCard'
import { ToolCard } from '@/components/cards/ToolCard'
import { MarkdownRenderer } from '@/components/markdown/MarkdownRenderer'
import { ErrorBoundary } from '@/components/ui/error-boundary'
import type { AgentSessionInfo } from '@/lib/api'
import type { MessageBlock } from '@/lib/block-types'

import { AskAgentCard } from './AskAgentCard'

const DiffCard = lazy(() =>
  import('@/components/cards/DiffCard').then((module) => ({ default: module.DiffCard })),
)
const HtmlCard = lazy(() =>
  import('@/components/cards/HtmlCard').then((module) => ({ default: module.HtmlCard })),
)
const PlanReviewCard = lazy(() =>
  import('@/components/cards/PlanReviewCard').then((module) => ({
    default: module.PlanReviewCard,
  })),
)
const PreviewCard = lazy(() =>
  import('@/components/cards/PreviewCard').then((module) => ({ default: module.PreviewCard })),
)

function AsyncBlock({ children, className }: { children: ReactNode; className: string }) {
  return (
    <ErrorBoundary>
      <Suspense
        fallback={
          <div
            className={`flex ${className} items-center rounded-lg border border-border bg-muted/30 px-4 py-3 text-xs text-muted-foreground`}
            role="status"
            aria-busy="true"
          >
            <span className="h-3 w-32 rounded skeleton-sheen" aria-hidden="true" />
            <span className="sr-only">正在载入结构化内容</span>
          </div>
        }
      >
        {children}
      </Suspense>
    </ErrorBoundary>
  )
}

export function BlockRenderer({
  block,
  taskId,
  sessionId,
  agentSessionLookup,
  expandedPreview,
  interactive,
}: {
  block: MessageBlock
  taskId?: string
  sessionId?: string
  agentSessionLookup?: Map<string, AgentSessionInfo>
  expandedPreview?: boolean
  interactive?: boolean
}) {
  switch (block.type) {
    case 'text':
      return <MarkdownRenderer content={block.content} />
    case 'html-render':
      return (
        <AsyncBlock className="my-2 min-h-64">
          <HtmlCard
            content={block.content}
            resourceId={block.resourceId}
            expanded={expandedPreview}
            streaming={block.streaming}
          />
        </AsyncBlock>
      )
    case 'image':
      return <ImageCard path={block.path} sessionId={sessionId} />
    case 'attachment':
      return <AttachmentCard path={block.path} sessionId={sessionId} />
    case 'diff':
      return (
        <AsyncBlock className="my-2 min-h-[18rem]">
          <DiffCard snapshotId={block.snapshotId} sessionId={sessionId} />
        </AsyncBlock>
      )
    case 'preview':
      return (
        <AsyncBlock className="my-2 min-h-64">
          <PreviewCard url={block.url} />
        </AsyncBlock>
      )
    case 'plan':
      return <PlanCard overview={block.overview} tasks={block.tasks} />
    case 'plan_review':
      return (
        <AsyncBlock className="my-2 min-h-[18rem]">
          <PlanReviewCard
            reviewKey={block.review_key}
            taskId={block.task_id ?? taskId}
            sessionId={block.session_id ?? sessionId}
            reviewType={block.review_type}
            sourceBranch={block.source_branch}
            targetBranch={block.target_branch}
            diffSnapshotId={block.diff_snapshot_id}
            overview={block.overview}
            tasks={block.tasks}
            waves={block.waves}
            status={block.status}
            interactive={interactive}
          />
        </AsyncBlock>
      )
    case 'runtime_status':
      return (
        <RuntimeStatus
          task_id={taskId ?? block.task_id}
          session_id={sessionId}
          conflict_id={block.conflict_id}
          conflict_files={block.conflict_files}
          attempt={block.attempt}
          error_message={block.error_message}
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
