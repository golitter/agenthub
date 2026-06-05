import { useState } from 'react'

import { initGitRepo, validateRepoPath } from '@/lib/api'
import { UI_ACTIONS, UI_ERRORS, UI_LABELS, UI_MESSAGES, UI_STATUS } from '@/lib/ui-text'

interface RepoPathInputProps {
  onValidationChange: (path: string, validated: boolean) => void
}

export function RepoPathInput({ onValidationChange }: RepoPathInputProps) {
  const [repoPath, setRepoPath] = useState('')
  const [validated, setValidated] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [validating, setValidating] = useState(false)

  // Git init confirmation state
  const [needsGitInit, setNeedsGitInit] = useState(false)
  const [confirmInput, setConfirmInput] = useState('')
  const [initError, setInitError] = useState<string | null>(null)
  const [initializing, setInitializing] = useState(false)

  const lastSegment = repoPath.trim().split('/').filter(Boolean).pop() || ''
  const confirmMatch = confirmInput === lastSegment

  const handleValidate = async () => {
    const path = repoPath.trim()
    if (!path) {
      setError(UI_ERRORS.REPO_PATH_REQUIRED)
      setValidated(false)
      setNeedsGitInit(false)
      onValidationChange('', false)
      return
    }
    setValidating(true)
    setError(null)
    setNeedsGitInit(false)
    setConfirmInput('')
    setInitError(null)
    try {
      const result = await validateRepoPath(path)
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
      setValidated(false)
      setError(UI_ERRORS.VALIDATE_FAILED)
      onValidationChange(path, false)
    } finally {
      setValidating(false)
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
      <label className="mb-1 block text-xs font-medium text-muted-foreground">
        {UI_LABELS.REPO_PATH}
      </label>
      <div className="flex items-center gap-2">
        <input
          value={repoPath}
          placeholder="/path/to/repo"
          className="flex-1 rounded-md border bg-background px-2 py-1.5 text-xs text-foreground outline-none"
          style={{
            borderColor: error
              ? 'var(--destructive)'
              : validated
                ? 'var(--color-success)'
                : 'var(--border)',
            opacity: initializing ? 0.6 : 1,
          }}
          onChange={(e) => {
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
          className="shrink-0 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground"
          style={{ opacity: validating ? 0.6 : 1 }}
          onClick={handleValidate}
          disabled={validating || initializing}
        >
          {validating ? UI_STATUS.VALIDATING : UI_ACTIONS.VALIDATE}
        </button>
      </div>
      {error && <p className="mt-1 text-xs text-destructive">{error}</p>}
      {validated && (
        <p className="mt-1 text-xs" style={{ color: 'var(--color-success)' }}>
          路径校验通过
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
              value={confirmInput}
              placeholder={lastSegment}
              className="flex-1 rounded-md border bg-background px-2 py-1.5 text-xs text-foreground outline-none"
              style={{
                borderColor: confirmInput && !confirmMatch ? 'var(--destructive)' : 'var(--border)',
              }}
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
              className="shrink-0 rounded-md px-3 py-1.5 text-xs font-medium text-primary-foreground"
              style={{
                backgroundColor: confirmMatch ? 'var(--color-success)' : 'var(--primary)',
                opacity: confirmMatch && !initializing ? 1 : 0.5,
              }}
              onClick={handleInitGit}
              disabled={!confirmMatch || initializing}
            >
              {initializing ? UI_STATUS.INITIALIZING_GIT : UI_ACTIONS.INIT_GIT}
            </button>
            <button
              className="shrink-0 text-xs text-muted-foreground hover:text-foreground"
              onClick={cancelGitInit}
              disabled={initializing}
            >
              {UI_ACTIONS.CANCEL}
            </button>
          </div>
          {confirmInput && !confirmMatch && (
            <p className="mt-1 text-xs text-destructive">{UI_MESSAGES.GIT_INIT_MISMATCH}</p>
          )}
          {initError && <p className="mt-1 text-xs text-destructive">{initError}</p>}
        </div>
      )}
    </div>
  )
}
