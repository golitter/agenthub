import { useQuery } from '@tanstack/react-query'
import { Camera } from 'lucide-react'
import { useRef, useState } from 'react'

import { AdminQueryError } from '@/components/admin/AdminQueryError'
import { getAdminAvatar, updateAdminAvatar, uploadAvatar } from '@/lib/api'
import { CURRENT_USER_NAME } from '@/lib/constants'
import { UI_LABELS, UI_MESSAGES, UI_STATUS } from '@/lib/ui-text'
import { useAdminStore } from '@/stores/admin'

export function UserManagementPage() {
  const { data, isError, isLoading, refetch } = useQuery({
    queryKey: ['admin-avatar'],
    queryFn: getAdminAvatar,
    staleTime: 30_000,
  })
  // Track a locally overridden URL (from upload); fall back to query data
  const [localAvatarUrl, setLocalAvatarUrl] = useState<string | null>(null)
  const setAdminAvatarUrl = useAdminStore((s) => s.setAdminAvatarUrl)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)

  const avatarUrl = localAvatarUrl || data?.url || '/favicon.svg'

  const handleAvatarChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setUploading(true)
    setError('')
    try {
      const url = await uploadAvatar(file)
      await updateAdminAvatar(url)
      setLocalAvatarUrl(url)
      setAdminAvatarUrl(url)
    } catch {
      setError(UI_MESSAGES.UPLOAD_FAILED)
    } finally {
      setUploading(false)
      e.target.value = ''
    }
  }

  if (isLoading && !localAvatarUrl) {
    return (
      <div className="p-6" aria-busy="true">
        <div className="mb-6 h-7 w-32 rounded-md skeleton-sheen" />
        <div className="rounded-lg border border-border bg-card p-6">
          <div className="mb-4 h-4 w-28 rounded-md skeleton-sheen" />
          <div className="flex items-center gap-4">
            <div className="h-20 w-20 rounded-lg skeleton-sheen" />
            <div className="space-y-2">
              <div className="h-4 w-24 rounded-md skeleton-sheen" />
              <div className="h-3 w-16 rounded-md skeleton-sheen" />
              <div className="h-4 w-20 rounded-md skeleton-sheen" />
            </div>
          </div>
        </div>
        <span className="sr-only">{UI_STATUS.LOADING}</span>
      </div>
    )
  }

  return (
    <div className="p-6">
      <h2 className="mb-6 text-lg font-semibold text-foreground">{UI_LABELS.USER_MANAGEMENT}</h2>
      {isError && <AdminQueryError onRetry={() => refetch()} />}

      <div className="rounded-lg border border-border bg-card p-6">
        <h3 className="mb-4 text-sm font-medium text-text-secondary">{UI_LABELS.UPLOAD_AVATAR}</h3>

        <div className="flex items-center gap-4">
          <div className="group relative">
            <div className="h-20 w-20 overflow-hidden rounded-lg">
              <img
                src={avatarUrl}
                alt={`${CURRENT_USER_NAME} 头像`}
                className="h-full w-full object-cover"
                onError={(event) => {
                  event.currentTarget.src = '/favicon.svg'
                }}
              />
            </div>
            <button
              type="button"
              className="absolute inset-0 flex items-center justify-center rounded-lg bg-black/45 opacity-100 transition-[opacity,transform] active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring md:opacity-0 md:group-hover:opacity-100"
              onClick={() => fileRef.current?.click()}
              disabled={uploading}
              aria-label={UI_LABELS.UPLOAD_AVATAR}
            >
              <Camera className="h-5 w-5 text-primary-foreground" strokeWidth={1.25} />
            </button>
            <input
              ref={fileRef}
              type="file"
              accept="image/jpeg,image/png,image/gif,image/webp"
              className="hidden"
              onChange={handleAvatarChange}
            />
          </div>

          <div>
            <p className="text-sm font-medium text-foreground">{CURRENT_USER_NAME}</p>
            <p className="mt-0.5 text-xs text-tertiary">管理员</p>
            <button
              type="button"
              className="mt-2 rounded-md px-0 py-1 text-xs text-brand transition-[color,transform,opacity] hover:text-primary/80 active:scale-[0.98] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              onClick={() => fileRef.current?.click()}
              disabled={uploading}
            >
              {uploading ? UI_STATUS.UPLOADING : UI_LABELS.CHANGE_AVATAR}
            </button>
            {error && (
              <p className="mt-1 text-xs text-error" role="alert">
                {error}
              </p>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
