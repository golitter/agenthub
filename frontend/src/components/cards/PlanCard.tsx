import type { PlanTask } from '@/lib/block-types'

interface PlanCardProps {
  overview: string
  tasks: PlanTask[]
}

const statusIcon: Record<string, string> = {
  running: '●',
  completed: '✓',
  failed: '✗',
  pending: '○',
}

const statusColor: Record<string, string> = {
  running: 'text-agent-claude',
  completed: 'text-agent-opencode',
  failed: 'text-red-500',
  pending: 'text-muted-foreground',
}

export function PlanCard({ overview, tasks }: PlanCardProps) {
  return (
    <div className="my-2 rounded-[10px] border border-agent-orchestrator/15 bg-agent-orchestrator/5 p-3">
      {overview && (
        <div className="mb-2 text-sm font-semibold text-agent-orchestrator">{overview}</div>
      )}
      <div className="flex flex-col gap-1.5">
        {tasks.map((task) => (
          <div
            key={task.task_id}
            className="flex items-center gap-2 rounded-lg bg-card px-2.5 py-2 text-[13px]"
          >
            <span className={`text-xs ${statusColor[task.status] ?? 'text-muted-foreground'}`}>
              {statusIcon[task.status] ?? '○'}
            </span>
            {task.agent.trim() && (
              <span className="rounded-full bg-accent px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
                {task.agent}
              </span>
            )}
            <span className="min-w-0 flex-1 truncate text-muted-foreground">{task.title}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
