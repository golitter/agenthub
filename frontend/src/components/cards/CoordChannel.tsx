import { useState } from 'react'

import type { CoordMessage } from '@/lib/block-types'

interface CoordChannelProps {
  messages: CoordMessage[]
  closed: boolean
  summary?: string
}

export function CoordChannel({ messages, closed, summary }: CoordChannelProps) {
  const [open, setOpen] = useState(true)
  const rounds = [...new Set(messages.map((m) => m.round))]

  return (
    <div className="my-1 overflow-hidden rounded-[10px] border border-agent-orchestrator/15 bg-agent-orchestrator/3">
      <button
        type="button"
        className="flex w-full items-center justify-between px-3.5 py-2.5 text-left hover:bg-agent-orchestrator/5"
        onClick={() => setOpen(!open)}
      >
        <span className="text-[13px] font-medium text-agent-orchestrator">🔗 协调通道</span>
        <span className="text-[11px] text-muted-foreground">
          {open ? '▼ 点击收起' : '▶ 点击展开'}
        </span>
      </button>

      {open && (
        <div className="border-t border-agent-orchestrator/10 px-3.5 py-2">
          {rounds.map((round) => (
            <div key={round} className="mb-2.5">
              <div className="mb-1.5 border-b border-dashed border-agent-orchestrator/15 pb-1 text-[11px] text-muted-foreground">
                第 {round} 轮协调
              </div>
              {messages
                .filter((m) => m.round === round)
                .map((msg, i) => (
                  <div
                    key={i}
                    className="mb-1.5 min-w-0 break-words rounded-lg border border-border bg-card px-3 py-2 text-[13px] leading-relaxed"
                  >
                    <span className="font-semibold text-agent-orchestrator">{msg.from}</span>
                    <span className="mx-1 text-muted-foreground">→</span>
                    <span className="font-semibold text-agent-orchestrator">{msg.to}</span>
                    <br />
                    {msg.text}
                  </div>
                ))}
            </div>
          ))}

          {closed && summary && (
            <div className="mt-2 rounded-lg border border-agent-orchestrator/15 bg-agent-orchestrator/5 p-2.5 text-[12px] text-muted-foreground">
              <div className="mb-1 text-[11px] font-semibold text-agent-orchestrator">协调结论</div>
              {summary}
            </div>
          )}

          <div className="mt-1 text-[11px] text-muted-foreground">
            {rounds.length} 轮协调，{messages.length} 条消息
          </div>
        </div>
      )}
    </div>
  )
}
