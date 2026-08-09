import type { ConfigFile, Draft } from "./schema"

export function changedFileCount(draft: Draft): number {
  return Object.keys(draft).length
}

export function contentForFile(draft: Draft, file: ConfigFile): string {
  return draft[file.id]?.content ?? file.actualContent
}

export function updateFileDraft(draft: Draft, file: ConfigFile, content: string): Draft {
  if (content === file.actualContent) {
    const next = { ...draft }
    delete next[file.id]
    return next
  }
  return { ...draft, [file.id]: { revision: file.revision, content } }
}
