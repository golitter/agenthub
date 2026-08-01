import { ArrowRight, ChevronDown, Link2 } from 'lucide-react'
import { useId, useState } from 'react'

import type { CoordMessage } from '@/lib/block-types'
import { UI_ACTIONS, UI_LABELS, UI_MISC } from '@/lib/ui-text'

interface CoordChannelProps {
  messages: CoordMessage[]
  closed: boolean
  summary?: string
}

export function CoordChannel({ messages, closed, summary }: CoordChannelProps) {
  const [open, setOpen] = useState(true)
  const channelBodyId = useId()
  const rounds = [...new Set(messages.map((m) => m.round))]

  return (
    <div className="my-1 overflow-hidden rounded-[10px] border border-agent-orchestrator/15 bg-agent-orchestrator/3">
      <button
        type="button"
        className="flex w-full items-center justify-between px-3.5 py-2.5 text-left transition-colors hover:bg-agent-orchestrator/5 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ring"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        aria-controls={channelBodyId}
      >
        <span className="flex items-center gap-1.5 text-[13px] font-medium text-agent-orchestrator">
          <Link2 className="h-3.5 w-3.5" strokeWidth={1.25} aria-hidden="true" />
          {UI_LABELS.COORD_CHANNEL}
        </span>
        <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
          {open ? (
            <>
              <ChevronDown className="h-3 w-3" strokeWidth={1.25} aria-hidden="true" />{' '}
              {UI_ACTIONS.COLLAPSE}
            </>
          ) : (
            <>
              <ChevronDown className="h-3 w-3 -rotate-90" strokeWidth={1.25} aria-hidden="true" />{' '}
              {UI_ACTIONS.EXPAND}
            </>
          )}
        </span>
      </button>

      {open && (
        <div id={channelBodyId} className="border-t border-agent-orchestrator/10 px-3.5 py-2">
          {rounds.map((round) => (
            <div key={round} className="mb-2.5">
              <div className="mb-1.5 border-b border-dashed border-agent-orchestrator/15 pb-1 text-[11px] text-muted-foreground">
                {`${UI_MISC.ROUND}${round}${UI_MISC.ROUND_SUFFIX}`}
              </div>
              {messages
                .filter((m) => m.round === round)
                .map((msg, i) => (
                  <div
                    key={i}
                    className="mb-1.5 min-w-0 break-words rounded-lg border border-border bg-card px-3 py-2 text-[13px] leading-relaxed"
                  >
                    <span className="font-semibold text-agent-orchestrator">{msg.from}</span>
                    <ArrowRight className="mx-1 h-3 w-3 text-muted-foreground" strokeWidth={1.25} />
                    <span className="font-semibold text-agent-orchestrator">{msg.to}</span>
                    <br />
                    {msg.text}
                  </div>
                ))}
            </div>
          ))}

          {closed && summary && (
            <div className="mt-2 rounded-lg border border-agent-orchestrator/15 bg-agent-orchestrator/5 p-2.5 text-[12px] text-muted-foreground">
              <div className="mb-1 text-[11px] font-semibold text-agent-orchestrator">
                {UI_LABELS.COORD_CONCLUSION}
              </div>
              {summary}
            </div>
          )}

          <div className="mt-1 text-[11px] text-muted-foreground">
            {`${rounds.length}${UI_MISC.SUFFIX_ROUND}，${messages.length}${UI_MISC.SUFFIX_MESSAGES}`}
          </div>
        </div>
      )}
    </div>
  )
}
