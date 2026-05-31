import { clsx } from 'clsx'
import { Check, Columns2, Pencil, RotateCcw, Rows, X } from 'lucide-react'
import type { ReactNode } from 'react'

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
  onEdit: () => void
  onAccept: () => void
  onReject: () => void
  actionStatus: ActionStatus
}

const BADGE_CONFIG: Record<string, { icon: ReactNode; label: string; className: string }> = {
  committed: {
    icon: <Check className="h-3 w-3" strokeWidth={1.25} />,
    label: '已接受',
    className: 'bg-green-500/10 text-green-600',
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
  onEdit,
  onAccept,
  onReject,
  actionStatus,
}: DiffHeaderProps) {
  return (
    <div className="flex items-center justify-between border-b border-border bg-muted/50 px-3 py-1.5">
      <span className="text-xs text-muted-foreground">
        {summary.filesChanged} file{summary.filesChanged !== 1 ? 's' : ''} changed,{' '}
        <span className="text-green-500">+{summary.additions}</span>{' '}
        <span className="text-red-500">-{summary.deletions}</span>
      </span>
      <div className="flex items-center gap-1">
        {/* View mode toggle */}
        <div className="mr-1 flex items-center rounded-md border border-border bg-background">
          <button
            onClick={() => onViewTypeChange('split')}
            className={clsx(
              'inline-flex items-center gap-1 rounded-l-md px-2 py-1 text-xs transition-colors',
              viewType === 'split'
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:text-foreground',
            )}
            title="Split view"
          >
            <Columns2 className="h-3 w-3" strokeWidth={1.25} />
          </button>
          <button
            onClick={() => onViewTypeChange('unified')}
            className={clsx(
              'inline-flex items-center gap-1 rounded-r-md px-2 py-1 text-xs transition-colors',
              viewType === 'unified'
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:text-foreground',
            )}
            title="Unified view"
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
        {!isSettled && hasSession && (
          <button
            onClick={onEdit}
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          >
            <Pencil className="h-3 w-3" strokeWidth={1.25} />
            编辑
          </button>
        )}
        {!isSettled && (
          <>
            <button
              onClick={onAccept}
              disabled={actionStatus !== 'idle'}
              className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:opacity-50"
            >
              <Check className="h-3 w-3" strokeWidth={1.25} />
              {actionStatus === 'committing' ? '提交中...' : '接受变更'}
            </button>
            <button
              onClick={onReject}
              disabled={actionStatus !== 'idle'}
              className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:opacity-50"
            >
              <RotateCcw className="h-3 w-3" strokeWidth={1.25} />
              {actionStatus === 'reverting' ? '撤销中...' : '拒绝变更'}
            </button>
          </>
        )}
      </div>
    </div>
  )
}
