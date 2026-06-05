import type { MessageBlock, PlanTask } from './block-types'

const BLOCK_MARKER = 'aka_yhy'

let _blockIdCounter = 0
function nextBlockId(): string {
  return `blk-${++_blockIdCounter}`
}

export function reduceEventToBlocks(fullText: string): MessageBlock[] {
  const blocks: MessageBlock[] = []
  let lastIndex = 0

  for (const match of findAkaBlocks(fullText)) {
    const matchStart = match.start
    if (matchStart > lastIndex) {
      const text = fullText.slice(lastIndex, matchStart)
      appendTextSegment(blocks, text)
    }

    const inner = match.inner.trim()
    const parsed = parseBlockContent(inner)
    if (parsed) {
      blocks.push(parsed)
    } else {
      blocks.push({ type: 'text', id: nextBlockId(), content: match.raw })
    }

    lastIndex = match.end
  }

  if (lastIndex < fullText.length) {
    appendTextSegment(blocks, fullText.slice(lastIndex))
  }

  if (blocks.length === 0) {
    return [{ type: 'text', id: nextBlockId(), content: fullText }]
  }

  return trimRedundantTailBlocks(blocks)
}

function findAkaBlocks(
  text: string,
): Array<{ start: number; end: number; inner: string; raw: string }> {
  const blocks: Array<{ start: number; end: number; inner: string; raw: string }> = []
  const openFence = '```' + BLOCK_MARKER
  let searchFrom = 0

  while (searchFrom < text.length) {
    const start = text.indexOf(openFence, searchFrom)
    if (start < 0) break

    const openLineEnd = text.indexOf('\n', start)
    if (openLineEnd < 0) break

    let cursor = openLineEnd + 1
    let closeStart = -1
    let closeEnd = -1

    while (cursor <= text.length) {
      const lineEnd = text.indexOf('\n', cursor)
      const end = lineEnd < 0 ? text.length : lineEnd
      const line = text.slice(cursor, end).trim()
      if (line === '```') {
        closeStart = cursor
        closeEnd = end
        break
      }
      if (lineEnd < 0) break
      cursor = lineEnd + 1
    }

    if (closeStart < 0) {
      searchFrom = openLineEnd + 1
      continue
    }

    const end = closeEnd
    blocks.push({
      start,
      end,
      inner: text.slice(openLineEnd + 1, closeStart),
      raw: text.slice(start, end),
    })
    searchFrom = end
  }

  return blocks
}

export function coalesceMessageBlocks(blocks: MessageBlock[]): MessageBlock[] {
  const merged: MessageBlock[] = []
  for (const block of blocks) {
    pushRuntimeBlock(merged, { ...block })
  }
  return trimRedundantTailBlocks(merged)
}

function trimRedundantTailBlocks(blocks: MessageBlock[]): MessageBlock[] {
  const finalSummaryIndex = blocks.findIndex((block) => block.type === 'final_summary')
  if (finalSummaryIndex >= 0) {
    return blocks.slice(0, finalSummaryIndex + 1)
  }
  return blocks
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
      appendTextWithFailureMarkers(blocks, content)
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
    case 'plan_review': {
      const plan = objectField(payload, 'plan')
      return {
        type: 'plan_review',
        id: nextBlockId(),
        review_key: optionalStringField(payload, 'review_key'),
        session_id: optionalStringField(payload, 'session_id'),
        task_id: optionalStringField(payload, 'task_id'),
        review_type:
          optionalStringField(payload, 'review_type') === 'merge_to_main'
            ? 'merge_to_main'
            : 'plan',
        source_branch: optionalStringField(payload, 'source_branch'),
        target_branch: optionalStringField(payload, 'target_branch'),
        diff_snapshot_id: optionalStringField(payload, 'diff_snapshot_id'),
        overview: stringField(plan, 'overview'),
        tasks: arrayField(plan, 'tasks')
          .map(planTask)
          .filter((task): task is PlanTask => task !== null),
        waves: unknownArrayField(payload, 'waves').map((wave) =>
          arrayValue(wave)
            .map(planTask)
            .filter((task): task is PlanTask => task !== null),
        ),
        status: planReviewStatusField(payload),
      }
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
    case 'task_failure':
      return {
        type: 'task_failure',
        id: nextBlockId(),
        task_id: optionalStringField(payload, 'task_id'),
        agent: optionalStringField(payload, 'agent'),
        reason: stringField(payload, 'reason') || stringField(payload, 'message'),
        failureType: failureTypeField(payload),
      }
    case 'final_summary':
      return {
        type: 'final_summary',
        id: nextBlockId(),
        status: finalStatusField(payload),
        completed: numberField(payload, 'completed', 0),
        failed: numberField(payload, 'failed', 0),
        nextAction: optionalStringField(payload, 'nextAction'),
        details: arrayField(payload, 'details').map((detail) => ({
          task_id: stringField(detail, 'task_id'),
          agent: stringField(detail, 'agent'),
          status: finalDetailStatusField(detail),
          summary: optionalStringField(detail, 'summary'),
        })),
      }
    default:
      return null
  }
}

