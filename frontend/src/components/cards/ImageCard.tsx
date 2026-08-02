import { useState } from 'react'

import { API_BASE } from '@/lib/constants'
import { UI_CARD_STATUS } from '@/lib/ui-text'
import { encodePathSegments } from '@/lib/utils'

interface ImageCardProps {
  path: string
  sessionId?: string
}

export function ImageCard({ path, sessionId }: ImageCardProps) {
  const [error, setError] = useState(false)

  const fileUrl = sessionId
    ? `${API_BASE}/session/${encodeURIComponent(sessionId)}/files/${encodePathSegments(path)}`
    : ''

  if (error || !fileUrl) {
    return (
      <div
        className="my-2 flex items-center justify-center rounded-lg border border-border bg-muted px-4 py-8 text-sm text-muted-foreground"
        role="alert"
      >
        {UI_CARD_STATUS.IMAGE_LOAD_FAILED}
      </div>
    )
  }

  return (
    <div className="my-2 overflow-hidden rounded-lg border border-border">
      <img
        src={fileUrl}
        alt={`聊天图片：${path}`}
        className="h-auto max-w-full"
        loading="lazy"
        onError={() => setError(true)}
      />
    </div>
  )
}
