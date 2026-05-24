import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'

import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { updateSession, uploadAvatar } from '@/lib/api'

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
  const fileRef = useRef<HTMLInputElement>(null)
  const queryClient = useQueryClient()

  // Reset form state when dialog opens
  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setName(initialName)
      setAvatarUrl(initialAvatarUrl)
    }
    onOpenChange(nextOpen)
  }

  const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    try {
      const url = await uploadAvatar(file)
      setAvatarUrl(url)
    } catch {
      // ignore — user can retry
    }
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      const data: { agent_name?: string; avatar_url?: string } = {}
      if (name !== initialName) data.agent_name = name
      if (avatarUrl !== initialAvatarUrl) data.avatar_url = avatarUrl
      if (Object.keys(data).length > 0) {
        await updateSession(sessionId, data)
        // Refresh conversation list so avatar/name update is visible
        await queryClient.invalidateQueries({ queryKey: ['conversations'] })
      }
      onOpenChange(false)
    } catch {
      // ignore
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        className="max-w-sm"
        style={{
          backgroundColor: 'var(--card)',
          borderColor: 'rgba(255,255,255,0.06)',
        }}
      >
        <DialogHeader>
          <DialogTitle style={{ color: 'var(--text-primary)' }}>编辑 Agent</DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-3">
            <div
              className="flex h-12 w-12 items-center justify-center overflow-hidden rounded-lg"
              style={{
                backgroundColor: 'var(--bg-canvas)',
                border: '1px solid rgba(255,255,255,0.06)',
              }}
            >
              {avatarUrl ? (
                <img src={avatarUrl} alt={name} width={48} height={48} className="rounded-lg" />
              ) : (
                <span className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
                  {name.charAt(0).toUpperCase()}
                </span>
              )}
            </div>
            <button
              className="rounded-md px-3 py-1.5 text-xs"
              style={{
                border: '1px solid rgba(255,255,255,0.06)',
                color: 'var(--text-secondary)',
              }}
              onClick={() => fileRef.current?.click()}
            >
              上传头像
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
            <label className="mb-1 block text-xs" style={{ color: 'var(--text-tertiary)' }}>
              名称
            </label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full rounded-md border px-3 py-2 text-sm outline-none"
              style={{
                borderColor: 'rgba(255,255,255,0.06)',
                backgroundColor: 'var(--bg-canvas)',
                color: 'var(--text-primary)',
              }}
            />
          </div>

          <button
            className="mt-1 w-full rounded-md py-2 text-sm font-medium"
            style={{ backgroundColor: 'var(--color-brand)', color: 'var(--text-primary)' }}
            onClick={handleSave}
            disabled={saving || !name.trim()}
          >
            {saving ? '保存中...' : '保存'}
          </button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