const FAILURE_MARKER_RE =
  /\[(Timeout|Error)\]\s*(?:Task\s+([A-Za-z0-9_-]+)\s*)?([\s\S]*?)(?=\[(?:Timeout|Error)\]|\n|$)/g

function appendTextWithFailureMarkers(blocks: MessageBlock[], text: string) {
  let lastIndex = 0
  let parsedAny = false

  for (const match of text.matchAll(FAILURE_MARKER_RE)) {
    const markerStart = match.index ?? 0
    const markerEnd = markerStart + match[0].length
    const markerType = match[1]
    const taskId = match[2]
    const reason = match[3]?.trim()

    if (markerStart > lastIndex) {
      const before = text.slice(lastIndex, markerStart)
      if (before.trim()) blocks.push({ type: 'text', id: nextBlockId(), content: before })
    }

    if (markerType === 'Timeout' && taskId && reason) {
      blocks.push({
        type: 'task_failure',
        id: nextBlockId(),
        task_id: taskId,
        reason,
        failureType: 'timeout',
      })
      parsedAny = true
      lastIndex = markerEnd
      continue
    }

    if (markerType === 'Error' && !reason && text.slice(markerEnd).startsWith('[Timeout]')) {
      parsedAny = true
      lastIndex = markerEnd
      continue
    }

    if (markerType === 'Error' && reason) {
      const nestedTimeout = reason.match(/^\[Timeout\]\s*Task\s+([A-Za-z0-9_-]+)\s*(.*)$/)
      if (nestedTimeout?.[1] && nestedTimeout[2]?.trim()) {
        blocks.push({
          type: 'task_failure',
          id: nextBlockId(),
          task_id: nestedTimeout[1],
          reason: nestedTimeout[2].trim(),
          failureType: 'timeout',
        })
        parsedAny = true
        lastIndex = markerEnd
        continue
      }

      const parsed = parseLegacyErrorReason(reason, taskId)
      blocks.push({
        type: 'task_failure',
        id: nextBlockId(),
        task_id: parsed.taskId,
        agent: parsed.agent,
        reason: parsed.reason,
        failureType: 'error',
      })
      parsedAny = true
      lastIndex = markerEnd
      continue
    }
  }

  const rest = text.slice(lastIndex)
  if (rest.trim() || !parsedAny) {
    blocks.push({ type: 'text', id: nextBlockId(), content: rest || text })
  }
}

