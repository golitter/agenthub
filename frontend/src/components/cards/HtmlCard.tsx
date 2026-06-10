import { Loader2 } from 'lucide-react'

import { UI_CARD_STATUS } from '@/lib/ui-text'

interface HtmlCardProps {
  content: string
  expanded?: boolean
  streaming?: boolean
}

export function HtmlCard({ content, expanded, streaming }: HtmlCardProps) {
  // 流式进行中（闭合 ``` 未到达）：显示占位，避免 iframe 随半成品 content 反复 reload 闪烁
  if (streaming) {
    return (
      <div className="my-2 flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-4 py-3 text-xs text-muted-foreground">
        <Loader2 className="h-3.5 w-3.5 animate-spin" strokeWidth={1.5} />
        {UI_CARD_STATUS.HTML_RENDERING}
      </div>
    )
  }

  return (
    <div className="my-2 overflow-hidden rounded-lg border border-border">
      <iframe
        sandbox=""
        srcDoc={content}
        className={expanded ? 'h-[min(72vh,760px)] w-full border-0' : 'h-64 w-full border-0'}
        title="HTML Preview"
      />
    </div>
  )
}
