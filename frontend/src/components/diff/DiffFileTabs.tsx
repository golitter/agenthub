import { clsx } from 'clsx'

import type { DiffType, ParsedDiffFile } from '@/lib/diff-parser'
import { cn } from '@/lib/utils'

type ChangeTypeLabel = {
  letter: string
  className: string
}

const CHANGE_TYPE_MAP: Record<DiffType, ChangeTypeLabel> = {
  add: { letter: 'A', className: 'bg-success/15 text-success' },
  delete: { letter: 'D', className: 'bg-destructive/15 text-destructive' },
  modify: { letter: 'M', className: 'bg-primary/15 text-primary' },
  rename: { letter: 'R', className: 'bg-orchestrator/15 text-agent-orchestrator' },
  copy: { letter: 'C', className: 'bg-muted text-muted-foreground' },
}

export function ChangeTypeBadge({ type }: { type: DiffType }) {
  const config = CHANGE_TYPE_MAP[type] ?? CHANGE_TYPE_MAP.modify
  return (
    <span
      className={cn(
        'shrink-0 rounded px-1 text-[11px] font-semibold leading-none',
        config.className,
      )}
    >
      {config.letter}
    </span>
  )
}

interface DiffFileTabsProps {
  files: ParsedDiffFile[]
  activeIndex: number
  onSelect: (index: number) => void
}

export function DiffFileTabs({ files, activeIndex, onSelect }: DiffFileTabsProps) {
  if (files.length <= 1) return null

  return (
    <div className="flex overflow-x-auto border-b border-border bg-muted/30">
      {files.map((file, i) => (
        <button
          type="button"
          key={`${file.oldPath}->${file.newPath}-${i}`}
          onClick={() => onSelect(i)}
          title={file.newPath}
          className={clsx(
            'flex shrink-0 items-center gap-1.5 border-b-2 px-3 py-1.5 text-xs transition-[transform,opacity] focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ring',
            activeIndex === i
              ? 'border-primary text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground',
          )}
        >
          <ChangeTypeBadge type={file.type} />
          <span className="truncate max-w-40">{file.newPath}</span>
          <span className="shrink-0 text-[11px]">
            {file.additions > 0 && <span className="text-success">+{file.additions}</span>}
            {file.additions > 0 && file.deletions > 0 && ' '}
            {file.deletions > 0 && <span className="text-destructive">-{file.deletions}</span>}
          </span>
        </button>
      ))}
    </div>
  )
}
