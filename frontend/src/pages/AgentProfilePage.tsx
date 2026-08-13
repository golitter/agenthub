import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Camera, Pencil, Plus, Trash2 } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router'

import { AgentMeta } from '@/components/chat/AgentMeta'
import { SkillCard } from '@/components/chat/SkillCard'
import type { AgentType } from '@/generated/request'
import { useDialogFocusTrap } from '@/hooks/use-dialog-focus-trap'
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
import { AGENT_NAMES } from '@/lib/constants'
import {
  UI_ACTIONS,
  UI_AGENT_STATUS,
  UI_ERRORS,
  UI_LABELS,
  UI_MESSAGES,
  UI_MISC,
  UI_PLACEHOLDERS,
  UI_PROFILE,
  UI_STATUS,
} from '@/lib/ui-text'
import { cn } from '@/lib/utils'

type Status = 'ready' | 'running' | 'offline' | 'error'

const STATUS_BADGE: Record<Status, { label: string; cls: string }> = {
  ready: { label: UI_AGENT_STATUS.READY, cls: 'bg-success/10 text-success' },
  running: { label: UI_AGENT_STATUS.RUNNING, cls: 'bg-warning/10 text-warning' },
  offline: { label: UI_AGENT_STATUS.OFFLINE, cls: 'bg-tertiary/10 text-tertiary' },
  error: { label: UI_AGENT_STATUS.ERROR, cls: 'bg-error/10 text-error' },
}

