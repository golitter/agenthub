import { useQueryClient } from '@tanstack/react-query'
import { Pencil } from 'lucide-react'
import { useId, useState } from 'react'

import { Button } from '@/components/ui/button'
import { updateAgentSoul, updateSession } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { UI_ACTIONS, UI_ERRORS, UI_LABELS, UI_MESSAGES, UI_PLACEHOLDERS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

interface AgentNameEditorProps {
  sessionId: string
  name: string
}

export function AgentNameEditor({ sessionId, name }: AgentNameEditorProps) {
  const queryClient = useQueryClient()
  const inputId = useId()
  const errorId = `${inputId}-error`
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(name)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const startEditing = () => {
    setDraft(name)
    setError('')
    setEditing(true)
  }

  const save = async () => {
    const trimmed = draft.trim()
    if (!trimmed || trimmed === name) {
      setEditing(false)
      return
    }
    setSaving(true)
    setError('')
    try {
      await updateSession(sessionId, { agent_name: trimmed })
      await queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
      await queryClient.invalidateQueries({ queryKey: queryKeys.conversations })
      setEditing(false)
    } catch {
      setError(UI_ERRORS.PROFILE_SAVE_FAILED)
    } finally {
      setSaving(false)
    }
  }

  if (editing) {
    return (
      <div className="w-full">
        <div className="flex items-center gap-2">
          <label htmlFor={inputId} className="sr-only">
            {UI_LABELS.NAME}
          </label>
          <input
            id={inputId}
            autoFocus
            value={draft}
            onChange={(event) => {
              setDraft(event.target.value)
              setError('')
            }}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing) return
              if (event.key === 'Enter') void save()
              if (event.key === 'Escape') setEditing(false)
            }}
            className="w-full rounded-md border border-border bg-background px-2 py-1 text-xl font-semibold text-foreground outline-none"
            disabled={saving}
            aria-invalid={Boolean(error) || undefined}
            aria-describedby={error ? errorId : undefined}
          />
          <Button
            type="button"
            size="sm"
            className="shrink-0 rounded-md px-3 py-1 text-xs"
            onClick={() => void save()}
            loading={saving}
            disabled={!draft.trim()}
          >
            {UI_ACTIONS.SAVE}
          </Button>
        </div>
        {error && (
          <p id={errorId} className="mt-1 text-xs text-destructive" role="alert">
            {error}
          </p>
        )}
      </div>
    )
  }

  return (
    <div className="flex min-w-0 items-center gap-2">
      <h1 className="truncate text-xl font-semibold">{name}</h1>
      <button
        type="button"
        className="rounded-md p-1 text-foreground/40 transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground/70 active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        onClick={startEditing}
        aria-label={UI_ACTIONS.EDIT}
      >
        <Pencil className="h-3.5 w-3.5" strokeWidth={1.25} aria-hidden="true" />
      </button>
    </div>
  )
}

interface AgentSoulEditorProps {
  sessionId: string
  content: string
}

function countCharacters(value: string) {
  return value.replace(/ /g, '').length
}

export function AgentSoulEditor({ sessionId, content }: AgentSoulEditorProps) {
  const queryClient = useQueryClient()
  const inputId = useId()
  const errorId = `${inputId}-error`
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const characterCount = countCharacters(content)

  const startEditing = () => {
    setDraft(content)
    setEditing(true)
    setError('')
  }

  const save = async () => {
    const trimmed = draft.trim()
    setError('')
    if (countCharacters(trimmed) > 300) {
      setError(`不能超过 300 字（不含空格），当前 ${countCharacters(trimmed)} 字`)
      return
    }
    setSaving(true)
    try {
      await updateAgentSoul(sessionId, trimmed)
      await queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
      setEditing(false)
    } catch {
      setError(UI_ERRORS.PROFILE_SAVE_FAILED)
    } finally {
      setSaving(false)
    }
  }

  const clear = async () => {
    setSaving(true)
    setError('')
    try {
      await updateAgentSoul(sessionId, '')
      await queryClient.invalidateQueries({ queryKey: ['agent-detail', sessionId] })
    } catch {
      setError(UI_ERRORS.PROFILE_SAVE_FAILED)
    } finally {
      setSaving(false)
    }
  }

  return (
    <section>
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-foreground/50">
          SOUL.md
        </h2>
        {!editing && (
          <button
            type="button"
            className="flex items-center gap-1 rounded p-1 text-foreground/40 transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground/70 active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            onClick={startEditing}
            aria-label={UI_ACTIONS.EDIT}
          >
            <Pencil className="h-3 w-3" strokeWidth={1.25} aria-hidden="true" />
          </button>
        )}
      </div>
      {editing ? (
        <div className="space-y-2">
          <label htmlFor={inputId} className="sr-only">
            SOUL.md
          </label>
          <textarea
            id={inputId}
            autoFocus
            value={draft}
            onChange={(event) => {
              setDraft(event.target.value)
              setError('')
            }}
            onKeyDown={(event) => {
              if (event.key === 'Escape') setEditing(false)
            }}
            className="min-h-[120px] w-full resize-y rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground outline-none"
            placeholder={UI_PLACEHOLDERS.SOUL_DESCRIPTION}
            maxLength={330}
            disabled={saving}
            aria-invalid={Boolean(error) || undefined}
            aria-describedby={error ? errorId : undefined}
          />
          <div className="flex items-center justify-between">
            <span
              className={cn(
                'text-xs',
                countCharacters(draft) > 300 ? 'text-destructive' : 'text-tertiary',
              )}
            >
              {countCharacters(draft)}/300
            </span>
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="quiet"
                size="sm"
                className="px-3 py-1 text-xs"
                onClick={() => {
                  setEditing(false)
                  setError('')
                }}
                disabled={saving}
              >
                {UI_ACTIONS.CANCEL}
              </Button>
              <Button
                type="button"
                size="sm"
                className="rounded-md px-3 py-1 text-xs"
                onClick={() => void save()}
                loading={saving}
                disabled={countCharacters(draft) > 300}
              >
                {UI_ACTIONS.SAVE}
              </Button>
            </div>
          </div>
          {error && (
            <p id={errorId} className="text-xs text-destructive" role="alert">
              {error}
            </p>
          )}
        </div>
      ) : content ? (
        <div className="rounded-md border border-border bg-background px-3 py-2">
          <p className="whitespace-pre-wrap text-sm text-foreground/80">{content}</p>
          <div className="mt-1.5 flex items-center justify-between">
            <span className="text-xs text-tertiary">{characterCount}/300 字（不含空格）</span>
            <Button
              type="button"
              variant="quiet"
              size="sm"
              className="px-2 py-1 text-xs text-destructive/60 hover:text-destructive"
              onClick={() => void clear()}
              loading={saving}
            >
              {UI_ACTIONS.CLEAR}
            </Button>
          </div>
        </div>
      ) : (
        <button
          type="button"
          className="rounded-md border border-dashed border-border px-3 py-2 text-sm text-muted-foreground transition-[border-color,color,transform] hover:border-foreground/20 hover:text-foreground/70 active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          onClick={startEditing}
        >
          {UI_MESSAGES.CLICK_TO_WRITE_SOUL}
        </button>
      )}
      {!editing && error && (
        <p className="mt-2 text-xs text-destructive" role="alert">
          {error}
        </p>
      )}
    </section>
  )
}
