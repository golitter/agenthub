import { Diff, Hunk } from 'react-diff-view'

import type { ParsedDiffFile } from '@/lib/diff-parser'

interface DiffFileViewProps {
  file: ParsedDiffFile
  viewType?: 'split' | 'unified'
}

export function DiffFileView({ file, viewType = 'split' }: DiffFileViewProps) {
  return (
    <Diff viewType={viewType} diffType={file.type} hunks={file.hunks}>
      {(hunks) => hunks.map((hunk) => <Hunk key={hunk.content} hunk={hunk} />)}
    </Diff>
  )
}
