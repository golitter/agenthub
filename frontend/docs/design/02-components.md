# Components — 组件体系

## 实现了什么

基于三层组件模型（Page / Smart / Dumb）构建的 IM 聊天 UI，分为 IM 侧栏、聊天区、Markdown 渲染三大模块。所有组件使用 Tailwind CSS + CSS 变量驱动样式，无硬编码颜色值。

## 怎么实现的

### IM 侧栏 (`components/im/`)

### ConversationList (`src/components/im/ConversationList.tsx`)

侧栏容器组件，宽度为 `w-full`（移动端全宽，由父 Grid 容器约束）/ `md:w-[280px]`（桌面端固定 280px），包含 Header、搜索栏、对话列表和新建弹窗四部分。数据源为 `useConversations()` (React Query) + `useChatNav()` (Zustand)：

```tsx
export function ConversationList() {
  const [search, setSearch] = useState('')
  const [showNewChat, setShowNewChat] = useState(false)
  const { data: conversations, isError, isLoading, refetch } = useConversations()
  const { currentSessionId, setCurrentSession } = useChatNav()
  const query = search.trim().toLowerCase()

  const filtered = conversations?.filter((c) => {
    if (!query) return true
    return [
      c.agentType,
      c.agentName,
      c.title,
      c.taskTitle,
      c.repoPath,
      ...(c.groupAgentNames ?? []),
      ...(c.groupAgentTypes ?? []),
    ].some((value) => value?.toLowerCase().includes(query))
  })
  // ... 渲染 Header / Search / ConversationItem 列表 / NewChatDialog
}
```

搜索在多字段上做包含匹配：`agentType` / `agentName` / `title` / `taskTitle` / `repoPath` 以及群聊场景下的 `groupAgentNames` / `groupAgentTypes`。空态、加载态（骨架卡片，使用 `skeleton-sheen` 动画）和错误态（含「重试」按钮调用 `refetch()`）分别显示对应占位 UI。

### ConversationItem (`src/components/im/ConversationItem.tsx`)

单条对话项，接收 `Conversation` 数据并渲染 Agent 头像（单聊用 `AgentAvatar`，群聊用 `GroupAvatar`）、名称（单聊取 `agentName` 或 `AGENT_NAMES[agentType]`，群聊取 `conversation.title`）和相对时间。通过 Tailwind 类实现选中态和悬停效果：

```tsx
export function ConversationItem({ conversation, isActive, onClick }: ConversationItemProps) {
  const isGroup = !!conversation.isGroupChat
  const singleName =
    conversation.agentName || AGENT_NAMES[conversation.agentType] || conversation.agentType
  const displayName = isGroup ? conversation.title : singleName

  return (
    <button
      type="button"
      className={cn(
        'flex w-full items-center gap-3 rounded-xl border px-3 py-2.5 text-left transition-[background,border-color,transform] active:scale-[0.99]',
        isActive
          ? 'border-primary-border bg-primary-soft'
          : 'border-transparent hover:border-border/70 hover:bg-accent',
      )}
      onClick={onClick}
    >
```

Active 态通过 `border-primary-border` + `bg-primary-soft` 品牌色边框/背景标识，非 Active 态透明边框 + `hover:bg-accent` 悬停效果，按下时 `active:scale-[0.99]` 微缩反馈。样式合并使用 `cn()`（`tailwind-merge` + `clsx`）。

### NewChatDialog (`src/components/im/NewChatDialog.tsx`)

新建对话弹窗（shadcn Dialog），流程为：输入仓库路径 -> 校验 -> 逐个添加 Agent（可自定义名称）-> 创建对话。弹窗自身不直接做路径校验，而是组合 `RepoPathInput`（校验 + 非 Git 目录初始化引导，结果经 `onValidationChange` 回传）与 `AgentSelectList`（Agent 选择，仅在 `repoPathValidated` 后展开），另含群聊名称输入与提交按钮：

