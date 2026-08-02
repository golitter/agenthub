import { Download, FileIcon } from 'lucide-react'

import { API_BASE } from '@/lib/constants'
import { UI_ACTIONS } from '@/lib/ui-text'
import { encodePathSegments, getFileName } from '@/lib/utils'

interface AttachmentCardProps {
  path: string
  sessionId?: string
}

export function AttachmentCard({ path, sessionId }: AttachmentCardProps) {
  const fileName = getFileName(path)
  const fileUrl = sessionId
    ? `${API_BASE}/session/${encodeURIComponent(sessionId)}/files/${encodePathSegments(path)}`
    : ''

  return (
    <div className="my-2 flex items-center gap-3 rounded-lg border border-border bg-card px-4 py-3">
      <FileIcon
        className="h-5 w-5 shrink-0 text-muted-foreground"
        strokeWidth={1.25}
        aria-hidden="true"
      />
      <span className="truncate text-sm">{fileName}</span>
      {fileUrl && (
        <a
          href={fileUrl}
          download
          className="ml-auto shrink-0 rounded-md p-1.5 text-muted-foreground transition-[background-color,color,transform] hover:bg-accent hover:text-accent-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring active:translate-y-px"
          aria-label={`${UI_ACTIONS.DOWNLOAD}: ${fileName}`}
        >
          <Download className="h-4 w-4" strokeWidth={1.25} aria-hidden="true" />
        </a>
      )}
    </div>
  )
}
