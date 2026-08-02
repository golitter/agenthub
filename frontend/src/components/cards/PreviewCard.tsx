import { ExternalLink } from 'lucide-react'

import { UI_CARD_STATUS } from '@/lib/ui-text'
import { getSafeHttpUrl } from '@/lib/utils'

interface PreviewCardProps {
  url: string
}

export function PreviewCard({ url }: PreviewCardProps) {
  const safeUrl = getSafeHttpUrl(url)

  if (!safeUrl) {
    return (
      <div
        className="my-2 rounded-lg border border-destructive/50 bg-card px-4 py-3 text-sm text-destructive"
        role="alert"
      >
        {UI_CARD_STATUS.INVALID_PREVIEW_URL}
      </div>
    )
  }

  return (
    <div className="my-2 overflow-hidden rounded-lg border border-border">
      <div className="flex items-center justify-between border-b border-border bg-muted/50 px-3 py-1.5">
        <span className="text-xs text-muted-foreground">{UI_CARD_STATUS.PREVIEW}</span>
        <a
          href={safeUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 rounded-[4px] text-xs text-muted-foreground transition-[color,transform] hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring active:translate-y-px"
          aria-label={`${UI_CARD_STATUS.OPEN_IN_NEW_TAB}: ${safeUrl}`}
        >
          {UI_CARD_STATUS.OPEN_IN_NEW_TAB}
          <ExternalLink className="h-3 w-3" strokeWidth={1.25} aria-hidden="true" />
        </a>
      </div>
      <iframe
        src={safeUrl}
        sandbox="allow-scripts allow-forms"
        className="h-80 w-full border-0"
        title={UI_CARD_STATUS.PREVIEW}
      />
    </div>
  )
}
