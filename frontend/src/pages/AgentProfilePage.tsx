import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Camera, Plus, Trash2 } from 'lucide-react'
import { useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router'

import { AgentMeta } from '@/components/chat/AgentMeta'
import { SkillCard } from '@/components/chat/SkillCard'
import { ImportSkillDialog } from '@/components/profile/ImportSkillDialog'
import { AgentNameEditor, AgentSoulEditor } from '@/components/profile/ProfileEditors'
import type { AgentType } from '@/generated/request'
import type { AgentDetail } from '@/lib/api'
import { fetchAgentDetail, removeSkill, updateSession, uploadAvatar } from '@/lib/api'
import { AGENT_NAMES, toAgentDisplayStatus } from '@/lib/constants'
import { usePageTitle } from '@/lib/page-title'
import { queryKeys } from '@/lib/query-keys'
import {
  UI_ACTIONS,
  UI_AGENT_STATUS,
  UI_ERRORS,
  UI_LABELS,
  UI_MESSAGES,
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

  const [avatarUploading, setAvatarUploading] = useState(false)
  const [avatarError, setAvatarError] = useState('')
  const [skillError, setSkillError] = useState('')
  const [removingSkill, setRemovingSkill] = useState<string | null>(null)

  const [showImportDialog, setShowImportDialog] = useState(false)
  const importTriggerRef = useRef<HTMLButtonElement>(null)

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

  const profileAgentType = detail?.agent_type as AgentType | undefined
  const profileName = detail
    ? detail.agent_name || (profileAgentType ? AGENT_NAMES[profileAgentType] : undefined)
    : undefined
  usePageTitle(profileName ? `Agent ${profileName}` : 'Agent')

  if (!sessionId) return null

  if (isLoading) {
    return (
      <main className="flex h-dvh min-h-dvh bg-background p-4 sm:p-6" aria-busy="true">
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
      </main>
    )
  }

  if (error || !detail) {
    return (
      <main className="flex h-dvh min-h-dvh flex-col items-center justify-center gap-3 bg-background">
        <span className="text-sm text-error" role="alert">
          {UI_MESSAGES.RENDER_ERROR}
        </span>
        <button
          type="button"
          onClick={() => navigate('/chat')}
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
      </main>
    )
  }

  const agentType = detail.agent_type as AgentType
  const name = detail.agent_name || AGENT_NAMES[agentType] || detail.agent_type
  const status: Status = toAgentDisplayStatus(detail.status)
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
      await queryClient.invalidateQueries({ queryKey: queryKeys.conversations })
    } catch {
      setAvatarError(UI_MESSAGES.UPLOAD_FAILED)
    } finally {
      setAvatarUploading(false)
      e.target.value = ''
    }
  }

  const isAdapterAgent = ['claude-code', 'opencode', 'codex', 'pi'].includes(detail.agent_type)

  return (
    <main className="flex h-dvh min-h-dvh overflow-y-auto bg-background">
      <div className="mx-auto w-full max-w-[640px] p-4 sm:p-6">
        <button
          type="button"
          onClick={() => navigate('/chat')}
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
            <AgentNameEditor sessionId={sessionId} name={name} />
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
        <AgentSoulEditor sessionId={sessionId} content={detail.soul_md || ''} />

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
              ref={importTriggerRef}
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
            onClose={() => {
              importTriggerRef.current?.focus()
              setShowImportDialog(false)
            }}
            onImported={() => {
              queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
              importTriggerRef.current?.focus()
              setShowImportDialog(false)
            }}
            restoreFocusRef={importTriggerRef}
          />
        )}
      </div>
    </main>
  )
}
