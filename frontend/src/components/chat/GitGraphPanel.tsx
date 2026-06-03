import { ChevronRight, GitBranch } from 'lucide-react'
import { useCallback, useMemo, useRef, useState } from 'react'

import type { GitGraphPanelProps } from './git-graph-types'
import { GIT_AUTHOR_COLORS, LANE_WIDTH, ROW_HEIGHT } from './git-graph-types'
import { useCollapsible } from './RightSidebar'

// ─── Helpers ────────────────────────────────────────────────────

/** Map branch names to X positions inside the lane area. */
function getLaneX(branches: string[]): Record<string, number> {
  const step = (LANE_WIDTH - 12) / Math.max(branches.length, 1)
  const result: Record<string, number> = {}
  branches.forEach((b, i) => {
    result[b] = 10 + step * i
  })
  return result
}

// ─── Component ──────────────────────────────────────────────────

export function GitGraphPanel({
  data,
  currentBranch,
  onBranchChange,
  branchLabels,
}: GitGraphPanelProps) {
  const [open, toggle] = useCollapsible('git-graph', true)
  const [hoveredIdx, setHoveredIdx] = useState<number | null>(null)
  const tooltipRef = useRef<HTMLDivElement>(null)

  const { commits, branches } = data
  const branchNames = useMemo(() => branches.map((b) => b.name), [branches])
  const laneX = useMemo(() => getLaneX(branchNames), [branchNames])
  const branchColorMap = useMemo(
    () => Object.fromEntries(branches.map((b) => [b.name, b.color])),
    [branches],
  )
  // Map branch name → index of its head commit in the commits array
  const branchHeadIdxMap = useMemo(() => {
    const hashToIdx = new Map(commits.map((c, i) => [c.hash, i]))
    const result: Record<string, number> = {}
    for (const b of branches) {
      if (b.headHash) {
        const idx = hashToIdx.get(b.headHash)
        if (idx !== undefined) result[b.name] = idx
      }
    }
    return result
  }, [commits, branches])
  const headsByCommitHash = useMemo(() => {
    const result: Record<string, string[]> = {}
    for (const b of branches) {
      if (!b.headHash) continue
      result[b.headHash] = [...(result[b.headHash] ?? []), b.name]
    }
    return result
  }, [branches])

  const headIdx = branchHeadIdxMap[currentBranch] ?? -1
  const total = commits.length
  const svgH = total * ROW_HEIGHT

  // ── Tooltip positioning ──
  const showTooltip = useCallback((e: React.MouseEvent, ci: number) => {
    setHoveredIdx(ci)
    const tip = tooltipRef.current
    if (!tip) return
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    tip.style.left = `${rect.left + 10}px`
    tip.style.top = `${rect.top - 68}px`
  }, [])
  const hideTooltip = useCallback(() => setHoveredIdx(null), [])

  // ── SVG: rails + connections + nodes ──
  const svgContent = useMemo(() => {
    const parts: string[] = []

    // Lane rails (faint vertical lines)
    branchNames.forEach((name) => {
      const x = laneX[name]
      parts.push(
        `<line x1="${x}" y1="0" x2="${x}" y2="${svgH}" stroke="${branchColorMap[name]}" stroke-width="1.5" opacity="0.12"/>`,
      )
    })

    // Connections — draw lines from each commit to its actual parents
    // Build a hash→row index so we can find parent positions
    const hashToRow = new Map<string, number>()
    for (let r = 0; r < total; r++) {
      const ci = total - 1 - r
      const commit = commits[ci]
      if (commit.fullHash) hashToRow.set(commit.fullHash, r)
      hashToRow.set(commit.hash, r)
    }

    for (let r = 0; r < total; r++) {
      const ci = total - 1 - r
      const commit = commits[ci]
      const cy = r * ROW_HEIGHT + ROW_HEIGHT / 2
      const childX = laneX[commit.lane] ?? laneX[branchNames[0]] ?? 32
      const color = branchColorMap[commit.lane]

      const parents = commit.parentHashes ?? []
      if (parents.length === 0) continue // root commit, no parents

      for (const parentHash of parents) {
        const parentRow = hashToRow.get(parentHash)
        if (parentRow === undefined) continue // parent not in our visible range
        const py = parentRow * ROW_HEIGHT + ROW_HEIGHT / 2

        // Find parent's lane
        const parentCi = total - 1 - parentRow
        const parentCommit = commits[parentCi]
        const parentX = laneX[parentCommit.lane] ?? laneX[branchNames[0]] ?? 32

        if (parentCommit.lane === commit.lane) {
          // Same lane → straight vertical line
          parts.push(
            `<line x1="${childX}" y1="${cy}" x2="${parentX}" y2="${py}" stroke="${color}" stroke-width="2" opacity="0.5"/>`,
          )
        } else {
          // Cross-lane → bezier curve from child to parent
          const midY = (cy + py) / 2
          parts.push(
            `<path d="M${childX},${cy} C${childX},${midY} ${parentX},${midY} ${parentX},${py}" fill="none" stroke="${color}" stroke-width="2" opacity="0.4"/>`,
          )
        }
      }
    }

    // Nodes
    for (let r = 0; r < total; r++) {
      const ci = total - 1 - r
      const commit = commits[ci]
      const cx = laneX[commit.lane]
      const cy = r * ROW_HEIGHT + ROW_HEIGHT / 2
      const isHead = ci === headIdx
      const color = isHead ? 'var(--color-success)' : branchColorMap[commit.lane]

      if (isHead) {
        parts.push(
          `<circle cx="${cx}" cy="${cy}" r="8" fill="var(--color-success)" opacity="0.2"/>`,
        )
      }
      parts.push(`<circle cx="${cx}" cy="${cy}" r="4.5" fill="${color}"/>`)

      for (const branchName of headsByCommitHash[commit.hash] ?? []) {
        if (branchName === commit.lane) continue
        const headX = laneX[branchName]
        if (headX === undefined) continue
        const headColor = branchColorMap[branchName]
        parts.push(
          `<line x1="${cx}" y1="${cy}" x2="${headX}" y2="${cy}" stroke="${headColor}" stroke-width="1.5" opacity="0.55"/>`,
        )
        parts.push(
          `<circle cx="${headX}" cy="${cy}" r="5.5" fill="var(--bg-card)" stroke="${headColor}" stroke-width="2"/>`,
        )
      }
    }

    return parts.join('')
  }, [commits, branchNames, laneX, branchColorMap, headIdx, total, svgH, headsByCommitHash])

  return (
    <div className="border-b border-sidebar-border">
      {/* Header */}
      <button
        type="button"
        className="flex w-full items-center justify-between px-4 py-3 pb-2.5 text-left"
        onClick={toggle}
      >
        <span className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-text-text-secondary">
          <GitBranch className="h-3.5 w-3.5" strokeWidth={1.25} />
          Git Graph
        </span>
        <ChevronRight
          className={`h-3.5 w-3.5 text-text-tertiary transition-transform ${open ? 'rotate-90' : ''}`}
          strokeWidth={1.25}
        />
      </button>

      {/* Body */}
      <div
        className={`overflow-hidden transition-[max-height] duration-200 ease-out ${
          open ? 'max-h-[600px]' : 'max-h-0'
        }`}
      >
        <div className="px-4 pb-3">
          {/* Branch labels */}
          <div className="mb-2 flex items-center gap-2">
            {branchNames.map((name) => {
              const branch = branches.find((b) => b.name === name)
              const exists = branch?.exists ?? Boolean(branch?.headHash)
              const isCurrent = name === currentBranch
              return (
                <button
                  key={name}
                  type="button"
                  className={`flex cursor-pointer items-center gap-1 rounded-full border px-2 py-0.5 font-mono text-[11px] font-medium transition-[transform,opacity] hover:brightness-110 ${
                    isCurrent ? 'bg-primary-soft text-primary' : 'bg-accent text-text-secondary'
                  } ${exists ? 'border-transparent' : 'border-dashed border-warning/60 opacity-70'}`}
                  title={exists ? name : `${name}（git ref 不存在）`}
                  onClick={(e) => {
                    e.stopPropagation()
                    if (!isCurrent) onBranchChange(name)
                  }}
                >
                  <span
                    className={`inline-block h-1.5 w-1.5 rounded-full ${
                      isCurrent ? 'bg-primary' : 'bg-secondary'
                    }`}
                  />
                  {branchLabels[name] ?? name}
                </button>
              )
            })}
          </div>

          {/* Graph rows */}
          <div className="relative overflow-x-auto overflow-y-auto" style={{ maxHeight: 320 }}>
            <div className="relative">
              {/* SVG overlay for lines */}
              <svg
                width={LANE_WIDTH}
                height={svgH}
                className="pointer-events-none absolute left-0 top-0"
                dangerouslySetInnerHTML={{ __html: svgContent }}
              />

              {/* Commit rows */}
              {Array.from({ length: total }, (_, r) => {
                const ci = total - 1 - r
                const commit = commits[ci]
                const isHead = ci === headIdx
                const authorColor = GIT_AUTHOR_COLORS[commit.author] ?? 'var(--text-text-tertiary)'

                return (
                  <div
                    key={commit.hash}
                    className={`flex h-7 items-center transition-[background] ${
                      isHead ? 'bg-primary-soft/30' : 'hover:bg-bg-hover/50'
                    }`}
                    onMouseEnter={(e) => showTooltip(e, ci)}
                    onMouseLeave={hideTooltip}
                  >
                    {/* Lane spacer */}
                    <div className="shrink-0" style={{ width: LANE_WIDTH }} />
                    {/* Info */}
                    <div className="flex min-w-0 flex-1 items-center gap-1.5 pr-1">
                      <span className="w-[52px] shrink-0 font-mono text-[10px] text-text-tertiary">
                        {commit.hash}
                      </span>
                      <span className="min-w-0 flex-1 truncate text-[11px] text-text-primary">
                        {commit.msg}
                      </span>
                      {(headsByCommitHash[commit.hash] ?? []).map((branchName) => (
                        <span
                          key={branchName}
                          className="max-w-[72px] shrink-0 truncate rounded-full border border-border bg-accent px-1.5 py-0.5 font-mono text-[9px] leading-none text-text-secondary"
                          title={branchName}
                          style={{
                            borderColor: branchColorMap[branchName],
                            color: branchColorMap[branchName],
                          }}
                        >
                          {branchLabels[branchName] ?? branchName}
                        </span>
                      ))}
                      <span
                        className="h-[5px] w-[5px] shrink-0 rounded-full"
                        style={{ background: authorColor }}
                        title={commit.author}
                      />
                      <span className="shrink-0 whitespace-nowrap text-[10px] text-text-tertiary">
                        {commit.time}
                      </span>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>

          {/* Stats */}
          <div className="mt-1 flex gap-3 border-t border-border pt-2.5">
            <span className="text-[11px] text-text-tertiary">
              <strong className="font-semibold text-text-secondary">{total}</strong> commits
            </span>
            <span className="text-[11px] text-text-tertiary">
              <strong className="font-semibold text-text-secondary">{branchNames.length}</strong>{' '}
              branches
            </span>
          </div>
        </div>
      </div>

      {/* Tooltip (portal-like) */}
      <div
        ref={tooltipRef}
        className={`pointer-events-none fixed z-[100] rounded-lg border border-sidebar-border bg-popover px-3 py-2 transition-opacity ${
          hoveredIdx !== null ? 'opacity-100' : 'opacity-0'
        }`}
      >
        {hoveredIdx !== null &&
          (() => {
            const c = commits[hoveredIdx]
            return (
              <>
                <div className="font-mono text-[11px] text-primary">{c.hash}</div>
                <div className="mt-0.5 text-xs text-text-primary">{c.msg}</div>
                <div className="mt-0.5 text-[10px] text-text-tertiary">
                  {c.author} · {c.lane} · {c.time}
                </div>
              </>
            )
          })()}
      </div>
    </div>
  )
}
