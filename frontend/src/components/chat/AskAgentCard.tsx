import { AlertCircle, Check, ChevronDown, Loader2 } from 'lucide-react'
import { useMemo, useState } from 'react'

import type { AgentType } from '@/generated/request'
import { AGENT_COLORS, AGENT_TYPES } from '@/lib/constants'

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
    value === AGENT_TYPES.ClaudeCode ||
    value === AGENT_TYPES.Opencode ||
    value === AGENT_TYPES.Orchestrator ||
    value === AGENT_TYPES.Codex
  )
}

function compact(text: string, max = 44): string {
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
  summary: _summary,
}: AskAgentCardProps) {
  const [manuallyToggled, setManuallyToggled] = useState(false)
  const [manualExpanded, setManualExpanded] = useState(false)
  const expanded = manuallyToggled ? manualExpanded : !collapsed

  const answered = status === 'answered'
  const failed = status === 'failed'
  const sourceType = isAgentType(sourceAgentType) ? sourceAgentType : AGENT_TYPES.Orchestrator
  const sourceColor = AGENT_COLORS[sourceType] ?? 'var(--primary)'
  const sourceLabel = sourceAgent || AGENT_TYPES.Orchestrator
  const agentType = isAgentType(targetAgentType) ? targetAgentType : AGENT_TYPES.ClaudeCode
  const targetColor = AGENT_COLORS[agentType] ?? 'var(--primary)'
  const targetLabel = targetAgent
  const headerSummary = compact(question)
  const hasLongQuestion = useMemo(
    () => compact(question, 999) !== headerSummary,
    [headerSummary, question],
  )
  const statusClass = answered
    ? 'border-success/25 bg-success/8 text-success'
    : failed
      ? 'border-destructive/25 bg-destructive/8 text-destructive'
      : 'border-warning/25 bg-warning/8 text-warning'

  return (
    <div
      className="mb-3 rounded-[8px] border border-border/80 bg-muted/25"
      data-question-id={questionId}
      data-source-session-id={sourceSessionId}
      data-target-session-id={targetSessionId}
      data-answer-summary={_summary}
    >
      <button
        type="button"
        className="flex w-full min-w-0 items-center gap-2 px-3 py-2 text-left"
        onClick={() => {
          setManuallyToggled(true)
          setManualExpanded((v) => !v)
        }}
      >
        <AgentAvatar
          agentType={sourceType}
          status={answered ? 'ready' : 'running'}
          size={22}
          avatarUrl={sourceAvatarUrl}
          agentName={sourceLabel}
          sessionId={sourceSessionId}
        />
        <span className="shrink-0 text-[12px] font-semibold" style={{ color: sourceColor }}>
          {sourceLabel}
        </span>
        <span className="shrink-0 text-[11px] text-muted-foreground">→</span>
        <AgentAvatar
          agentType={agentType}
          status={answered ? 'ready' : 'running'}
          size={22}
          avatarUrl={targetAvatarUrl}
          agentName={targetLabel}
          sessionId={targetSessionId}
        />
        <span className="shrink-0 text-[12px] font-semibold" style={{ color: targetColor }}>
          {targetLabel}
        </span>
        <span
          className={[
            'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-semibold',
            statusClass,
          ].join(' ')}
        >
          {answered ? (
            <>
              <Check className="h-3 w-3" strokeWidth={1.5} />
              已回答
            </>
          ) : failed ? (
            <>
              <AlertCircle className="h-3 w-3" strokeWidth={1.5} />
              未回答
            </>
          ) : (
            <>
              <Loader2 className="h-3 w-3 animate-spin" strokeWidth={1.5} />
              等待回答
            </>
          )}
        </span>
        <span className="min-w-0 flex-1 truncate text-[12px] italic text-muted-foreground">
          {headerSummary}
        </span>
        <ChevronDown
          className={[
            'h-4 w-4 shrink-0 text-muted-foreground transition-transform',
            expanded ? 'rotate-180' : '',
          ].join(' ')}
          strokeWidth={1.5}
        />
      </button>
      {expanded && (
        <div className="border-t border-border/70 px-3 py-2">
          <div className="rounded-[6px] bg-background/60 px-3 py-2 text-[12px] leading-6 text-muted-foreground">
            {question}
          </div>
        </div>
      )}
      {!expanded && hasLongQuestion && (
        <div className="px-3 pb-2 text-[11px] text-muted-foreground">点击查看完整 query</div>
      )}
    </div>
  )
}