```tsx
const { data: agentTypes } = useQuery({
  queryKey: ['agent-types'],
  queryFn: fetchAgentTypes,
})

// 接口失败/为空时回退到内置 Agent 清单（含 pi），并补充 AGENT_DESCRIPTIONS 描述
const types = agentTypes?.length
  ? agentTypes
  : [AGENT_TYPES.ClaudeCode, AGENT_TYPES.Opencode, AGENT_TYPES.Orchestrator, AGENT_TYPES.Codex, AGENT_TYPES.Pi]
      .map((t) => ({ type: t, name: t, description: AGENT_DESCRIPTIONS[t] ?? '' }))
```

提交规则：已选 Agent ≥ 2 时必须填写群聊名称（`needsGroupTitle`，错误时 shake 动画提示）；Orchestrator 单独成群被禁止（`orchestratorAlone`，展示 "Orchestrator 不能单独成群" 提示）。校验通过后调用 `createConversation` mutation（参数 `{ agents, repoPath, title }`），成功后 `setCurrentSession(conversation.sessionId)` 并关闭弹窗。弹窗打开时通过 `prevOpen` render-phase 重置表单、`resetCreateMutation()` 清除上次失败态。

### AgentSelectList (`src/components/im/AgentSelectList.tsx`)

Agent 选择组件，未选中时渲染 3×3 九宫格（`AGENT_GRID_POSITIONS` 为各 Agent 类型固定网格坐标），每个格子为 `AgentOptionIcon` 图标 + 名称按钮，点击后原地切换为内联输入行：为该 Agent 输入自定义显示名称 ->「添加」（`Enter` 提交，`isComposing` 防误发）。已添加的成员展示在「已选 Agent（N）」列表中（图标 + 名称 + 类型 + 删除按钮），通过 `onChange` 将 `AgentEntry[]`（`{ type, name }`）回传父组件。规则校验：显示名称不允许重复（`UI_ERRORS.DUPLICATE_NAME`）；Orchestrator 仅可添加一个（`UI_ERRORS.ONE_ORCHESTRATOR`），Orchestrator 格子在九宫格中以 `border-agent-orchestrator/35` 高亮区分。仅在 `repoPathValidated` 为 true 时渲染选择区域。

### AgentOptionIcon (`src/components/im/AgentOptionIcon.tsx`)

「新建对话」专用的 Agent 图标组件（不与会话头像 / `avatarUrl` 共用）。按 `agentType` 从 `public/agent-icons/`（claude-code.svg / opencode.png / orchestrator.svg / codex.svg / pi.svg）加载本地静态图标，渲染为 36px 圆角方块（边框 + 阴影 + `bg-card` 底）；codex / orchestrator 图标在暗色主题下加 `dark:invert` 保证对比度。加载失败（`onError`）时回退为 `AGENT_COLORS` 品牌色底 + 名称首字母：

```tsx
const LOCAL_AGENT_ICON_PATHS: Record<AgentType, string> = {
  'claude-code': '/agent-icons/claude-code.svg',
  opencode: '/agent-icons/opencode.png',
  orchestrator: '/agent-icons/orchestrator.svg',
  codex: '/agent-icons/codex.svg',
  pi: '/agent-icons/pi.svg',
}
```

### RepoPathInput (`src/components/im/RepoPathInput.tsx`)

仓库路径输入组件。点击「校验」按钮或 `Enter` 触发 `validateRepoPath` API 校验（输入变更时立即重置校验态），结果通过 `onValidationChange(path, validated)` 回调通知父组件；用 `validationRequestRef` 递增序号丢弃过期的异步响应。当后端返回「不是 git 仓库」时进入 Git 初始化引导：输入路径最后一段作为确认口令，输入匹配后调用 `initGitRepo` 完成自动初始化（详见 [11-git-auto-init.md](11-git-auto-init.md)）。

---

### 聊天区 (`components/chat/`)

### ChatArea (`src/components/chat/ChatArea.tsx`)

