import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router'

import { Popover, PopoverAnchor, PopoverContent } from '@/components/ui/popover'
import type { AgentType } from '@/generated/request'
import type { AgentProfile } from '@/lib/api'
import { fetchAgentProfile } from '@/lib/api'
import { AGENT_NAMES } from '@/lib/constants'
import { UI_ACTIONS, UI_AGENT_STATUS, UI_MESSAGES, UI_MISC } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

import { AgentAvatar } from './AgentAvatar'

type Status = 'ready' | 'running' | 'offline' | 'error'

const STATUS_BADGE: Record<Status, { label: string; cls: string }> = {
  ready: { label: UI_AGENT_STATUS.READY, cls: 'bg-success/10 text-success' },
  running: { label: UI_AGENT_STATUS.RUNNING, cls: 'bg-warning/10 text-warning' },
  offline: { label: UI_AGENT_STATUS.OFFLINE, cls: 'bg-tertiary/10 text-tertiary' },
  error: { label: UI_AGENT_STATUS.ERROR, cls: 'bg-error/10 text-error' },
}

const MAX_VISIBLE_SKILLS = 3
const SHOW_DELAY = 300
const HIDE_DELAY = 200

interface AgentHoverCardProps {
  sessionId: string
  agentType: string
  agentName?: string
  avatarUrl?: string
  status?: Status
  keyboardInteractive?: boolean
}

function truncateId(id: string): string {
  return id.length > 16 ? `${id.slice(0, 12)}…` : id
}

function HoverCardContent({
  sessionId,
  agentType,
  agentName,
  avatarUrl,
  status = 'offline',
}: AgentHoverCardProps) {
  const { data: profile } = useQuery<AgentProfile>({
    queryKey: ['agent-profile', sessionId],
    queryFn: () => fetchAgentProfile(sessionId),
    enabled: Boolean(sessionId),
    staleTime: 60_000,
  })

  const name = profile?.agent_name ?? agentName ?? AGENT_NAMES[agentType as AgentType] ?? agentType
  const skills = profile?.skills ?? []
  const displaySkills = skills.slice(0, MAX_VISIBLE_SKILLS)
  const remaining = skills.length - MAX_VISIBLE_SKILLS
  const badge = STATUS_BADGE[status]

  return (
    <div className="w-full min-w-0 space-y-3 p-3 sm:p-4">
      {/* 身份信息 */}
      <div className="flex items-center gap-3">
        <AgentAvatar
          agentType={agentType as never}
          status={status}
          avatarUrl={avatarUrl ?? profile?.avatar_url}
          agentName={name}
          sessionId={sessionId}
          size={32}
        />
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold text-popover-foreground">{name}</div>
          <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs text-popover-foreground/70">
            <span className="min-w-0 break-all">{agentType}</span>
            <span
              className={cn(
                'inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px]',
                badge.cls,
              )}
            >
              <span className="h-1 w-1 rounded-full bg-current" />
              {badge.label}
            </span>
          </div>
        </div>
      </div>

      {/* 技能 */}
      {skills.length > 0 && (
        <>
          <div className="h-px bg-border" />
          <div>
            <div className="mb-1.5 text-[11px] font-medium uppercase tracking-wider text-popover-foreground/60">
              Skills
            </div>
            {displaySkills.map((s) => (
              <div key={s.name} className="space-y-0.5 py-1">
                <div className="text-[13px] font-semibold">{s.name}</div>
                <div className="truncate text-xs text-popover-foreground/70">{s.description}</div>
              </div>
            ))}
            {remaining > 0 && (
              <div className="pt-1 text-xs text-brand">{`+${remaining} ${UI_MISC.MORE}`}</div>
            )}
          </div>
        </>
      )}

      {/* 元信息 + 链接 */}
      <div className="h-px bg-border" />
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
        <span className="font-mono text-xs text-popover-foreground/60">
          {truncateId(sessionId)}
        </span>
        {sessionId ? (
          <Link
            to={`/agent/${encodeURIComponent(sessionId)}`}
            className="text-xs text-brand hover:underline"
          >
            {UI_ACTIONS.VIEW_DETAIL}
          </Link>
        ) : (
          <span className="text-xs text-tertiary">{UI_MESSAGES.NO_DATA}</span>
        )}
      </div>
    </div>
  )
}

