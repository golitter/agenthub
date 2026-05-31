# Architecture — 架构与目录结构

## 实现了什么

基于 React 19 + Vite 8 的单页应用，采用 QQ 风格三栏布局。最左为图标导航栏（IconSidebar），中栏为对话列表或管理菜单（根据 NavTab 切换），右栏为聊天区或管理页面。使用 Zustand 管理聊天导航与会话状态，TanStack React Query 管理服务端数据缓存。

## 怎么实现的

### 应用入口 (`src/main.tsx`)

顶层挂载 `StrictMode` + `QueryClientProvider` + `BrowserRouter`，定义两条路由：Agent 详情页 + IM 主页 catch-all：

```tsx
const queryClient = new QueryClient()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/agent/:sessionId" element={<AgentProfilePage />} />
          <Route path="/*" element={<ImPage />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
```

### 主页面 (`src/pages/ImPage.tsx`)

三栏布局编排：`IconSidebar` 56px 图标导航栏 + 中栏（`ConversationList` 或 `AdminMenu`，根据 `activeTab` 切换）+ 右栏（`ChatArea` 或管理页面）。通过 `useActiveTab()` 读取当前导航 Tab，`useConversations()` 获取对话列表，`useChatNav()` 读取当前选中会话：

```tsx
export function ImPage() {
  const { data: conversations } = useConversations()
  const { currentSessionId } = useChatNav()
  const { activeTab } = useActiveTab()

  const active = conversations?.find((c) => c.sessionId === currentSessionId)

  return (
    <div className="flex h-screen bg-background">
      <IconSidebar />
      <AdminPasswordDialog />

      {activeTab === 'chat' ? (
        <>
          <ConversationList />
          <div className="flex-1">
            {active ? <ChatArea ... /> : <EmptyState />}
          </div>
        </>
      ) : activeTab === 'admin' ? (
        <>
          <AdminMenu />
          <div className="flex-1 overflow-auto">
            <AdminContent />
          </div>
        </>
      ) : <PlaceholderPage />}
    </div>
  )
}
```

NavTab 类型：`'chat' | 'contacts' | 'admin' | 'settings'`，其中 `contacts` 和 `settings` 为占位状态。`admin` Tab 进入时需先通过密码验证（`AdminPasswordDialog`），验证后根据 `activeMenuKey` 渲染对应管理页面。

### 构建配置 (`vite.config.ts`)

Vite 插件链为 `@vitejs/plugin-react` + `@tailwindcss/vite`，路径别名 `@` 指向 `./src`，开发代理将 `/api` 转发到后端 Go 服务：

```ts
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
```

### 目录结构