聊天主容器（Smart 组件），纵向三段式布局：Header（`min-h-14`）、消息区、输入区。核心 hook `useChatStream` 返回 `{ state, sendMessage, stopRun, isCancelling, historyError, retryHistory }`（群聊场景透传 `includeTaskMessages` 等选项）：

```tsx
export function ChatArea({ taskId, sessionId, agentType = AGENT_TYPES.ClaudeCode, agentName, avatarUrl, repoPath, isGroupChat, ... }: ChatAreaProps) {
  const { state, sendMessage, stopRun, isCancelling, historyError, retryHistory } = useChatStream(taskId, sessionId, agentType, {
    includeTaskMessages: Boolean(isGroupChat),
  })
  const isStreaming = ACTIVE_STATUSES.has(state.status)
```

其中 `ACTIVE_STATUSES`（来自 `lib/constants.ts`）是 `{ loading, streaming, tool_running }` 的只读集合。`loadMoreMessages` 取 `state.messages[0]?.dbId` 作为 cursor，群聊场景下传 `mode: 'group'` + `primarySessionId`；向上翻页加载由 `MessageList` 在 `scrollTop === 0 && hasMore` 时触发该回调。

发送消息直接调用 `sendMessage(message, agentType)`；仓库路径校验（`validateRepoPath`）发生在 `RepoPathInput` / `NewChatDialog` 新建会话阶段，`ChatArea` 仅在 Header 显示 `repoPath`，发送时不再次校验。Header 区域显示 Agent 显示名 + "正在回复..." 状态。空态时居中显示大尺寸 `AgentAvatar`（size 48）+ 显示名 + "还没有消息 / 第一条消息会把这次任务的上下文固定下来。" 提示（`UI_MESSAGES.CHAT_EMPTY_TITLE / CHAT_EMPTY_DESC`）。

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
  isLong?: boolean
  isStructured?: boolean
}
interface SystemBubbleProps extends BaseProps {
  variant: 'system'
}
type MessageBubbleProps = UserBubbleProps | AgentBubbleProps | SystemBubbleProps
```

宽度常量按内容类型区分：纯文本用 `AGENT_TEXT_WIDTH = 'max-w-[min(100%,38rem)]'`，结构化/长消息用 `AGENT_STRUCTURED_WIDTH = 'w-full max-w-[min(100%,46rem)]'`（由 `isStructured || isLong` 决定）。

- **user**：右对齐，`bg-primary/15` 背景 + `border-primary/25` 边框，圆角 `rounded-[15px]`（发送端 `rounded-br-[4px]`），气泡右侧紧贴管理员头像（`adminAvatarUrl`，加载失败回退 `/favicon.svg`）；气泡内嵌元素仍走 `primary-soft` / `primary-border` token（如 `[&_blockquote]:bg-primary-soft`、`[&_pre]:border-primary-border`）
- **agent**：左对齐 + AgentHoverCard（悬停展示 Agent 信息），顶部 Agent 名称与 agentType 标签（标签背景为 `${agentColor}1A`）。内容区分两种变体：**结构化**（`isStructured`，含 blocks）`border-border/60` + `bg-card/80` + `rounded-[12px]` + 阴影；**纯文本** `border-l-2 border-primary/30` + `bg-card/25` + `rounded-r-[10px]`。流式输出时显示闪烁光标 `▌`
- **system**：居中，小字 `text-muted-foreground`

### MessageRenderer (`src/components/chat/MessageRenderer.tsx`)

消息渲染编排组件，根据消息 `role` 选择渲染方式，统一管理 Agent 类型解析、头像查找和 Markdown 渲染：

```tsx
export function MessageRenderer({
  msg, isStreaming, avatarUrl, agentName, sessionId,
  sessionAgentType, agentSessionLookup, streamingAgentName,
}: MessageRendererProps) {
  if (msg.role === 'user') {
    return (
      <MessageBubble variant="user">
        <MarkdownRenderer content={msg.content} />
      </MessageBubble>
    )
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

输入框组件，支持两种模式：单栏模式（默认）和 Markdown 双栏预览模式。通过工具栏 "Markdown" 按钮切换。

单栏模式：textarea 自动高度（最小 48px，最大 200px），`Enter` 发送，`Shift+Enter` 换行。支持 IME 输入法组合状态检测（`compositionstart`/`compositionend`），组合输入中 `Enter` 不触发发送。

双栏模式：左栏 textarea 编辑（`Enter` 插入换行，不触发发送），右栏 `MarkdownRenderer` 实时预览（150ms 防抖）。支持滚动同步和自动高度调整（最小 120px，最大 60vh）。

```tsx
const adjustHeight = useCallback(() => {
  const el = textareaRef.current
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${Math.min(el.scrollHeight, 200)}px`
}, [])
```

两种模式均支持 @提及（`mentionSessions`）自动补全。发送后清空输入框并重置高度。流式输出时 `disabled`，按钮和输入框同时变为不可用。

### AgentAvatar (`src/components/chat/AgentAvatar.tsx`)

Agent 头像组件，圆角方块显示首字母或上传的头像图片。颜色映射通过 `AGENT_COLORS` 常量（来自 `lib/constants.ts`）和 CSS 变量：

```tsx
import { AGENT_COLORS, AGENT_NAMES } from '@/lib/constants'
// AGENT_COLORS 在 lib/constants.ts 中定义：
// { 'claude-code': 'var(--agent-claude)', opencode: 'var(--agent-opencode)', ... }
```

状态指示灯（右下角小圆点）使用 `STATUS_COLORS` 映射（定义在 `AgentAvatar.tsx` 本地常量，非 `lib/constants.ts`），`ready` 脉冲动画 `status-ready-pulse`，`running` 旋转动画 `status-running-spin`。支持自定义头像 URL，无自定义头像时若有 `agentName` 则通过 DiceBear API 生成 bottts 风格（机器人）头像（`https://api.dicebear.com/9.x/bottts/svg?seed=<agentName>`）。

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

