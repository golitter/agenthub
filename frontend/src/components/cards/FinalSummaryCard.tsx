import { CheckCircle2, CircleAlert, ListChecks } from 'lucide-react'

import type { FinalSummaryDetail } from '@/lib/block-types'
import { cn } from '@/lib/utils'

interface FinalSummaryCardProps {
  status: 'success' | 'partial' | 'failed'
  completed: number
  failed: number
  nextAction?: string
  details: FinalSummaryDetail[]
}

const statusCopy = {
  success: { label: '已完成', color: 'text-emerald-300', bg: 'bg-emerald-500/10' },
  partial: { label: '部分完成', color: 'text-amber-300', bg: 'bg-amber-500/10' },
  failed: { label: '执行失败', color: 'text-red-300', bg: 'bg-red-500/10' },
}

export function FinalSummaryCard({
  status,
  completed,
  failed,
  nextAction,
  details,
}: FinalSummaryCardProps) {
  const copy = statusCopy[status]

  return (
    <div className="rounded-lg border border-border bg-muted/25 p-3 text-sm">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <span
          className={cn(
            'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium',
            copy.bg,
            copy.color,
          )}
        >
          {status === 'success' ? (
            <CheckCircle2 className="h-3.5 w-3.5" strokeWidth={1.25} />
          ) : (
            <CircleAlert className="h-3.5 w-3.5" strokeWidth={1.25} />
          )}
          {copy.label}
        </span>
        <span className="text-xs text-muted-foreground">
          完成 {completed} 个，失败 {failed} 个
        </span>
      </div>

      {nextAction && (
        <p className="mb-3 rounded-md bg-card/70 px-3 py-2 text-xs text-muted-foreground">
          下一步：{nextAction}
        </p>
      )}

      {details.length > 0 && (
        <div className="space-y-1.5">
          <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
            <ListChecks className="h-3.5 w-3.5" strokeWidth={1.25} />
            任务概览
          </div>
          <div className="max-h-56 overflow-y-auto rounded-md border border-border/70">
            {details.map((detail) => (
              <div
                key={`${detail.task_id}-${detail.agent}`}
                className="grid grid-cols-[7rem_6rem_1fr] gap-3 border-b border-border/50 px-3 py-2 text-xs last:border-b-0"
              >
                <span className="truncate font-mono text-muted-foreground">{detail.task_id}</span>
                <span className={detail.status === 'failed' ? 'text-red-300' : 'text-emerald-300'}>
                  {detail.status === 'failed' ? '失败' : '完成'}
                </span>
                <span className="min-w-0 truncate text-muted-foreground">
                  {detail.agent}
                  {detail.summary ? ` · ${detail.summary}` : ''}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
