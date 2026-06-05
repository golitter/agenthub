import { cn } from '@/lib/utils'

interface RuntimeStatusProps {
  task_id: string
  agent: string
  status: string
  title?: string
  streamingText?: string
}

const statusConfig: Record<string, { bg: string; color: string; label: string; pulse: boolean }> = {
  running: { bg: 'bg-agent-codex/10', color: 'text-agent-codex', label: '执行中', pulse: true },
  completed: {
    bg: 'bg-success/10',
    color: 'text-success',
    label: '完成',
    pulse: false,
  },
  failed: { bg: 'bg-destructive/10', color: 'text-destructive', label: '失败', pulse: false },
  pending: { bg: 'bg-muted', color: 'text-muted-foreground', label: '等待', pulse: false },
}

export function RuntimeStatus({ agent, status, title, streamingText }: RuntimeStatusProps) {
  const config = statusConfig[status] ?? statusConfig.pending

  return (
    <div className="space-y-1.5">
      <div
        className={cn(
          'flex min-w-0 items-center gap-2 rounded-[8px] border border-border/80 bg-muted/30 px-3 py-2 text-[12px]',
        )}
      >
        <span
          className={cn('h-1.5 w-1.5 rounded-full bg-current', config.pulse && 'animate-pulse')}
        />
        <span className="shrink-0 font-medium text-foreground">{agent || '任务'}</span>
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
    </div>
  )
}
