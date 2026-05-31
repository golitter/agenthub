# Cards — 技能输出卡片系统

## 实现了什么

基于 `MessageBlock` 类型的卡片渲染组件，将 Agent 输出的结构化内容（Diff、HTML、图片、附件、预览、计划、运行时状态、协调通道、最终汇总、任务失败、工具调用）以独立 UI 卡片呈现。11 种卡片位于 `components/cards/index.ts` 统一导出，`ask_agent` 块由 `components/chat/AskAgentCard.tsx` 渲染。

## 怎么实现的

### DiffCard (`src/components/cards/DiffCard.tsx`)

Diff 卡片是核心组件，负责展示、编辑和操作文件变更。采用 Snapshot-first 加载策略：

```typescript
type SnapshotStatus = 'pending' | 'committed' | 'reverted' | 'cancelled'
```

加载流程：先尝试 `GET /api/diff-snapshots/:snapshotId`，404 时退化为 `GET /api/session/:sessionId/diff` 获取工作区 diff，再 `PUT` 创建 pending snapshot。

操作按钮：
- **接受变更** — `POST /api/session/:sessionId/commit` + 更新 snapshot 状态为 `committed`
- **拒绝变更** — `POST /api/session/:sessionId/revert` + 更新 snapshot 状态为 `reverted`
- **编辑** — 切换为 `DiffFileEditor`（CodeMirror），保存时 `PUT /api/session/:sessionId/files/:path`

已定稿状态（committed/reverted/cancelled）显示 Badge 并禁用操作按钮。

### HtmlCard (`src/components/cards/HtmlCard.tsx`)

HTML 沙箱渲染卡片，使用 `<iframe sandbox="">` 安全渲染 HTML 内容：

```tsx
export function HtmlCard({ content }: HtmlCardProps) {
  return (
    <div className="my-2 overflow-hidden rounded-lg border border-border">
      <iframe sandbox="" srcDoc={content} className="h-64 w-full border-0" title="HTML Preview" />
    </div>
  )
}
```

### ImageCard (`src/components/cards/ImageCard.tsx`)

图片卡片，通过代理 URL `/api/session/:sessionId/files/:path` 加载图片，加载失败时显示降级 UI。

### AttachmentCard (`src/components/cards/AttachmentCard.tsx`)

附件下载卡片，显示文件图标 + 文件名 + 下载按钮。下载链接使用代理 URL。

### PreviewCard (`src/components/cards/PreviewCard.tsx`)

外部页面预览卡片，Header 含"在新标签页打开"链接，主体为 iframe 嵌入。

### PlanCard (`src/components/cards/PlanCard.tsx`)

多 Agent 任务计划卡片，展示 Orchestrator 分派的子任务列表。每个子任务显示任务标题、负责 Agent 和执行状态（pending/running/completed/failed）。

### RuntimeStatus (`src/components/cards/RuntimeStatus.tsx`)

Agent 运行时状态卡片，实时展示 Agent 执行状态（running/completed/failed），支持 streaming 文本输出。用于多 Agent 场景下跟踪各 Agent 的执行进度。

### CoordChannel (`src/components/cards/CoordChannel.tsx`)

多 Agent 协调通道卡片，展示 Agent 间的协作消息流（from/to/text/round）。支持显示协调摘要，通道关闭后展示最终决策结果。

### ToolCard (`src/components/cards/ToolCard.tsx`)

工具调用卡片，展示 `tool_call` 和 `tool_result` 类型的 MessageBlock，显示工具名称、输入参数和执行结果。

### FinalSummaryCard (`src/components/cards/FinalSummaryCard.tsx`)

Orchestrator 执行最终汇总卡片，展示整体状态（success/partial/failed）、完成/失败计数、下一步建议和任务概览列表：

```tsx
export function FinalSummaryCard({
  status, completed, failed, nextAction, details,
}: FinalSummaryCardProps) {
  const copy = statusCopy[status]  // success → 已完成 / partial → 部分完成 / failed → 执行失败
  // 顶部状态 Badge + 计数
  // 可选：下一步建议（nextAction）
  // 任务概览列表：grid 布局显示 task_id / 状态 / agent 名称 + 摘要
}
```

`FinalSummaryDetail` 类型定义在 `lib/block-types.ts`，每条记录包含 `task_id`、`agent`、`status`（completed/failed）和可选 `summary`。

### TaskFailureCard (`src/components/cards/TaskFailureCard.tsx`)

任务失败卡片，展示超时或执行失败的子任务。使用红色边框和背景区分：

```tsx
export function TaskFailureCard({ taskId, agent, reason, failureType }: TaskFailureCardProps) {
  // failureType: 'timeout'（超时图标 Clock3）/ 'error'（错误图标 AlertTriangle）
  // 标题行：图标 + "任务超时"/"任务失败" + 可选 task_id / agent
  // 原因文本：whitespace-pre-wrap 保留格式
}
```
