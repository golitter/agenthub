import { Diff, Hunk } from 'react-diff-view'

import type { ParsedDiffFile } from '@/lib/diff-parser'

interface DiffFileViewProps {
  file: ParsedDiffFile
  viewType?: 'split' | 'unified'
}

export function DiffFileView({ file, viewType = 'split' }: DiffFileViewProps) {
  if (file.hunks.length === 0) {
    return (
      <div className="px-3 py-4 text-center text-xs text-muted-foreground">
        该文件无 hunk（新增 / 删除 / 重命名 / 二进制），无可展示的行级差异。
      </div>
    )
  }
  return (
    <Diff viewType={viewType} diffType={file.type} hunks={file.hunks}>
      {(hunks) => hunks.map((hunk) => <Hunk key={hunk.content} hunk={hunk} />)}
    </Diff>
  )
}
