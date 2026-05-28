import type { AgentType } from '@/generated/request'
import { AGENT_COLORS } from '@/lib/constants'

interface GroupAvatarProps {
  agentTypes: AgentType[]
  agentNames: string[]
  size?: number
}

export function GroupAvatar({ agentTypes, agentNames, size = 32 }: GroupAvatarProps) {
  const maxShow = 3
  const shown = agentTypes.slice(0, maxShow)
  const overlap = Math.max(4, size * 0.25)
  const inner = size - overlap * (shown.length - 1)

  return (
    <div className="relative inline-flex shrink-0" title={agentNames.join(', ')}>
      <div className="flex items-center" style={{ width: size, height: size }}>
        {shown.map((type, i) => {
          const color = AGENT_COLORS[type] ?? 'var(--primary)'
          const label = agentNames[i] ?? type
          return (
            <div
              key={i}
              className="flex items-center justify-center overflow-hidden rounded-full border-2 border-background text-[11px] font-semibold text-foreground"
              style={{
                width: inner,
                height: inner,
                backgroundColor: color,
                marginLeft: i === 0 ? 0 : -overlap,
                zIndex: shown.length - i,
              }}
            >
              {label.charAt(0).toUpperCase()}
            </div>
          )
        })}
      </div>
      {agentTypes.length > maxShow && (
        <span
          className="absolute -right-0.5 -bottom-0.5 flex items-center justify-center rounded-full bg-muted text-[11px] font-medium text-muted-foreground"
          style={{ width: 12, height: 12 }}
        >
          +{agentTypes.length - maxShow}
        </span>
      )}
    </div>
  )
}
