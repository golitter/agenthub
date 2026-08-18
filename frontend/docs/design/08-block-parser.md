# Block Parser — 消息结构化解析

## 实现了什么

将 Agent 输出的原始文本解析为 `MessageBlock[]` 结构化数组，支持 text、html-render、image、attachment、diff、preview、plan、plan_review、runtime_status、coordination、ask_agent、task_failure、final_summary、tool_call、tool_result 十五种块类型。解析器识别 `aka_yhy` 标记的代码块协议，将 Agent 技能输出转换为对应的渲染卡片。其中 plan、plan_review、runtime_status、coordination、ask_agent、task_failure、final_summary、tool_call、tool_result 类型由 SSE 事件直接构建（不经由文本解析），存储在 `SessionChatState.runtimeBlocks` 中。

## 怎么实现的

### 块类型定义 (`src/lib/block-types.ts`)

TypeScript discriminated union 定义十五种块类型：

```typescript
export interface PlanTask {
  task_id: string
  agent: string
  title: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  content?: string
  session_id?: string
}

export interface PlanReviewPayload {
  review_key?: string
  session_id?: string
  task_id?: string
  review_type?: 'plan' | 'merge_to_main'
  source_branch?: string
  target_branch?: string
  diff_snapshot_id?: string
  overview: string
  tasks: PlanTask[]
  waves: PlanTask[][]
  status: 'pending' | 'submitted' | 'approved'
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
  | { type: 'html-render'; id: string; content: string; resourceId?: string; streaming?: boolean }
  | { type: 'image'; id: string; path: string }
  | { type: 'attachment'; id: string; path: string }
  | { type: 'diff'; id: string; snapshotId: string }
  | { type: 'preview'; id: string; url: string }
  | { type: 'plan'; id: string; overview: string; tasks: PlanTask[] }
  | ({ type: 'plan_review'; id: string } & PlanReviewPayload)
  | { type: 'runtime_status'; id: string; task_id: string; agent: string; status: string; title?: string; streamingText?: string }
  | { type: 'coordination'; id: string; messages: CoordMessage[]; closed: boolean; summary?: string }
  | { type: 'ask_agent'; id: string; question_id: string; source_agent?: string; source_agent_type?: string; source_session_id?: string; target_agent: string; target_agent_type?: string; target_session_id: string; question: string; status: 'pending' | 'answered' | 'failed'; collapsed: boolean; summary?: string }
  | { type: 'task_failure'; id: string; task_id?: string; agent?: string; reason: string; failureType: 'timeout' | 'error' }
  | { type: 'final_summary'; id: string; status: 'success' | 'partial' | 'failed'; completed: number; failed: number; nextAction?: string; details: FinalSummaryDetail[] }
  | { type: 'tool_call'; id: string; name: string; input?: string }
  | { type: 'tool_result'; id: string; output?: string }
```

### 解析器 (`src/lib/block-reducer.ts`)

`reduceEventToBlocks(fullText)` 解析（流式进行中的）事件文本：

1. 用 `BLOCK_MARKER = 'aka_yhy'` 常量定位标记块（```aka_yhy ... ```）
2. 通过 `findAkaBlocks()` 扫描所有标记块，返回每个块的 `{ start, end, inner, raw, open }` —— `open` 表示闭合 ``` 尚未到达（流式进行中）
3. 标记块之前和之后的文本生成 `text` 块
4. 标记块内部按 `type:` 行判断块类型，提取对应字段
5. **流式 html-render 特判**：若块 `open === true` 且类型为 `html-render`，打上 `streaming: true` 标记，供 `HtmlCard` 显示占位而非反复 reload 半成品 iframe
6. 未识别但已闭合的类型降级为 `text` 块；进行中尚不可解析的块吞掉围栏标记（避免 ```aka_yhy / type: xxx 原文闪现）
7. 无标记块时返回单个 `text` 块
8. 末尾通过 `trimRedundantTailBlocks` 去掉多余尾部空文本块

```typescript
const BLOCK_MARKER = 'aka_yhy'

export function reduceEventToBlocks(fullText: string): MessageBlock[] {
  const blocks: MessageBlock[] = []
  let lastIndex = 0

  for (const match of findAkaBlocks(fullText)) {
    if (match.start > lastIndex) appendTextSegment(blocks, fullText.slice(lastIndex, match.start))
    const parsed = parseBlockContent(match.inner.trim())
    if (parsed) {
      // 流式进行中的 html-render 标记 streaming
      blocks.push(match.open && parsed.type === 'html-render' ? { ...parsed, streaming: true } : parsed)
    } else if (match.open) {
      // 进行中尚不可解析：吞掉围栏，仅当无任何块时塞空占位
      if (blocks.length === 0) blocks.push({ type: 'text', id: nextBlockId(), content: '' })
    } else {
      blocks.push({ type: 'text', id: nextBlockId(), content: match.raw })
    }
    lastIndex = match.end
  }
  if (lastIndex < fullText.length) appendTextSegment(blocks, fullText.slice(lastIndex))
  if (blocks.length === 0) return [{ type: 'text', id: nextBlockId(), content: fullText }]
  return trimRedundantTailBlocks(blocks)
}
```

`coalesceMessageBlocks(blocks)` 会合并相邻的同类型块（如同类 `text` 块拼接、同 `task_id` 的 `runtime_status` 累积 `streamingText` 等），在 `buildAgentMessage` 终态化消息和 `loadHistory` / `prependMessages` 加载历史时统一调用。

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
