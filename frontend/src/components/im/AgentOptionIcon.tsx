import { useState } from 'react'

import type { AgentType } from '@/generated/request'
import { AGENT_COLORS, AGENT_NAMES } from '@/lib/constants'
import { cn } from '@/lib/utils'

// 仅供“新建对话”里的 Agent 选项使用，不与会话头像 / avatarUrl 共用。
// 对应文件放在 frontend/public/agent-icons/ 下，浏览器访问路径从 / 开始。
const LOCAL_AGENT_ICON_PATHS: Record<AgentType, string> = {
  'claude-code': '/agent-icons/claude-code.svg',
  opencode: '/agent-icons/opencode.png',
  orchestrator: '/agent-icons/orchestrator.svg',
  codex: '/agent-icons/codex.svg',
  pi: '/agent-icons/pi.svg',
}

interface AgentOptionIconProps {
  agentType: AgentType
}

export function AgentOptionIcon({ agentType }: AgentOptionIconProps) {
  const [iconFailed, setIconFailed] = useState(false)
  const color = AGENT_COLORS[agentType] ?? 'var(--primary)'
  const label = AGENT_NAMES[agentType] ?? agentType
  const iconPath = LOCAL_AGENT_ICON_PATHS[agentType]
  const needsDarkContrast = agentType === 'codex' || agentType === 'orchestrator'

  return (
    <span className="relative inline-flex h-9 w-9 shrink-0" title={label}>
      <span
        className="flex h-9 w-9 items-center justify-center overflow-hidden rounded-[9px] border border-border/70 bg-card p-0.5 shadow-[0_8px_20px_rgba(0,0,0,0.16)]"
      >
        <span
          className="flex h-8 w-8 items-center justify-center overflow-hidden rounded-[7px] text-xs font-semibold text-foreground"
          style={{ backgroundColor: iconFailed ? color : 'var(--bg-card)' }}
        >
          {iconFailed ? (
            label.charAt(0).toUpperCase()
          ) : (
            <img
              src={iconPath}
              alt=""
              width={32}
              height={32}
              className={cn(
                'h-full w-full rounded-[6px] object-contain p-1',
                needsDarkContrast && 'dark:invert',
              )}
              draggable={false}
              onError={() => setIconFailed(true)}
            />
          )}
        </span>
      </span>
    </span>
  )
}
