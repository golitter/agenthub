import { ExternalLink } from 'lucide-react'

import { UI_CARD_STATUS } from '@/lib/ui-text'

interface PreviewCardProps {
  url: string
}

export function PreviewCard({ url }: PreviewCardProps) {
  return (
    <div className="my-2 overflow-hidden rounded-lg border border-border">
      <div className="flex items-center justify-between border-b border-border bg-muted/50 px-3 py-1.5">
        <span className="text-xs text-muted-foreground">{UI_CARD_STATUS.PREVIEW}</span>
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 rounded-[4px] text-xs text-muted-foreground transition-[color,transform] hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring active:translate-y-px"
          aria-label={`${UI_CARD_STATUS.OPEN_IN_NEW_TAB}: ${url}`}
        >
          {UI_CARD_STATUS.OPEN_IN_NEW_TAB}
          <ExternalLink className="h-3 w-3" strokeWidth={1.25} aria-hidden="true" />
        </a>
      </div>
      <iframe src={url} className="h-80 w-full border-0" title={UI_CARD_STATUS.PREVIEW} />
    </div>
  )
}
