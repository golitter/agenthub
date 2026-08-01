import { Maximize2 } from 'lucide-react'
import type { ReactNode } from 'react'
import { useState } from 'react'

import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import type { AgentSessionInfo } from '@/lib/api'
import type { MessageBlock } from '@/lib/block-types'
import { UI_LABELS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

import { BlockRenderer } from './BlockRenderer'

const LONG_MESSAGE_PREVIEW_HEIGHT = 'h-[22rem]'

function canInteractWithPlanReview(
  blocks: MessageBlock[],
  index: number,
  interactive?: boolean,
): boolean {
  if (!interactive) return false
  if (blocks.some((block) => block.type === 'runtime_status')) return false
  return !blocks.slice(index + 1).some((block) => block.type !== 'text')
}

export function AgentMessageContent({
  blocks,
  children,
  taskId,
  sessionId,
  agentSessionLookup,
  isStreaming,
  isLong,
  interactive,
  agentLabel,
  agentColor,
}: {
  blocks?: MessageBlock[]
  children?: ReactNode
  taskId?: string
  sessionId?: string
  agentSessionLookup?: Map<string, AgentSessionInfo>
  isStreaming?: boolean
  isLong?: boolean
  interactive?: boolean
  agentLabel?: string
  agentColor?: string
}) {
  const [zoomed, setZoomed] = useState(false)
  const hasBlocks = blocks && blocks.length > 0
  const showAgentLabel = Boolean(agentLabel && !hasBlocks)

  const renderContent = (expandedPreview = false) => (
    <div className="min-w-0 max-w-full space-y-3">
      {hasBlocks
        ? blocks!.map((block, index) => (
            <BlockRenderer
              key={block.id}
              block={block}
              taskId={taskId}
              sessionId={sessionId}
              agentSessionLookup={agentSessionLookup}
              expandedPreview={expandedPreview}
              interactive={
                block.type === 'plan_review'
                  ? canInteractWithPlanReview(blocks!, index, interactive)
                  : interactive
              }
            />
          ))
        : children}
      {isStreaming && <span className="inline-block animate-pulse text-foreground">▌</span>}
    </div>
  )

  if (!isLong) {
    return (
      <>
        {showAgentLabel && (
          <div className="mb-2 flex min-w-0 items-center">
            <span
              className="max-w-full truncate rounded-full bg-accent px-2 py-0.5 text-[11px] font-medium"
              style={{ color: agentColor }}
            >
              @{agentLabel}
            </span>
          </div>
        )}
        {renderContent()}
      </>
    )
  }

  return (
    <>
      <div className="mb-2 flex min-w-0 items-center justify-between gap-2 border-b border-border/70 pb-2">
        {showAgentLabel ? (
          <span
            className="min-w-0 truncate rounded-full bg-accent px-2 py-0.5 text-[11px] font-medium"
            style={{ color: agentColor }}
          >
            @{agentLabel}
          </span>
        ) : (
          <span />
        )}
        <button
          type="button"
          className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-[background-color,color,transform] hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring active:translate-y-px"
          onClick={(event) => {
            event.stopPropagation()
            setZoomed(true)
          }}
          title={UI_LABELS.ZOOM_IN}
          aria-label={UI_LABELS.ZOOM_IN}
        >
          <Maximize2 className="h-4 w-4" strokeWidth={1.25} aria-hidden="true" />
        </button>
      </div>

      <div
        role="button"
        tabIndex={0}
        aria-label={UI_LABELS.CLICK_TO_ZOOM}
        className={cn(
          LONG_MESSAGE_PREVIEW_HEIGHT,
          'block w-full min-w-0 cursor-zoom-in overflow-y-auto overflow-x-hidden overscroll-contain rounded-md pr-2 text-left',
          'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
        )}
        onClick={() => setZoomed(true)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            setZoomed(true)
          }
        }}
        title={UI_LABELS.CLICK_TO_ZOOM}
      >
        {renderContent()}
      </div>

      <Dialog open={zoomed} onOpenChange={setZoomed}>
        <DialogContent className="flex h-[min(86vh,900px)] max-h-[86vh] max-w-[min(92vw,1200px)] flex-col gap-0 overflow-hidden border-border bg-card p-0">
          <DialogTitle className="sr-only">{UI_LABELS.MESSAGE_DETAIL}</DialogTitle>
          <DialogDescription className="sr-only">{UI_LABELS.MESSAGE_DETAIL_DESC}</DialogDescription>
          <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-6 py-5 pr-8">
            {renderContent(true)}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
