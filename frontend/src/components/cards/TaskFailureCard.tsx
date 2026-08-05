import { AlertTriangle, Clock3 } from 'lucide-react'

import { UI_CARD_STATUS } from '@/lib/ui-text'

interface TaskFailureCardProps {
  taskId?: string
  agent?: string
  reason: string
  failureType: 'timeout' | 'error'
}

export function TaskFailureCard({ taskId, agent, reason, failureType }: TaskFailureCardProps) {
  const Icon = failureType === 'timeout' ? Clock3 : AlertTriangle
  const title = failureType === 'timeout' ? UI_CARD_STATUS.TASK_TIMEOUT : UI_CARD_STATUS.TASK_FAILED

  return (
    <div className="rounded-lg border border-destructive/25 bg-destructive/[0.08] px-3 py-2 text-xs text-destructive">
      <div className="mb-1 flex min-w-0 items-center gap-2 font-medium text-destructive">
        <Icon className="h-3.5 w-3.5 shrink-0" strokeWidth={1.25} />
        <span>{title}</span>
        {taskId && <span className="truncate text-destructive/75">· {taskId}</span>}
        {agent && <span className="truncate text-destructive/75">· {agent}</span>}
      </div>
      <p className="whitespace-pre-wrap break-words text-destructive/85">{reason}</p>
    </div>
  )
}
