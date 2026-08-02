import { ChevronDown } from 'lucide-react'
import { useNavigate } from 'react-router'

import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'
import { ACTIVE_STATUSES, AGENT_NAMES, CURRENT_USER_NAME } from '@/lib/constants'
import { UI_LABELS, UI_MISC } from '@/lib/ui-text'
import { useAdminStore } from '@/stores/admin'
import { useChatStore } from '@/stores/chat'

import { AgentHoverCard } from './AgentHoverCard'
import { useCollapsible } from './RightSidebar'

interface MembersSectionProps {
  agentTypes: AgentType[]
  agentNames: string[]
  sessions: AgentSessionInfo[]
}

type AgentDisplayStatus = 'ready' | 'running' | 'offline' | 'error'

function getAgentTypeLabel(agentType: AgentType): string {
  return AGENT_NAMES[agentType] ?? agentType
}

function getDisplayStatus(
  sessionId: string,
  sessions: Record<string, { status: string }>,
): AgentDisplayStatus {
  const session = sessions[sessionId]
  if (!session) return 'offline'
  if (ACTIVE_STATUSES.has(session.status)) return 'running'
  if (session.status === 'error' || session.status === 'failed') return 'error'
  if (session.status === 'idle' || session.status === 'done') return 'ready'
  return 'offline'
}

export function MembersSection({ agentTypes, agentNames, sessions }: MembersSectionProps) {
  const [open, toggleOpen] = useCollapsible('members')
  const chatSessions = useChatStore((s) => s.sessions)
  const navigate = useNavigate()
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
      navigate(`/agent/${encodeURIComponent(sessionId)}`)
    }
  }

  return (
    <div className="border-b border-sidebar-border">
      {/* Header */}
      <button
        type="button"
        className="flex w-full items-center justify-between px-4 py-3 pb-2.5 text-left user-select-none focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ring"
        onClick={toggleOpen}
      >
        <span className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-text-secondary transition-[transform,opacity] hover:text-foreground">
          {UI_LABELS.GROUP_MEMBERS}
          <span className="rounded-full bg-accent px-1.5 py-px text-[11px] font-normal tracking-normal text-tertiary">
            {members.length + 1}
          </span>
        </span>
        <ChevronDown
          className={`h-3.5 w-3.5 text-tertiary transition-transform ${open ? '' : '-rotate-90'}`}
          strokeWidth={1.25}
        />
      </button>

      {/* Body */}
      <div
        className={`overflow-hidden transition-[max-height] duration-200 ease-out ${open ? 'max-h-[600px] overflow-y-auto' : 'max-h-0'}`}
      >
        <div className="px-4 pb-3.5">
          {/* Owner (self) — uses real admin avatar, same style as IconSidebar */}
          <div className="flex items-center gap-2.5 rounded-md px-2 py-1.5">
            <div className="relative inline-flex shrink-0">
              <div className="rounded-[9px] border border-border/70 bg-card p-0.5 shadow-[0_8px_20px_rgba(0,0,0,0.16)]">
                <div className="flex h-8 w-8 items-center justify-center overflow-hidden rounded-[7px]">
                  <img
                    src={
                      adminAvatarUrl ||
                      'https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede'
                    }
                    alt={CURRENT_USER_NAME}
                    className="h-full w-full rounded-[7px] object-cover"
                    onError={(event) => {
                      event.currentTarget.src = '/favicon.svg'
                    }}
                  />
                </div>
              </div>
              <span className="absolute -right-0.5 -bottom-0.5 block h-1.5 w-1.5 rounded-full border border-background bg-success" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="text-[13px] font-medium">{CURRENT_USER_NAME}</div>
              <div className="text-[11px] text-tertiary">{UI_MISC.USER}</div>
            </div>
          </div>

          {/* Agent members */}
          {members.map((member, i) => {
            const displayStatus = getDisplayStatus(member.sessionId, chatSessions)

            return (
              <button
                type="button"
                key={i}
                className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left transition-[background,transform,opacity] hover:bg-bg-hover active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => handleNavigate(member.sessionId)}
              >
                <AgentHoverCard
                  agentType={member.type}
                  agentName={member.name}
                  sessionId={member.sessionId}
                  avatarUrl={member.avatarUrl}
                  status={displayStatus}
                  keyboardInteractive={false}
                />
                <div className="min-w-0 flex-1">
                  <div className="text-[13px] font-medium">{member.name}</div>
                  <div className="text-[11px] text-tertiary">{getAgentTypeLabel(member.type)}</div>
                </div>
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}
