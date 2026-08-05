import { GIT_AUTHOR_COLORS, LANE_WIDTH } from './git-graph-types'

interface GraphRendererProps {
  svgContent: string
  commits: readonly {
    hash: string
    msg: string
    author: string
    lane: string
    time: string
    fullHash?: string
    parentHashes?: string[]
  }[]
  total: number
  svgH: number
  headIdx: number
  branchColorMap: Record<string, string>
  headsByCommitHash: Record<string, string[]>
  branchLabels: Record<string, string>
  onHover: (e: React.MouseEvent, ci: number) => void
  onHoverEnd: () => void
}

export function GraphRenderer({
  svgContent,
  commits,
  total,
  svgH,
  headIdx,
  branchColorMap,
  headsByCommitHash,
  branchLabels,
  onHover,
  onHoverEnd,
}: GraphRendererProps) {
  return (
    <div className="relative overflow-x-auto overflow-y-auto" style={{ maxHeight: 320 }}>
      <div className="relative">
        <svg
          width={LANE_WIDTH}
          height={svgH}
          className="pointer-events-none absolute left-0 top-0"
          dangerouslySetInnerHTML={{ __html: svgContent }}
        />

        {/* 提交记录行 */}
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
              onMouseEnter={(e) => onHover(e, ci)}
              onMouseLeave={onHoverEnd}
            >
              <div className="shrink-0" style={{ width: LANE_WIDTH }} />
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
  )
}
