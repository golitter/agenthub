import { useQuery } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { type RefObject, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { fetchSkills, importSkill } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { UI_ACTIONS, UI_ERRORS, UI_MESSAGES, UI_MISC, UI_PROFILE, UI_STATUS } from '@/lib/ui-text'
import { cn, isFocusableTarget } from '@/lib/utils'

interface ImportSkillDialogProps {
  sessionId: string
  currentSkills: string[]
  onClose: () => void
  onImported: () => void
  restoreFocusRef?: RefObject<HTMLButtonElement | null>
}

export function ImportSkillDialog({
  sessionId,
  currentSkills,
  onClose,
  onImported,
  restoreFocusRef,
}: ImportSkillDialogProps) {
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(false)
  const [importError, setImportError] = useState('')

  const {
    data: hubSkills = [],
    isError: skillsError,
    isLoading: skillsLoading,
    refetch: refetchSkills,
  } = useQuery({
    queryKey: queryKeys.skills,
    queryFn: fetchSkills,
  })

  const externals = hubSkills.filter((skill) => !skill.builtin)
  const alreadyImported = new Set(currentSkills)

  const toggle = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
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

  const handleClose = () => {
    if (!loading) onClose()
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) handleClose()
      }}
    >
      <DialogContent
        showCloseButton={!loading}
        className="max-w-[440px] border-border bg-card shadow-[var(--shadow-popup)]"
        onCloseAutoFocus={(event) => {
          const trigger = restoreFocusRef?.current
          if (!isFocusableTarget(trigger)) return
          event.preventDefault()
          trigger.focus()
        }}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-[15px]">
            <Plus className="h-[18px] w-[18px] text-primary" aria-hidden="true" />
            {UI_PROFILE.IMPORT_SKILL}
          </DialogTitle>
          <DialogDescription className="sr-only">从技能库选择并导入外部 Skill</DialogDescription>
        </DialogHeader>
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
                  aria-pressed={!imported && !unavailable ? isSelected : undefined}
                  onClick={() => !imported && !unavailable && toggle(skill.name)}
                >
                  <div
                    className={cn(
                      'flex h-4 w-4 shrink-0 items-center justify-center rounded-[4px] border',
                      isSelected ? 'border-primary bg-primary' : 'border-tertiary',
                    )}
                    aria-hidden="true"
                  >
                    {isSelected && (
                      <svg viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth={2.5}>
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
          <Button
            type="button"
            variant="secondary"
            size="sm"
            className="rounded-[8px] px-4 py-2 text-[12px]"
            onClick={handleClose}
            disabled={loading}
          >
            {UI_ACTIONS.CANCEL}
          </Button>
          <Button
            type="button"
            size="sm"
            className="rounded-[8px] px-4 py-2 text-[12px]"
            onClick={() => void handleImport()}
            loading={loading}
            disabled={selected.size === 0}
          >
            {UI_MISC.CONFIRM_IMPORT}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