群聊右侧边栏组件，可折叠、可拖拽调整宽度。宽度调整由父组件（`ImPage.tsx` 的 `ChatContent`）通过 `useResize({ storageKey: 'right-sidebar' })` hook 管理并以 props 传入：`width`（0 表示折叠）/ `isDragging` / `onResizeHandleMouseDown` / `onResizeHandleKeyDown` / `onExpand`。内部包含多个可折叠区块：`AgentInfoSection`、`MembersSection`、`AnnouncementsSection`、`HistorySearch`、`GitGraphPanel`（群聊）、`SidebarPathSection`。`useResize` 支持 localStorage 持久化宽度和折叠阈值（宽度低于阈值自动折叠）。

### MembersSection (`src/components/chat/MembersSection.tsx`)

群聊成员列表区块，显示群聊中的所有 Agent 成员（AgentHoverCard + 名称 + 状态）。使用 `useCollapsible` hook（从 RightSidebar 导出）管理折叠状态。

### AnnouncementsSection (`src/components/chat/AnnouncementsSection.tsx`)

群聊公告区块，支持展示、创建、删除和置顶公告。调用 `fetchAnnouncements`、`createAnnouncement`、`deleteAnnouncement` API 管理公告数据，公告列表按时间排序，置顶公告优先显示。

### HistorySearch (`src/components/chat/HistorySearch.tsx`)

消息历史搜索组件，支持关键词搜索和消息角色筛选（user/agent/system）。搜索结果高亮匹配片段，点击结果可跳转到对应消息。内部使用 `MESSAGE_ROLES` 常量进行角色筛选。

### git-graph-types (`src/components/chat/git-graph-types.ts`)

Git Graph 与 Terminal 共享的类型定义与常量模块。核心类型：

