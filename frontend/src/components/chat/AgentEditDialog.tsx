import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { updateSession, uploadAvatar } from '@/lib/api'
import { UI_ACTIONS, UI_ERRORS, UI_LABELS, UI_STATUS } from '@/lib/ui-text'

interface AgentEditDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  sessionId: string
  agentName: string
  avatarUrl?: string
}

export function AgentEditDialog({
  open,
  onOpenChange,
  sessionId,
  agentName: initialName,
  avatarUrl: initialAvatarUrl,
}: AgentEditDialogProps) {
  const [name, setName] = useState(initialName)
  const [avatarUrl, setAvatarUrl] = useState(initialAvatarUrl)
  const [saving, setSaving] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)
  const queryClient = useQueryClient()

  const [prevOpen, setPrevOpen] = useState(open)
  if (prevOpen !== open) {
    setPrevOpen(open)
    if (open) {
      setName(initialName)
      setAvatarUrl(initialAvatarUrl)
      setError('')
    }
  }

  const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    if (uploading) return
    setUploading(true)
    setError('')
    try {
      const url = await uploadAvatar(file)
      setAvatarUrl(url)
    } catch {
      setError(UI_ERRORS.AVATAR_UPLOAD_FAILED)
    } finally {
      setUploading(false)
      e.target.value = ''
    }
  }

  const handleSave = async () => {
    setSaving(true)
    setError('')
    try {
      const data: { agent_name?: string; avatar_url?: string } = {}
      if (name !== initialName) data.agent_name = name
      if (avatarUrl !== initialAvatarUrl) data.avatar_url = avatarUrl
      if (Object.keys(data).length > 0) {
        await updateSession(sessionId, data)
        await queryClient.invalidateQueries({ queryKey: ['conversations'] })
      }
      onOpenChange(false)
    } catch {
      setError(UI_ERRORS.PROFILE_SAVE_FAILED)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm bg-card border-border">
        <DialogHeader>
          <DialogTitle className="text-foreground">{UI_LABELS.EDIT_AGENT}</DialogTitle>
          <DialogDescription className="sr-only">{UI_LABELS.EDIT_AGENT_DESC}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-3">
            <div className="flex h-12 w-12 items-center justify-center overflow-hidden rounded-lg bg-background border border-border">
              {avatarUrl ? (
                <img src={avatarUrl} alt={name} width={48} height={48} className="rounded-lg" />
              ) : (
                <span className="text-sm font-semibold text-foreground">
                  {name.charAt(0).toUpperCase()}
                </span>
              )}
            </div>
            <button
              type="button"
              className="rounded-md border border-border px-3 py-1.5 text-xs text-muted-foreground transition-[background,opacity] hover:bg-hover disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              onClick={() => fileRef.current?.click()}
              disabled={saving || uploading}
            >
              {uploading ? UI_STATUS.UPLOADING : UI_LABELS.UPLOAD_AVATAR}
            </button>
            <input
              ref={fileRef}
              type="file"
              accept="image/jpeg,image/png,image/gif,image/webp"
              className="hidden"
              onChange={handleFileChange}
            />
          </div>

          <div>
            <label htmlFor="agent-edit-name" className="mb-1 block text-xs text-tertiary">
              {UI_LABELS.NAME}
            </label>
            <input
              id="agent-edit-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground outline-none focus:border-primary-border focus:ring-2 focus:ring-primary/15"
            />
          </div>

          {error && (
            <p className="text-xs text-destructive" role="alert">
              {error}
            </p>
          )}

          <button
            type="button"
            className="mt-1 w-full rounded-md bg-primary py-2 text-sm font-medium text-primary-foreground"
            onClick={handleSave}
            disabled={saving || uploading || !name.trim()}
          >
            {saving ? UI_STATUS.SAVING : UI_ACTIONS.SAVE}
          </button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
