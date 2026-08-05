import type { ChangeData, DiffType, FileData, HunkData } from 'react-diff-view'
import { parseDiff } from 'react-diff-view'

import { getFileName } from '@/lib/utils'

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
  // 对于没有 hunks 的文件（新增/删除/重命名/二进制），返回空字符串。
  // 调用方在提供编辑/保存前必须以 `hunks.length > 0` 进行判断，因为
  // 保存空的重构内容会把目标文件清空。
  // 注意：CRLF 行结尾在此处会被规范化为 LF；若需完整保真，需要后端
  // 提供的原始内容（单独跟踪）。
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
      oldPath: oldPath || getFileName(newPath),
      newPath: newPath || getFileName(oldPath),
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
