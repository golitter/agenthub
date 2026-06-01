# Components — 组件体系

## 实现了什么

基于三层组件模型（Page / Smart / Dumb）构建的 IM 聊天 UI，分为 IM 侧栏、聊天区、Markdown 渲染三大模块。所有组件使用 Tailwind CSS + CSS 变量驱动样式，无硬编码颜色值。

## 怎么实现的

### IM 侧栏 (`components/im/`)

### ConversationList (`src/components/im/ConversationList.tsx`)

侧栏容器组件，固定宽度 280px，包含 Header、搜索栏、对话列表和新建弹窗四部分。数据源为 `useConversations()` (React Query) + `useChatNav()` (Zustand)：

```tsx
export function ConversationList() {
  const [search, setSearch] = useState('')
  const [showNewChat, setShowNewChat] = useState(false)
  const { data: conversations, isLoading } = useConversations()
  const { currentSessionId, setCurrentSession } = useChatNav()

  const filtered = conversations?.filter((c) => {
    if (!search) return true
    const q = search.toLowerCase()
    return c.agentType.includes(q) || c.taskTitle.toLowerCase().includes(q)
  })
  // ... 渲染 Header / Search / ConversationItem 列表 / NewChatDialog
}
```

搜索支持按 `agentType` 和 `taskTitle` 过滤。空态和加载态分别显示对应占位 UI。

### ConversationItem (`src/components/im/ConversationItem.tsx`)

单条对话项，接收 `Conversation` 数据并渲染 Agent 头像、名称、任务标题和相对时间。通过 `useHoverStyle` hook 实现悬停效果：

```tsx
export function ConversationItem({ conversation, isActive, onClick }: ConversationItemProps) {
  const name =
    conversation.agentName || AGENT_NAMES[conversation.agentType] || conversation.agentType
  const hoverStyle = useHoverStyle()

  return (
    <button
      className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors duration-120 ease-out"
      style={{
        backgroundColor: isActive ? 'var(--accent)' : 'transparent',
        borderLeft: isActive ? '2px solid var(--primary)' : '2px solid transparent',
      }}
      onClick={onClick}
      {...(!isActive && hoverStyle)}
    >
```

Active 态通过 `borderLeft: 2px solid var(--primary)` 品牌色竖线标识，非 Active 态使用 `useHoverStyle` 提供的 `onMouseEnter/Leave` 切换背景色。

### NewChatDialog (`src/components/im/NewChatDialog.tsx`)

新建对话弹窗（shadcn Dialog），流程为：输入仓库路径 -> 校验 -> 选择 Agent 类型 -> 创建对话。仓库路径通过 `validateRepoPath()` API 校验后才能选择 Agent：

```tsx
const handleValidate = async () => {
  const path = repoPath.trim()
  if (!path) {
    setRepoPathError('请输入仓库路径')
    setRepoPathValidated(false)
    return
  }
  setValidating(true)
  try {
    const result = await validateRepoPath(path)
    if (result.valid) {
      setRepoPathValidated(true)
    } else {
      setRepoPathError(result.errors.join('; '))
    }
  } finally {
    setValidating(false)
  }
}
```

可用 Agent 列表通过 `useQuery({ queryKey: ['agent-types'], queryFn: fetchAgentTypes })` 拉取，失败时 fallback 到内置列表 `['claude-code', 'opencode', 'orchestrator', 'codex']`。支持多选 Agent（多选时自动注入 orchestrator 创建群聊），选中后调用 `createConversation` mutation，成功后自动选中并关闭弹窗。

### AgentSelectList (`src/components/im/AgentSelectList.tsx`)

Agent 多选列表组件，支持搜索过滤。在 `NewChatDialog` 中使用，用户可同时选择多个 Agent 创建群聊。每个选项显示 Agent 头像 + 名称 + 描述，已选项显示勾选标记。

### RepoPathInput (`src/components/im/RepoPathInput.tsx`)

仓库路径输入组件，带实时校验功能。在 `NewChatDialog` 中使用，输入仓库路径后调用 `validateRepoPath` API 校验有效性，校验状态通过 `onValidationChange` 回调通知父组件。

---

### 聊天区 (`components/chat/`)

### ChatArea (`src/components/chat/ChatArea.tsx`)

聊天主容器（Smart 组件），纵向三段式布局：Header（h-12）、消息区、输入区。核心 hook `useChatStream` 返回 `{ state, sendMessage }`：

