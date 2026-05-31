import type { MessageBlock, PlanTask } from './block-types'

const BLOCK_MARKER = 'aka_yhy'

const BLOCK_RE = new RegExp('```' + BLOCK_MARKER + '\\n([\\s\\S]*?)```', 'g')

let _blockIdCounter = 0
function nextBlockId(): string {
  return `blk-${++_blockIdCounter}`
}

export function reduceEventToBlocks(fullText: string): MessageBlock[] {
  const blocks: MessageBlock[] = []
  let lastIndex = 0

  for (const match of fullText.matchAll(BLOCK_RE)) {
    const matchStart = match.index!
    if (matchStart > lastIndex) {
      const text = fullText.slice(lastIndex, matchStart)
      appendTextSegment(blocks, text)
    }

    const inner = match[1].trim()
    const parsed = parseBlockContent(inner)
    if (parsed) {
      blocks.push(parsed)
    } else {
      blocks.push({ type: 'text', id: nextBlockId(), content: match[0] })
    }

    lastIndex = matchStart + match[0].length
  }

  if (lastIndex < fullText.length) {
    appendTextSegment(blocks, fullText.slice(lastIndex))
  }

  if (blocks.length === 0) {
    return [{ type: 'text', id: nextBlockId(), content: fullText }]
  }

  return blocks
}

export function coalesceMessageBlocks(blocks: MessageBlock[]): MessageBlock[] {
  const merged: MessageBlock[] = []
  for (const block of blocks) {
    pushRuntimeBlock(merged, { ...block })
  }
  return merged
}

function appendTextSegment(blocks: MessageBlock[], text: string) {
  if (!text) return

  const lines = text.split('\n')
  const textLines: string[] = []
  let parsedAny = false

  const flushText = () => {
    if (textLines.length === 0) return
    const content = textLines.join('\n')
    if (content.trim()) {
      blocks.push({ type: 'text', id: nextBlockId(), content })
    }
    textLines.length = 0
  }

  for (let i = 0; i < lines.length; i += 1) {
    const fence = parseLegacyFence(lines, i)
    if (fence) {
      const legacyBlocks = parseLegacyBlocks(fence.innerLines)
      if (legacyBlocks) {
        flushText()
        for (const block of legacyBlocks) {
          pushRuntimeBlock(blocks, block)
        }
        parsedAny = true
        i = fence.endIndex
        continue
      }
    }

    const type = parseLegacyTypeLine(lines[i])
    const json = i + 1 < lines.length ? parseLegacyJsonLine(lines[i + 1]) : null

    if (type && json) {
      const block = legacyRuntimeBlock(type, json)
      if (block) {
        flushText()
        pushRuntimeBlock(blocks, block)
        parsedAny = true
        i += 1
        continue
      }
    }

    textLines.push(lines[i])
  }

  flushText()

  if (!parsedAny && blocks.length === 0 && text === '') {
    blocks.push({ type: 'text', id: nextBlockId(), content: text })
  }
}

function parseLegacyFence(
  lines: string[],
  startIndex: number,
): { innerLines: string[]; endIndex: number } | null {
  if (!lines[startIndex].trim().startsWith('```')) return null

  for (let i = startIndex + 1; i < lines.length; i += 1) {
    if (lines[i].trim() === '```') {
      return {
        innerLines: lines.slice(startIndex + 1, i),
        endIndex: i,
      }
    }
  }

  return null
}

function parseLegacyBlocks(lines: string[]): MessageBlock[] | null {
  const blocks: MessageBlock[] = []
  let parsedAny = false

  for (let i = 0; i < lines.length; i += 1) {
    if (!lines[i].trim()) continue

    const type = parseLegacyTypeLine(lines[i])
    const json = i + 1 < lines.length ? parseLegacyJsonLine(lines[i + 1]) : null
    if (!type || !json) return null

    const block = legacyRuntimeBlock(type, json)
    if (!block) return null

    pushRuntimeBlock(blocks, block)
    parsedAny = true
    i += 1
  }

  return parsedAny ? blocks : null
}

function parseLegacyTypeLine(line: string): string | null {
  const trimmed = line.trim()
  return trimmed.startsWith('type:') ? trimmed.slice('type:'.length).trim() : null
}

function parseLegacyJsonLine(line: string): Record<string, unknown> | null {
  const trimmed = line.trim()
  if (!trimmed.startsWith('json:')) return null
  try {
    const parsed = JSON.parse(trimmed.slice('json:'.length).trim())
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : null
  } catch {
    return null
  }
}

function legacyRuntimeBlock(type: string, payload: Record<string, unknown>): MessageBlock | null {
  switch (type) {
    case 'plan':
      return {
        type: 'plan',
        id: nextBlockId(),
        overview: stringField(payload, 'overview'),
        tasks: arrayField(payload, 'tasks')
          .map(planTask)
          .filter((task): task is PlanTask => task !== null),
      }
    case 'runtime_status':
      return {
        type: 'runtime_status',
        id: nextBlockId(),
        task_id: stringField(payload, 'task_id'),
        agent: stringField(payload, 'agent'),
        status: stringField(payload, 'status') || 'running',
        title: optionalStringField(payload, 'title'),
        streamingText: optionalStringField(payload, 'streamingText'),
      }
    case 'coordination':
      return {
        type: 'coordination',
        id: nextBlockId(),
        messages: arrayField(payload, 'messages').map((msg) => ({
          from: stringField(msg, 'from'),
          to: stringField(msg, 'to'),
          text: stringField(msg, 'text'),
          round: numberField(msg, 'round', 1),
        })),
        closed: Boolean(payload.closed),
        summary: optionalStringField(payload, 'summary'),
      }
    case 'ask_agent': {
      const status = askStatusField(payload)
      return {
        type: 'ask_agent',
        id: nextBlockId(),
        question_id: stringField(payload, 'question_id'),
        source_agent: optionalStringField(payload, 'source_agent'),
        source_agent_type: optionalStringField(payload, 'source_agent_type'),
        source_session_id: optionalStringField(payload, 'source_session_id'),
        target_agent: stringField(payload, 'target_agent'),
        target_agent_type: optionalStringField(payload, 'target_agent_type'),
        target_session_id: stringField(payload, 'target_session_id'),
        question: stringField(payload, 'question'),
        status,
        collapsed: booleanField(payload, 'collapsed', status === 'answered'),
        summary: optionalStringField(payload, 'summary'),
      }
    }
    default:
      return null
  }
}