```typescript
interface GitCommit {
  hash: string
  fullHash?: string
  msg: string
  author: string
  lane: string
  time: string
  parentHashes?: string[]
}

interface GitBranchConfig {
  name: string
  color: string
  headHash?: string
  headMsg?: string
  headAuthor?: string
  headTime?: string
  exists?: boolean
}

interface GitGraphData {
  repoPath?: string
  commits: GitCommit[]
  branches: GitBranchConfig[]
  currentBranch: string
}

interface GitGraphPanelProps {
  data: GitGraphData
  currentBranch: string
  onBranchChange: (branch: string) => void
  branchLabels: Record<string, string>
}

interface TerminalPanelProps {
  currentBranch: string
  availableBranches: string[]
  gitGraphData: GitGraphData
  onBranchChange: (branch: string) => void
  branchLabels: Record<string, string>
}
```

还导出 `GIT_AUTHOR_COLORS`（作者→Agent 色映射）、`ROW_HEIGHT`（28）/ `LANE_WIDTH`（64）布局常量、`getBranchColor()` 分支颜色函数和 `buildBranchLabels()` 分支名→显示标签映射函数。

### GitGraphPanel (`src/components/chat/GitGraphPanel.tsx`)

群聊右侧栏 Git Graph 面板，使用 SVG 渲染分支拓扑图。可折叠（通过 `useCollapsible` hook）。Props 接收 `GitGraphPanelProps`：

```tsx
export function GitGraphPanel({
  data,            // GitGraphData：commits + branches
  currentBranch,   // 当前分支名
  onBranchChange,  // 分支切换回调
  branchLabels,    // 分支名→显示标签映射
}: GitGraphPanelProps)
```

渲染分三层：
1. **Branch labels** — 顶部显示所有分支标签（圆点 + 名称），当前分支高亮为 `bg-primary-soft`，点击触发 `onBranchChange`
2. **SVG graph** — 左侧固定宽度 64px 的 SVG 区域，绘制 lane rail（半透明竖线）、跨分支 bezier 连接线、commit 节点（当前 HEAD 为绿色大圆点），右侧显示 commit hash + message + branch badge + author dot + time
3. **Stats footer** — 底部统计 commits 数和 branches 数

鼠标悬停 commit 行时显示 tooltip（fixed 定位），展示完整 hash、message、author、lane、time。当前 HEAD commit 行高亮为 `bg-primary-soft/30`。

### TerminalPanel (`src/components/chat/TerminalPanel.tsx`)

群聊右侧栏终端面板，模拟命令行界面，支持 `git checkout`/`git switch` 真实分支切换。可折叠。Props 接收 `TerminalPanelProps`：

```tsx
export function TerminalPanel({
  currentBranch,      // 当前分支名
  availableBranches,  // 可用分支列表
  gitGraphData,       // GitGraphData（用于 git log 等命令）
  onBranchChange,     // 分支切换回调
  branchLabels,       // 分支名→显示标签映射
}: TerminalPanelProps)
```

渲染为仿 macOS 终端窗口：标题栏（红黄绿圆点 + 路径）、输出区域（`terminal-output` 类，`max-h-[200px]`）、输入行（分支名提示符 + 闪烁光标）。

支持命令：`help`、`clear`、`pwd`、`ls`、`whoami`、`git status`、`git log`、`git branch`、`git checkout <branch>`、`git switch <branch>`、`npm run build`、`npm test`、`echo`、`cat`。其中 `git checkout`/`git switch` 调用 `onBranchChange` 真实切换分支。输出使用 `dangerouslySetInnerHTML` + ANSI 风格 HTML span 着色，通过 `.terminal-output` CSS 类映射颜色变量。

---

### Markdown 渲染 (`components/markdown/`)

### MarkdownRenderer (`src/components/markdown/MarkdownRenderer.tsx`)

基于 `react-markdown` + `remark-gfm` 的渲染器，通过自定义 `components` 对象覆盖默认渲染。还内置 `fenceTreeBlocks` 预处理，自动检测树形结构文本（`│├└` 等）并包裹为 ` ```text ` 代码块：

```tsx
// 预处理：检测树形文本自动包裹为代码块
const processed = fenceTreeBlocks(content)

