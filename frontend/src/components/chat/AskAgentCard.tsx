import { AlertCircle, Check, ChevronDown, ChevronRight } from 'lucide-react'
import { useState } from 'react'

import type { AgentType } from '@/generated/request'
import { AGENT_COLORS } from '@/lib/constants'

import { AgentAvatar } from './AgentAvatar'

interface AskAgentCardProps {
  questionId: string
  sourceAgent?: string
  sourceAgentType?: string
  sourceSessionId?: string
  sourceAvatarUrl?: string
  targetAgent: string
  targetAgentType?: string
  targetSessionId: string
  targetAvatarUrl?: string
  question: string
  status: 'pending' | 'answered' | 'failed'
  collapsed: boolean
  summary?: string
}

function isAgentType(value: string | undefined): value is AgentType {
  return (
    value === 'claude-code' || value === 'opencode' || value === 'orchestrator' || value === 'codex'
  )
}

function compact(text: string, max = 30): string {
  const normalized = text.replace(/\s+/g, ' ').trim()
  return normalized.length > max ? `${normalized.slice(0, max)}...` : normalized
}

export function AskAgentCard({
  questionId,
  sourceAgent,
  sourceAgentType,
  sourceSessionId,
  sourceAvatarUrl,
  targetAgent,
  targetAgentType,
  targetSessionId,
  targetAvatarUrl,
  question,
  status,
  collapsed,
  summary,
}: AskAgentCardProps) {
  const [isCollapsed, setIsCollapsed] = useState(collapsed)
  const answered = status === 'answered'
  const failed = status === 'failed'
  const canToggle = answered || failed
  const sourceType = isAgentType(sourceAgentType) ? sourceAgentType : 'orchestrator'
  const sourceColor = AGENT_COLORS[sourceType] ?? 'var(--primary)'
  const sourceLabel = sourceAgent || 'orchestrator'
  const agentType = isAgentType(targetAgentType) ? targetAgentType : 'claude-code'
  const targetColor = AGENT_COLORS[agentType] ?? 'var(--primary)'
  const targetLabel = targetAgent
  const headerSummary = compact(question)

  return (
    <div
      className={[
        'my-2 overflow-hidden rounded-[10px] border border-border bg-card',
        canToggle ? 'cursor-pointer hover:bg-hover' : '',
      ].join(' ')}
      data-question-id={questionId}
      data-source-session-id={sourceSessionId}
      data-target-session-id={targetSessionId}
      data-answer-summary={summary}
      onClick={() => {
        if (canToggle) setIsCollapsed((value) => !value)
      }}
    >
      <div
        className={[
          'flex min-w-0 items-center gap-2 bg-hover px-3 py-2',
          isCollapsed ? '' : 'border-b border-border',
        ].join(' ')}
      >
        <AgentAvatar
          agentType={sourceType}
          status={answered ? 'ready' : 'running'}
          size={24}
          avatarUrl={sourceAvatarUrl}
          agentName={sourceLabel}
          sessionId={sourceSessionId}
        />
        <span className="shrink-0 text-xs font-semibold" style={{ color: sourceColor }}>
          {sourceLabel}
        </span>
        <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" strokeWidth={1.5} />
        <AgentAvatar
          agentType={agentType}
          status={answered ? 'ready' : 'running'}
          size={24}
          avatarUrl={targetAvatarUrl}
          agentName={targetLabel}
          sessionId={targetSessionId}
        />
        <span className="min-w-0 shrink-0 text-xs font-semibold" style={{ color: targetColor }}>
          {targetLabel}
        </span>
        {isCollapsed && (
          <span className="min-w-0 flex-1 truncate text-[11px] text-muted-foreground">
            {headerSummary}
          </span>
        )}
        <span
          className={[
            'ml-auto inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium',
            answered
              ? 'bg-success/10 text-success'
              : failed
                ? 'bg-destructive/10 text-destructive'
                : 'bg-primary/10 text-primary',
          ].join(' ')}
        >
          {answered ? (
            <>
              <Check className="h-3 w-3" strokeWidth={1.6} />
              已回答
            </>
          ) : failed ? (
            <>
              <AlertCircle className="h-3 w-3" strokeWidth={1.6} />
              未回答
            </>
          ) : (
            <>
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-current" />
              等待回答
            </>
          )}
        </span>
        {canToggle &&
          (isCollapsed ? (
            <ChevronRight
              className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
              strokeWidth={1.5}
            />
          ) : (
            <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" strokeWidth={1.5} />
          ))}
      </div>
      {!isCollapsed && (
        <div className="min-w-0 whitespace-pre-wrap break-words px-3 py-2 text-[13px] leading-6 text-muted-foreground">
          {question}
        </div>
      )}
    </div>
  )
}
