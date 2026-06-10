import {
  AttachmentCard,
  CoordChannel,
  DiffCard,
  FinalSummaryCard,
  HtmlCard,
  ImageCard,
  PlanCard,
  PlanReviewCard,
  PreviewCard,
  RuntimeStatus,
  TaskFailureCard,
  ToolCard,
} from '@/components/cards'
import { MarkdownRenderer } from '@/components/markdown/MarkdownRenderer'
import type { AgentSessionInfo } from '@/lib/api'
import type { MessageBlock } from '@/lib/block-types'

import { AskAgentCard } from './AskAgentCard'

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
        <HtmlCard content={block.content} expanded={expandedPreview} streaming={block.streaming} />
      )
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
    case 'plan_review':
      return (
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
      )
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
