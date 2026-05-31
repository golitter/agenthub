import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'
import { AGENT_NAMES } from '@/lib/constants'
import { useAdminStore } from '@/stores/admin'
import { useChatStore } from '@/stores/chat'

import { AgentHoverCard } from './AgentHoverCard'
import { useCollapsible } from './RightSidebar'

interface MembersSectionProps {
  agentTypes: AgentType[]
  agentNames: string[]
  sessions: AgentSessionInfo[]
}

function getAgentTypeLabel(agentType: AgentType): string {
  return AGENT_NAMES[agentType] ?? agentType
}

function isOnline(sessionId: string, sessions: Record<string, { status: string }>): boolean {
  const session = sessions[sessionId]
  if (!session) return false
  return (
    session.status === 'streaming' ||
    session.status === 'loading' ||
    session.status === 'tool_running'
  )
}

export function MembersSection({ agentTypes, agentNames, sessions }: MembersSectionProps) {
  const [open, toggleOpen] = useCollapsible('members')
  const chatSessions = useChatStore((s) => s.sessions)
  const setCurrentSession = useChatStore((s) => s.setCurrentSession)
  const adminAvatarUrl = useAdminStore((s) => s.adminAvatarUrl)

  // Build member list: user (owner) + agents
  const members = agentTypes.map((type, i) => ({
    type,
    name: agentNames[i] ?? AGENT_NAMES[type] ?? type,
    sessionId: sessions[i]?.sessionId ?? '',
    avatarUrl: sessions[i]?.avatarUrl,
  }))

  const handleNavigate = (sessionId: string) => {
    if (sessionId) {
      setCurrentSession(sessionId)
    }
  }

  return (
    <div className="border-b border-sidebar-border">
      {/* Header */}
      <button
        type="button"
        className="flex w-full items-center justify-between px-4 py-3 pb-2.5 text-left user-select-none"
        onClick={toggleOpen}
      >
        <span className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-secondary transition-colors hover:text-foreground">
          群成员
          <span className="rounded-full bg-accent px-1.5 py-px text-[11px] font-normal tracking-normal text-tertiary">
            {members.length + 1}
          </span>
        </span>
        <svg
          className={`h-3.5 w-3.5 text-tertiary transition-transform ${open ? '' : '-rotate-90'}`}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>

      {/* Body */}
      <div
        className={`overflow-hidden transition-[max-height] duration-200 ease-out ${open ? 'max-h-[600px] overflow-y-auto' : 'max-h-0'}`}
      >
        <div className="px-4 pb-3.5">
          {/* Owner (self) — uses real admin avatar, same style as IconSidebar */}
          <div className="flex items-center gap-2.5 rounded-md px-2 py-1.5">
            <div className="relative inline-flex shrink-0">
              <div
                className="rounded-md p-0.5"
                style={{ background: 'transparent', boxShadow: '0 0 8px var(--agent-codex)' }}
              >
                <div className="flex h-8 w-8 items-center justify-center overflow-hidden rounded-md">
                  <img
                    src={
                      adminAvatarUrl ||
                      'https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede'
                    }
                    alt="田乐檬"
                    className="h-full w-full rounded-md object-cover"
                  />
                </div>
              </div>
              <span
                className="absolute -right-0.5 -bottom-0.5 block rounded-full border border-background"
                style={{ width: 6, height: 6, backgroundColor: 'var(--color-success)' }}
              />
            </div>
            <div className="min-w-0 flex-1">
              <div className="text-[13px] font-medium">田乐檬</div>
              <div className="text-[11px] text-tertiary">用户</div>
            </div>
          </div>

          {/* Agent members */}
          {members.map((member, i) => {
            const online = isOnline(member.sessionId, chatSessions)

            return (
              <div
                key={i}
                className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 transition-colors hover:bg-bg-hover"
                onClick={() => handleNavigate(member.sessionId)}
                role="button"
                tabIndex={0}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleNavigate(member.sessionId)
                }}
              >
                <AgentHoverCard
                  agentType={member.type}
                  agentName={member.name}
                  sessionId={member.sessionId}
                  avatarUrl={member.avatarUrl}
                  status={online ? 'running' : 'offline'}
                />
                <div className="min-w-0 flex-1 cursor-pointer">
                  <div className="text-[13px] font-medium">{member.name}</div>
                  <div className="text-[11px] text-tertiary">{getAgentTypeLabel(member.type)}</div>
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
