import { useEffect, useState } from 'react'

import {
  applyConflictAction,
  fetchConflict,
  type ConflictAction,
  type ConflictProjection,
} from '@/lib/api'
import { UI_CARD_STATUS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

interface RuntimeStatusProps {
  task_id: string
  session_id?: string
  conflict_id?: string
  conflict_files?: string[]
  attempt?: number
  error_message?: string
  agent: string
  status: string
  title?: string
  streamingText?: string
}

const statusConfig: Record<string, { bg: string; color: string; label: string; pulse: boolean }> = {
  running: {
    bg: 'bg-agent-codex/10',
    color: 'text-agent-codex',
    label: UI_CARD_STATUS.RUNNING,
    pulse: true,
  },
  integrating: {
    bg: 'bg-agent-codex/10',
    color: 'text-agent-codex',
    label: UI_CARD_STATUS.INTEGRATING,
    pulse: true,
  },
  conflict: {
    bg: 'bg-warning/10',
    color: 'text-warning',
    label: UI_CARD_STATUS.CONFLICT,
    pulse: true,
  },
  resolving: {
    bg: 'bg-warning/10',
    color: 'text-warning',
    label: UI_CARD_STATUS.RESOLVING,
    pulse: true,
  },
  verifying: {
    bg: 'bg-agent-codex/10',
    color: 'text-agent-codex',
    label: UI_CARD_STATUS.VERIFYING,
    pulse: true,
  },
  awaiting_user: {
    bg: 'bg-warning/10',
    color: 'text-warning',
    label: UI_CARD_STATUS.AWAITING_USER,
    pulse: false,
  },
  completed: {
    bg: 'bg-success/10',
    color: 'text-success',
    label: UI_CARD_STATUS.DONE,
    pulse: false,
  },
  failed: {
    bg: 'bg-destructive/10',
    color: 'text-destructive',
    label: UI_CARD_STATUS.FAILED,
    pulse: false,
  },
  cancelled: {
    bg: 'bg-muted',
    color: 'text-muted-foreground',
    label: '已取消',
    pulse: false,
  },
  pending: {
    bg: 'bg-muted',
    color: 'text-muted-foreground',
    label: UI_CARD_STATUS.WAITING,
    pulse: false,
  },
  partial: {
    bg: 'bg-warning/10',
    color: 'text-warning',
    label: '部分完成',
    pulse: false,
  },
}

const actionLabels: Array<{ action: ConflictAction; label: string; confirm?: string }> = [
  { action: 'retry', label: '再试一次' },
  { action: 'accept_current', label: '保留当前结果', confirm: '确认保留当前 task 分支结果吗？' },
  { action: 'accept_source', label: '采用来源结果', confirm: '确认用来源分支覆盖当前 task 分支吗？此操作不可撤销。' },
  { action: 'accept_target', label: '采用目标结果', confirm: '确认放弃来源改动并保留目标结果吗？' },
  { action: 'accept_partial', label: '接受部分结果', confirm: '确认以部分完成状态结束本次冲突吗？' },
  { action: 'cancel', label: '取消任务', confirm: '确认取消本次冲突任务吗？' },
]

function newIdempotencyKey() {
  return typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export function RuntimeStatus({
  task_id,
  session_id,
  conflict_id,
  conflict_files,
  attempt,
  error_message,
  agent,
  status,
  title,
  streamingText,
}: RuntimeStatusProps) {
  const config = statusConfig[status] ?? statusConfig.pending
  const [projection, setProjection] = useState<ConflictProjection | null>(null)
  const [showConflict, setShowConflict] = useState(false)
  const [busy, setBusy] = useState<ConflictAction | null>(null)
  const [actionError, setActionError] = useState('')
  const [actionMessage, setActionMessage] = useState('')

  useEffect(() => {
    let active = true
    if (!conflict_id || !task_id || !['conflict', 'awaiting_user', 'resolving', 'verifying'].includes(status)) {
      setProjection(null)
      return () => {
        active = false
      }
    }
    fetchConflict(task_id, conflict_id)
      .then((value) => {
        if (active) setProjection(value)
      })
      .catch((error: unknown) => {
        if (active) setActionError(error instanceof Error ? error.message : '无法读取冲突详情')
      })
    return () => {
      active = false
    }
  }, [task_id, conflict_id, status])

  const currentAttempt = projection?.attempt ?? attempt ?? 0
  const currentFiles = projection?.conflict_files ?? conflict_files ?? []
  const canAct = Boolean(
    conflict_id &&
      session_id &&
      projection &&
      (projection.status === 'awaiting_user' || projection.status === 'retryable'),
  )

  async function handleAction(item: (typeof actionLabels)[number]) {
    if (!canAct || !conflict_id || !session_id || !projection) return
    if (item.confirm && !window.confirm(item.confirm)) return
    setBusy(item.action)
    setActionError('')
    setActionMessage('')
    try {
      const response = await applyConflictAction(task_id, conflict_id, {
        action: item.action,
        session_id,
        root_run_id: projection.root_run_id,
        expected_attempt: currentAttempt,
        confirmation: Boolean(item.confirm),
        idempotency_key: newIdempotencyKey(),
      })
      if (!response.accepted) {
        throw new Error(response.message || '冲突操作未受理')
      }
      setActionMessage(response.message || '操作已受理，正在继续处理')
      setProjection((current) =>
        current
        ? {
            ...current,
            status: item.action === 'retry' ? 'resolving' : item.action === 'cancel' ? 'cancelled' : 'resolved',
            last_error_message: '',
          }
          : current,
      )
    } catch (error: unknown) {
      setActionError(error instanceof Error ? error.message : '冲突操作失败')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="space-y-1.5" role="status" aria-live={config.pulse ? 'polite' : 'off'}>
      <div
        className={cn(
          'flex min-w-0 items-center gap-2 rounded-[8px] border border-border/80 bg-muted/30 px-3 py-2 text-[12px]',
        )}
      >
        <span
          className={cn('h-1.5 w-1.5 rounded-full bg-current', config.pulse && 'animate-pulse')}
          aria-hidden="true"
        />
        <span className="shrink-0 font-medium text-foreground">{agent || UI_CARD_STATUS.TASK}</span>
        <span
          className={cn(
            'shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold',
            config.bg,
            config.color,
          )}
        >
          {config.label}
        </span>
        {title && <span className="min-w-0 truncate text-muted-foreground">{title}</span>}
      </div>
      {streamingText && (
        <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-[8px] bg-background/60 px-3 py-2 font-mono text-xs text-muted-foreground">
          {streamingText}
        </pre>
      )}
      {conflict_id && (status === 'conflict' || status === 'awaiting_user' || projection) && (
        <div className="rounded-[8px] border border-warning/30 bg-warning/5 px-3 py-2 text-xs">
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              className="rounded border border-border px-2 py-1 text-muted-foreground hover:text-foreground"
              onClick={() => setShowConflict((value) => !value)}
            >
              {showConflict ? '收起冲突' : '查看冲突'}
            </button>
            {canAct &&
              actionLabels.map((item) => (
                <button
                  key={item.action}
                  type="button"
                  disabled={busy !== null}
                  className={cn(
                    'rounded border border-border px-2 py-1 text-foreground transition-colors hover:border-warning hover:text-warning disabled:cursor-not-allowed disabled:opacity-50',
                    item.action === 'accept_source' && 'border-destructive/40 text-destructive',
                  )}
                  onClick={() => void handleAction(item)}
                >
                  {busy === item.action ? '处理中…' : item.label}
                </button>
              ))}
          </div>
          {showConflict && (
            <div className="mt-2 space-y-1 text-muted-foreground">
              <div>冲突文件：{currentFiles.length ? currentFiles.join('、') : 'Git 未提供文件名'}</div>
              {(projection?.last_error_message || error_message) && (
                <div>原因：{projection?.last_error_message || error_message}</div>
              )}
            </div>
          )}
          {actionMessage && <div className="mt-2 text-success">{actionMessage}</div>}
          {actionError && <div className="mt-2 text-destructive">{actionError}</div>}
        </div>
      )}
    </div>
  )
}
