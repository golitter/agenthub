import { ExternalLink } from 'lucide-react'

interface PreviewCardProps {
  url: string
}

export function PreviewCard({ url }: PreviewCardProps) {
  return (
    <div className="my-2 overflow-hidden rounded-lg border border-border">
      <div className="flex items-center justify-between border-b border-border bg-muted/50 px-3 py-1.5">
        <span className="text-xs text-muted-foreground">Preview</span>
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
        >
          在新标签页打开
          <ExternalLink className="h-3 w-3" />
        </a>
      </div>
      <iframe src={url} className="h-80 w-full border-0" title="Preview" />
    </div>
  )
}
