import type { ParsedDiffFile } from '@/lib/diff-parser'

import { ChangeTypeBadge } from './DiffFileTabs'

interface DiffFileInfoProps {
  file: ParsedDiffFile
}

export function DiffFileInfo({ file }: DiffFileInfoProps) {
  return (
    <div className="flex items-center gap-2 border-b border-border bg-muted/20 px-3 py-1 text-xs text-muted-foreground">
      <span className="truncate font-mono text-[11px]">{file.newPath}</span>
      <ChangeTypeBadge type={file.type} />
      <span className="ml-auto shrink-0 text-[11px]">
        {file.additions > 0 && <span className="text-green-500">+{file.additions}</span>}
        {file.additions > 0 && file.deletions > 0 && ' '}
        {file.deletions > 0 && <span className="text-red-500">-{file.deletions}</span>}
      </span>
    </div>
  )
}
