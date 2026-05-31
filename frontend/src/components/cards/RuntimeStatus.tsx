import { cn } from '@/lib/utils'

interface RuntimeStatusProps {
  task_id: string
  agent: string
  status: string
  title?: string
  streamingText?: string
}

const statusConfig: Record<string, { bg: string; color: string; label: string; pulse: boolean }> = {
  running: { bg: 'bg-agent-claude/10', color: 'text-agent-claude', label: '执行中', pulse: true },
  completed: {
    bg: 'bg-agent-opencode/10',
    color: 'text-agent-opencode',
    label: '完成',
    pulse: false,
  },
  failed: { bg: 'bg-red-500/10', color: 'text-red-500', label: '失败', pulse: false },
  pending: { bg: 'bg-muted', color: 'text-muted-foreground', label: '等待', pulse: false },
}

export function RuntimeStatus({ agent, status, title, streamingText }: RuntimeStatusProps) {
  const config = statusConfig[status] ?? statusConfig.pending
  const hasText = Boolean(streamingText?.trim())
  const label = [agent, config.label].filter(Boolean).join(' ')

  if (!hasText && status !== 'failed') {
    return null
  }

  return (
    <div className="space-y-1.5">
      <span
        className={cn(
          'inline-flex items-center gap-1.5 rounded-lg px-2.5 py-0.5 text-[11px]',
          config.bg,
          config.color,
        )}
      >
        <span
          className={cn('h-1.5 w-1.5 rounded-full bg-current', config.pulse && 'animate-pulse')}
        />
        {label || config.label}
        {title && <span className="max-w-[28rem] truncate opacity-75">· {title}</span>}
      </span>
      {streamingText && (
        <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted/50 px-3 py-2 font-mono text-xs text-muted-foreground">
          {streamingText}
        </pre>
      )}
    </div>
  )
}
