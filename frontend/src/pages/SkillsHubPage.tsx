import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  CheckCircle2,
  Package,
  Search,
  Shield,
  Star,
  Trash2,
  Upload,
  Wrench,
  X,
  XCircle,
} from 'lucide-react'
import { type ReactNode, useCallback, useEffect, useRef, useState } from 'react'

import type { SkillUploadResponse } from '@/generated/skill-storage'
import { useDialogFocusTrap } from '@/hooks/use-dialog-focus-trap'
import { confirmSkill, deleteSkill, fetchSkills, type SkillHubItem, uploadSkill } from '@/lib/api'
import {
  UI_ACTIONS,
  UI_ERRORS,
  UI_LABELS,
  UI_MESSAGES,
  UI_MISC,
  UI_PLACEHOLDERS,
  UI_PROFILE,
} from '@/lib/ui-text'
import { cn } from '@/lib/utils'
import { useAdminStore } from '@/stores/admin'

// ── 类型 ──

type ValidationResponse = SkillUploadResponse

// ── SkillsHub 页面 ──

export function SkillsHubPage() {
  const [search, setSearch] = useState('')
  const [showUpload, setShowUpload] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const queryClient = useQueryClient()
  const isAdmin = useAdminStore((state) => state.isAuthenticated)

  const {
    data: skills = [],
    isError,
    isLoading,
    refetch,
  } = useQuery({
    queryKey: ['skills'],
    queryFn: fetchSkills,
  })

  const deleteMutation = useMutation({
    mutationFn: deleteSkill,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['skills'] })
      setDeleteTarget(null)
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
          <button
            type="button"
            className="inline-flex shrink-0 items-center gap-1.5 rounded-[10px] bg-primary px-3 py-2.5 text-[12px] font-semibold text-primary-foreground shadow-[0_12px_28px_rgba(15,118,110,0.16)] transition-[transform,background,opacity] hover:bg-primary/90 active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring sm:px-4"
            onClick={() => setShowUpload(true)}
            disabled={!isAdmin}
            title={isAdmin ? undefined : '请先登录管理员账户'}
          >
            <Upload className="h-3.5 w-3.5" strokeWidth={1.5} />
            {UI_ACTIONS.UPLOAD}
          </button>
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
                          ? () => {
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
          onClose={() => setShowUpload(false)}
          onSuccess={() => {
            queryClient.invalidateQueries({ queryKey: ['skills'] })
            setShowUpload(false)
          }}
        />
      )}

      {/* 删除确认 */}
      {deleteTarget && (
        <DeleteConfirmDialog
          name={deleteTarget}
          onConfirm={() => deleteMutation.mutate(deleteTarget)}
          onCancel={() => setDeleteTarget(null)}
          loading={deleteMutation.isPending}
          error={
            deleteMutation.isError
              ? deleteMutation.error instanceof Error
                ? deleteMutation.error.message
                : UI_ERRORS.DELETE_SKILL_FAILED
              : undefined
          }
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

// ── Hub 技能卡片 ──

function HubSkillCard({ skill, onDelete }: { skill: SkillHubItem; onDelete?: () => void }) {
  return (
    <div className="min-h-[8rem] rounded-[14px] border border-border/70 bg-card/80 p-4 shadow-[0_12px_32px_rgba(0,0,0,0.08)] transition-[background,border-color,transform] hover:border-primary-border hover:bg-card active:scale-[0.995]">
      <div className="mb-2 flex min-w-0 flex-wrap items-center gap-2.5">
        <div
          className={cn(
            'flex h-9 w-9 items-center justify-center rounded-[10px] text-base',
            skill.builtin ? 'bg-success/10' : 'bg-primary/10',
          )}
        >
          {skill.builtin ? (
            <Wrench className="h-4 w-4" strokeWidth={1.25} />
          ) : (
            <Package className="h-4 w-4" strokeWidth={1.25} />
          )}
        </div>
        <span className="min-w-0 flex-[1_1_8rem] break-words text-[14px] font-semibold">
          {skill.name}
        </span>
        <span
          className={cn(
            'rounded-[6px] px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide',
            skill.builtin
              ? 'border border-success/15 bg-success/10 text-success'
              : 'border border-primary/15 bg-primary/10 text-primary',
          )}
        >
          {skill.builtin ? '内置' : '外部'}
        </span>
        {!skill.builtin && skill.status && skill.status !== 'ready' && (
          <span className="rounded-[6px] border border-warning/20 bg-warning/10 px-2 py-0.5 text-[10px] font-semibold text-warning">
            {skill.status === 'storage_error'
              ? '存储异常'
              : skill.status === 'deleting'
                ? '删除中'
                : skill.status === 'migrating'
                  ? '迁移中'
                  : skill.status}
          </span>
        )}
      </div>
      <p className="mb-3 break-words text-[12px] leading-relaxed text-text-secondary sm:pl-[46px]">
        {skill.description}
      </p>
      {!skill.builtin && (
        <div className="sm:pl-[46px]">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-[11px] text-tertiary">
              已被 {skill.import_count} 个 Agent 导入
            </span>
            {onDelete && (
              <button
                type="button"
                className="inline-flex items-center gap-1 rounded-[6px] border border-destructive/20 bg-destructive/10 px-2.5 py-1 text-[11px] text-destructive transition-[transform,background,opacity] hover:bg-destructive/20 active:scale-[0.98]"
                onClick={(e) => {
                  e.stopPropagation()
                  onDelete()
                }}
                aria-label={`${UI_ACTIONS.DELETE} ${skill.name}`}
              >
                <Trash2 className="h-3 w-3" />
                {UI_ACTIONS.DELETE}
              </button>
            )}
          </div>
          {(skill.uploaded_by || skill.sha256) && (
            <div className="mt-2 space-y-0.5 text-[10px] text-tertiary">
              {skill.uploaded_by && <p>来源：{skill.uploaded_by}</p>}
              {skill.sha256 && <p className="break-all font-mono">SHA-256：{skill.sha256}</p>}
              {skill.files && skill.files.length > 0 && <p>文件：{skill.files.join('、')}</p>}
              {(skill.contains_executable || skill.contains_binary) && (
                <p className="text-warning">
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
          )}
          {!skill.uploaded_by &&
          !skill.sha256 &&
          (skill.files?.length || skill.contains_executable || skill.contains_binary) ? (
            <div className="mt-2 space-y-0.5 text-[10px] text-tertiary">
              {skill.files && skill.files.length > 0 && <p>文件：{skill.files.join('、')}</p>}
              {(skill.contains_executable || skill.contains_binary) && (
                <p className="text-warning">
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
          ) : null}
        </div>
      )}
    </div>
  )
}

// ── 上传对话框 ──

function UploadDialog({ onClose, onSuccess }: { onClose: () => void; onSuccess: () => void }) {
  const [step, setStep] = useState<'upload' | 'validate'>('upload')
  const [dragging, setDragging] = useState(false)
  const [validation, setValidation] = useState<ValidationResponse | null>(null)
  const [confirmName, setConfirmName] = useState('')
  const [uploading, setUploading] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)

  useDialogFocusTrap(dialogRef)

  const handleFile = useCallback(
    async (file: File) => {
      if (uploading) return
      if (!file.name.toLowerCase().endsWith('.zip')) {
        setStep('upload')
        setConfirmName('')
        setValidation({ valid: false, errors: [UI_ERRORS.SKILL_ZIP_REQUIRED] })
        return
      }
      setUploading(true)
      setSubmitError('')
      try {
        const result = await uploadSkill(file)
        setValidation(result)
        if (result.valid && result.name) {
          setConfirmName(result.name)
          setStep('validate')
        }
      } catch (err) {
        setValidation({ valid: false, errors: [(err as Error).message] })
      } finally {
        setUploading(false)
      }
    },
    [uploading],
  )

  const handleConfirm = async () => {
    if (!validation || (!validation.upload_id && !confirmName.trim())) return
    setUploading(true)
    setSubmitError('')
    try {
      const confirmPayload = validation.upload_id
        ? { upload_id: validation.upload_id }
        : {
            name: confirmName,
            description: validation.description || '',
            file_count: validation.file_count || 0,
            total_size: validation.total_size || 0,
            tmp_dir: validation.tmp_dir || '',
          }
      await confirmSkill(confirmPayload)
      setUploading(false)
      onSuccess()
    } catch (err) {
      setSubmitError((err as Error).message)
      setUploading(false)
    }
  }

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !uploading) onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose, uploading])

  const handleClose = () => {
    if (!uploading) onClose()
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="skill-upload-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-neutral-950/55 p-4 backdrop-blur-[2px]"
      onClick={handleClose}
    >
      <div
        ref={dialogRef}
        tabIndex={-1}
        className="max-h-[calc(100dvh-2rem)] w-full max-w-[520px] overflow-y-auto rounded-xl border border-border bg-card p-4 shadow-[var(--shadow-popup)] sm:p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <h3
          id="skill-upload-title"
          className="mb-2 flex items-center gap-2 text-[15px] font-semibold"
        >
          <Upload className="h-[18px] w-[18px] text-primary" />
          {UI_ACTIONS.UPLOAD}
        </h3>
        <p className="mb-1 text-[13px] text-text-secondary">
          上传一个 .zip 压缩包，zip 文件名须与 SKILL.md 中的 name 一致。
        </p>
        <p className="mb-4 rounded-[6px] bg-muted px-3 py-2 font-mono text-[11px] leading-relaxed text-tertiary">
          例: <span className="text-foreground">course.zip</span> → 解压后结构:
          <br />
          &nbsp;&nbsp;course/
          <br />
          &nbsp;&nbsp;├── SKILL.md &nbsp;（frontmatter 含 name: course）
          <br />
          &nbsp;&nbsp;└── ...
        </p>

        {step === 'upload' && (
          <div
            role="button"
            tabIndex={0}
            aria-label={UI_PROFILE.UPLOAD_OR_DRAG}
            className={cn(
              'flex cursor-pointer flex-col items-center rounded-[10px] border-2 border-dashed p-6 text-center transition-[background,border-color,transform] sm:p-10',
              dragging
                ? 'border-primary bg-primary/8'
                : 'border-border bg-muted hover:border-primary hover:bg-primary/8 active:scale-[0.99]',
            )}
            onClick={() => !uploading && fileRef.current?.click()}
            onKeyDown={(e) => {
              if (e.key !== 'Enter' && e.key !== ' ') return
              e.preventDefault()
              if (!uploading) fileRef.current?.click()
            }}
            onDragOver={(e) => {
              e.preventDefault()
              setDragging(true)
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={(e) => {
              e.preventDefault()
              setDragging(false)
              const file = e.dataTransfer.files[0]
              if (file) void handleFile(file)
            }}
            aria-disabled={uploading}
          >
            <span className="mb-2 opacity-60">
              <Package className="h-8 w-8" strokeWidth={1.25} />
            </span>
            <p className="text-[13px] font-medium text-foreground">点击或拖拽上传 .zip 文件</p>
            <p className="mt-1 text-[11px] text-tertiary">
              支持 .zip 格式，上传不超过 10MB、解压后不超过 50MB，文件数不超过 200
            </p>
            <input
              ref={fileRef}
              type="file"
              accept=".zip"
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0]
                if (file) void handleFile(file)
                e.currentTarget.value = ''
              }}
              disabled={uploading}
            />
          </div>
        )}

        {validation && !validation.valid && (
          <div
            className="mt-4 rounded-[8px] border border-destructive/20 bg-destructive/5 p-3.5"
            role="alert"
          >
            <p className="mb-2 flex items-center gap-1.5 text-[13px] font-semibold text-destructive">
              <XCircle className="h-4 w-4" strokeWidth={1.25} /> 校验失败
            </p>
            {validation.errors?.map((err, i) => (
              <p key={i} className="text-[12px] text-text-secondary">
                ✗ {err}
              </p>
            ))}
          </div>
        )}

        {step === 'validate' && validation?.valid && (
          <>
            <div className="mt-4 rounded-[8px] border border-success/20 bg-success/5 p-3.5">
              <p className="mb-2 flex items-center gap-1.5 text-[13px] font-semibold text-success">
                <CheckCircle2 className="h-4 w-4" strokeWidth={1.25} /> 校验通过
              </p>
              <p className="text-[12px] text-text-secondary">✓ SKILL.md 存在</p>
              <p className="text-[12px] text-text-secondary">
                ✓ frontmatter: name={validation.name}
              </p>
              <p className="text-[12px] text-text-secondary">
                ✓ 来源：{validation.uploaded_by || '当前管理员'}
              </p>
              <p className="text-[12px] text-text-secondary">✓ 文件数: {validation.file_count}</p>
              {validation.files && validation.files.length > 0 && (
                <p className="break-all text-[11px] text-text-secondary">
                  文件清单：{validation.files.join('、')}
                </p>
              )}
              <p className="text-[12px] text-text-secondary">
                ✓ 大小: {((validation.total_size || 0) / 1024).toFixed(0)} KB
              </p>
              {validation.package_size && (
                <p className="text-[12px] text-text-secondary">
                  ✓ 规范包: {(validation.package_size / 1024).toFixed(0)} KB
                </p>
              )}
              {validation.sha256 && (
                <p className="break-all text-[11px] text-text-secondary">
                  SHA-256: {validation.sha256}
                </p>
              )}
              {(validation.contains_executable || validation.contains_binary) && (
                <p className="text-[12px] text-warning">
                  ⚠ 包含{validation.contains_executable ? '可执行文件' : ''}
                  {validation.contains_executable && validation.contains_binary ? '和' : ''}
                  {validation.contains_binary ? '二进制内容' : ''}，请人工审阅
                </p>
              )}
              <p className="text-[12px] text-text-secondary">✓ 结构校验通过（不代表内容可信）</p>
            </div>
            <div className="mt-4">
              <label className="mb-1.5 block text-[12px] font-medium text-text-secondary">
                Skill 名称（确认后不可修改）
              </label>
              <input
                className="w-full rounded-[8px] border border-border bg-code-bg px-3.5 py-2.5 text-[13px] text-foreground outline-none transition-[border-color] focus:border-primary/40"
                value={confirmName}
                onChange={(e) => setConfirmName(e.target.value)}
                readOnly={Boolean(validation.upload_id)}
              />
            </div>
          </>
        )}

        {submitError && (
          <p
            className="mt-4 rounded-[8px] border border-destructive/20 bg-destructive/5 px-3 py-2 text-[12px] text-destructive"
            role="alert"
          >
            {submitError}
          </p>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            className="rounded-[8px] border border-border bg-muted px-4 py-2 text-[12px] font-medium text-text-secondary transition-[transform,background,color,opacity] hover:bg-bg-hover hover:text-foreground active:scale-[0.98]"
            onClick={handleClose}
          >
            {UI_ACTIONS.CANCEL}
          </button>
          {step === 'validate' && (
            <button
              type="button"
              className="inline-flex items-center gap-1.5 rounded-[8px] bg-primary px-4 py-2 text-[12px] font-medium text-primary-foreground transition-[transform,background,opacity] hover:bg-primary/90 active:scale-[0.98] disabled:opacity-50"
              onClick={handleConfirm}
              disabled={uploading || !confirmName.trim()}
            >
              {UI_MISC.CONFIRM_IMPORT}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

// ── 删除确认对话框 ──

function DeleteConfirmDialog({
  name,
  onConfirm,
  onCancel,
  loading,
  error,
}: {
  name: string
  onConfirm: () => void
  onCancel: () => void
  loading: boolean
  error?: string
}) {
  const dialogRef = useRef<HTMLDivElement>(null)

  useDialogFocusTrap(dialogRef)

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !loading) onCancel()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [loading, onCancel])

  const handleCancel = () => {
    if (!loading) onCancel()
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="skill-delete-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-neutral-950/55 px-4 backdrop-blur-[2px]"
      onClick={handleCancel}
    >
      <div
        ref={dialogRef}
        tabIndex={-1}
        className="max-h-[calc(100dvh-2rem)] w-full max-w-[400px] overflow-y-auto rounded-xl border border-border bg-card p-4 shadow-[var(--shadow-popup)] sm:p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 id="skill-delete-title" className="mb-2 text-[15px] font-semibold">
          <span className="mr-1.5 text-amber-500">
            <AlertTriangle className="h-4 w-4 inline" strokeWidth={1.25} />
          </span>
          确认删除
        </h3>
        <p className="text-[13px] leading-relaxed text-text-secondary">
          确定从技能库中删除 <span className="font-medium text-destructive">{name}</span>？
          <br />
          <span className="text-tertiary">
            此操作仅删除技能库中的源文件，<span className="text-foreground">不影响</span>
            已导入到 Agent 的副本。已导入的 Skill 需到对应 Agent 详情页移除。
          </span>
        </p>
        {error && (
          <p
            className="mt-4 rounded-[8px] border border-destructive/20 bg-destructive/5 px-3 py-2 text-[12px] text-destructive"
            role="alert"
          >
            {error}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            className="rounded-[8px] border border-border bg-muted px-4 py-2 text-[12px] font-medium text-text-secondary transition-[transform,background,opacity] hover:bg-bg-hover active:scale-[0.98]"
            onClick={handleCancel}
          >
            {UI_ACTIONS.CANCEL}
          </button>
          <button
            type="button"
            className="rounded-[8px] border border-destructive/20 bg-destructive/10 px-4 py-2 text-[12px] font-medium text-destructive transition-[transform,background,opacity] hover:bg-destructive/20 active:scale-[0.98] disabled:opacity-50"
            onClick={onConfirm}
            disabled={loading}
          >
            确认删除
          </button>
        </div>
      </div>
    </div>
  )
}