export function AgentHoverCard(props: AgentHoverCardProps) {
  const [open, setOpen] = useState(false)
  const showTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const hideTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pointerInside = useRef(false)
  const focusInside = useRef(false)
  const keyboardOpen = useRef(false)
  const focusGuardTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const suppressFocusOpen = useRef(false)
  const keyboardInteractive = props.keyboardInteractive !== false

  const cancelHide = useCallback(() => {
    if (hideTimer.current !== null) {
      clearTimeout(hideTimer.current)
      hideTimer.current = null
    }
  }, [])

  const cancelShow = useCallback(() => {
    if (showTimer.current !== null) {
      clearTimeout(showTimer.current)
      showTimer.current = null
    }
  }, [])

  const guardFocusOpen = useCallback(() => {
    suppressFocusOpen.current = true
    if (focusGuardTimer.current !== null) clearTimeout(focusGuardTimer.current)
    focusGuardTimer.current = setTimeout(() => {
      suppressFocusOpen.current = false
      focusGuardTimer.current = null
    }, HIDE_DELAY)
  }, [])

  const handleShow = useCallback(() => {
    cancelHide()
    showTimer.current = setTimeout(() => setOpen(true), SHOW_DELAY)
  }, [cancelHide])

  const handleHide = useCallback(() => {
    cancelShow()
    hideTimer.current = setTimeout(() => {
      if (!pointerInside.current && !focusInside.current) setOpen(false)
    }, HIDE_DELAY)
  }, [cancelShow])

  useEffect(() => {
    return () => {
      cancelShow()
      cancelHide()
      if (focusGuardTimer.current !== null) clearTimeout(focusGuardTimer.current)
    }
  }, [cancelHide, cancelShow])

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        // Radix 只会请求关闭（Escape / outside click），
        // 打开完全由 hover 控制，所以忽略 open=true 的请求
        if (!nextOpen) {
          guardFocusOpen()
          setOpen(false)
        }
      }}
    >
      <PopoverAnchor asChild>
        <div
          role={keyboardInteractive ? 'button' : undefined}
          tabIndex={keyboardInteractive ? 0 : undefined}
          aria-label={
            keyboardInteractive ? `查看 ${props.agentName ?? props.agentType} 信息` : undefined
          }
          aria-expanded={keyboardInteractive ? open : undefined}
          onMouseEnter={handleShow}
          onMouseLeave={handleHide}
          onFocus={
            keyboardInteractive
              ? () => {
                  if (suppressFocusOpen.current) {
                    suppressFocusOpen.current = false
                    cancelHide()
                    return
                  }
                  pointerInside.current = true
                  focusInside.current = true
                  handleShow()
                }
              : undefined
          }
          onBlur={
            keyboardInteractive
              ? () => {
                  pointerInside.current = false
                  focusInside.current = false
                  handleHide()
                }
              : undefined
          }
          onKeyDown={
            keyboardInteractive
              ? (event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault()
                    cancelShow()
                    suppressFocusOpen.current = false
                    keyboardOpen.current = true
                    setOpen((current) => !current)
                  } else if (event.key === 'Escape') {
                    event.preventDefault()
                    setOpen(false)
                  }
                }
              : undefined
          }
          onClick={
            keyboardInteractive
              ? () => {
                  cancelShow()
                  cancelHide()
                  suppressFocusOpen.current = false
                  keyboardOpen.current = true
                  setOpen((current) => !current)
                }
              : undefined
          }
        >
          <AgentAvatar
            agentType={props.agentType as never}
            status={props.status ?? 'offline'}
            avatarUrl={props.avatarUrl}
            agentName={props.agentName}
            sessionId={props.sessionId}
          />
        </div>
      </PopoverAnchor>

      <PopoverContent
        side="right"
        align="start"
        sideOffset={8}
        className="w-auto p-0"
        onOpenAutoFocus={(event) => {
          if (!keyboardOpen.current) event.preventDefault()
          keyboardOpen.current = false
        }}
        onEscapeKeyDown={guardFocusOpen}
        onFocus={() => {
          focusInside.current = true
          cancelHide()
        }}
        onBlur={(event) => {
          if (
            event.relatedTarget instanceof Node &&
            event.currentTarget.contains(event.relatedTarget)
          ) {
            return
          }
          focusInside.current = false
          handleHide()
        }}
        onMouseEnter={() => {
          pointerInside.current = true
          cancelHide()
        }}
        onMouseLeave={() => {
          pointerInside.current = false
          handleHide()
        }}
      >
        <HoverCardContent {...props} />
      </PopoverContent>
    </Popover>
  )
}
