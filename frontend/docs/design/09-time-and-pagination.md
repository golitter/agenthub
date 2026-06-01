# 消息时间分隔线与 Cursor 分页 — 前端实现

## 实现了什么

消息列表中的**时间分隔线**（自动插入相对时间标签）和 **cursor 分页**（向上滚动加载历史消息），让长对话的浏览体验接近 IM 应用。

## 怎么实现的

### 时间工具 (`src/utils/time.ts`)

两个纯函数，无副作用：

```ts
formatRelativeTime(timestamp: number): string
shouldShowTimeSeparator(prevTimestamp: number | undefined, currentTimestamp: number): boolean
```

`formatRelativeTime` 规则：

| 条件 | 格式 | 示例 |
|------|------|------|
| 今天 | `HH:mm` | `14:30` |
| 昨天 | `昨天 HH:mm` | `昨天 09:15` |
| 2-7 天 | `N天前` | `3天前` |
| 今年 | `M月D日 HH:mm` | `5月26日 14:30` |
| 跨年 | `YYYY年M月D日` | `2025年12月1日` |

`shouldShowTimeSeparator` 触发条件：首条消息（无 prev）、间隔 >5 分钟、跨日历日。

### TimeDivider 组件 (`src/components/chat/TimeDivider.tsx`)

```tsx
export function TimeDivider({ timestamp }: { timestamp: number }) {
  return (
    <div className="flex items-center gap-2 px-6 py-2">
      <div className="h-px flex-1 bg-border" />
      <span className="shrink-0 text-xs text-muted-foreground">
        {formatRelativeTime(timestamp)}
      </span>
      <div className="h-px flex-1 bg-border" />
    </div>
  )
}
```

水平分隔线 + 居中时间文本，用 `muted-foreground` 保持视觉层级低于消息。

### MessageList 统一渲染 (`src/components/chat/MessageList.tsx`)

通过 `DisplayItem` 联合类型将消息和时间分隔线统一管理：

```ts
type DisplayItem =
  | { type: 'message'; msg: ChatMessage; isStreamingMsg: boolean }
  | { type: 'time-divider'; timestamp: number }
```

构建 `displayItems` 时，每条消息前调用 `shouldShowTimeSeparator()` 判断是否插入 `time-divider`。阈值 50 条以上启用 `@tanstack/react-virtual` 虚拟滚动。

### Cursor 分页 — Store (`src/stores/message-store.ts`)

SessionChatState 新增两个字段：

```ts
hasMore: boolean        // 是否还有更早的消息
isLoadingMore: boolean  // 正在加载更多
```

新增 actions：

- `loadHistory(sessionId, messages, hasMore?)` — 首次加载，初始化会话消息
- `prependMessages(sessionId, messages, hasMore)` — 向前插入历史消息
- `setLoadingMore(sessionId, loading)` — 切换加载状态

`ChatMessage` 新增 `dbId?: number` 字段，用于 cursor（自增主键 ID）。

### Cursor 分页 — API (`src/lib/api.ts`)

```ts
interface TaskMessagesResponse {
  data: TaskMessage[]
  has_more: boolean
}

getTaskMessages(taskId, params?: { limit?: number; before?: number; sessionId?: string })
```

`before` 参数为消息的自增 ID（cursor），后端返回 `id < before` 的消息。

### Cursor 分页 — 加载触发 (`src/components/chat/ChatArea.tsx` + `MessageList.tsx`)

`ChatArea.loadMoreMessages()`：
1. 取当前最早消息的 `dbId` 作为 cursor
2. 调用 `getTaskMessages(taskId, { limit: 20, before: dbId })`
3. 调用 `prependMessages` 插入到消息数组头部
4. `requestAnimationFrame` 恢复滚动位置（`scrollHeight - oldScrollHeight`）

`MessageList` 在 `handleScroll` 中检测 `scrollTop === 0 && hasMore` 触发 `onLoadMore`。

### 首次加载 (`src/hooks/use-chat-stream.ts`)

挂载时调用 `getTaskMessages(taskId, { limit: 20, sessionId })`，传入 `has_more` 给 `loadHistory`。若响应中有 `status=streaming` 的 agent 消息，自动重连 SSE 流。
