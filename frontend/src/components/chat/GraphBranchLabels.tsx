import { UI_MISC } from '@/lib/ui-text'

import type { GitBranchInfo } from './git-graph-types'

interface GraphBranchLabelsProps {
  branches: readonly GitBranchInfo[]
  branchNames: readonly string[]
  currentBranch: string
  branchLabels: Record<string, string>
  onBranchChange: (name: string) => void
}

export function GraphBranchLabels({
  branches,
  branchNames,
  currentBranch,
  branchLabels,
  onBranchChange,
}: GraphBranchLabelsProps) {
  return (
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
            title={exists ? name : `${name}${UI_MISC.GIT_REF_NOT_EXIST}`}
            aria-pressed={isCurrent}
            onClick={(e) => {
              e.stopPropagation()
              if (!isCurrent) onBranchChange(name)
            }}
          >
            <span
              className={`inline-block h-1.5 w-1.5 rounded-full ${
                isCurrent ? 'bg-primary' : 'bg-bg-hover'
              }`}
            />
            {branchLabels[name] ?? name}
          </button>
        )
      })}
    </div>
  )
}
