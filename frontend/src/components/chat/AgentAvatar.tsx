import type { AgentType } from '@/generated/request'

const AGENT_COLORS: Record<AgentType, string> = {
  'claude-code': 'var(--agent-claude)',
  opencode: 'var(--agent-opencode)',
  orchestrator: 'var(--agent-orchestrator)',
}

const AGENT_SHADOW_COLORS: Record<AgentType, string> = {
  'claude-code': '#DA7756',
  opencode: '#10B981',
  orchestrator: '#EAB308',
}

const AGENT_LABELS: Record<AgentType, string> = {
  'claude-code': 'Claude',
  opencode: 'OpenCode',
  orchestrator: 'Orchestrator',
}

type Status = 'ready' | 'running' | 'offline' | 'error'

const STATUS_COLORS: Record<Status, string> = {
  ready: '#22C55E',
  running: '#F59E0B',
  offline: '#5A6070',
  error: '#EF4444',
}

function diceBearUrl(name: string): string {
  return `https://api.dicebear.com/7.x/initials/svg?seed=${encodeURIComponent(name)}`
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
  const color = AGENT_COLORS[agentType] ?? 'var(--color-brand)'
  const shadowColor = AGENT_SHADOW_COLORS[agentType] ?? '#6366F1'
  const label = agentName ?? AGENT_LABELS[agentType] ?? agentType

  const statusAnimation =
    status === 'ready'
      ? 'status-ready-pulse 2s ease-in-out infinite'
      : status === 'running'
        ? 'status-running-spin 1.5s linear infinite'
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
          className="absolute -right-0.5 -bottom-0.5 block rounded-full border-2 border-[var(--bg-canvas)]"
          style={{
            width: 8,
            height: 8,
            backgroundColor: STATUS_COLORS[status],
            animation: statusAnimation,
          }}
        />
      )}
    </div>
  )
}
