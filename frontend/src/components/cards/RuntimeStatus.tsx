interface RuntimeStatusProps {
  task_id: string
  agent: string
  status: string
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

export function RuntimeStatus({ agent, status, streamingText }: RuntimeStatusProps) {
  const config = statusConfig[status] ?? statusConfig.pending

  return (
    <div className="space-y-1.5">
      <span
        className={`inline-flex items-center gap-1.5 rounded-lg px-2.5 py-0.5 text-[11px] ${config.bg} ${config.color}`}
      >
        <span
          className={`h-1.5 w-1.5 rounded-full bg-current ${config.pulse ? 'animate-pulse' : ''}`}
        />
        {agent} {config.label}
      </span>
      {streamingText && (
        <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted/50 px-3 py-2 font-mono text-xs text-muted-foreground">
          {streamingText}
        </pre>
      )}
    </div>
  )
}
