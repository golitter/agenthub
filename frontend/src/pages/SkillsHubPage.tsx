import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Package, Search, Shield, Star, Upload, X } from 'lucide-react'
import { type ReactNode, useRef, useState } from 'react'

import { HubSkillCard } from '@/components/skills/HubSkillCard'
import { DeleteConfirmDialog, UploadDialog } from '@/components/skills/SkillHubDialogs'
import { deleteSkill, fetchSkills } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { UI_ACTIONS, UI_ERRORS, UI_LABELS, UI_MESSAGES, UI_PLACEHOLDERS } from '@/lib/ui-text'
import { useAdminStore } from '@/stores/admin'

// ── SkillsHub 页面 ──

export function SkillsHubPage() {
  const [search, setSearch] = useState('')
  const [showUpload, setShowUpload] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const uploadTriggerRef = useRef<HTMLButtonElement>(null)
  const deleteTriggerRef = useRef<HTMLButtonElement>(null)
  const queryClient = useQueryClient()
  const isAdmin = useAdminStore((state) => state.isAuthenticated)

  const {
    data: skills = [],
    isError,
    isLoading,
    refetch,
  } = useQuery({
    queryKey: queryKeys.skills,
    queryFn: fetchSkills,
  })

  const deleteMutation = useMutation({
    mutationFn: deleteSkill,
    onSuccess: async () => {
      // 删除后原触发按钮会随列表刷新卸载，等缓存完成后把焦点交给稳定的
      // 上传按钮，避免先 focus 已删除节点再立即丢焦点。
      await queryClient.invalidateQueries({ queryKey: queryKeys.skills })
      deleteTriggerRef.current = null
      setDeleteTarget(null)
      uploadTriggerRef.current?.focus()
    },
  })

  const query = search.trim().toLowerCase()
  const filtered = skills.filter((s) => {
    if (!query) return true
    return [s.name, s.description].some((value) => value.toLowerCase().includes(query))
  })
  const builtins = filtered.filter((s) => s.builtin)
  const externals = filtered.filter((s) => !s.builtin)
  const totalBuiltins = skills.filter((s) => s.builtin).length
  const totalExternals = skills.length - totalBuiltins

  return (
    <div className="chat-canvas flex h-full flex-col">
      {/* 头部 */}
      <div className="border-b border-border bg-card/80 px-4 py-4 sm:px-6">
        <div className="mx-auto flex w-full max-w-[88rem] items-start justify-between gap-3 sm:items-center sm:gap-4">
          <div className="min-w-0">
            <h2 className="flex items-center gap-2 text-[17px] font-semibold text-foreground">
              <Star className="h-[18px] w-[18px] text-primary" strokeWidth={1.5} />
              技能库
            </h2>
            <p className="mt-1 text-[12px] text-text-secondary">
              管理 Agent 可导入的工具能力和外部 Skill 包。
            </p>
          </div>
          <div className="flex shrink-0 flex-col items-end gap-1">
            <button
              ref={uploadTriggerRef}
              type="button"
              className="inline-flex items-center gap-1.5 rounded-[10px] bg-primary px-3 py-2.5 text-[12px] font-semibold text-primary-foreground shadow-[0_12px_28px_rgba(15,118,110,0.16)] transition-[transform,background,opacity] hover:bg-primary/90 active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring sm:px-4"
              onClick={() => setShowUpload(true)}
              disabled={!isAdmin}
              aria-describedby={!isAdmin ? 'skills-upload-help' : undefined}
            >
              <Upload className="h-3.5 w-3.5" strokeWidth={1.5} />
              {UI_ACTIONS.UPLOAD}
            </button>
            {!isAdmin && (
              <p id="skills-upload-help" className="text-[11px] text-text-secondary">
                请先登录管理员账户
              </p>
            )}
          </div>
        </div>
      </div>

      {/* 主体 */}
      <div className="min-h-0 flex-1 overflow-auto px-4 py-4 sm:px-6 sm:py-6">
        <div className="mx-auto grid w-full max-w-[88rem] gap-4 sm:gap-6 xl:grid-cols-[minmax(0,1fr)_18rem]">
          <div className="min-w-0">
            {/* 搜索 */}
            <div className="mb-5 flex items-center gap-2 rounded-[12px] border border-border/80 bg-muted/80 px-3.5 py-2.5 shadow-[inset_0_1px_0_rgba(255,255,255,0.03)] transition-[border-color,box-shadow] focus-within:border-primary-border focus-within:ring-2 focus-within:ring-primary/10">
              <Search className="h-3.5 w-3.5 text-text-secondary" strokeWidth={1.5} />
              <input
                type="text"
                placeholder={UI_PLACEHOLDERS.SEARCH_SKILLS}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="flex-1 border-none bg-transparent text-[13px] text-foreground outline-none placeholder:text-text-secondary"
                aria-label={UI_PLACEHOLDERS.SEARCH_SKILLS}
              />
              {search && (
                <button
                  type="button"
                  className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-tertiary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.94] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                  onClick={() => setSearch('')}
                  aria-label={UI_ACTIONS.CLEAR_SEARCH}
                  title={UI_ACTIONS.CLEAR_SEARCH}
                >
                  <X className="h-3.5 w-3.5" strokeWidth={1.25} />
                </button>
              )}
            </div>

            {isLoading ? (
              <div className="grid gap-3 md:grid-cols-2">
                {Array.from({ length: 4 }).map((_, index) => (
                  <div
                    key={index}
                    className="h-28 rounded-[14px] border border-border/70 skeleton-sheen"
                  />
                ))}
              </div>
            ) : isError ? (
              <div className="flex min-h-[18rem] flex-col items-center justify-center rounded-[16px] border border-dashed border-destructive/30 bg-card/50 text-center">
                <p className="text-sm text-destructive" role="alert">
                  {UI_ERRORS.LOAD_SKILLS_FAILED}
                </p>
                <button
                  type="button"
                  className="mt-4 rounded-[7px] border border-border px-3 py-1.5 text-xs text-text-secondary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                  onClick={() => refetch()}
                >
                  {UI_ACTIONS.RETRY}
                </button>
              </div>
            ) : (
              <>
                {/* 内置技能区 */}
                {builtins.length > 0 && (
                  <SectionLabel
                    icon={<Shield className="h-3.5 w-3.5" strokeWidth={1.25} />}
                    label={UI_LABELS.INNER_SKILLS}
                  />
                )}
                <div className="grid gap-3 md:grid-cols-2">
                  {builtins.map((skill) => (
                    <HubSkillCard key={skill.name} skill={skill} onDelete={undefined} />
                  ))}
                </div>

                {/* 外部技能区 */}
                {externals.length > 0 && (
                  <SectionLabel
                    icon={<Package className="h-3.5 w-3.5" strokeWidth={1.25} />}
                    label={UI_LABELS.EXTERNAL_SKILLS}
                  />
                )}
                <div className="grid gap-3 md:grid-cols-2">
                  {externals.map((skill) => (
                    <HubSkillCard
                      key={skill.name}
                      skill={skill}
                      onDelete={
                        isAdmin
                          ? (trigger) => {
                              deleteTriggerRef.current = trigger
                              deleteMutation.reset()
                              setDeleteTarget(skill.name)
                            }
                          : undefined
                      }
                    />
                  ))}
                </div>

                {filtered.length === 0 && (
                  <div className="flex min-h-[18rem] flex-col items-center justify-center rounded-[16px] border border-dashed border-border bg-card/50 text-tertiary">
                    <Star className="mb-3 h-8 w-8 opacity-40" strokeWidth={1.25} />
                    <p className="text-[13px] font-medium">
                      {query ? UI_MESSAGES.NO_MATCHING_MESSAGES : UI_MESSAGES.NO_SKILLS}
                    </p>
                    <p className="mt-1 text-[12px]">
                      {query ? UI_MESSAGES.SKILL_SEARCH_EMPTY_DESC : UI_MESSAGES.SKILL_EMPTY_DESC}
                    </p>
                    {query && (
                      <button
                        type="button"
                        className="mt-4 rounded-[7px] border border-border px-3 py-1.5 text-xs text-text-secondary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                        onClick={() => setSearch('')}
                      >
                        {UI_ACTIONS.CLEAR_SEARCH}
                      </button>
                    )}
                  </div>
                )}
              </>
            )}
          </div>

          <aside className="hidden xl:block">
            <div className="sticky top-0 rounded-[16px] border border-border/70 bg-card/70 p-4 shadow-[0_18px_44px_rgba(0,0,0,0.10)]">
              <p className="text-[11px] font-semibold uppercase tracking-wider text-text-secondary">
                技能库状态
              </p>
              <div className="mt-4 grid grid-cols-2 gap-2">
                <StatPill label="内置" value={totalBuiltins} />
                <StatPill label="外部" value={totalExternals} />
              </div>
              <div className="mt-4 rounded-[12px] bg-muted/70 p-3">
                <p className="text-[12px] font-medium text-foreground">上传前检查</p>
                <p className="mt-1 text-[11px] leading-relaxed text-text-secondary">
                  zip 文件名需要和 SKILL.md 中的 name 保持一致，确认后会进入技能库。
                </p>
              </div>
            </div>
          </aside>
        </div>
      </div>

      {/* 上传对话框 */}
      {showUpload && (
        <UploadDialog
          onClose={() => {
            uploadTriggerRef.current?.focus()
            setShowUpload(false)
          }}
          onSuccess={() => {
            queryClient.invalidateQueries({ queryKey: queryKeys.skills })
            uploadTriggerRef.current?.focus()
            setShowUpload(false)
          }}
          restoreFocusRef={uploadTriggerRef}
        />
      )}

      {/* 删除确认 */}
      {deleteTarget && (
        <DeleteConfirmDialog
          name={deleteTarget}
          onConfirm={() => deleteMutation.mutate(deleteTarget)}
          onCancel={() => {
            deleteTriggerRef.current?.focus()
            setDeleteTarget(null)
          }}
          loading={deleteMutation.isPending}
          error={
            deleteMutation.isError
              ? deleteMutation.error instanceof Error
                ? deleteMutation.error.message
                : UI_ERRORS.DELETE_SKILL_FAILED
              : undefined
          }
          restoreFocusRef={deleteTriggerRef}
          fallbackFocusRef={uploadTriggerRef}
        />
      )}
    </div>
  )
}

// ── 分区标签 ──

function SectionLabel({ icon, label }: { icon: ReactNode; label: string }) {
  return (
    <div className="mb-2 mt-5 flex items-center gap-1.5 px-0.5 py-2 text-[11px] font-semibold uppercase tracking-wider text-text-secondary first:mt-0">
      {icon}
      {label}
    </div>
  )
}

function StatPill({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-[12px] border border-border/70 bg-bg-canvas/60 p-3">
      <p className="text-[11px] text-text-secondary">{label}</p>
      <p className="mt-1 font-mono text-lg font-semibold tabular-nums text-foreground">{value}</p>
    </div>
  )
}
