import { UI_CARD_STATUS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

interface RuntimeStatusProps {
  task_id: string
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
  pending: {
    bg: 'bg-muted',
    color: 'text-muted-foreground',
    label: UI_CARD_STATUS.WAITING,
    pulse: false,
  },
}

export function RuntimeStatus({ agent, status, title, streamingText }: RuntimeStatusProps) {
  const config = statusConfig[status] ?? statusConfig.pending

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
    </div>
  )
}
