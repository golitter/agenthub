import { Download, FileIcon } from 'lucide-react'

const API_BASE = '/api'

interface AttachmentCardProps {
  path: string
}

export function AttachmentCard({ path }: AttachmentCardProps) {
  const fileName = path.split('/').pop() || path

  return (
    <div className="my-2 flex items-center gap-3 rounded-lg border border-border bg-card px-4 py-3">
      <FileIcon className="h-5 w-5 shrink-0 text-muted-foreground" />
      <span className="truncate text-sm">{fileName}</span>
      <a
        href={`${API_BASE}/workspace/_placeholder/files/${path}`}
        download
        className="ml-auto shrink-0 rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
      >
        <Download className="h-4 w-4" />
      </a>
    </div>
  )
}
