import { Check, Loader2, MessageSquareText, PencilLine } from 'lucide-react'
import { useMemo, useState } from 'react'

import { submitPlanReview } from '@/lib/api'
import type { PlanTask } from '@/lib/block-types'
import { cn } from '@/lib/utils'
import { useChatStore } from '@/stores/chat'

import { DiffCard } from './DiffCard'

interface PlanReviewCardProps {
  reviewKey?: string
  taskId?: string
  sessionId?: string
  reviewType?: 'plan' | 'merge_to_main'
  sourceBranch?: string
  targetBranch?: string
  diffSnapshotId?: string
  overview: string
  tasks: PlanTask[]
  waves: PlanTask[][]
  status?: 'pending' | 'submitted' | 'approved'
  interactive?: boolean
}

type ReviewAction = 'approve' | 'discuss' | 'modify'
type ReviewDisplayStatus =
  | 'pending'
  | 'approved'
  | 'submitted'
  | 'discussing'
  | 'modifying'
  | 'stale'

export function PlanReviewCard({
  reviewKey,
  taskId,
  sessionId,
  reviewType = 'plan',
  sourceBranch,
  targetBranch,
  diffSnapshotId,
  overview,
  tasks,
  waves,
  status = 'pending',
  interactive = true,
}: PlanReviewCardProps) {
  const resolvedReviewKey = reviewKey || `${taskId ?? ''}:${sessionId ?? ''}`
  const activePlanReviewKey = useChatStore((s) =>
    sessionId ? s.sessions[sessionId]?.activePlanReviewKey : undefined,
  )
  const setPlanReviewStatus = useChatStore((s) => s.setPlanReviewStatus)

  const [content, setContent] = useState('')
  const [submitting, setSubmitting] = useState<ReviewAction | null>(null)
  const [submittedAction, setSubmittedAction] = useState<ReviewAction | null>(
    status === 'approved' ? 'approve' : null,
  )
  const [stale, setStale] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Derive baseline action from props — used when no local action has been taken
  const baselineAction: ReviewAction | null = status === 'approved' ? 'approve' : null
  const activeAction = submittedAction ?? baselineAction

  const canSubmit =
    Boolean(interactive && taskId && sessionId && activePlanReviewKey === resolvedReviewKey) &&
    !stale
  const disabled = Boolean(submitting || activeAction || !canSubmit)
  const displayWaves = useMemo(() => waves.filter((wave) => wave.length > 0), [waves])

  let effectiveStatus: ReviewDisplayStatus = 'pending'
  if (status === 'approved' || activeAction === 'approve') {
    effectiveStatus = 'approved'
  } else if (activeAction === 'discuss') {
    effectiveStatus = 'discussing'
  } else if (activeAction === 'modify') {
    effectiveStatus = 'modifying'
  } else if (status === 'submitted') {
    effectiveStatus = 'submitted'
  } else if (stale) {
    effectiveStatus = 'stale'
  }
  const isResolved =
    effectiveStatus === 'approved' ||
    effectiveStatus === 'submitted' ||
    effectiveStatus === 'discussing' ||
    effectiveStatus === 'modifying'
  const isReadonlyHistory = stale || (!canSubmit && !isResolved)
  const title =
    effectiveStatus === 'approved'
      ? reviewType === 'merge_to_main'
        ? '合并已批准'
        : '规划已批准'
      : effectiveStatus === 'discussing'
        ? '继续讨论中'
        : effectiveStatus === 'modifying'
          ? '正在修改规划'
          : isReadonlyHistory
            ? reviewType === 'merge_to_main'
              ? '合并审查记录'
              : '规划审查记录'
            : reviewType === 'merge_to_main'
              ? '合并 main 待确认'
              : '规划待审查'
  const subtitle =
    effectiveStatus === 'approved'
      ? reviewType === 'merge_to_main'
        ? '已开始合并到 main'
        : '已开始分派执行'
      : effectiveStatus === 'discussing'
        ? '不会执行当前规划，等待 Orchestrator 回复'
        : effectiveStatus === 'modifying'
          ? '不会执行当前规划，等待新规划'
          : isReadonlyHistory
            ? '已离开审查等待点'
            : reviewType === 'merge_to_main'
              ? '批准后才会执行 task → main 合并'
              : '批准后才会开始分派执行'
  const badgeText =
    effectiveStatus === 'approved'
      ? '已批准'
      : effectiveStatus === 'discussing'
        ? '讨论中'
        : effectiveStatus === 'modifying'
          ? '修改中'
          : effectiveStatus === 'submitted'
            ? '已提交'
            : isReadonlyHistory
              ? '历史记录'
              : '等待审查'

  const submit = async (action: ReviewAction) => {
    if (!taskId || !sessionId) return
    const trimmed = content.trim()
    if ((action === 'discuss' || action === 'modify') && !trimmed) {
      setError('请先写下你的反馈。')
      return
    }
    setSubmitting(action)
    setSubmittedAction(action)
    setPlanReviewStatus(
      sessionId,
      resolvedReviewKey,
      action === 'approve' ? 'approved' : 'submitted',
    )
    setError(null)
    try {
      await submitPlanReview(taskId, {
        session_id: sessionId,
        action,
        content: action === 'approve' ? undefined : trimmed,
      })
      if (action !== 'approve') setContent('')
    } catch (err) {
      const message = err instanceof Error ? err.message : '提交审查失败'
      if (message.includes('no pending plan review')) {
        setStale(true)
        setPlanReviewStatus(sessionId, resolvedReviewKey, 'stale')
        setError(null)
      } else {
        setSubmittedAction(null)
        setPlanReviewStatus(sessionId, resolvedReviewKey, 'pending')
        setError(message)
      }
    } finally {
      setSubmitting(null)
    }
  }

  return (
    <div className="my-2 overflow-hidden rounded-[10px] border border-border/80 bg-card/95">
      <div className="flex items-center gap-2 border-b border-border/80 bg-muted/35 px-4 py-3">
        <div className="flex h-7 w-7 items-center justify-center rounded-md bg-agent-orchestrator/10 text-agent-orchestrator">
          <PencilLine className="h-4 w-4" strokeWidth={1.25} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-[13px] font-semibold text-foreground">{title}</div>
          <div className="text-[11px] text-muted-foreground">{subtitle}</div>
        </div>
        <span
          className={cn(
            'rounded-full border px-2 py-0.5 text-[10px] font-semibold',
            isResolved
              ? 'border-success/25 bg-success/8 text-success'
              : isReadonlyHistory
                ? 'border-border bg-muted text-muted-foreground'
                : 'border-warning/25 bg-warning/8 text-warning',
          )}
        >
          {badgeText}
        </span>
      </div>

      <div className="space-y-3 px-4 py-3">
        {overview && <p className="text-[13px] leading-6 text-muted-foreground">{overview}</p>}

        <div className="space-y-2">
          {tasks.map((task, index) => (
            <div
              key={`${task.task_id}-${index}`}
              className="rounded-[8px] border border-border/80 bg-muted/30 px-3 py-2"
            >
              <div className="flex items-start gap-2">
                <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-agent-orchestrator/10 text-[11px] font-semibold text-agent-orchestrator">
                  {index + 1}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[13px] font-medium text-foreground">
                    {task.title || task.task_id}
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-1.5">
                    {task.agent && (
                      <span className="shrink-0 rounded-[4px] bg-accent px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                        {task.agent}
                      </span>
                    )}
                    <span className="text-[10px] text-muted-foreground">Wave 1 · 并行</span>
                  </div>
                </div>
              </div>
              {task.content && (
                <p className="mt-1.5 line-clamp-3 pl-7 text-xs leading-5 text-muted-foreground">
                  {task.content}
                </p>
              )}
            </div>
          ))}
        </div>

        {reviewType === 'merge_to_main' && (
          <div className="space-y-2 rounded-[8px] border border-border/80 bg-background/60 p-3">
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <span className="font-medium text-foreground">Code diff</span>
              {sourceBranch && <span className="rounded bg-muted px-2 py-0.5">{sourceBranch}</span>}
              {targetBranch && (
                <>
                  <span>→</span>
                  <span className="rounded bg-muted px-2 py-0.5">{targetBranch}</span>
                </>
              )}
            </div>
            {diffSnapshotId ? (
              <DiffCard snapshotId={diffSnapshotId} />
            ) : (
              <div className="rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
                没有检测到 task 分支相对 main 的代码差异。
              </div>
            )}
          </div>
        )}

        {displayWaves.length > 0 && (
          <div className="flex flex-wrap gap-2 border-t border-border/80 pt-3">
            {displayWaves.map((wave, index) => (
              <span
                key={index}
                className="rounded-full bg-agent-orchestrator/10 px-2.5 py-1 text-[11px] text-agent-orchestrator"
              >
                Wave {index + 1}: {wave.map((task) => task.task_id).join(', ')}
              </span>
            ))}
          </div>
        )}

        {!activeAction && canSubmit && (
          <div className="space-y-2 border-t border-border/80 pt-3">
            <textarea
              value={content}
              onChange={(event) => setContent(event.target.value)}
              placeholder="有修改意见或想继续讨论，可以写在这里..."
              className="min-h-20 w-full resize-none rounded-[8px] border border-border/80 bg-background px-3 py-2 text-sm text-foreground outline-none placeholder:text-tertiary focus:border-agent-orchestrator/40"
              disabled={disabled}
            />
            {error && <div className="text-xs text-destructive">{error}</div>}
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                className="inline-flex h-8 items-center gap-1.5 rounded-[6px] border border-success/20 bg-success/10 px-3 text-xs font-medium text-success disabled:opacity-50"
                disabled={disabled}
                onClick={() => submit('approve')}
              >
                {submitting === 'approve' ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Check className="h-3.5 w-3.5" />
                )}
                批准执行
              </button>
              <button
                type="button"
                className="inline-flex h-8 items-center gap-1.5 rounded-[6px] border border-border bg-muted/40 px-3 text-xs font-medium text-foreground disabled:opacity-50"
                disabled={disabled}
                onClick={() => submit('discuss')}
              >
                <MessageSquareText className="h-3.5 w-3.5" />
                继续讨论
              </button>
              <button
                type="button"
                className="inline-flex h-8 items-center gap-1.5 rounded-[6px] border border-agent-orchestrator/20 bg-agent-orchestrator/10 px-3 text-xs font-medium text-agent-orchestrator disabled:opacity-50"
                disabled={disabled}
                onClick={() => submit('modify')}
              >
                <PencilLine className="h-3.5 w-3.5" />
                请求修改
              </button>
            </div>
          </div>
        )}
        {!activeAction && !canSubmit && (
          <div className="border-t border-border/80 pt-3 text-xs text-muted-foreground">
            这是一条历史审查记录，当前没有等待中的规划审查。
          </div>
        )}
      </div>
    </div>
  )
}