export function AgentProfilePage() {
  const { sessionId } = useParams<{ sessionId: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const fileRef = useRef<HTMLInputElement>(null)
  const nameInputId = useId()
  const nameErrorId = `${nameInputId}-error`
  const soulInputId = useId()
  const soulErrorId = `${soulInputId}-error`

  const [editingName, setEditingName] = useState(false)
  const [nameDraft, setNameDraft] = useState('')
  const [nameError, setNameError] = useState('')
  const [saving, setSaving] = useState(false)

  const [editingSoul, setEditingSoul] = useState(false)
  const [soulDraft, setSoulDraft] = useState('')
  const [soulSaving, setSoulSaving] = useState(false)
  const [soulError, setSoulError] = useState('')
  const [avatarUploading, setAvatarUploading] = useState(false)
  const [avatarError, setAvatarError] = useState('')
  const [skillError, setSkillError] = useState('')
  const [removingSkill, setRemovingSkill] = useState<string | null>(null)

  const [showImportDialog, setShowImportDialog] = useState(false)

  const {
    data: detail,
    isLoading,
    error,
    refetch,
  } = useQuery<AgentDetail>({
    queryKey: ['agent-detail', sessionId],
    queryFn: () => fetchAgentDetail(sessionId!),
    enabled: !!sessionId,
  })

  if (!sessionId) return null

  if (isLoading) {
    return (
      <div className="flex h-dvh min-h-dvh bg-background p-4 sm:p-6" aria-busy="true">
        <div className="mx-auto w-full max-w-[640px]">
          <div className="mb-6 h-7 w-24 rounded-md skeleton-sheen" />
          <div className="mb-6 flex items-center gap-4">
            <div className="h-16 w-16 rounded-lg skeleton-sheen" />
            <div className="space-y-2">
              <div className="h-6 w-40 rounded-md skeleton-sheen" />
              <div className="h-4 w-32 rounded-md skeleton-sheen" />
            </div>
          </div>
          <div className="space-y-4">
            <div className="h-24 rounded-lg skeleton-sheen" />
            <div className="h-32 rounded-lg skeleton-sheen" />
          </div>
        </div>
        <span className="sr-only">{UI_STATUS.LOADING}</span>
      </div>
    )
  }

  if (error || !detail) {
    return (
      <div className="flex h-dvh min-h-dvh flex-col items-center justify-center gap-3 bg-background">
        <span className="text-sm text-error" role="alert">
          {UI_MESSAGES.RENDER_ERROR}
        </span>
        <button
          type="button"
          onClick={() => navigate(-1)}
          className="rounded-md px-3 py-1.5 text-sm text-brand transition-[background,color,transform] hover:bg-bg-hover hover:text-primary active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {UI_ACTIONS.BACK}
        </button>
        <button
          type="button"
          onClick={() => refetch()}
          className="rounded-md border border-border px-3 py-1.5 text-sm text-text-secondary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {UI_ACTIONS.RETRY}
        </button>
      </div>
    )
  }

  const agentType = detail.agent_type as AgentType
  const name = detail.agent_name || AGENT_NAMES[agentType] || detail.agent_type
  const status = detail.status as Status
  const badge = STATUS_BADGE[status] ?? STATUS_BADGE.offline
  const handleAvatarChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setAvatarUploading(true)
    setAvatarError('')
    try {
      const url = await uploadAvatar(file)
      await updateSession(sessionId, { avatar_url: url })
      await queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
      await queryClient.invalidateQueries({ queryKey: ['conversations'] })
    } catch {
      setAvatarError(UI_MESSAGES.UPLOAD_FAILED)
    } finally {
      setAvatarUploading(false)
      e.target.value = ''
    }
  }

  const startEditName = () => {
    setNameDraft(name)
    setNameError('')
    setEditingName(true)
  }

  const saveName = async () => {
    const trimmed = nameDraft.trim()
    if (!trimmed || trimmed === name) {
      setEditingName(false)
      return
    }
    setSaving(true)
    setNameError('')
    try {
      await updateSession(sessionId, { agent_name: trimmed })
      await queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
      await queryClient.invalidateQueries({ queryKey: ['conversations'] })
      setEditingName(false)
    } catch {
      setNameError(UI_ERRORS.PROFILE_SAVE_FAILED)
    } finally {
      setSaving(false)
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
    setSoulError('')
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
      setSoulError(UI_ERRORS.PROFILE_SAVE_FAILED)
    } finally {
      setSoulSaving(false)
    }
  }

  const clearSoul = async () => {
    setSoulSaving(true)
    setSoulError('')
    try {
      await updateAgentSoul(sessionId, '')
      await queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
    } catch {
      setSoulError(UI_ERRORS.PROFILE_SAVE_FAILED)
    } finally {
      setSoulSaving(false)
    }
  }

  const soulContent = detail.soul_md || ''
  const soulCharCount = countChars(soulContent)

  const isAdapterAgent = ['claude-code', 'opencode', 'codex'].includes(detail.agent_type)

  return (
    <div className="flex h-dvh min-h-dvh overflow-y-auto bg-background">
      <div className="mx-auto w-full max-w-[640px] p-4 sm:p-6">
        <button
          type="button"
          onClick={() => navigate(-1)}
          className="mb-6 flex items-center gap-1.5 rounded-md px-1 py-1 text-[13px] text-text-secondary transition-[color,background,transform] hover:bg-bg-hover hover:text-primary active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          <ArrowLeft className="h-4 w-4" strokeWidth={1.25} aria-hidden="true" />
          {UI_PROFILE.BACK_TO_CHAT}
        </button>

        {/* 头部 */}
        <div className="mb-6 flex items-start gap-3 sm:items-center sm:gap-4">
          <div className="group relative">
            <div className="flex h-16 w-16 items-center justify-center overflow-hidden rounded-lg border border-primary-border shadow-[0_14px_32px_rgba(23,33,31,0.12)]">
              <img
                src={
                  detail.avatar_url ||
                  `https://api.dicebear.com/9.x/bottts/svg?seed=${encodeURIComponent(name)}`
                }
                alt={name}
                className="h-full w-full rounded-lg object-cover"
                onError={(event) => {
                  event.currentTarget.src = '/favicon.svg'
                }}
              />
            </div>
            <button
              type="button"
              className="absolute inset-0 flex items-center justify-center rounded-lg bg-neutral-950/45 opacity-100 transition-[opacity,transform] active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100"
              onClick={() => fileRef.current?.click()}
              aria-label={UI_LABELS.UPLOAD_AVATAR}
              disabled={avatarUploading}
            >
              <Camera
                className="h-5 w-5 text-primary-foreground"
                strokeWidth={1.25}
                aria-hidden="true"
              />
            </button>
            <input
              ref={fileRef}
              type="file"
              accept="image/jpeg,image/png,image/gif,image/webp"
              className="hidden"
              onChange={handleAvatarChange}
            />
            {avatarError && (
              <p
                className="absolute left-0 top-[calc(100%+0.5rem)] w-48 text-xs text-error"
                role="alert"
              >
                {avatarError}
              </p>
            )}
          </div>
          <div className="min-w-0 flex-1">
            {editingName ? (
              <div className="w-full">
                <div className="flex items-center gap-2">
                  <label htmlFor={nameInputId} className="sr-only">
                    {UI_LABELS.NAME}
                  </label>
                  <input
                    id={nameInputId}
                    autoFocus
                    value={nameDraft}
                    onChange={(e) => {
                      setNameDraft(e.target.value)
                      setNameError('')
                    }}
                    onKeyDown={(e) => {
                      if (e.nativeEvent.isComposing) return
                      if (e.key === 'Enter') saveName()
                      if (e.key === 'Escape') setEditingName(false)
                    }}
                    className="w-full rounded-md border border-border bg-background px-2 py-1 text-xl font-semibold text-foreground outline-none"
                    disabled={saving}
                    aria-invalid={Boolean(nameError) || undefined}
                    aria-describedby={nameError ? nameErrorId : undefined}
                  />
                  <button
                    type="button"
                    onClick={saveName}
                    disabled={saving || !nameDraft.trim()}
                    className="shrink-0 rounded-md bg-primary px-3 py-1 text-xs font-medium text-primary-foreground transition-[transform,background,opacity] hover:bg-primary/90 active:scale-[0.98] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                  >
                    {saving ? '...' : UI_ACTIONS.SAVE}
                  </button>
                </div>
                {nameError && (
                  <p id={nameErrorId} className="mt-1 text-xs text-destructive" role="alert">
                    {nameError}
                  </p>
                )}
              </div>
            ) : (
              <div className="flex min-w-0 items-center gap-2">
                <h1 className="truncate text-xl font-semibold">{name}</h1>
                <button
                  type="button"
                  className="rounded-md p-1 text-foreground/40 transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground/70 active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                  onClick={startEditName}
                  aria-label={UI_ACTIONS.EDIT}
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

        {/* 元信息 */}
        <section className="mb-6">
          <h2 className="mb-3 text-xs font-semibold uppercase tracking-wider text-foreground/50">
            元数据
          </h2>
          <AgentMeta detail={detail} />
        </section>

        {/* SOUL.md */}
        <section>
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-xs font-semibold uppercase tracking-wider text-foreground/50">
              SOUL.md
            </h2>
            {!editingSoul && (
              <button
                type="button"
                className="flex items-center gap-1 rounded p-1 text-foreground/40 transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground/70 active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={startEditSoul}
                aria-label={UI_ACTIONS.EDIT}
              >
                <Pencil className="h-3 w-3" strokeWidth={1.25} />
              </button>
            )}
          </div>
          {editingSoul ? (
            <div className="space-y-2">
              <label htmlFor={soulInputId} className="sr-only">
                SOUL.md
              </label>
              <textarea
                id={soulInputId}
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
                placeholder={UI_PLACEHOLDERS.SOUL_DESCRIPTION}
                maxLength={330}
                disabled={soulSaving}
                aria-invalid={Boolean(soulError) || undefined}
                aria-describedby={soulError ? soulErrorId : undefined}
              />
              <div className="flex items-center justify-between">
                <span
                  className={cn(
                    'text-xs',
                    countChars(soulDraft) > 300 ? 'text-destructive' : 'text-tertiary',
                  )}
                >
                  {countChars(soulDraft)}/300
                </span>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    className="rounded-md px-3 py-1 text-xs text-muted-foreground transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                    onClick={() => {
                      setEditingSoul(false)
                      setSoulError('')
                    }}
                  >
                    {UI_ACTIONS.CANCEL}
                  </button>
                  <button
                    type="button"
                    className="rounded-md bg-primary px-3 py-1 text-xs font-medium text-primary-foreground transition-[transform,background,opacity] hover:bg-primary/90 active:scale-[0.98] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                    onClick={saveSoul}
                    disabled={soulSaving || countChars(soulDraft) > 300}
                  >
                    {soulSaving ? UI_STATUS.SAVING : UI_ACTIONS.SAVE}
                  </button>
                </div>
              </div>
              {soulError && (
                <p id={soulErrorId} className="text-xs text-destructive" role="alert">
                  {soulError}
                </p>
              )}
            </div>
          ) : soulContent ? (
            <div className="rounded-md border border-border bg-background px-3 py-2">
              <p className="whitespace-pre-wrap text-sm text-foreground/80">{soulContent}</p>
              <div className="mt-1.5 flex items-center justify-between">
                <span className="text-xs text-tertiary">{soulCharCount}/300 字（不含空格）</span>
                <button
                  type="button"
                  className="rounded-md px-2 py-1 text-xs text-destructive/60 transition-[background,color,transform,opacity] hover:bg-danger-bg hover:text-destructive active:scale-[0.97] disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                  onClick={clearSoul}
                  disabled={soulSaving}
                >
                  {soulSaving ? UI_STATUS.SAVING : UI_ACTIONS.CLEAR}
                </button>
              </div>
            </div>
          ) : (
            <button
              type="button"
              className="rounded-md border border-dashed border-border px-3 py-2 text-sm text-muted-foreground transition-[border-color,color,transform] hover:border-foreground/20 hover:text-foreground/70 active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              onClick={startEditSoul}
            >
              {UI_MESSAGES.CLICK_TO_WRITE_SOUL}
            </button>
          )}
          {!editingSoul && soulError && (
            <p className="mt-2 text-xs text-destructive" role="alert">
              {soulError}
            </p>
          )}
        </section>

        {/* 技能 */}
        <section className="mt-6">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-xs font-semibold uppercase tracking-wider text-foreground/50">
              Skills
            </h2>
            <span className="text-[11px] text-tertiary">{detail.skills.length} 个技能</span>
          </div>
          {skillError && (
            <p className="mb-2 text-xs text-destructive" role="alert">
              {skillError}
            </p>
          )}
          {detail.skills.length > 0 ? (
            <div className="space-y-2">
              {detail.skills.map((s) => (
                <div key={s.name} className="flex items-center gap-2">
                  <div className="flex-1">
                    <SkillCard skill={s} />
                  </div>
                  {!s.builtin && isAdapterAgent && (
                    <button
                      type="button"
                      className="shrink-0 rounded-[6px] border border-destructive/20 bg-destructive/10 p-1.5 text-destructive transition-[transform,background,opacity] hover:bg-destructive/20 active:scale-[0.96] disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                      title={UI_PROFILE.REMOVE_SKILL}
                      aria-label={`${UI_PROFILE.REMOVE_SKILL} ${s.name}`}
                      disabled={removingSkill !== null}
                      onClick={async () => {
                        if (removingSkill !== null) return
                        setRemovingSkill(s.name)
                        setSkillError('')
                        try {
                          await removeSkill(s.name, sessionId)
                          await queryClient.invalidateQueries({
                            queryKey: ['agent-detail', sessionId],
                          })
                          setSkillError('')
                        } catch {
                          setSkillError(UI_ERRORS.REMOVE_SKILL_FAILED)
                        } finally {
                          setRemovingSkill(null)
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
            <p className="text-sm text-tertiary">{UI_MESSAGES.NO_SKILLS}</p>
          )}
          {isAdapterAgent && (
            <button
              type="button"
              className="mt-3 flex w-full items-center justify-center gap-2 rounded-[8px] border border-dashed border-border py-2.5 text-[12px] text-tertiary transition-[transform,background,border-color,color,opacity] hover:border-primary hover:bg-primary/8 hover:text-primary active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              onClick={() => setShowImportDialog(true)}
            >
              <Plus className="h-4 w-4" />
              {UI_PROFILE.IMPORT_SKILL}
            </button>
          )}
        </section>

        {/* 导入对话框 */}
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
      </div>
    </div>
  )
}

// ── 导入技能对话框 ──

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
  const [importError, setImportError] = useState('')
  const dialogRef = useRef<HTMLDivElement>(null)

  useDialogFocusTrap(dialogRef)

  const {
    data: hubSkills = [],
    isError: skillsError,
    isLoading: skillsLoading,
    refetch: refetchSkills,
  } = useQuery({
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
    setImportError('')
    try {
      await Promise.all(Array.from(selected).map((name) => importSkill(name, sessionId)))
      setLoading(false)
      onImported()
    } catch {
      setImportError(UI_ERRORS.IMPORT_SKILL_FAILED)
      setLoading(false)
    }
  }

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !loading) onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [loading, onClose])

  const handleClose = () => {
    if (!loading) onClose()
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="import-skill-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-neutral-950/55 px-4 backdrop-blur-[2px]"
      onClick={handleClose}
    >
      <div
        ref={dialogRef}
        tabIndex={-1}
        className="max-h-[calc(100dvh-2rem)] w-full max-w-[440px] overflow-auto rounded-xl border border-border bg-card p-4 shadow-[var(--shadow-popup)] sm:p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <h3
          id="import-skill-title"
          className="mb-2 flex items-center gap-2 text-[15px] font-semibold"
        >
          <Plus className="h-[18px] w-[18px] text-primary" />
          {UI_PROFILE.IMPORT_SKILL}
        </h3>
        <p className="mb-4 text-[13px] text-text-secondary">
          {UI_MESSAGES.IMPORT_EXTERNAL_SKILL_DESC}
        </p>

        <div className="flex max-h-[300px] flex-col gap-1.5 overflow-auto">
          {skillsLoading && (
            <p className="py-8 text-center text-[12px] text-tertiary">{UI_STATUS.LOADING}</p>
          )}
          {skillsError && (
            <div
              className="flex items-center justify-between gap-2 rounded-[8px] border border-destructive/20 bg-danger-bg px-3 py-2 text-[12px] text-destructive"
              role="alert"
            >
              <span>{UI_ERRORS.LOAD_SKILLS_FAILED}</span>
              <button
                type="button"
                className="rounded-[5px] px-2 py-1 font-medium underline-offset-4 hover:bg-destructive/10 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => refetchSkills()}
              >
                {UI_ACTIONS.RETRY}
              </button>
            </div>
          )}
          {!skillsLoading && !skillsError && externals.length === 0 && (
            <p className="py-8 text-center text-[12px] text-tertiary">
              {UI_MESSAGES.NO_EXTERNAL_SKILLS}
            </p>
          )}
          {!skillsError &&
            externals.map((skill) => {
              const imported = alreadyImported.has(skill.name)
              const unavailable = Boolean(skill.status && skill.status !== 'ready')
              const isSelected = selected.has(skill.name)
              return (
                <button
                  key={skill.name}
                  type="button"
                  className={cn(
                    'flex items-center gap-2.5 rounded-[8px] border p-2.5 text-left transition-[background,border-color,opacity,transform] active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
                    imported
                      ? 'cursor-not-allowed border-border bg-muted/40 opacity-40'
                      : unavailable
                        ? 'cursor-not-allowed border-warning/20 bg-warning/5 opacity-70'
                        : isSelected
                          ? 'border-primary/15 bg-primary/8'
                          : 'border-border hover:bg-bg-hover',
                  )}
                  disabled={imported || unavailable}
                  onClick={() => !imported && !unavailable && toggle(skill.name)}
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
                        strokeWidth={2.5}
                        className="h-2.5 w-2.5"
                      >
                        <polyline points="20 6 9 17 4 12" />
                      </svg>
                    )}
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[13px] font-medium">{skill.name}</p>
                    <p className="mt-0.5 truncate text-[10px] text-tertiary">
                      {skill.uploaded_by ? `来源：${skill.uploaded_by}` : '来源：未知'}
                      {skill.file_count > 0 ? ` · ${skill.file_count} 个文件` : ''}
                    </p>
                    {unavailable && (
                      <p className="text-[10px] text-warning">
                        状态：
                        {skill.status === 'storage_error'
                          ? '存储异常'
                          : skill.status === 'deleting'
                            ? '删除中'
                            : skill.status === 'migrating'
                              ? '迁移中'
                              : skill.status}
                      </p>
                    )}
                    {skill.sha256 && (
                      <p
                        className="truncate font-mono text-[10px] text-tertiary"
                        title={skill.sha256}
                      >
                        SHA-256：{skill.sha256}
                      </p>
                    )}
                    {skill.files && skill.files.length > 0 && (
                      <p
                        className="truncate text-[10px] text-tertiary"
                        title={skill.files.join('、')}
                      >
                        文件：{skill.files.join('、')}
                      </p>
                    )}
                    {(skill.contains_executable || skill.contains_binary) && (
                      <p className="text-[10px] text-warning">
                        内容提示：
                        {[
                          skill.contains_executable && '可执行文件',
                          skill.contains_binary && '二进制文件',
                        ]
                          .filter(Boolean)
                          .join('、')}
                      </p>
                    )}
                  </div>
                  <span className="ml-auto shrink-0 text-[10px] text-tertiary">
                    {imported ? UI_MESSAGES.IMPORTED : ''}
                  </span>
                </button>
              )
            })}
        </div>

        {importError && (
          <p
            className="mt-4 rounded-[8px] border border-destructive/20 bg-destructive/5 px-3 py-2 text-[12px] text-destructive"
            role="alert"
          >
            {importError}
          </p>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            className="rounded-[8px] border border-border bg-muted px-4 py-2 text-[12px] font-medium text-text-secondary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            onClick={handleClose}
          >
            {UI_ACTIONS.CANCEL}
          </button>
          <button
            type="button"
            className="inline-flex items-center gap-1.5 rounded-[8px] bg-primary px-4 py-2 text-[12px] font-medium text-primary-foreground transition-[background,transform,opacity] hover:bg-primary/90 active:scale-[0.97] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            onClick={handleImport}
            disabled={loading || selected.size === 0}
          >
            {UI_MISC.CONFIRM_IMPORT}
          </button>
        </div>
      </div>
    </div>
  )
}
