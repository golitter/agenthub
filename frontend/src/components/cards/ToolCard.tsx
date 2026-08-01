import { UI_CARD_STATUS } from '@/lib/ui-text'

interface ToolCardProps {
  name?: string
  input?: string
  output?: string
}

export function ToolCard({ name, input, output }: ToolCardProps) {
  return (
    <div
      className="my-1 min-w-0 rounded-lg border border-border bg-bg-card px-3 py-2 text-[13px]"
      role="group"
      aria-label={name ? `${UI_CARD_STATUS.TOOL_CALL}: ${name}` : UI_CARD_STATUS.TOOL_CALL}
    >
      {name && (
        <div className="mb-1 text-xs font-medium text-muted-foreground">
          {UI_CARD_STATUS.TOOL_CALL} · {name}
        </div>
      )}
      {input && (
        <div className="space-y-1">
          <div className="text-[10px] font-medium text-tertiary">{UI_CARD_STATUS.TOOL_INPUT}</div>
          <code className="block break-all rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
            {input}
          </code>
        </div>
      )}
      {output && (
        <div className="mt-1 space-y-1">
          <div className="text-[10px] font-medium text-tertiary">{UI_CARD_STATUS.TOOL_OUTPUT}</div>
          <div className="break-words text-xs text-agent-opencode">{output}</div>
        </div>
      )}
    </div>
  )
}