```
src/
├── main.tsx                          # 应用入口：StrictMode + QueryClient + BrowserRouter
├── index.css                         # 全局样式：Tailwind + CSS 变量暗色主题
│
├── pages/
│   ├── ImPage.tsx                    # 主页面：三栏布局编排（IconSidebar + 中栏 + 右栏）+ NavTab 路由
│   ├── AgentProfilePage.tsx          # Agent 详情页：头像/名称内联编辑 + 元数据 + Skills
│   └── admin/                        # 管理面板页面（7 模块）
│       ├── DashboardPage.tsx         #   总览仪表盘（磁盘/内存/Redis 用量）
│       ├── SessionCleanupPage.tsx    #   会话清理（批量删除 + 筛选）
│       ├── WorkspacePage.tsx         #   工作区管理
│       ├── AgentOverviewPage.tsx     #   Agent 概览
│       ├── ServiceHealthPage.tsx     #   服务健康
│       ├── StatisticsPage.tsx        #   数据统计
│       └── UserManagementPage.tsx    #   用户管理（头像上传 + 更新）
│
├── components/
│   ├── im/                           # 对话列表侧栏
│   │   ├── ConversationList.tsx      # 侧栏容器：Header + 搜索 + 列表 + 新建弹窗
│   │   ├── ConversationItem.tsx      # 单条对话：头像 + 名称 + 时间
│   │   ├── NewChatDialog.tsx         # 新建对话弹窗：仓库路径校验 + 选择 Agent 类型
│   │   ├── AgentSelectList.tsx       # Agent 选择列表（多选 + 搜索过滤）
│   │   └── RepoPathInput.tsx         # 仓库路径输入（校验 + 状态管理）
│   │
│   ├── chat/                         # 聊天区
│   │   ├── ChatArea.tsx              # 聊天容器：Header + 消息列表 + 输入框
│   │   ├── MessageList.tsx           # 消息列表（支持虚拟滚动 + 向上翻页加载）
│   │   ├── MessageBubble.tsx         # 消息气泡（user / agent / system 三种变体）
│   │   ├── MessageRenderer.tsx       # 消息渲染器（消息气泡 + Markdown + 卡片编排）
│   │   ├── MessageInput.tsx          # 输入框（自动高度 + Enter 发送）
│   │   ├── AgentAvatar.tsx           # Agent 头像（颜色 + 状态指示灯 + DiceBear initials）
│   │   ├── GroupAvatar.tsx           # 群聊头像（多 Agent 头像叠加）
│   │   ├── AgentHoverCard.tsx        # Agent 悬停卡片（Popover + 技能预览 + 跳转详情页）
│   │   ├── AgentEditDialog.tsx       # Agent 编辑弹窗（修改名称 + 上传头像）
│   │   ├── AgentMeta.tsx             # Agent 元数据网格（Session ID / Task ID / Repo Path 等）
│   │   ├── AskAgentCard.tsx          # 跨 Agent 提问卡片（源 → 目标 Agent + 问答展示）
│   │   ├── SkillCard.tsx             # Agent 技能卡片（名称 + 描述 + builtin 标记）
│   │   └── TimeDivider.tsx           # 时间分隔线（相对时间 + 分隔线）
│   │
│   ├── cards/                        # 技能输出卡片（Artifact 渲染）
│   │   ├── DiffCard.tsx              # Diff 卡片：多文件 tab + accept/revert + 编辑
│   │   ├── HtmlCard.tsx              # HTML 渲染卡片（sandbox iframe）
│   │   ├── ImageCard.tsx             # 图片卡片（代理下载 + 错误降级）
│   │   ├── AttachmentCard.tsx        # 附件卡片（文件图标 + 下载链接）
│   │   ├── PreviewCard.tsx           # 预览卡片（外部链接 + iframe）
│   │   ├── PlanCard.tsx              # 计划卡片（多 Agent 任务计划展示）
│   │   ├── RuntimeStatus.tsx         # 运行时状态卡片（Agent 执行状态 + streaming 文本）
│   │   ├── CoordChannel.tsx          # 协调通道卡片（多 Agent 协作消息流）
│   │   ├── FinalSummaryCard.tsx      # 最终汇总卡片（Orchestrator 执行结果 + 任务概览）
│   │   ├── TaskFailureCard.tsx       # 任务失败卡片（超时/错误 + 原因展示）
│   │   ├── ToolCard.tsx              # 工具调用卡片（tool_call / tool_result 展示）
│   │   └── index.ts                  # 统一导出
│   │
│   ├── diff/                         # Diff 查看器（可编辑多文件）
│   │   ├── DiffHeader.tsx            # Diff 卡片头部（文件信息 + 操作按钮）
│   │   ├── DiffFileInfo.tsx          # 文件变更统计信息
│   │   ├── DiffFileTabs.tsx          # 多文件 tab 切换
│   │   ├── DiffFileView.tsx          # react-diff-view 统一视图渲染
│   │   ├── DiffFileEditor.tsx        # 懒加载编辑器外壳（Suspense）
│   │   └── DiffFileEditorInner.tsx   # CodeMirror 编辑器（语法高亮 + 保存）
│   │
│   ├── layout/                       # 布局组件
│   │   ├── IconSidebar.tsx           # 图标导航栏（56px 左栏：用户头像 + NavTab 切换）
│   │   ├── AdminMenu.tsx             # 管理面板侧边菜单（6 模块导航）
│   │   └── AdminPasswordDialog.tsx   # 管理员密码验证弹窗（登录 + 敏感操作二次确认）
│   │
│   ├── markdown/                     # Markdown 渲染
│   │   ├── MarkdownRenderer.tsx      # react-markdown + remark-gfm + 自定义组件
│   │   └── CodeBlock.tsx             # 代码块（Shiki 高亮 + 行号）
│   │
│   └── ui/                           # shadcn/ui 基础组件
│       ├── dialog.tsx
│       ├── error-boundary.tsx
│       └── popover.tsx
│
├── hooks/
│   ├── use-chat-stream.ts            # 聊天流：SSE 连接 + store actions 驱动状态
│   ├── use-conversations.ts          # 对话列表查询 + 新建 mutation
│   ├── use-hover-style.ts            # 悬停样式工具 hook
│   └── use-message-scroll.ts         # 消息滚动控制（自动滚底 + 向上翻页加载）
│
├── lib/
│   ├── api.ts                        # REST API 封装（含 cursor 分页 getTaskMessages）
│   ├── sse.ts                        # SSE 客户端（EventSource 封装）
│   ├── constants.ts                  # 常量定义（AGENT_NAMES / AGENT_DESCRIPTIONS）
│   ├── utils.ts                      # cn() 工具函数
│   ├── block-types.ts                # MessageBlock 联合类型（text/html-render/image/attachment/diff/preview/plan/runtime_status/coordination/ask_agent/task_failure/final_summary/tool_call/tool_result）
│   ├── block-reducer.ts              # 事件文本 → MessageBlock[] 解析器（aka_yhy 标记协议）
│   ├── diff-parser.ts                # Unified Diff 解析器（react-diff-view 封装 + 统计）
│   └── __tests__/                    # lib 单元测试
│       └── block-reducer.test.ts
│
├── stores/
│   ├── chat.ts                       # Zustand Store：聊天导航 + 各会话独立状态（含分页）+ NavTab
│   └── admin.ts                      # Zustand Store：管理面板认证状态 + 菜单选择
│
├── utils/
│   └── time.ts                       # 时间工具（formatRelativeTime + shouldShowTimeSeparator）
│
└── generated/                        # 契约生成的类型文件（勿手改）
    ├── events.ts                     # StreamEvent 类型
    ├── message.ts                    # 消息类型
    ├── request.ts                    # AgentType 等请求类型
    ├── response.ts                   # AgentResponse 类型
    ├── session.ts                    # SessionState 类型
    └── validate-repo-path.ts         # 仓库路径校验请求类型
```

### 依赖总览

| 类别 | 库 | 用途 |
|------|------|------|
| 框架 | React 19 + React Router 7 | UI 渲染 + 路由 |
| 样式 | Tailwind CSS 4 + shadcn/ui | 原子化 CSS + 组件库 |
| 状态 | Zustand | 全局轻量状态 |
| 数据 | TanStack React Query | 服务端状态管理 |
| Markdown | react-markdown + remark-gfm | Markdown 渲染 |
| 代码高亮 | Shiki | VS Code 级别语法高亮 |
| 虚拟滚动 | @tanstack/react-virtual | 大量消息时的性能优化 |
| Diff 渲染 | react-diff-view | Unified Diff 视图渲染 |
| 代码编辑 | @uiw/react-codemirror + @codemirror/* | Diff 文件编辑器（语法高亮 + 懒加载） |
| 图标 | Lucide React | 统一图标系统 |
| 字体 | @fontsource-variable/geist | Geist Variable 字体 |
| 无障碍原语 | radix-ui + @radix-ui/react-dialog | shadcn/ui 底层 Popover / Dialog 原语 |
