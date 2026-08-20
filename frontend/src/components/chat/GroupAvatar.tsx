import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'

import { AgentAvatar } from './AgentAvatar'

interface GroupAvatarProps {
  agentTypes: AgentType[]
  agentNames: string[]
  sessions?: AgentSessionInfo[]
  size?: number
}

export function GroupAvatar({ agentTypes, agentNames, sessions, size = 32 }: GroupAvatarProps) {
  const maxShow = 3
  const shown = agentTypes.slice(0, maxShow)
  const overlap = Math.max(4, size * 0.25)
  const inner = size - overlap * (shown.length - 1)

  return (
    <div className="relative inline-flex shrink-0" title={agentNames.join(', ')}>
      <div className="flex items-center" style={{ width: size, height: size }}>
        {shown.map((type, i) => {
          const session = sessions?.[i]
          const label = session?.agentName ?? agentNames[i] ?? type
          // AgentAvatar 的 size 是图片内容尺寸，外框还会增加 border + padding。
          // 这里扣除外框占用后，保证重叠布局仍然保持原有的整体尺寸。
          const avatarSize = Math.max(8, inner - 6)
          return (
            <div
              key={i}
              className="relative shrink-0"
              style={{
                width: inner,
                height: inner,
                marginLeft: i === 0 ? 0 : -overlap,
                zIndex: shown.length - i,
              }}
            >
              <AgentAvatar
                agentType={type}
                status={null}
                size={avatarSize}
                avatarUrl={session?.avatarUrl}
                agentName={label}
                sessionId={session?.sessionId}
              />
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
