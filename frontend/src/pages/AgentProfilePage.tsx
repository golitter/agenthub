import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Camera, Pencil, Plus, Trash2 } from 'lucide-react'
import { useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router'

import { AgentMeta } from '@/components/chat/AgentMeta'
import { SkillCard } from '@/components/chat/SkillCard'
import type { AgentType } from '@/generated/request'
import type { AgentDetail } from '@/lib/api'
import {
  fetchAgentDetail,
  fetchSkills,
  importSkill,
  removeSkill,
  updateAgentSoul,
  updateSession,
  uploadAvatar,
} from '@/lib/api'
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

  const [showImportDialog, setShowImportDialog] = useState(false)

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

  const isAdapterAgent = ['claude-code', 'opencode', 'codex'].includes(detail.agent_type)

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
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-xs font-semibold uppercase tracking-wider text-foreground/50">
              Skills
            </h2>
            <span className="text-[11px] text-tertiary">{detail.skills.length} 个技能</span>
          </div>
          {detail.skills.length > 0 ? (
            <div className="space-y-2">
              {detail.skills.map((s) => (
                <div key={s.name} className="flex items-center gap-2">
                  <div className="flex-1">
                    <SkillCard skill={s} />
                  </div>
                  {!s.builtin && isAdapterAgent && (
                    <button
                      className="shrink-0 rounded-[6px] border border-red-500/20 bg-red-500/10 p-1.5 text-red-500 transition-[transform,opacity] hover:bg-red-500/20"
                      title="移除 Skill"
                      onClick={async () => {
                        try {
                          await removeSkill(s.name, sessionId)
                          await queryClient.invalidateQueries({
                            queryKey: ['agent-detail', sessionId],
                          })
                        } catch {
                          /* ignore */
                        }
                      }}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-tertiary">暂无技能</p>
          )}
          {isAdapterAgent && (
            <button
              className="mt-3 flex w-full items-center justify-center gap-2 rounded-[8px] border border-dashed border-border py-2.5 text-[12px] text-tertiary transition-[transform,opacity] hover:border-primary hover:text-primary hover:bg-primary/8"
              onClick={() => setShowImportDialog(true)}
            >
              <Plus className="h-4 w-4" />
              导入外部 Skill
            </button>
          )}
        </section>

        {/* Import Dialog */}
        {showImportDialog && (
          <ImportSkillDialog
            sessionId={sessionId}
            currentSkills={detail.skills.map((s) => s.name)}
            onClose={() => setShowImportDialog(false)}
            onImported={() => {
              queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
              setShowImportDialog(false)
            }}
          />
        )}

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

// ── Import Skill Dialog ──

function ImportSkillDialog({
  sessionId,
  currentSkills,
  onClose,
  onImported,
}: {
  sessionId: string
  currentSkills: string[]
  onClose: () => void
  onImported: () => void
}) {
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(false)

  const { data: hubSkills = [] } = useQuery({
    queryKey: ['skills'],
    queryFn: fetchSkills,
  })

  const externals = hubSkills.filter((s) => !s.builtin)
  const alreadyImported = new Set(currentSkills)

  const toggle = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(name)) {
        next.delete(name)
      } else {
        next.add(name)
      }
      return next
    })
  }

  const handleImport = async () => {
    if (selected.size === 0) return
    setLoading(true)
    try {
      await Promise.all(Array.from(selected).map((name) => importSkill(name, sessionId)))
      onImported()
    } catch {
      /* ignore */
    } finally {
      setLoading(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={onClose}
    >
      <div
        className="max-h-[85vh] w-[90%] max-w-[440px] overflow-auto rounded-xl border border-border bg-card p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="mb-2 flex items-center gap-2 text-[15px] font-semibold">
          <Plus className="h-[18px] w-[18px] text-primary" />
          导入外部 Skill
        </h3>
        <p className="mb-4 text-[13px] text-text-secondary">
          从技能库中选择要导入到此 Agent 的外部 Skill。
        </p>

        <div className="flex max-h-[300px] flex-col gap-1.5 overflow-auto">
          {externals.length === 0 && (
            <p className="py-8 text-center text-[12px] text-tertiary">技能库中暂无外部 Skill</p>
          )}
          {externals.map((skill) => {
            const imported = alreadyImported.has(skill.name)
            const isSelected = selected.has(skill.name)
            return (
              <button
                key={skill.name}
                className={cn(
                  'flex items-center gap-2.5 rounded-[8px] border p-2.5 text-left transition-all',
                  imported
                    ? 'cursor-not-allowed border-border bg-muted/40 opacity-40'
                    : isSelected
                      ? 'border-primary/15 bg-primary/8'
                      : 'border-border hover:bg-hover',
                )}
                disabled={imported}
                onClick={() => !imported && toggle(skill.name)}
              >
                <div
                  className={cn(
                    'flex h-4 w-4 shrink-0 items-center justify-center rounded-[4px] border',
                    isSelected ? 'border-primary bg-primary' : 'border-tertiary',
                  )}
                >
                  {isSelected && (
                    <svg
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="white"
                      strokeWidth="3"
                      className="h-2.5 w-2.5"
                    >
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  )}
                </div>
                <span className="text-[13px] font-medium">{skill.name}</span>
                <span className="ml-auto shrink-0 text-[10px] text-tertiary">
                  {imported ? '已导入' : ''}
                </span>
              </button>
            )
          })}
        </div>

        <div className="mt-5 flex justify-end gap-2">
          <button
            className="rounded-[8px] border border-border bg-muted px-4 py-2 text-[12px] font-medium text-text-secondary hover:bg-hover"
            onClick={onClose}
          >
            取消
          </button>
          <button
            className="inline-flex items-center gap-1.5 rounded-[8px] bg-primary px-4 py-2 text-[12px] font-medium text-white disabled:opacity-50"
            onClick={handleImport}
            disabled={loading || selected.size === 0}
          >
            确认导入
          </button>
        </div>
      </div>
    </div>
  )
}
