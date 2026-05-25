import { clsx } from 'clsx'

import type { ParsedDiffFile } from '@/lib/diff-parser'
import { getFileName } from '@/lib/diff-parser'

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
          key={i}
          onClick={() => onSelect(i)}
          title={file.newPath}
          className={clsx(
            'flex shrink-0 items-center gap-1.5 border-b-2 px-3 py-1.5 text-xs transition-colors',
            activeIndex === i
              ? 'border-primary text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground',
          )}
        >
          <span className="truncate max-w-32">{getFileName(file.newPath)}</span>
          <span className="shrink-0 text-[10px]">
            {file.additions > 0 && <span className="text-green-500">+{file.additions}</span>}
            {file.additions > 0 && file.deletions > 0 && ' '}
            {file.deletions > 0 && <span className="text-red-500">-{file.deletions}</span>}
          </span>
        </button>
      ))}
    </div>
  )
}
