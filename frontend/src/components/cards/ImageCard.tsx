import { useState } from 'react'

const API_BASE = '/api'

interface ImageCardProps {
  path: string
}

export function ImageCard({ path }: ImageCardProps) {
  const [error, setError] = useState(false)

  if (error) {
    return (
      <div className="my-2 flex items-center justify-center rounded-lg border border-border bg-muted px-4 py-8 text-sm text-muted-foreground">
        图片加载失败
      </div>
    )
  }

  return (
    <div className="my-2 overflow-hidden rounded-lg border border-border">
      <img
        src={`${API_BASE}/workspace/_placeholder/files/${path}`}
        alt={path}
        className="max-w-full"
        onError={() => setError(true)}
      />
    </div>
  )
}
