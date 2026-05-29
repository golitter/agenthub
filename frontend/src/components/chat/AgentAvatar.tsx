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

const SIZE_CLASSES: Record<number, string> = {
  24: 'h-6 w-6',
  32: 'h-8 w-8',
  40: 'h-10 w-10',
  48: 'h-12 w-12',
}

function diceBearUrl(seed: string): string {
  return `https://api.dicebear.com/9.x/bottts/svg?seed=${encodeURIComponent(seed)}`
}

interface AgentAvatarProps {
  agentType: AgentType
  status?: Status
  size?: number
  avatarUrl?: string
  agentName?: string
  sessionId?: string
}

export function AgentAvatar({
  agentType,
  status = 'offline',
  size = 32,
  avatarUrl,
  agentName,
  sessionId,
}: AgentAvatarProps) {
  const color = AGENT_COLORS[agentType] ?? 'var(--primary)'
  const label = agentName ?? AGENT_NAMES[agentType] ?? agentType

  const statusAnimation =
    status === 'ready'
      ? `status-ready-pulse ${STATUS_READY_DURATION} ease-out infinite`
      : status === 'running'
        ? `status-running-spin ${STATUS_RUNNING_DURATION} linear infinite`
        : undefined

  const imgSrc =
    avatarUrl ||
    (sessionId ? diceBearUrl(sessionId) : agentName ? diceBearUrl(agentName) : undefined)
  const sizeClass = SIZE_CLASSES[size]
  const sizeStyle = sizeClass ? {} : { width: size, height: size }

  return (
    <div className="relative inline-flex shrink-0" title={label}>
      <div
        className="rounded-lg p-0.5"
        style={{ background: imgSrc ? 'transparent' : `${color}20` }}
      >
        <div
          className={`flex items-center justify-center overflow-hidden rounded-lg text-xs font-semibold text-foreground ${sizeClass ?? ''}`}
          style={{
            ...sizeStyle,
            backgroundColor: imgSrc ? 'transparent' : color,
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
