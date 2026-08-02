import { ChevronRight, GitBranch } from 'lucide-react'
import { useCallback, useId, useMemo, useRef, useState } from 'react'

import { UI_ACTIONS, UI_LABELS, UI_MISC } from '@/lib/ui-text'

import type { GitGraphPanelProps } from './git-graph-types'
import { ROW_HEIGHT } from './git-graph-types'
import { GraphBranchLabels } from './GraphBranchLabels'
import { GraphRenderer } from './GraphRenderer'
import { GraphTooltip } from './GraphTooltip'
import { useCollapsible } from './useCollapsible'

/** Map branch names to X positions inside the lane area. */
function getLaneX(branches: string[]): Record<string, number> {
  const LANE_WIDTH = 220
  const step = (LANE_WIDTH - 12) / Math.max(branches.length, 1)
  const result: Record<string, number> = {}
  branches.forEach((b, i) => {
    result[b] = 10 + step * i
  })
  return result
}

// ─── SVG builder ─────────────────────────────────────────────────

function buildSvgContent(
  commits: readonly { hash: string; fullHash?: string; lane: string; parentHashes?: string[] }[],
  branchNames: readonly string[],
  laneX: Record<string, number>,
  branchColorMap: Record<string, string>,
  headIdx: number,
  total: number,
  svgH: number,
  headsByCommitHash: Record<string, string[]>,
): string {
  const parts: string[] = []

  branchNames.forEach((name) => {
    const x = laneX[name]
    parts.push(
      `<line x1="${x}" y1="0" x2="${x}" y2="${svgH}" stroke="${branchColorMap[name]}" stroke-width="1.5" opacity="0.12"/>`,
    )
  })

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
    const color = branchColorMap[commit.lane] ?? 'var(--primary)'
    const parents = commit.parentHashes ?? []
    if (parents.length === 0) continue

    for (const parentHash of parents) {
      const parentRow = hashToRow.get(parentHash)
      if (parentRow === undefined) continue
      const py = parentRow * ROW_HEIGHT + ROW_HEIGHT / 2
      const parentCi = total - 1 - parentRow
      const parentCommit = commits[parentCi]
      const parentX = laneX[parentCommit.lane] ?? laneX[branchNames[0]] ?? 32

      if (parentCommit.lane === commit.lane) {
        parts.push(
          `<line x1="${childX}" y1="${cy}" x2="${parentX}" y2="${py}" stroke="${color}" stroke-width="2" opacity="0.5"/>`,
        )
      } else {
        const midY = (cy + py) / 2
        parts.push(
          `<path d="M${childX},${cy} C${childX},${midY} ${parentX},${midY} ${parentX},${py}" fill="none" stroke="${color}" stroke-width="2" opacity="0.4"/>`,
        )
      }
    }
  }

  for (let r = 0; r < total; r++) {
    const ci = total - 1 - r
    const commit = commits[ci]
    const cx = laneX[commit.lane] ?? laneX[branchNames[0]] ?? 32
    const cy = r * ROW_HEIGHT + ROW_HEIGHT / 2
    const isHead = ci === headIdx
    const color = isHead
      ? 'var(--color-success)'
      : (branchColorMap[commit.lane] ?? 'var(--primary)')

    if (isHead) {
      parts.push(`<circle cx="${cx}" cy="${cy}" r="8" fill="var(--color-success)" opacity="0.2"/>`)
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
}

// ─── Main Component ──────────────────────────────────────────────

export function GitGraphPanel({
  data,
  currentBranch,
  onBranchChange,
  branchLabels,
}: GitGraphPanelProps) {
  const [open, toggle] = useCollapsible('git-graph', true)
  const [hoveredIdx, setHoveredIdx] = useState<number | null>(null)
  const tooltipRef = useRef<HTMLDivElement>(null)
  const graphBodyId = useId()

  const { commits, branches } = data
  const branchNames = useMemo(() => branches.map((b) => b.name), [branches])
  const laneX = useMemo(() => getLaneX(branchNames as readonly string[] as string[]), [branchNames])
  const branchColorMap = useMemo(
    () => Object.fromEntries(branches.map((b) => [b.name, b.color])),
    [branches],
  )

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

  const showTooltip = useCallback((e: React.MouseEvent, ci: number) => {
    setHoveredIdx(ci)
    const tip = tooltipRef.current
    if (!tip) return
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    tip.style.left = `${rect.left + 10}px`
    tip.style.top = `${rect.top - 68}px`
  }, [])
  const hideTooltip = useCallback(() => setHoveredIdx(null), [])

  const svgContent = useMemo(
    () =>
      buildSvgContent(
        commits,
        branchNames,
        laneX,
        branchColorMap,
        headIdx,
        total,
        svgH,
        headsByCommitHash,
      ),
    [commits, branchNames, laneX, branchColorMap, headIdx, total, svgH, headsByCommitHash],
  )

  return (
    <div className="border-b border-sidebar-border">
      {/* Header */}
      <button
        type="button"
        className="flex w-full items-center justify-between px-4 py-3 pb-2.5 text-left transition-colors hover:bg-hover/40 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ring"
        onClick={toggle}
        aria-expanded={open}
        aria-controls={graphBodyId}
        aria-label={`${open ? UI_ACTIONS.COLLAPSE : UI_ACTIONS.EXPAND}${UI_LABELS.GIT_GRAPH}`}
      >
        <span className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-text-secondary">
          <GitBranch className="h-3.5 w-3.5" strokeWidth={1.25} aria-hidden="true" />
          {UI_LABELS.GIT_GRAPH}
        </span>
        <ChevronRight
          className={`h-3.5 w-3.5 text-text-tertiary transition-transform ${open ? 'rotate-90' : ''}`}
          strokeWidth={1.25}
          aria-hidden="true"
        />
      </button>

      {/* Body */}
      <div
        id={graphBodyId}
        className={`overflow-hidden transition-[max-height] duration-200 ease-out ${
          open ? 'max-h-[600px]' : 'max-h-0'
        }`}
      >
        <div className="px-4 pb-3">
          <GraphBranchLabels
            branches={branches}
            branchNames={branchNames}
            currentBranch={currentBranch}
            branchLabels={branchLabels}
            onBranchChange={onBranchChange}
          />

          <GraphRenderer
            svgContent={svgContent}
            commits={commits}
            total={total}
            svgH={svgH}
            headIdx={headIdx}
            branchColorMap={branchColorMap}
            headsByCommitHash={headsByCommitHash}
            branchLabels={branchLabels}
            onHover={showTooltip}
            onHoverEnd={hideTooltip}
          />

          {/* Stats */}
          <div className="mt-1 flex gap-3 border-t border-border pt-2.5">
            <span className="text-[11px] text-text-tertiary">
              <strong className="font-semibold text-text-secondary">{total}</strong>{' '}
              {UI_MISC.COMMITS}
            </span>
            <span className="text-[11px] text-text-tertiary">
              <strong className="font-semibold text-text-secondary">{branchNames.length}</strong>{' '}
              {UI_MISC.BRANCHES}
            </span>
          </div>
        </div>
      </div>

      <GraphTooltip hoveredIdx={hoveredIdx} commits={commits} tooltipRef={tooltipRef} />
    </div>
  )
}
