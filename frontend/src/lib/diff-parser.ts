import type { ChangeData, DiffType, FileData, HunkData } from 'react-diff-view'
import { parseDiff } from 'react-diff-view'

export type { DiffType }
export type { FileData as ParsedFileData }
export type { HunkData }
export type { ChangeData }

export interface ParsedDiffFile {
  oldPath: string
  newPath: string
  type: DiffType
  hunks: HunkData[]
  oldContent: string
  newContent: string
  additions: number
  deletions: number
}

export interface ParsedDiffResult {
  files: ParsedDiffFile[]
  summary: { additions: number; deletions: number; filesChanged: number }
}

function extractFileName(path: string): string {
  return path.split('/').pop() ?? path
}

function countChanges(hunks: HunkData[]): { additions: number; deletions: number } {
  let additions = 0
  let deletions = 0
  for (const hunk of hunks) {
    for (const change of hunk.changes) {
      if (change.type === 'insert') additions++
      else if (change.type === 'delete') deletions++
    }
  }
  return { additions, deletions }
}

function reconstructContent(hunks: HunkData[], side: 'old' | 'new'): string {
  const lines: string[] = []
  for (const hunk of hunks) {
    for (const change of hunk.changes) {
      if (side === 'old') {
        if (change.type === 'normal' || change.type === 'delete') {
          lines.push(change.content)
        }
      } else {
        if (change.type === 'normal' || change.type === 'insert') {
          lines.push(change.content)
        }
      }
    }
  }
  return lines.join('\n')
}

export function parseUnifiedDiff(diffText: string): ParsedDiffResult {
  if (!diffText?.trim())
    return { files: [], summary: { additions: 0, deletions: 0, filesChanged: 0 } }

  const files: ParsedDiffFile[] = []
  const parsed = parseDiff(diffText, { nearbySequences: 'zip' })

  for (const file of parsed as FileData[]) {
    const oldPath = file.oldPath?.replace(/^a\//, '') ?? ''
    const newPath = file.newPath?.replace(/^b\//, '') ?? ''
    const { additions, deletions } = countChanges(file.hunks)

    files.push({
      oldPath: oldPath || extractFileName(newPath),
      newPath: newPath || extractFileName(oldPath),
      type: file.type as DiffType,
      hunks: file.hunks,
      oldContent: reconstructContent(file.hunks, 'old'),
      newContent: reconstructContent(file.hunks, 'new'),
      additions,
      deletions,
    })
  }

  const summary = files.reduce(
    (acc, f) => ({
      additions: acc.additions + f.additions,
      deletions: acc.deletions + f.deletions,
      filesChanged: acc.filesChanged + 1,
    }),
    { additions: 0, deletions: 0, filesChanged: 0 },
  )

  return { files, summary }
}

export function getFileName(path: string): string {
  return path.split('/').pop() ?? path
}
