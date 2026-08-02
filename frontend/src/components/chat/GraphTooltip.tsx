interface GraphTooltipProps {
  hoveredIdx: number | null
  commits: readonly { hash: string; msg: string; author: string; lane: string; time: string }[]
  tooltipRef: React.RefObject<HTMLDivElement | null>
}

export function GraphTooltip({ hoveredIdx, commits, tooltipRef }: GraphTooltipProps) {
  return (
    <div
      ref={tooltipRef}
      className={`pointer-events-none fixed z-[100] rounded-lg border border-sidebar-border bg-popover px-3 py-2 transition-opacity ${
        hoveredIdx !== null ? 'opacity-100' : 'opacity-0'
      }`}
    >
      {hoveredIdx !== null && commits[hoveredIdx] && (
        <>
          <div className="font-mono text-[11px] text-primary">{commits[hoveredIdx].hash}</div>
          <div className="mt-0.5 text-xs text-text-primary">{commits[hoveredIdx].msg}</div>
          <div className="mt-0.5 text-[10px] text-text-tertiary">
            {commits[hoveredIdx].author} · {commits[hoveredIdx].lane} · {commits[hoveredIdx].time}
          </div>
        </>
      )}
    </div>
  )
}
