import type { MessageBlock } from './block-types'

const BLOCK_MARKER = 'aka_yhy'

const BLOCK_RE = new RegExp('```' + BLOCK_MARKER + '\\n([\\s\\S]*?)```', 'g')

export function reduceEventToBlocks(fullText: string): MessageBlock[] {
  const blocks: MessageBlock[] = []
  let lastIndex = 0

  for (const match of fullText.matchAll(BLOCK_RE)) {
    const matchStart = match.index!
    // Text before this block
    if (matchStart > lastIndex) {
      const text = fullText.slice(lastIndex, matchStart)
      if (text) blocks.push({ type: 'text', content: text })
    }

    const inner = match[1].trim()
    const parsed = parseBlockContent(inner)
    if (parsed) {
      blocks.push(parsed)
    } else {
      // Unknown type → degrade to plain code block
      blocks.push({ type: 'text', content: match[0] })
    }

    lastIndex = matchStart + match[0].length
  }

  // Remaining text after last block
  if (lastIndex < fullText.length) {
    blocks.push({ type: 'text', content: fullText.slice(lastIndex) })
  }

  // No blocks found → single text block
  if (blocks.length === 0) {
    return [{ type: 'text', content: fullText }]
  }

  return blocks
}

function parseBlockContent(inner: string): MessageBlock | null {
  const lines = inner.split('\n')
  const typeLine = lines.find((l) => l.startsWith('type:'))
  if (!typeLine) return null

  const blockType = typeLine.slice('type:'.length).trim()

  switch (blockType) {
    case 'html-render': {
      // Everything after the type line is the HTML content
      const content = lines.slice(1).join('\n').trim()
      return { type: 'html-render', content }
    }
    case 'image': {
      const path = extractField(lines, 'path')
      return path ? { type: 'image', path } : null
    }
    case 'attachment': {
      const path = extractField(lines, 'path')
      return path ? { type: 'attachment', path } : null
    }
    case 'diff':
      return { type: 'diff' }
    case 'preview': {
      const url = extractField(lines, 'url')
      return url ? { type: 'preview', url } : null
    }
    default:
      return null
  }
}

function extractField(lines: string[], field: string): string | null {
  const line = lines.find((l) => l.startsWith(`${field}:`))
  return line ? line.slice(field.length + 1).trim() : null
}
