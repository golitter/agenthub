import { useQuery } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import { useNavigate, useParams } from 'react-router'

import { AgentMeta } from '@/components/chat/AgentMeta'
import { SkillCard } from '@/components/chat/SkillCard'
import type { AgentDetail } from '@/lib/api'
import { fetchAgentDetail } from '@/lib/api'
import { AGENT_COLORS, AGENT_NAMES } from '@/lib/constants'

type Status = 'ready' | 'running' | 'offline' | 'error'

const STATUS_BADGE: Record<Status, { label: string; cls: string }> = {
  ready: { label: 'ready', cls: 'bg-success/10 text-success' },
  running: { label: 'running', cls: 'bg-warning/10 text-warning' },
  offline: { label: 'offline', cls: 'bg-tertiary/10 text-tertiary' },
  error: { label: 'error', cls: 'bg-error/10 text-error' },
}

export function AgentProfilePage() {
  const { sessionId } = useParams<{ sessionId: string }>()
  const navigate = useNavigate()

  const {
    data: detail,
    isLoading,
    error,
  } = useQuery<AgentDetail>({
    queryKey: ['agent-detail', sessionId],
    queryFn: () => fetchAgentDetail(sessionId!),
    enabled: !!sessionId,
  })

  if (!sessionId) return null

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <span className="text-sm text-tertiary">Loading...</span>
      </div>
    )
  }

  if (error || !detail) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-3 bg-background">
        <span className="text-sm text-error">Failed to load agent profile</span>
        <button onClick={() => navigate(-1)} className="text-sm text-brand hover:underline">
          返回
        </button>
      </div>
    )
  }

  const name = detail.agent_name || AGENT_NAMES[detail.agent_type] || detail.agent_type
  const status = detail.status as Status
  const badge = STATUS_BADGE[status] ?? STATUS_BADGE.offline
  const color = AGENT_COLORS[detail.agent_type] ?? 'var(--primary)'

  return (
    <div className="flex h-screen bg-background">
      <div className="mx-auto w-full max-w-[640px] p-6">
        <button
          onClick={() => navigate(-1)}
          className="mb-6 flex items-center gap-1.5 text-[13px] text-secondary hover:text-primary"
        >
          <ArrowLeft className="h-4 w-4" />
          返回对话
        </button>

        {/* Header */}
        <div className="mb-6 flex items-center gap-4">
          <div
            className="flex h-16 w-16 items-center justify-center rounded-xl text-2xl font-semibold text-white"
            style={{ background: color, boxShadow: `0 0 12px ${color}` }}
          >
            {name.charAt(0).toUpperCase()}
          </div>
          <div>
            <h1 className="text-xl font-semibold">{name}</h1>
            <div className="mt-1 flex items-center gap-1.5 text-sm text-foreground/70">
              <span>{detail.agent_type}</span>
              <span
                className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] ${badge.cls}`}
              >
                <span className="h-1 w-1 rounded-full bg-current" />
                {badge.label}
              </span>
            </div>
          </div>
        </div>

        {/* Meta */}
        <section className="mb-6">
          <h2 className="mb-3 text-xs font-semibold uppercase tracking-wider text-foreground/50">
            元数据
          </h2>
          <AgentMeta detail={detail} />
        </section>

        {/* Skills */}
        <section>
          <h2 className="mb-3 text-xs font-semibold uppercase tracking-wider text-foreground/50">
            Skills
          </h2>
          {detail.skills.length > 0 ? (
            <div className="space-y-2">
              {detail.skills.map((s) => (
                <SkillCard key={s.name} skill={s} />
              ))}
            </div>
          ) : (
            <p className="text-sm text-tertiary">暂无技能</p>
          )}
        </section>
      </div>
    </div>
  )
}
