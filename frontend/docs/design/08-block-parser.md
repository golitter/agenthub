# Block Parser — 消息结构化解析

## 实现了什么

将 Agent 输出的原始文本解析为 `MessageBlock[]` 结构化数组，支持 text、html-render、image、attachment、diff、preview、plan、runtime_status、coordination、ask_agent、task_failure、final_summary、tool_call、tool_result 十四种块类型。解析器识别 `aka_yhy` 标记的代码块协议，将 Agent 技能输出转换为对应的渲染卡片。其中 plan、runtime_status、coordination、ask_agent、task_failure、final_summary、tool_call、tool_result 类型由 SSE 事件直接构建（不经由文本解析），存储在 `SessionChatState.runtimeBlocks` 中。

## 怎么实现的

### 块类型定义 (`src/lib/block-types.ts`)

TypeScript discriminated union 定义十四种块类型：

```typescript
export interface PlanTask {
  task_id: string
  agent: string
  title: string
  status: 'pending' | 'running' | 'completed' | 'failed'
}

export interface CoordMessage {
  from: string
  to: string
  text: string
  round: number
}

export interface FinalSummaryDetail {
  task_id: string
  agent: string
  status: 'completed' | 'failed'
  summary?: string
}

export type MessageBlock =
  | { type: 'text'; id: string; content: string }
  | { type: 'html-render'; id: string; content: string }
  | { type: 'image'; id: string; path: string }
  | { type: 'attachment'; id: string; path: string }
  | { type: 'diff'; id: string; snapshotId: string }
  | { type: 'preview'; id: string; url: string }
  | { type: 'plan'; id: string; overview: string; tasks: PlanTask[] }
  | { type: 'runtime_status'; id: string; task_id: string; agent: string; status: string; title?: string; streamingText?: string }
  | { type: 'coordination'; id: string; messages: CoordMessage[]; closed: boolean; summary?: string }
  | { type: 'ask_agent'; id: string; question_id: string; source_agent?: string; source_agent_type?: string; source_session_id?: string; target_agent: string; target_agent_type?: string; target_session_id: string; question: string; status: 'pending' | 'answered' | 'failed'; collapsed: boolean; summary?: string }
  | { type: 'task_failure'; id: string; task_id?: string; agent?: string; reason: string; failureType: 'timeout' | 'error' }
  | { type: 'final_summary'; id: string; status: 'success' | 'partial' | 'failed'; completed: number; failed: number; nextAction?: string; details: FinalSummaryDetail[] }
  | { type: 'tool_call'; id: string; name: string; input?: string }
  | { type: 'tool_result'; id: string; output?: string }
```

### 解析器 (`src/lib/block-reducer.ts`)

`reduceEventToBlocks(fullText)` 解析完整事件文本：

1. 用正则 ` /``aka_yhy\n([\s\S]*?)```/g` 匹配所有标记块
2. 标记块之前和之后的文本生成 `text` 块
3. 标记块内部按 `type:` 行判断块类型，提取对应字段
4. 未识别类型降级为 `text` 块
5. 无标记块时返回单个 `text` 块

```typescript
export function reduceEventToBlocks(fullText: string): MessageBlock[] {
  const blocks: MessageBlock[] = []
  let lastIndex = 0

  for (const match of fullText.matchAll(BLOCK_RE)) {
    if (matchStart > lastIndex) {
      const text = fullText.slice(lastIndex, matchStart)
      if (text) blocks.push({ type: 'text', id: nextBlockId(), content: text })
    }
    const inner = match[1].trim()
    const parsed = parseBlockContent(inner)
    if (parsed) blocks.push(parsed)
    else blocks.push({ type: 'text', id: nextBlockId(), content: match[0] })
    lastIndex = matchStart + match[0].length
  }
  if (lastIndex < fullText.length) {
    blocks.push({ type: 'text', id: nextBlockId(), content: fullText.slice(lastIndex) })
  }
  if (blocks.length === 0) return [{ type: 'text', id: nextBlockId(), content: fullText }]
  return blocks
}
```

### Diff 解析器 (`src/lib/diff-parser.ts`)

`parseUnifiedDiff(diffText)` 基于 `react-diff-view` 的 `parseDiff` 封装，输出 `ParsedDiffResult`：

```typescript
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
```

`reconstructContent` 从 hunks 中分别重建 old/new 侧的完整文件内容，供 `DiffFileEditor` 编辑使用。