```tsx
export function ChatArea({ taskId, sessionId, agentType = 'claude-code', agentName, avatarUrl, repoPath }: ChatAreaProps) {
  const { state, sendMessage } = useChatStream(taskId, sessionId, agentType)
  const isStreaming = ['loading', 'streaming', 'tool_running'].includes(state.status)
```

发送消息前会先验证 `repoPath`（如果存在），通过 `validateRepoPath()` API 校验路径有效性。Header 区域显示 Agent 显示名 + "正在回复..." 状态。空态时居中显示大尺寸 `AgentAvatar` + 显示名 + "发送消息开始对话" 提示。

### MessageList (`src/components/chat/MessageList.tsx`)

消息列表组件，支持两种渲染模式，阈值为 50 条 DisplayItem。内部通过 `DisplayItem` 联合类型将消息和时间分隔线统一管理：

```tsx
type DisplayItem =
  | { type: 'message'; msg: ChatMessage; isStreamingMsg: boolean }
  | { type: 'time-divider'; timestamp: number }

const VIRTUALIZE_THRESHOLD = 50

const displayItems = useMemo<DisplayItem[]>(() => {
  const allMsgs =
    isStreaming && streamingContent
      ? [...messages, { id: 'streaming', role: 'agent' as const, ... }]
      : messages
  const items: DisplayItem[] = []
  for (let i = 0; i < allMsgs.length; i++) {
    const msg = allMsgs[i]
    const prevMsg = i > 0 ? allMsgs[i - 1] : undefined
    if (shouldShowTimeSeparator(prevMsg?.timestamp, msg.timestamp)) {
      items.push({ type: 'time-divider', timestamp: msg.timestamp })
    }
    items.push({ type: 'message', msg, isStreamingMsg: ... })
  }
  return items
}, [messages, isStreaming, streamingContent, streamingAgentType])

const useVirtual = displayItems.length > VIRTUALIZE_THRESHOLD
```

**时间分隔线**：通过 `shouldShowTimeSeparator()`（来自 `utils/time.ts`）判断是否在两条消息之间插入 `TimeDivider` 组件。触发条件：首条消息、间隔 >5 分钟、或跨日历日。

**向上翻页加载**（cursor 分页）：监听 `scrollTop === 0` 时触发 `onLoadMore` 回调加载更早的历史消息，加载完成后恢复滚动位置（`scrollHeight - oldScrollHeight` 偏移）。

流式消息以 `id: 'streaming'` 临时追加到列表末尾。虚拟滚动使用 `@tanstack/react-virtual`，通过 `estimateSize` 根据内容长度估算行高。内置自动滚动逻辑：监听 `scrollHeight - scrollTop - clientHeight < 60` 判断是否在底部，手动上滑时隐藏自动滚动并显示 "回到底部" 按钮。

### MessageBubble (`src/components/chat/MessageBubble.tsx`)

消息气泡组件，使用 TypeScript discriminated union 定义三种变体：

```tsx
interface UserBubbleProps extends BaseProps {
  variant: 'user'
}
interface AgentBubbleProps extends BaseProps {
  variant: 'agent'
  agentType: AgentType
  avatarUrl?: string
  agentName?: string
  status?: 'ready' | 'running' | 'offline' | 'error'
  isStreaming?: boolean
}
interface SystemBubbleProps extends BaseProps {
  variant: 'system'
}
type MessageBubbleProps = UserBubbleProps | AgentBubbleProps | SystemBubbleProps
```

- **user**：右对齐，`bg-primary-soft` 背景 + `border-primary-border` 边框
- **agent**：左对齐 + AgentHoverCard（悬停展示 Agent 信息），`bg-card` 背景 + 左侧 3px Agent 色竖线，流式输出时显示闪烁光标 `▌`
- **system**：居中，小字 `text-muted-foreground`

### MessageRenderer (`src/components/chat/MessageRenderer.tsx`)

消息渲染编排组件，根据消息 `role` 选择渲染方式，统一管理 Agent 类型解析、头像查找和 Markdown 渲染：

```tsx
export function MessageRenderer({
  msg, isStreaming, avatarUrl, agentName, sessionId,
  sessionAgentType, agentSessionLookup, streamingAgentName,
}: MessageRendererProps) {
  if (msg.role === 'user') {
    return <MessageBubble variant="user">{msg.content}</MessageBubble>
  }
  if (msg.role === 'agent') {
    // 解析 agentType、agentSession、avatarUrl 等
    return (
      <MessageBubble variant="agent" agentType={resolvedAgentType} ...>
        <MarkdownRenderer content={msg.content} />
      </MessageBubble>
    )
  }
  return <MessageBubble variant="system">{msg.content}</MessageBubble>
}
```

