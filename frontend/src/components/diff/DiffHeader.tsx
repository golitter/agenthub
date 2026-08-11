import { clsx } from 'clsx'
import { Check, Columns2, Pencil, RotateCcw, Rows, X } from 'lucide-react'
import type { ReactNode } from 'react'

import { UI_ACTIONS, UI_LABELS, UI_STATUS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

type SnapshotStatus = 'pending' | 'committed' | 'reverted' | 'cancelled'
type ViewType = 'split' | 'unified'
type ActionStatus = 'idle' | 'committing' | 'reverting'

interface DiffHeaderProps {
  summary: { filesChanged: number; additions: number; deletions: number }
  viewType: ViewType
  onViewTypeChange: (vt: ViewType) => void
  snapshotStatus: SnapshotStatus | null
  isSettled: boolean
  hasSession: boolean
  canEdit: boolean
  onEdit: () => void
  onAccept: () => void
  onReject: () => void
  actionStatus: ActionStatus
}

const BADGE_CONFIG: Record<string, { icon: ReactNode; label: string; className: string }> = {
  committed: {
    icon: <Check className="h-3 w-3" strokeWidth={1.25} />,
    label: '已接受',
    className: 'bg-success/10 text-success',
  },
  reverted: {
    icon: <RotateCcw className="h-3 w-3" strokeWidth={1.25} />,
    label: '已拒绝',
    className: 'bg-muted text-muted-foreground',
  },
  cancelled: {
    icon: <X className="h-3 w-3" strokeWidth={1.25} />,
    label: '已取消',
    className: 'bg-muted text-muted-foreground',
  },
}

export function DiffHeader({
  summary,
  viewType,
  onViewTypeChange,
  snapshotStatus,
  isSettled,
  hasSession,
  canEdit,
  onEdit,
  onAccept,
  onReject,
  actionStatus,
}: DiffHeaderProps) {
  return (
    <div className="flex flex-col items-stretch gap-2 border-b border-border bg-muted/50 px-3 py-2 sm:flex-row sm:items-center sm:justify-between sm:py-1.5">
      <span className="min-w-0 text-xs text-muted-foreground">
        {summary.filesChanged} 个文件变更，{' '}
        <span className="text-success">+{summary.additions}</span>{' '}
        <span className="text-destructive">-{summary.deletions}</span>
      </span>
      <div className="flex flex-wrap items-center gap-1">
        {/* 视图模式切换 */}
        <div className="mr-1 flex items-center rounded-md border border-border bg-background">
          <button
            type="button"
            onClick={() => onViewTypeChange('split')}
            className={clsx(
              'inline-flex items-center gap-1 rounded-l-md px-2 py-1 text-xs transition-[background,color,transform,opacity] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
              viewType === 'split'
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:bg-bg-hover hover:text-foreground',
            )}
            title={UI_LABELS.SPLIT_DIFF_VIEW}
            aria-label={UI_LABELS.SPLIT_DIFF_VIEW}
          >
            <Columns2 className="h-3 w-3" strokeWidth={1.25} />
          </button>
          <button
            type="button"
            onClick={() => onViewTypeChange('unified')}
            className={clsx(
              'inline-flex items-center gap-1 rounded-r-md px-2 py-1 text-xs transition-[background,color,transform,opacity] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
              viewType === 'unified'
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:bg-bg-hover hover:text-foreground',
            )}
            title={UI_LABELS.UNIFIED_DIFF_VIEW}
            aria-label={UI_LABELS.UNIFIED_DIFF_VIEW}
          >
            <Rows className="h-3 w-3" strokeWidth={1.25} />
          </button>
        </div>
        {snapshotStatus && BADGE_CONFIG[snapshotStatus] && (
          <span
            className={cn(
              'mr-2 inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
              BADGE_CONFIG[snapshotStatus].className,
            )}
          >
            {BADGE_CONFIG[snapshotStatus].icon} {BADGE_CONFIG[snapshotStatus].label}
          </span>
        )}
        {!isSettled && hasSession && canEdit && (
          <button
            type="button"
            onClick={onEdit}
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-[background,color,transform,opacity] hover:bg-accent hover:text-accent-foreground active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            <Pencil className="h-3 w-3" strokeWidth={1.25} />
            {UI_ACTIONS.EDIT}
          </button>
        )}
        {!isSettled && (
          <>
            <button
              type="button"
              onClick={onAccept}
              disabled={actionStatus !== 'idle'}
              className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-[background,color,transform,opacity] hover:bg-accent hover:text-accent-foreground active:scale-[0.98] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              <Check className="h-3 w-3" strokeWidth={1.25} />
              {actionStatus === 'committing' ? UI_STATUS.COMMITTING : UI_ACTIONS.ACCEPT_CHANGE}
            </button>
            <button
              type="button"
              onClick={onReject}
              disabled={actionStatus !== 'idle'}
              className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-[background,color,transform,opacity] hover:bg-accent hover:text-accent-foreground active:scale-[0.98] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              <RotateCcw className="h-3 w-3" strokeWidth={1.25} />
              {actionStatus === 'reverting' ? UI_STATUS.REVERTING : UI_ACTIONS.REJECT_CHANGE}
            </button>
          </>
        )}
      </div>
    </div>
  )
}
