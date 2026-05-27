import type { AgentType } from '@/generated/request'
import { AGENT_COLORS, AGENT_NAMES } from '@/lib/constants'

type Status = 'ready' | 'running' | 'offline' | 'error'

const STATUS_COLORS: Record<Status, string> = {
  ready: 'var(--color-success)',
  running: 'var(--color-warning)',
  offline: 'var(--text-tertiary)',
  error: 'var(--destructive)',
}

const STATUS_READY_DURATION = '2s'
const STATUS_RUNNING_DURATION = '1.5s'

function diceBearUrl(name: string): string {
  return `https://api.dicebear.com/9.x/bottts/svg?seed=${encodeURIComponent(name)}`
}

interface AgentAvatarProps {
  agentType: AgentType
  status?: Status
  size?: number
  avatarUrl?: string
  agentName?: string
}

export function AgentAvatar({
  agentType,
  status = 'offline',
  size = 32,
  avatarUrl,
  agentName,
}: AgentAvatarProps) {
  const color = AGENT_COLORS[agentType] ?? 'var(--primary)'
  const shadowColor = AGENT_COLORS[agentType] ?? 'var(--primary)'
  const label = agentName ?? AGENT_NAMES[agentType] ?? agentType

  const statusAnimation =
    status === 'ready'
      ? `status-ready-pulse ${STATUS_READY_DURATION} ease-in-out infinite`
      : status === 'running'
        ? `status-running-spin ${STATUS_RUNNING_DURATION} linear infinite`
        : undefined

  const imgSrc = avatarUrl || (agentName ? diceBearUrl(agentName) : undefined)

  return (
    <div className="relative inline-flex shrink-0" title={label}>
      <div
        className="flex items-center justify-center overflow-hidden rounded-lg text-xs font-semibold text-white"
        style={{
          width: size,
          height: size,
          backgroundColor: imgSrc ? 'transparent' : color,
          borderRadius: 8,
          boxShadow: `0 0 8px ${shadowColor}`,
        }}
      >
        {imgSrc ? (
          <img
            src={imgSrc}
            alt={label}
            width={size}
            height={size}
            className="rounded-lg"
            style={{ objectFit: 'cover' }}
          />
        ) : (
          label.charAt(0).toUpperCase()
        )}
      </div>
      {status && (
        <span
          className="absolute -right-0.5 -bottom-0.5 block rounded-full border border-background"
          style={{
            width: 4,
            height: 4,
            backgroundColor: STATUS_COLORS[status],
            animation: statusAnimation,
          }}
        />
      )}
    </div>
  )
}