// components 覆盖约 20 种元素：
const components: Components = {
  // 标题：h1/h2/h3/h4，使用 --prose-heading / --prose-heading-h1 CSS 变量
  // 段落：p，mb-3 leading-7
  // 链接：a，--prose-link + 下划线 + target="_blank"
  // 引用块：blockquote，3px --prose-bq-border 左边框 + --prose-bq-bg 背景
  // 列表：ul/ol/li，list-disc/list-decimal + --prose-li-marker 颜色
  // 分隔线：hr，--prose-hr
  // 粗体/斜体：strong/em，--prose-bold / text-secondary
  // 图片：img，圆角 + 边框
  // 行内代码/代码块：code/pre，带 \n 检测委托 CodeBlock，否则 inline 样式
  // 表格：table/th/td，圆角外框 + 半透明表头
  pre({ children }) {
    return <>{children}</>
  },
  code({ className, children, ...props }) {
    const match = /language-(\w+)/.exec(className ?? '')
    const code = String(children).replace(/\n$/, '')
    if (match) {
      return <CodeBlock code={code} language={match[1]} />
    }
    if (code.includes('\n')) {
      return <CodeBlock code={code} />
    }
    return (
      <code className="inline rounded-md bg-[var(--prose-code-bg)] px-1.5 py-0.5 text-[13px] text-[var(--prose-code-text)]"
        style={{ fontFamily: "'Geist Mono', monospace", letterSpacing: 0 }}
        {...props}>
        {children}
      </code>
    )
  },
  // ... 其他覆盖元素
}
```

外层使用 `prose dark:prose-invert` 类 + `@tailwindcss/typography` 插件提供基础排版，CSS 变量覆盖暗色主题配色。代码块（带 `language-` 前缀）委托给 `CodeBlock`，无语言标记但含换行的代码也委托给 `CodeBlock`，行内代码使用 `--prose-code-bg` + `--prose-code-text` 变量。

### CodeBlock (`src/components/markdown/CodeBlock.tsx`)

代码高亮组件，使用 `@shikijs/core` 构建常驻 highlighter 实例（`tokyo-night` 主题），按需动态 import 语言包。高亮完成前 fallback 到纯文本 + 行号显示：

```tsx
let highlighterPromise: Promise<SyntaxHighlighter> | undefined

function getHighlighter(): Promise<SyntaxHighlighter> {
  highlighterPromise ??= Promise.all([
    import('@shikijs/core'),
    import('@shikijs/engine-javascript'),
    import('@shikijs/themes/tokyo-night'),
    import('@shikijs/langs/javascript'),
    // ...其余语言包动态 import
  ]).then(([{ createHighlighterCore }, { createJavaScriptRegexEngine }, theme, ...languages]) =>
    createHighlighterCore({
      themes: [theme],
      langs: languages,
      engine: createJavaScriptRegexEngine(),
    })
  ).then((h) => ({
    codeToHtml: (code: string, options: { lang: string; theme: string }) =>
      h.codeToHtml(code, options),
  }))
}

useEffect(() => {
  let cancelled = false
  if (language) {
    getHighlighter().then((h) => {
      try {
        const html = h.codeToHtml(code, { lang: language, theme: 'tokyo-night' })
        if (!cancelled) setHtml(html)
      } catch {
        // language not supported — fallback to plain text
      }
    })
  }
  return () => { cancelled = true }
}, [code, language])
```

highlighter 实例全局单例（`highlighterPromise` 缓存，首次代码块触发懒加载后复用），通过 `cancelled` 标志避免组件卸载后的 state 更新。语言名经 `LANGUAGE_ALIASES` 归一化并校验是否在 `SUPPORTED_LANGUAGES` 白名单内，否则降级为 `text`。代码块可横向滚动，字体 `Geist Mono`，字号 13px，行高 1.65。
