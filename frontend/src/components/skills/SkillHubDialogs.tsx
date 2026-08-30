import { AlertTriangle, CheckCircle2, Package, Upload, XCircle } from 'lucide-react'
import { type RefObject, useCallback, useId, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { SkillUploadResponse } from '@/generated/skill-storage'
import { confirmSkill, uploadSkill } from '@/lib/api'
import { UI_ACTIONS, UI_ERRORS, UI_MISC, UI_PROFILE } from '@/lib/ui-text'
import { cn, isFocusableTarget } from '@/lib/utils'

type ValidationResponse = SkillUploadResponse
type RestoreFocusRef = RefObject<HTMLButtonElement | null>

interface UploadDialogProps {
  onClose: () => void
  onSuccess: () => void
  restoreFocusRef?: RestoreFocusRef
}

export function UploadDialog({ onClose, onSuccess, restoreFocusRef }: UploadDialogProps) {
  const [step, setStep] = useState<'upload' | 'validate'>('upload')
  const [dragging, setDragging] = useState(false)
  const [validation, setValidation] = useState<ValidationResponse | null>(null)
  const [confirmName, setConfirmName] = useState('')
  const [uploading, setUploading] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)
  const confirmNameId = useId()

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

  const handleClose = () => {
    if (!uploading) onClose()
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) handleClose()
      }}
    >
      <DialogContent
        showCloseButton={!uploading}
        className="max-w-[520px] border-border bg-card shadow-[var(--shadow-popup)]"
        onCloseAutoFocus={(event) => {
          const trigger = restoreFocusRef?.current
          if (!isFocusableTarget(trigger)) return
          event.preventDefault()
          trigger.focus()
        }}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-[15px]">
            <Upload className="h-[18px] w-[18px] text-primary" aria-hidden="true" />
            {UI_ACTIONS.UPLOAD}
          </DialogTitle>
          <DialogDescription className="sr-only">上传并校验一个外部 Skill 压缩包</DialogDescription>
        </DialogHeader>
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
            onKeyDown={(event) => {
              if (event.key !== 'Enter' && event.key !== ' ') return
              event.preventDefault()
              if (!uploading) fileRef.current?.click()
            }}
            onDragOver={(event) => {
              event.preventDefault()
              setDragging(true)
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={(event) => {
              event.preventDefault()
              setDragging(false)
              const file = event.dataTransfer.files[0]
              if (file) void handleFile(file)
            }}
            aria-disabled={uploading}
          >
            <span className="mb-2 opacity-60">
              <Package className="h-8 w-8" strokeWidth={1.25} aria-hidden="true" />
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
              onChange={(event) => {
                const file = event.target.files?.[0]
                if (file) void handleFile(file)
                event.currentTarget.value = ''
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
              <XCircle className="h-4 w-4" strokeWidth={1.25} aria-hidden="true" /> 校验失败
            </p>
            {validation.errors?.map((error, index) => (
              <p key={index} className="text-[12px] text-text-secondary">
                ✗ {error}
              </p>
            ))}
          </div>
        )}

        {step === 'validate' && validation?.valid && (
          <>
            <div className="mt-4 rounded-[8px] border border-success/20 bg-success/5 p-3.5">
              <p className="mb-2 flex items-center gap-1.5 text-[13px] font-semibold text-success">
                <CheckCircle2 className="h-4 w-4" strokeWidth={1.25} aria-hidden="true" /> 校验通过
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
              <label
                htmlFor={confirmNameId}
                className="mb-1.5 block text-[12px] font-medium text-text-secondary"
              >
                Skill 名称（确认后不可修改）
              </label>
              <input
                id={confirmNameId}
                className="w-full rounded-[8px] border border-border bg-code-bg px-3.5 py-2.5 text-[13px] text-foreground outline-none transition-[border-color] focus:border-primary/40"
                value={confirmName}
                onChange={(event) => setConfirmName(event.target.value)}
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
          <Button
            type="button"
            variant="secondary"
            size="sm"
            className="rounded-[8px] px-4 py-2 text-[12px]"
            onClick={handleClose}
            disabled={uploading}
          >
            {UI_ACTIONS.CANCEL}
          </Button>
          {step === 'validate' && (
            <Button
              type="button"
              size="sm"
              className="rounded-[8px] px-4 py-2 text-[12px]"
              onClick={() => void handleConfirm()}
              loading={uploading}
              disabled={!confirmName.trim()}
            >
              {UI_MISC.CONFIRM_IMPORT}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

interface DeleteConfirmDialogProps {
  name: string
  onConfirm: () => void
  onCancel: () => void
  loading: boolean
  error?: string
  restoreFocusRef?: RestoreFocusRef
  fallbackFocusRef?: RestoreFocusRef
}

export function DeleteConfirmDialog({
  name,
  onConfirm,
  onCancel,
  loading,
  error,
  restoreFocusRef,
  fallbackFocusRef,
}: DeleteConfirmDialogProps) {
  const handleCancel = () => {
    if (!loading) onCancel()
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) handleCancel()
      }}
    >
      <DialogContent
        showCloseButton={!loading}
        className="max-w-[400px] border-border bg-card shadow-[var(--shadow-popup)]"
        onCloseAutoFocus={(event) => {
          const trigger = restoreFocusRef?.current
          const fallback = fallbackFocusRef?.current
          const focusTarget = isFocusableTarget(trigger)
            ? trigger
            : isFocusableTarget(fallback)
              ? fallback
              : null
          if (!focusTarget) return
          event.preventDefault()
          focusTarget.focus()
        }}
      >
        <DialogHeader>
          <DialogTitle className="text-[15px]">
            <span className="mr-1.5 text-amber-500">
              <AlertTriangle className="inline h-4 w-4" strokeWidth={1.25} aria-hidden="true" />
            </span>
            确认删除
          </DialogTitle>
          <DialogDescription className="sr-only">确认删除技能库中的外部 Skill</DialogDescription>
        </DialogHeader>
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
          <Button
            type="button"
            variant="secondary"
            size="sm"
            className="rounded-[8px] px-4 py-2 text-[12px]"
            onClick={handleCancel}
            disabled={loading}
          >
            {UI_ACTIONS.CANCEL}
          </Button>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            className="rounded-[8px] px-4 py-2 text-[12px]"
            onClick={onConfirm}
            loading={loading}
          >
            确认删除
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
