import { AlertTriangle, Clock3 } from 'lucide-react'

interface TaskFailureCardProps {
  taskId?: string
  agent?: string
  reason: string
  failureType: 'timeout' | 'error'
}

export function TaskFailureCard({ taskId, agent, reason, failureType }: TaskFailureCardProps) {
  const Icon = failureType === 'timeout' ? Clock3 : AlertTriangle
  const title = failureType === 'timeout' ? '任务超时' : '任务失败'

  return (
    <div className="rounded-lg border border-red-500/25 bg-red-500/[0.08] px-3 py-2 text-xs text-red-100">
      <div className="mb-1 flex min-w-0 items-center gap-2 font-medium text-red-300">
        <Icon className="h-3.5 w-3.5 shrink-0" strokeWidth={1.25} />
        <span>{title}</span>
        {taskId && <span className="truncate text-red-200/75">· {taskId}</span>}
        {agent && <span className="truncate text-red-200/75">· {agent}</span>}
      </div>
      <p className="whitespace-pre-wrap break-words text-red-100/85">{reason}</p>
    </div>
  )
}
