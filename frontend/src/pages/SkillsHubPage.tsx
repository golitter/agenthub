import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Search, Star, Trash2, Upload } from 'lucide-react'
import { useCallback, useRef, useState } from 'react'

import { confirmSkill, deleteSkill, fetchSkills, type SkillHubItem, uploadSkill } from '@/lib/api'
import { cn } from '@/lib/utils'

// ── Types ──

interface ValidationResponse {
  valid: boolean
  name?: string
  description?: string
  file_count?: number
  total_size?: number
  tmp_dir?: string
  errors?: string[]
}

// ── SkillsHub Page ──

export function SkillsHubPage() {
  const [search, setSearch] = useState('')
  const [showUpload, setShowUpload] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const queryClient = useQueryClient()

  const { data: skills = [], isLoading } = useQuery({
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

  const filtered = skills.filter((s) => s.name.toLowerCase().includes(search.toLowerCase()))
  const builtins = filtered.filter((s) => s.builtin)
  const externals = filtered.filter((s) => !s.builtin)

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-border bg-card px-5 py-3.5">
        <h2 className="flex items-center gap-2 text-[15px] font-semibold">
          <Star className="h-[18px] w-[18px] text-primary" />
          技能库
        </h2>
        <button
          className="inline-flex items-center gap-1.5 rounded-[8px] bg-primary px-3.5 py-2 text-[12px] font-medium text-white transition-[transform,opacity] hover:bg-primary/90"
          onClick={() => setShowUpload(true)}
        >
          <Upload className="h-3.5 w-3.5" />
          上传 Skill
        </button>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-auto p-5">
        {/* Search */}
        <div className="mb-4 flex items-center gap-2 rounded-[8px] border border-border bg-muted px-3 py-2">
          <Search className="h-3.5 w-3.5 text-tertiary" />
          <input
            type="text"
            placeholder="搜索技能..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="flex-1 border-none bg-transparent text-[13px] text-foreground outline-none placeholder:text-tertiary"
          />
        </div>

        {isLoading ? (
          <div className="flex items-center justify-center py-20 text-sm text-tertiary">
            加载中...
          </div>
        ) : (
          <>
            {/* Builtin Section */}
            {builtins.length > 0 && <SectionLabel icon="🛡️" label="内置 Skills" />}
            {builtins.map((skill) => (
              <HubSkillCard key={skill.name} skill={skill} onDelete={undefined} />
            ))}

            {/* External Section */}
            {externals.length > 0 && <SectionLabel icon="📦" label="外部 Skills" />}
            {externals.map((skill) => (
              <HubSkillCard
                key={skill.name}
                skill={skill}
                onDelete={() => setDeleteTarget(skill.name)}
              />
            ))}

            {filtered.length === 0 && (
              <div className="flex flex-col items-center justify-center py-20 text-tertiary">
                <Star className="mb-3 h-8 w-8 opacity-40" />
                <p className="text-[13px] font-medium">暂无技能</p>
                <p className="text-[12px]">点击右上角「上传 Skill」添加</p>
              </div>
            )}
          </>
        )}
      </div>

      {/* Upload Dialog */}
      {showUpload && (
        <UploadDialog
          onClose={() => setShowUpload(false)}
          onSuccess={() => {
            queryClient.invalidateQueries({ queryKey: ['skills'] })
            setShowUpload(false)
          }}
        />
      )}

      {/* Delete Confirmation */}
      {deleteTarget && (
        <DeleteConfirmDialog
          name={deleteTarget}
          onConfirm={() => deleteMutation.mutate(deleteTarget)}
          onCancel={() => setDeleteTarget(null)}
          loading={deleteMutation.isPending}
        />
      )}
    </div>
  )
}

// ── Section Label ──

function SectionLabel({ icon, label }: { icon: string; label: string }) {
  return (
    <div className="mb-1.5 mt-4 flex items-center gap-1.5 px-0.5 py-2 text-[11px] font-semibold uppercase tracking-wider text-tertiary first:mt-0">
      <span>{icon}</span>
      {label}
    </div>
  )
}

// ── Hub Skill Card ──

function HubSkillCard({ skill, onDelete }: { skill: SkillHubItem; onDelete?: () => void }) {
  return (
    <div className="mb-2.5 rounded-[10px] border border-border bg-card p-4 transition-[background,border-color] hover:bg-hover hover:border-white/10">
      <div className="mb-1.5 flex items-center gap-2.5">
        <div
          className={cn(
            'flex h-8 w-8 items-center justify-center rounded-[8px] text-base',
            skill.builtin ? 'bg-success/10' : 'bg-primary/10',
          )}
        >
          {skill.builtin ? '⚙️' : '📦'}
        </div>
        <span className="flex-1 text-[14px] font-semibold">{skill.name}</span>
        <span
          className={cn(
            'rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide',
            skill.builtin
              ? 'bg-success/10 text-success border border-success/15'
              : 'bg-primary/10 text-primary border border-primary/15',
          )}
        >
          {skill.builtin ? 'builtin' : 'external'}
        </span>
      </div>
      <p className="mb-2 pl-[42px] text-[12px] text-text-secondary">{skill.description}</p>
      {!skill.builtin && (
        <div className="flex items-center justify-between pl-[42px]">
          <span className="text-[11px] text-tertiary">已被 {skill.import_count} 个 Agent 导入</span>
          {onDelete && (
            <button
              className="inline-flex items-center gap-1 rounded-[6px] border border-red-500/20 bg-red-500/10 px-2.5 py-1 text-[11px] text-red-500 transition-[transform,opacity] hover:bg-red-500/20"
              onClick={(e) => {
                e.stopPropagation()
                onDelete()
              }}
            >
              <Trash2 className="h-3 w-3" />
              删除
            </button>
          )}
        </div>
      )}
    </div>
  )
}

// ── Upload Dialog ──

function UploadDialog({ onClose, onSuccess }: { onClose: () => void; onSuccess: () => void }) {
  const [step, setStep] = useState<'upload' | 'validate'>('upload')
  const [dragging, setDragging] = useState(false)
  const [validation, setValidation] = useState<ValidationResponse | null>(null)
  const [confirmName, setConfirmName] = useState('')
  const [uploading, setUploading] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  const handleFile = useCallback(async (file: File) => {
    if (!file.name.endsWith('.zip')) return
    setUploading(true)
    try {
      const result = await uploadSkill(file)
      setValidation(result as unknown as ValidationResponse)
      if (result.valid && result.name) {
        setConfirmName(result.name)
        setStep('validate')
      }
    } catch (err) {
      setValidation({ valid: false, errors: [(err as Error).message] })
    } finally {
      setUploading(false)
    }
  }, [])

  const handleConfirm = async () => {
    if (!validation || !confirmName.trim()) return
    setUploading(true)
    try {
      await confirmSkill({
        name: confirmName,
        description: validation.description || '',
        file_count: validation.file_count || 0,
        total_size: validation.total_size || 0,
        tmp_dir: validation.tmp_dir || '',
      })
      onSuccess()
    } catch (err) {
      alert((err as Error).message)
    } finally {
      setUploading(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={onClose}
    >
      <div
        className="w-[90%] max-w-[520px] rounded-xl border border-border bg-card p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="mb-2 flex items-center gap-2 text-[15px] font-semibold">
          <Upload className="h-[18px] w-[18px] text-primary" />
          上传 Skill
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
            className={cn(
              'flex cursor-pointer flex-col items-center rounded-[10px] border-2 border-dashed p-10 text-center transition-all',
              dragging
                ? 'border-primary bg-primary/8'
                : 'border-border bg-muted hover:border-primary hover:bg-primary/8',
            )}
            onClick={() => fileRef.current?.click()}
            onDragOver={(e) => {
              e.preventDefault()
              setDragging(true)
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={(e) => {
              e.preventDefault()
              setDragging(false)
              const file = e.dataTransfer.files[0]
              if (file) handleFile(file)
            }}
          >
            <span className="mb-2 text-3xl opacity-60">📦</span>
            <p className="text-[13px] font-medium text-foreground">点击或拖拽上传 .zip 文件</p>
            <p className="mt-1 text-[11px] text-tertiary">
              支持 .zip 格式，解压后不超过 10MB，文件数不超过 100
            </p>
            <input
              ref={fileRef}
              type="file"
              accept=".zip"
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0]
                if (file) handleFile(file)
              }}
            />
          </div>
        )}

        {validation && !validation.valid && (
          <div className="mt-4 rounded-[8px] border border-red-500/20 bg-red-500/5 p-3.5">
            <p className="mb-2 text-[13px] font-semibold text-red-500">❌ 校验失败</p>
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
              <p className="mb-2 text-[13px] font-semibold text-success">✅ 校验通过</p>
              <p className="text-[12px] text-text-secondary">✓ SKILL.md 存在</p>
              <p className="text-[12px] text-text-secondary">
                ✓ frontmatter: name={validation.name}
              </p>
              <p className="text-[12px] text-text-secondary">✓ 文件数: {validation.file_count}</p>
              <p className="text-[12px] text-text-secondary">
                ✓ 大小: {((validation.total_size || 0) / 1024).toFixed(0)} KB
              </p>
              <p className="text-[12px] text-text-secondary">✓ 安全检查通过</p>
            </div>
            <div className="mt-4">
              <label className="mb-1.5 block text-[12px] font-medium text-text-secondary">
                Skill 名称（确认后不可修改）
              </label>
              <input
                className="w-full rounded-[8px] border border-border bg-code-bg px-3.5 py-2.5 text-[13px] text-foreground outline-none transition-[border-color] focus:border-primary/40"
                value={confirmName}
                onChange={(e) => setConfirmName(e.target.value)}
              />
            </div>
          </>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <button
            className="rounded-[8px] border border-border bg-muted px-4 py-2 text-[12px] font-medium text-text-secondary transition-[transform,opacity] hover:bg-hover hover:text-foreground"
            onClick={onClose}
          >
            取消
          </button>
          {step === 'validate' && (
            <button
              className="inline-flex items-center gap-1.5 rounded-[8px] bg-primary px-4 py-2 text-[12px] font-medium text-white transition-[transform,opacity] hover:bg-primary/90 disabled:opacity-50"
              onClick={handleConfirm}
              disabled={uploading || !confirmName.trim()}
            >
              确认入库
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

// ── Delete Confirm Dialog ──

function DeleteConfirmDialog({
  name,
  onConfirm,
  onCancel,
  loading,
}: {
  name: string
  onConfirm: () => void
  onCancel: () => void
  loading: boolean
}) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={onCancel}
    >
      <div
        className="w-[90%] max-w-[400px] rounded-xl border border-border bg-card p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="mb-2 text-[15px] font-semibold">
          <span className="mr-1.5 text-amber-500">⚠️</span>
          确认删除
        </h3>
        <p className="text-[13px] leading-relaxed text-text-secondary">
          确定从技能库中删除 <span className="font-medium text-red-500">{name}</span>？
          <br />
          <span className="text-tertiary">
            此操作仅删除技能库中的源文件，<span className="text-foreground">不影响</span>
            已导入到 Agent 的副本。已导入的 Skill 需到对应 Agent 详情页移除。
          </span>
        </p>
        <div className="mt-5 flex justify-end gap-2">
          <button
            className="rounded-[8px] border border-border bg-muted px-4 py-2 text-[12px] font-medium text-text-secondary transition-[transform,opacity] hover:bg-hover"
            onClick={onCancel}
          >
            取消
          </button>
          <button
            className="rounded-[8px] border border-red-500/20 bg-red-500/10 px-4 py-2 text-[12px] font-medium text-red-500 transition-[transform,opacity] hover:bg-red-500/20 disabled:opacity-50"
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
