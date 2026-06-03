import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Camera, Pencil } from 'lucide-react'
import { useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router'

import { AgentMeta } from '@/components/chat/AgentMeta'
import { SkillCard } from '@/components/chat/SkillCard'
import type { AgentType } from '@/generated/request'
import type { AgentDetail } from '@/lib/api'
import { fetchAgentDetail, updateAgentSoul, updateSession, uploadAvatar } from '@/lib/api'
import { AGENT_COLORS, AGENT_NAMES } from '@/lib/constants'
import { cn } from '@/lib/utils'

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
  const queryClient = useQueryClient()
  const fileRef = useRef<HTMLInputElement>(null)

  const [editingName, setEditingName] = useState(false)
  const [nameDraft, setNameDraft] = useState('')
  const [saving, setSaving] = useState(false)

  const [editingSoul, setEditingSoul] = useState(false)
  const [soulDraft, setSoulDraft] = useState('')
  const [soulSaving, setSoulSaving] = useState(false)
  const [soulError, setSoulError] = useState('')

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

  const agentType = detail.agent_type as AgentType
  const name = detail.agent_name || AGENT_NAMES[agentType] || detail.agent_type
  const status = detail.status as Status
  const badge = STATUS_BADGE[status] ?? STATUS_BADGE.offline
  const color = AGENT_COLORS[agentType] ?? 'var(--primary)'

  const handleAvatarChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    try {
      const url = await uploadAvatar(file)
      await updateSession(sessionId, { avatar_url: url })
      await queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
      await queryClient.invalidateQueries({ queryKey: ['conversations'] })
    } catch {
      // ignore — user can retry
    }
  }

  const startEditName = () => {
    setNameDraft(name)
    setEditingName(true)
  }

  const saveName = async () => {
    const trimmed = nameDraft.trim()
    if (!trimmed || trimmed === name) {
      setEditingName(false)
      return
    }
    setSaving(true)
    try {
      await updateSession(sessionId, { agent_name: trimmed })
      await queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
      await queryClient.invalidateQueries({ queryKey: ['conversations'] })
    } catch {
      // ignore
    } finally {
      setSaving(false)
      setEditingName(false)
    }
  }

  const startEditSoul = () => {
    setSoulDraft(detail.soul_md || '')
    setEditingSoul(true)
    setSoulError('')
  }

  const countChars = (s: string) => s.replace(/ /g, '').length

  const saveSoul = async () => {
    const trimmed = soulDraft.trim()
    if (countChars(trimmed) > 300) {
      setSoulError(`不能超过 300 字（不含空格），当前 ${countChars(trimmed)} 字`)
      return
    }
    setSoulSaving(true)
    try {
      await updateAgentSoul(sessionId, trimmed)
      await queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
      setEditingSoul(false)
      setSoulError('')
    } catch {
      // ignore
    } finally {
      setSoulSaving(false)
    }
  }

  const clearSoul = async () => {
    setSoulSaving(true)
    try {
      await updateAgentSoul(sessionId, '')
      await queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
    } catch {
      // ignore
    } finally {
      setSoulSaving(false)
    }
  }

  const soulContent = detail.soul_md || ''
  const soulCharCount = countChars(soulContent)

  return (
    <div className="flex h-screen bg-background">
      <div className="mx-auto w-full max-w-[640px] p-6">
        <button
          onClick={() => navigate(-1)}
          className="mb-6 flex items-center gap-1.5 text-[13px] text-text-secondary hover:text-primary"
        >
          <ArrowLeft className="h-4 w-4" strokeWidth={1.25} />
          返回对话
        </button>

        {/* Header */}
        <div className="mb-6 flex items-center gap-4">
          <div className="group relative">
            <div
              className="flex h-16 w-16 items-center justify-center overflow-hidden rounded-lg"
              style={{ boxShadow: `0 0 12px ${color}` }}
            >
              <img
                src={
                  detail.avatar_url ||
                  `https://api.dicebear.com/9.x/bottts/svg?seed=${encodeURIComponent(name)}`
                }
                alt={name}
                className="h-full w-full rounded-lg object-cover"
              />
            </div>
            <button
              className="absolute inset-0 flex items-center justify-center rounded-lg bg-black/40 opacity-0 transition-opacity group-hover:opacity-100"
              onClick={() => fileRef.current?.click()}
            >
              <Camera className="h-5 w-5 text-primary-foreground" strokeWidth={1.25} />
            </button>
            <input
              ref={fileRef}
              type="file"
              accept="image/jpeg,image/png,image/gif,image/webp"
              className="hidden"
              onChange={handleAvatarChange}
            />
          </div>
          <div className="flex-1">
            {editingName ? (
              <div className="flex items-center gap-2">
                <input
                  autoFocus
                  value={nameDraft}
                  onChange={(e) => setNameDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') saveName()
                    if (e.key === 'Escape') setEditingName(false)
                  }}
                  className="w-full rounded-md border border-border bg-background px-2 py-1 text-xl font-semibold text-foreground outline-none"
                  disabled={saving}
                />
                <button
                  onClick={saveName}
                  disabled={saving || !nameDraft.trim()}
                  className="shrink-0 rounded-md bg-primary px-3 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
                >
                  {saving ? '...' : '保存'}
                </button>
              </div>
            ) : (
              <div className="flex items-center gap-2">
                <h1 className="text-xl font-semibold">{name}</h1>
                <button
                  className="rounded-md p-1 text-foreground/40 hover:bg-foreground/5 hover:text-foreground/70"
                  onClick={startEditName}
                >
                  <Pencil className="h-3.5 w-3.5" strokeWidth={1.25} />
                </button>
              </div>
            )}
            <div className="mt-1 flex items-center gap-1.5 text-sm text-foreground/70">
              <span>{detail.agent_type}</span>
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

        {/* SOUL.md */}
        <section className="mt-6">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-xs font-semibold uppercase tracking-wider text-foreground/50">
              SOUL.md
            </h2>
            {!editingSoul && (
              <button
                className="flex items-center gap-1 rounded p-1 text-foreground/40 hover:bg-foreground/5 hover:text-foreground/70"
                onClick={startEditSoul}
              >
                <Pencil className="h-3 w-3" strokeWidth={1.25} />
              </button>
            )}
          </div>
          {editingSoul ? (
            <div className="space-y-2">
              <textarea
                autoFocus
                value={soulDraft}
                onChange={(e) => {
                  setSoulDraft(e.target.value)
                  setSoulError('')
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') setEditingSoul(false)
                }}
                className="min-h-[120px] w-full resize-y rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground outline-none"
                placeholder="描述这个 Agent 的身份和性格（不超过 300 字，不含空格）"
                maxLength={330}
                disabled={soulSaving}
              />
              <div className="flex items-center justify-between">
                <span
                  className="text-xs"
                  style={{
                    color:
                      countChars(soulDraft) > 300 ? 'var(--destructive)' : 'var(--text-tertiary)',
                  }}
                >
                  {countChars(soulDraft)}/300
                </span>
                <div className="flex items-center gap-2">
                  <button
                    className="rounded-md px-3 py-1 text-xs text-muted-foreground hover:text-foreground"
                    onClick={() => {
                      setEditingSoul(false)
                      setSoulError('')
                    }}
                  >
                    取消
                  </button>
                  <button
                    className="rounded-md bg-primary px-3 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
                    onClick={saveSoul}
                    disabled={soulSaving || countChars(soulDraft) > 300}
                  >
                    {soulSaving ? '保存中...' : '保存'}
                  </button>
                </div>
              </div>
              {soulError && <p className="text-xs text-destructive">{soulError}</p>}
            </div>
          ) : soulContent ? (
            <div className="rounded-md border border-border bg-background px-3 py-2">
              <p className="whitespace-pre-wrap text-sm text-foreground/80">{soulContent}</p>
              <div className="mt-1.5 flex items-center justify-between">
                <span className="text-xs text-tertiary">{soulCharCount}/300 字（不含空格）</span>
                <button
                  className="text-xs text-destructive/60 hover:text-destructive"
                  onClick={clearSoul}
                >
                  清除
                </button>
              </div>
            </div>
          ) : (
            <button
              className="rounded-md border border-dashed border-border px-3 py-2 text-sm text-muted-foreground hover:border-foreground/20 hover:text-foreground/70"
              onClick={startEditSoul}
            >
              点击编写 SOUL.md — 描述 Agent 身份和性格
            </button>
          )}
        </section>
      </div>
    </div>
  )
}
