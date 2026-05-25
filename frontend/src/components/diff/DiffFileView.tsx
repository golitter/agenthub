import { Diff, Hunk } from 'react-diff-view'

import type { ParsedDiffFile } from '@/lib/diff-parser'

interface DiffFileViewProps {
  file: ParsedDiffFile
}

export function DiffFileView({ file }: DiffFileViewProps) {
  return (
    <Diff viewType="unified" diffType={file.type} hunks={file.hunks}>
      {(hunks) => hunks.map((hunk) => <Hunk key={hunk.content} hunk={hunk} />)}
    </Diff>
  )
}