在 `MessageList` 中替代了直接使用 `MessageBubble` + `MarkdownRenderer` 的组合，集中处理多 Agent 群聊场景下的 Agent 身份解析逻辑。

### GroupAvatar (`src/components/chat/GroupAvatar.tsx`)

群聊头像组件，当 Task 有多个 Session（多 Agent 协作）时，显示叠加的多 Agent 头像。接收 `agentTypes` 和 `agentNames` 数组，渲染为堆叠的 `AgentAvatar`：

```tsx
export function GroupAvatar({ agentTypes, agentNames, size = 32 }: GroupAvatarProps) {
  // 多头像叠加渲染
}
```

### MessageInput (`src/components/chat/MessageInput.tsx`)

输入框组件，textarea 自动高度（最小 48px，最大 200px），`Enter` 发送，`Shift+Enter` 换行：

```tsx
const adjustHeight = useCallback(() => {
  const el = textareaRef.current
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${Math.min(el.scrollHeight, 200)}px`
}, [])
```

发送后清空输入框并重置高度为 48px。流式输出时 `disabled`，按钮和输入框同时变为不可用。

### AgentAvatar (`src/components/chat/AgentAvatar.tsx`)

Agent 头像组件，圆角方块显示首字母或上传的头像图片。颜色映射通过 `AGENT_COLORS` 常量（来自 `lib/constants.ts`）和 CSS 变量：

```tsx
import { AGENT_COLORS, AGENT_NAMES } from '@/lib/constants'
// AGENT_COLORS 在 lib/constants.ts 中定义：
// { 'claude-code': 'var(--agent-claude)', opencode: 'var(--agent-opencode)', ... }
```

状态指示灯（右下角小圆点）使用 `STATUS_COLORS` 映射，`ready` 脉冲动画 `status-ready-pulse`，`running` 旋转动画 `status-running-spin`。支持自定义头像 URL，无自定义头像时若有 `agentName` 则通过 DiceBear API 生成 initials 头像。

### AgentEditDialog (`src/components/chat/AgentEditDialog.tsx`)

Agent 编辑弹窗（shadcn Dialog），支持修改名称和上传头像。上传头像调用 `uploadAvatar` API，保存时调用 `updateSession` API 并 invalidate `conversations` 缓存。打开时通过 `prevOpen` 状态同步重置表单值：

```tsx
const handleSave = async () => {
  const data: { agent_name?: string; avatar_url?: string } = {}
  if (name !== initialName) data.agent_name = name
  if (avatarUrl !== initialAvatarUrl) data.avatar_url = avatarUrl
  if (Object.keys(data).length > 0) {
    await updateSession(sessionId, data)
    await queryClient.invalidateQueries({ queryKey: ['conversations'] })
  }
}
```

### AgentHoverCard (`src/components/chat/AgentHoverCard.tsx`)

Agent 悬停卡片（Popover），鼠标悬停 300ms 后展示。通过 `fetchAgentProfile` API 获取 Agent 技能列表（最多显示 3 个），底部提供 "查看 Agent 详情" 链接跳转到 `AgentProfilePage`。使用 `PopoverAnchor` 包裹 `AgentAvatar` 作为触发区域，内容区显示头像 + 名称 + 状态 Badge + Skills 预览 + Session ID。

```tsx
export function AgentHoverCard(props: AgentHoverCardProps) {
  // 300ms show delay, 200ms hide delay, pointer-inside tracking
  // Popover + PopoverAnchor wrapping AgentAvatar
  // Content: AgentAvatar + name + status badge + skills (max 3) + link to /agent/:sessionId
}
```

在 `MessageBubble` 的 agent 变体中，替代了直接使用的 `AgentAvatar`——通过 `AgentHoverCard` 包裹，实现悬停即展示 Agent 信息。

### AgentMeta (`src/components/chat/AgentMeta.tsx`)

Agent 元数据网格组件，在 `AgentProfilePage` 中使用。2 列 grid 布局展示 Session ID、Task ID、Repo Path、Workspace、创建时间、消息数：

```tsx
export function AgentMeta({ detail }: { detail: AgentDetail }) {
  return (
    <div className="grid grid-cols-2 gap-4 rounded-[10px] border border-border bg-card p-4">
      <MetaItem label="Session ID" value={detail.session_id} mono />
      <MetaItem label="Task ID" value={detail.task_id} mono />
      {/* Repo Path、Workspace、创建时间、消息数 */}
    </div>
  )
}
```

### SkillCard (`src/components/chat/SkillCard.tsx`)

Agent 技能卡片，显示技能名称、描述、来源，builtin 技能显示绿色 Badge。在 `AgentProfilePage` 中列表渲染：

```tsx
export function SkillCard({ skill }: { skill: AgentSkill }) {
  return (
    <div className="rounded-[10px] border border-border bg-card p-3.5">
      <div className="mb-1 flex items-center justify-between">
        <span className="text-sm font-semibold">{skill.name}</span>
        {skill.builtin && <span className="badge">builtin</span>}
      </div>
      <p className="text-[13px] leading-relaxed text-foreground/75">{skill.description}</p>
    </div>
  )
}
```

### TimeDivider (`src/components/chat/TimeDivider.tsx`)

时间分隔线组件，在消息列表中显示相对时间标签（如 "14:30"、"昨天 09:15"、"3天前"）：

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

时间格式化通过 `formatRelativeTime()`（来自 `utils/time.ts`）实现，规则为：今天 "HH:mm"、昨天 "昨天 HH:mm"、2-7 天 "N天前"、今年 "M月D日 HH:mm"、跨年 "YYYY年M月D日"。

### RightSidebar (`src/components/chat/RightSidebar.tsx`)

群聊右侧边栏组件，可折叠、可拖拽调整宽度（通过 `useResize` hook 管理）。内部包含三个可折叠区块：MembersSection、AnnouncementsSection、HistorySearch。`useResize` 支持 localStorage 持久化宽度和折叠阈值（宽度低于阈值自动折叠）。

### MembersSection (`src/components/chat/MembersSection.tsx`)

群聊成员列表区块，显示群聊中的所有 Agent 成员（AgentHoverCard + 名称 + 状态）。使用 `useCollapsible` hook（从 RightSidebar 导出）管理折叠状态。

### AnnouncementsSection (`src/components/chat/AnnouncementsSection.tsx`)

群聊公告区块，支持展示、创建、删除和置顶公告。调用 `fetchAnnouncements`、`createAnnouncement`、`deleteAnnouncement` API 管理公告数据，公告列表按时间排序，置顶公告优先显示。

### HistorySearch (`src/components/chat/HistorySearch.tsx`)

消息历史搜索组件，支持关键词搜索和消息角色筛选（user/agent/system）。搜索结果高亮匹配片段，点击结果可跳转到对应消息。内部使用 `MESSAGE_ROLES` 常量进行角色筛选。

---

### Markdown 渲染 (`components/markdown/`)

### MarkdownRenderer (`src/components/markdown/MarkdownRenderer.tsx`)

基于 `react-markdown` + `remark-gfm` 的渲染器，通过自定义 `components` 对象覆盖默认渲染：

```tsx
const components: Components = {
  pre({ children }) {
    return <>{children}</>
  },
  code({ className, children, ...props }) {
    const match = /language-(\w+)/.exec(className ?? '')
    const code = String(children).replace(/\n$/, '')
    if (match) {
      return <CodeBlock code={code} language={match[1]} />
    }
    return (
      <code className="rounded bg-code px-1.5 py-0.5 text-[13px]"
        style={{ fontFamily: "'Geist Mono', monospace", letterSpacing: 0 }}
        {...props}>
        {children}
      </code>
    )
  },
  table({ children }) {
    return (
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-sm border-border">{children}</table>
      </div>
    )
  },
}
```

代码块（带 `language-` 前缀）委托给 `CodeBlock`，行内代码使用深色背景 + Geist Mono 字体，表格带边框和表头背景色。

### CodeBlock (`src/components/markdown/CodeBlock.tsx`)

代码高亮组件，使用 Shiki（`tokyo-night` 主题）异步高亮。高亮完成前 fallback 到纯文本 + 行号显示：

```tsx
useEffect(() => {
  async function highlight() {
    const shiki = await import('shiki')
    const highlighter = await shiki.createHighlighter({
      themes: ['tokyo-night'],
      langs: language ? [language] : [],
    })
    const result = highlighter.codeToHtml(code, {
      lang: language ?? 'text',
      theme: 'tokyo-night',
    })
    setHtml(result)
    highlighter.dispose()
  }
  if (language) highlight()
}, [code, language])
```

Shiki 通过动态 `import()` 按需加载，`highlighter` 使用后立即 `dispose()` 释放资源。代码块可横向滚动，字体 `Geist Mono`，字号 13px，行高 1.65。