function pushRuntimeBlock(blocks: MessageBlock[], block: MessageBlock) {
  if (block.type === 'plan') {
    const existing = blocks.find((item) => item.type === 'plan')
    if (existing?.type === 'plan') {
      const taskMap = new Map(existing.tasks.map((task) => [task.task_id, task]))
      for (const task of block.tasks) {
        taskMap.set(task.task_id, task)
      }
      existing.tasks = [...taskMap.values()]
      if (!existing.overview && block.overview) existing.overview = block.overview
      return
    }
  }

  if (block.type === 'runtime_status') {
    for (let i = blocks.length - 1; i >= 0; i -= 1) {
      const existing = blocks[i]
      if (existing.type === 'runtime_status' && existing.task_id === block.task_id) {
        existing.agent = block.agent || existing.agent
        existing.status = block.status || existing.status
        existing.title = block.title || existing.title
        existing.streamingText = `${existing.streamingText ?? ''}${block.streamingText ?? ''}`
        return
      }
    }
  }

  if (block.type === 'coordination') {
    const existing = blocks.find((item) => item.type === 'coordination')
    if (existing?.type === 'coordination') {
      existing.messages = [...existing.messages, ...block.messages]
      existing.closed = existing.closed || block.closed
      existing.summary = block.summary || existing.summary
      return
    }
  }

  if (block.type === 'ask_agent') {
    const existing = blocks.find(
      (item) => item.type === 'ask_agent' && item.question_id === block.question_id,
    )
    if (existing?.type === 'ask_agent') {
      existing.source_agent = block.source_agent || existing.source_agent
      existing.source_agent_type = block.source_agent_type || existing.source_agent_type
      existing.source_session_id = block.source_session_id || existing.source_session_id
      existing.target_agent = block.target_agent || existing.target_agent
      existing.target_agent_type = block.target_agent_type || existing.target_agent_type
      existing.target_session_id = block.target_session_id || existing.target_session_id
      existing.question = block.question || existing.question
      existing.status = block.status
      existing.collapsed = block.collapsed
      existing.summary = block.summary || existing.summary
      return
    }
  }

  blocks.push(block)
}

function planTask(value: Record<string, unknown>): PlanTask | null {
  const status = stringField(value, 'status')
  if (!['pending', 'running', 'completed', 'failed'].includes(status)) return null
  return {
    task_id: stringField(value, 'task_id'),
    agent: stringField(value, 'agent'),
    title: stringField(value, 'title'),
    status: status as PlanTask['status'],
  }
}

function stringField(value: Record<string, unknown>, key: string): string {
  const raw = value[key]
  return typeof raw === 'string' ? raw : ''
}

function optionalStringField(value: Record<string, unknown>, key: string): string | undefined {
  const raw = stringField(value, key)
  return raw || undefined
}

function askStatusField(value: Record<string, unknown>): 'pending' | 'answered' | 'failed' {
  const raw = stringField(value, 'status')
  if (raw === 'completed' || raw === 'answered') return 'answered'
  if (raw === 'failed') return 'failed'
  return 'pending'
}

function booleanField(value: Record<string, unknown>, key: string, fallback: boolean): boolean {
  const raw = value[key]
  return typeof raw === 'boolean' ? raw : fallback
}

function numberField(value: Record<string, unknown>, key: string, fallback: number): number {
  const raw = value[key]
  return typeof raw === 'number' ? raw : fallback
}

function arrayField(value: Record<string, unknown>, key: string): Record<string, unknown>[] {
  const raw = value[key]
  return Array.isArray(raw)
    ? raw.filter(
        (item): item is Record<string, unknown> =>
          item !== null && typeof item === 'object' && !Array.isArray(item),
      )
    : []
}

function parseBlockContent(inner: string): MessageBlock | null {
  const lines = inner.split('\n')
  const typeLine = lines.find((l) => l.startsWith('type:'))
  if (!typeLine) return null

  const blockType = typeLine.slice('type:'.length).trim()

  switch (blockType) {
    case 'html-render': {
      const content = lines.slice(1).join('\n').trim()
      return { type: 'html-render', id: nextBlockId(), content }
    }
    case 'image': {
      const path = extractField(lines, 'path')
      return path ? { type: 'image', id: nextBlockId(), path } : null
    }
    case 'attachment': {
      const path = extractField(lines, 'path')
      return path ? { type: 'attachment', id: nextBlockId(), path } : null
    }
    case 'diff': {
      const snapshotId = extractField(lines, 'snapshotId')
      return snapshotId ? { type: 'diff', id: nextBlockId(), snapshotId } : null
    }
    case 'preview': {
      const url = extractField(lines, 'url')
      return url ? { type: 'preview', id: nextBlockId(), url } : null
    }
    default:
      return null
  }
}

function extractField(lines: string[], field: string): string | null {
  const line = lines.find((l) => l.startsWith(`${field}:`))
  return line ? line.slice(field.length + 1).trim() : null
}
