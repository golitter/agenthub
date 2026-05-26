import { useState } from 'react'

import { API_BASE } from '@/lib/constants'

interface ImageCardProps {
  path: string
  sessionId?: string
}

export function ImageCard({ path, sessionId }: ImageCardProps) {
  const [error, setError] = useState(false)

  const fileUrl = sessionId ? `${API_BASE}/session/${sessionId}/files/${path}` : ''

  if (error || !fileUrl) {
    return (
      <div className="my-2 flex items-center justify-center rounded-lg border border-border bg-muted px-4 py-8 text-sm text-muted-foreground">
        图片加载失败
      </div>
    )
  }

  return (
    <div className="my-2 overflow-hidden rounded-lg border border-border">
      <img src={fileUrl} alt={path} className="max-w-full" onError={() => setError(true)} />
    </div>
  )
}
