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
  success: { label: '已完成', color: 'text-success', bg: 'bg-success/10' },
  partial: { label: '部分完成', color: 'text-warning', bg: 'bg-warning/10' },
  failed: { label: '执行失败', color: 'text-destructive', bg: 'bg-destructive/10' },
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
    <div className="rounded-[10px] border border-border/80 bg-card/95 p-3 text-sm">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <span
          className={cn(
            'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[10px] font-semibold',
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
        <p className="mb-3 rounded-[8px] border border-border/70 bg-muted/25 px-3 py-2 text-xs text-muted-foreground">
          下一步：{nextAction}
        </p>
      )}

      {details.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
            <ListChecks className="h-3.5 w-3.5" strokeWidth={1.25} />
            任务概览
          </div>
          <div className="max-h-56 overflow-y-auto rounded-[8px] border border-border/80 bg-muted/20">
            {details.map((detail) => (
              <div
                key={`${detail.task_id}-${detail.agent}`}
                className="grid grid-cols-[1fr_auto] gap-2 border-b border-border/60 px-3 py-2 text-xs last:border-b-0"
              >
                <div className="min-w-0">
                  <div className="truncate text-foreground">{detail.agent}</div>
                  <div className="truncate text-muted-foreground">
                    {detail.summary || detail.task_id}
                  </div>
                </div>
                <span
                  className={cn(
                    'shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold',
                    detail.status === 'failed'
                      ? 'bg-destructive/10 text-destructive'
                      : 'bg-success/10 text-success',
                  )}
                >
                  {detail.status === 'failed' ? '失败' : '完成'}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
