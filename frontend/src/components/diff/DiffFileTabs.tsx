import { clsx } from 'clsx'

import type { DiffType, ParsedDiffFile } from '@/lib/diff-parser'
import { cn } from '@/lib/utils'

type ChangeTypeLabel = {
  letter: string
  className: string
}

const CHANGE_TYPE_MAP: Record<DiffType, ChangeTypeLabel> = {
  add: { letter: 'A', className: 'bg-green-500/15 text-green-600' },
  delete: { letter: 'D', className: 'bg-red-500/15 text-red-600' },
  modify: { letter: 'M', className: 'bg-blue-500/15 text-blue-600' },
  rename: { letter: 'R', className: 'bg-purple-500/15 text-purple-600' },
  copy: { letter: 'C', className: 'bg-gray-500/15 text-gray-600' },
}

export function ChangeTypeBadge({ type }: { type: DiffType }) {
  const config = CHANGE_TYPE_MAP[type] ?? CHANGE_TYPE_MAP.modify
  return (
    <span
      className={cn(
        'shrink-0 rounded px-1 text-[10px] font-semibold leading-none',
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
          key={file.newPath}
          onClick={() => onSelect(i)}
          title={file.newPath}
          className={clsx(
            'flex shrink-0 items-center gap-1.5 border-b-2 px-3 py-1.5 text-xs transition-colors',
            activeIndex === i
              ? 'border-primary text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground',
          )}
        >
          <ChangeTypeBadge type={file.type} />
          <span className="truncate max-w-40">{file.newPath}</span>
          <span className="shrink-0 text-[11px]">
            {file.additions > 0 && <span className="text-green-500">+{file.additions}</span>}
            {file.additions > 0 && file.deletions > 0 && ' '}
            {file.deletions > 0 && <span className="text-red-500">-{file.deletions}</span>}
          </span>
        </button>
      ))}
    </div>
  )
}
