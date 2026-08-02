import { useId, useRef, useState } from 'react'

import { initGitRepo, validateRepoPath } from '@/lib/api'
import { UI_ACTIONS, UI_ERRORS, UI_LABELS, UI_MESSAGES, UI_STATUS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

interface RepoPathInputProps {
  onValidationChange: (path: string, validated: boolean) => void
}

export function RepoPathInput({ onValidationChange }: RepoPathInputProps) {
  const [repoPath, setRepoPath] = useState('')
  const [validated, setValidated] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [validating, setValidating] = useState(false)
  const validationRequestRef = useRef(0)

  // Git init confirmation state
  const [needsGitInit, setNeedsGitInit] = useState(false)
  const [confirmInput, setConfirmInput] = useState('')
  const [initError, setInitError] = useState<string | null>(null)
  const [initializing, setInitializing] = useState(false)
  const inputId = useId()
  const confirmInputId = `${inputId}-confirm`

  const lastSegment = repoPath.trim().split('/').filter(Boolean).pop() || ''
  const confirmMatch = confirmInput === lastSegment

  const handleValidate = async () => {
    if (validating || initializing) return
    const path = repoPath.trim()
    if (!path) {
      setError(UI_ERRORS.REPO_PATH_REQUIRED)
      setValidated(false)
      setNeedsGitInit(false)
      onValidationChange('', false)
      return
    }
    const requestId = ++validationRequestRef.current
    setValidating(true)
    setError(null)
    setNeedsGitInit(false)
    setConfirmInput('')
    setInitError(null)
    try {
      const result = await validateRepoPath(path)
      if (requestId !== validationRequestRef.current) return
      if (result.valid) {
        setValidated(true)
        setError(null)
        onValidationChange(path, true)
      } else {
        // Check if the error is specifically "not a git repo"
        const notGitRepo = result.errors.some((e) => e.includes('不是 git 仓库'))
        if (notGitRepo) {
          setNeedsGitInit(true)
          setError(null)
          setValidated(false)
        } else {
          setValidated(false)
          setError(result.errors.join('; '))
        }
        onValidationChange(path, false)
      }
    } catch {
      if (requestId !== validationRequestRef.current) return
      setValidated(false)
      setError(UI_ERRORS.VALIDATE_FAILED)
      onValidationChange(path, false)
    } finally {
      if (requestId === validationRequestRef.current) setValidating(false)
    }
  }

  const handleInitGit = async () => {
    const path = repoPath.trim()
    if (!confirmMatch) return

    setInitializing(true)
    setInitError(null)
    try {
      const result = await initGitRepo(path)
      if (result.success) {
        setNeedsGitInit(false)
        setValidated(true)
        setConfirmInput('')
        onValidationChange(path, true)
      } else {
        setInitError(result.errors.join('; '))
      }
    } catch {
      setInitError(UI_ERRORS.GIT_INIT_FAILED)
    } finally {
      setInitializing(false)
    }
  }

  const cancelGitInit = () => {
    setNeedsGitInit(false)
    setConfirmInput('')
    setInitError(null)
  }

  return (
    <div className="mb-3">
      <label htmlFor={inputId} className="mb-1 block text-xs font-medium text-muted-foreground">
        {UI_LABELS.REPO_PATH}
      </label>
      <div className="flex items-center gap-2">
        <input
          id={inputId}
          value={repoPath}
          placeholder="/path/to/repo"
          className={cn(
            'flex-1 rounded-md border bg-background px-2 py-1.5 text-xs text-foreground outline-none transition-[border-color,box-shadow,opacity] focus:ring-2 focus:ring-primary/15 disabled:opacity-60',
            error
              ? 'border-destructive'
              : validated
                ? 'border-success focus:ring-success/15'
                : 'border-border',
          )}
          aria-invalid={Boolean(error) || undefined}
          onChange={(e) => {
            validationRequestRef.current += 1
            setRepoPath(e.target.value)
            setValidated(false)
            setError(null)
            setNeedsGitInit(false)
            setConfirmInput('')
            setInitError(null)
            onValidationChange(e.target.value.trim(), false)
          }}
          onKeyDown={(e) => {
            if (e.nativeEvent.isComposing) return
            if (e.key === 'Enter') handleValidate()
          }}
          disabled={initializing}
        />
        <button
          type="button"
          className="shrink-0 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-[background,transform,opacity] hover:bg-primary/90 active:scale-[0.97] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          onClick={handleValidate}
          disabled={validating || initializing}
        >
          {validating ? UI_STATUS.VALIDATING : UI_ACTIONS.VALIDATE}
        </button>
      </div>
      {error && (
        <p className="mt-1 text-xs text-destructive" role="alert">
          {error}
        </p>
      )}
      {validated && (
        <p className="mt-1 text-xs text-success" role="status">
          {UI_MESSAGES.REPO_PATH_VALID}
        </p>
      )}
      {needsGitInit && (
        <div
          className="mt-2 rounded-md border border-amber-500/30 bg-amber-500/5 p-2.5"
          style={{ opacity: initializing ? 0.7 : 1 }}
        >
          <p className="mb-2 text-xs text-amber-600">
            {UI_MESSAGES.GIT_INIT_PROMPT}：
            <strong className="text-foreground">{lastSegment}</strong>
          </p>
          <div className="flex items-center gap-2">
            <input
              id={confirmInputId}
              value={confirmInput}
              placeholder={lastSegment}
              className={cn(
                'flex-1 rounded-md border bg-background px-2 py-1.5 text-xs text-foreground outline-none transition-[border-color,box-shadow] focus:ring-2 focus:ring-primary/15',
                confirmInput && !confirmMatch ? 'border-destructive' : 'border-border',
              )}
              aria-invalid={Boolean(confirmInput && !confirmMatch) || undefined}
              onChange={(e) => {
                setConfirmInput(e.target.value)
                setInitError(null)
              }}
              onKeyDown={(e) => {
                if (e.nativeEvent.isComposing) return
                if (e.key === 'Enter') handleInitGit()
              }}
              disabled={initializing}
            />
            <button
              type="button"
              className={cn(
                'shrink-0 rounded-md px-3 py-1.5 text-xs font-medium text-primary-foreground transition-[background,transform,opacity] active:scale-[0.97] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
                confirmMatch ? 'bg-success hover:bg-success/90' : 'bg-primary',
              )}
              onClick={handleInitGit}
              disabled={!confirmMatch || initializing}
            >
              {initializing ? UI_STATUS.INITIALIZING_GIT : UI_ACTIONS.INIT_GIT}
            </button>
            <button
              type="button"
              className="shrink-0 rounded-md px-2 py-1.5 text-xs text-muted-foreground transition-[background,color,transform] hover:bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              onClick={cancelGitInit}
              disabled={initializing}
            >
              {UI_ACTIONS.CANCEL}
            </button>
          </div>
          {confirmInput && !confirmMatch && (
            <p className="mt-1 text-xs text-destructive" role="alert">
              {UI_MESSAGES.GIT_INIT_MISMATCH}
            </p>
          )}
          {initError && (
            <p className="mt-1 text-xs text-destructive" role="alert">
              {initError}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