function parseLegacyErrorReason(
  reason: string,
  markerTaskId?: string,
): { taskId?: string; agent?: string; reason: string } {
  const capturedAgentError = reason.match(/^agent=([^\s]+)\s+failed:\s*(.*)$/)
  if (capturedAgentError) {
    return {
      taskId: markerTaskId,
      agent: capturedAgentError[1],
      reason: capturedAgentError[2]?.trim() || reason,
    }
  }
  const taskError = reason.match(/^Task\s+([A-Za-z0-9_-]+)(?:\s+agent=([^\s]+))?\s+failed:\s*(.*)$/)
  if (!taskError) return { taskId: markerTaskId, reason }
  return {
    taskId: taskError[1],
    agent: taskError[2],
    reason: taskError[3]?.trim() || reason,
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

  if (block.type === 'plan_review') {
    const key = block.review_key || `${block.task_id ?? ''}:${block.session_id ?? ''}`
    const existing = blocks.find(
      (item) =>
        item.type === 'plan_review' &&
        (item.review_key || `${item.task_id ?? ''}:${item.session_id ?? ''}`) === key,
    )
    if (existing?.type === 'plan_review') {
      existing.session_id = block.session_id || existing.session_id
      existing.task_id = block.task_id || existing.task_id
      existing.review_key = block.review_key || existing.review_key
      existing.overview = block.overview || existing.overview
      existing.tasks = block.tasks.length ? block.tasks : existing.tasks
      existing.waves = block.waves.length ? block.waves : existing.waves
      existing.status = block.status
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
  const status = stringField(value, 'status') || 'pending'
  if (!['pending', 'running', 'completed', 'failed'].includes(status)) return null
  return {
    task_id: stringField(value, 'task_id'),
    agent: stringField(value, 'agent') || stringField(value, 'session_id'),
    title: stringField(value, 'title') || stringField(value, 'content').slice(0, 80),
    status: status as PlanTask['status'],
    content: optionalStringField(value, 'content'),
    session_id: optionalStringField(value, 'session_id'),
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

function failureTypeField(value: Record<string, unknown>): 'timeout' | 'error' {
  return stringField(value, 'failureType') === 'timeout' || stringField(value, 'type') === 'timeout'
    ? 'timeout'
    : 'error'
}

function finalStatusField(value: Record<string, unknown>): 'success' | 'partial' | 'failed' {
  const raw = stringField(value, 'status')
  if (raw === 'success' || raw === 'partial' || raw === 'failed') return raw
  return 'partial'
}

function finalDetailStatusField(value: Record<string, unknown>): 'completed' | 'failed' {
  return stringField(value, 'status') === 'failed' ? 'failed' : 'completed'
}

function planReviewStatusField(
  value: Record<string, unknown>,
): 'pending' | 'submitted' | 'approved' {
  const raw = stringField(value, 'status')
  if (raw === 'submitted' || raw === 'approved') return raw
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

function unknownArrayField(value: Record<string, unknown>, key: string): unknown[] {
  const raw = value[key]
  return Array.isArray(raw) ? raw : []
}

function arrayValue(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value)
    ? value.filter(
        (item): item is Record<string, unknown> =>
          item !== null && typeof item === 'object' && !Array.isArray(item),
      )
    : []
}

function objectField(value: Record<string, unknown>, key: string): Record<string, unknown> {
  const raw = value[key]
  return raw !== null && typeof raw === 'object' && !Array.isArray(raw)
    ? (raw as Record<string, unknown>)
    : {}
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
    case 'task_failure': {
      const payload = extractJsonPayload(lines)
      return payload ? legacyRuntimeBlock('task_failure', payload) : null
    }
    case 'final_summary': {
      const payload = extractJsonPayload(lines)
      return payload ? legacyRuntimeBlock('final_summary', payload) : null
    }
    case 'plan_review': {
      const payload = extractJsonPayload(lines)
      return payload ? legacyRuntimeBlock('plan_review', payload) : null
    }
    default:
      return null
  }
}

function extractField(lines: string[], field: string): string | null {
  const line = lines.find((l) => l.startsWith(`${field}:`))
  return line ? line.slice(field.length + 1).trim() : null
}

function extractJsonPayload(lines: string[]): Record<string, unknown> | null {
  const line = lines.find((l) => l.trim().startsWith('json:'))
  return line ? parseLegacyJsonLine(line) : null
}
