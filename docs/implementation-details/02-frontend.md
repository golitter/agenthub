# Frontend 超详细实现

## 实现了什么

Frontend 是 React 19 + TypeScript 6 + Vite 8 的单页应用。它提供聊天、通讯录、SkillsHub、Agent 详情、管理后台、流式消息卡片、Diff 编辑与 Workspace 操作。服务端状态由 TanStack React Query 管理，瞬时会话流状态由 Zustand 管理，页面选择由 React Router 管理，三者分工明确。

## 怎么实现的

### 入口与 Provider

`frontend/src/main.tsx` 在 `StrictMode` 中依次挂载：

- `QueryClientProvider`：查询默认 `staleTime=30s`、`retry=1`、关闭窗口聚焦重取；mutation 不重试，防止重复写。
- `BrowserRouter`：URL 是页面级导航事实来源。
- `ThemeSync`：在根部保持 system/light/dark 监听生效。
- 顶层 `Suspense`：懒加载 `AgentProfilePage` 和 `ImPage`，fallback 使用完整应用骨架。

顶层路由只有 `/agent/:sessionId` 和 `/*`。主页面 `ImPage` 再定义 `/chat`、`/contacts`、`/skills`、`/admin/:section`，根路径重定向到 `/chat`，非法路径进入 NotFound。

### 布局与响应式行为

桌面端外层 Grid 左侧固定 56px `IconSidebar`，右侧是路由内容。聊天页内部再分：

- 中屏：280px ConversationList + 自适应 ChatArea。
- 超宽屏：追加可折叠、可拖拽的 RightSidebar。
- 小屏：IconSidebar 变成底部导航；选中会话后列表隐藏；详情侧栏通过 Sheet 打开。

`useResize` 保存右栏宽度到 localStorage，并处理折叠阈值。Sheet 关闭后恢复触发按钮焦点。页面提供跳转到 `main-content` 的键盘可达链接。路由级页面使用 lazy + Suspense + ErrorBoundary 隔离加载失败和渲染失败。

### 路由到组件映射

| 路由 | 组件 | 数据来源 |
|---|---|---|
| `/chat` | `ChatContent` | tasks、sessions、messages、SSE |
| `/contacts` | `ContactsPage` | contact group API |
| `/skills` | `SkillsHubPage` | skills API |
| `/agent/:sessionId` | `AgentProfilePage` | profile/detail/soul/skills API |
| `/admin/dashboard` | `DashboardPage` | resources |
| `/admin/sessions` | `SessionCleanupPage` | tasks 聚合 + delete sessions |
| `/admin/workspaces` | `WorkspacePage` | AgentEnd workspace 代理 |
| `/admin/agents` | `AgentOverviewPage` | agents/configs |
| `/admin/services` | `ServiceHealthPage` | Backend/AgentEnd health |
| `/admin/statistics` | `StatisticsPage` | 统计 API |
| `/admin/users` | `UserManagementPage` | 管理头像 |

### 状态分工

| 状态类型 | 实现 | 示例 |
|---|---|---|
| URL 状态 | React Router | 当前页面、admin section、agent profile、`?session=` |
| 服务端状态 | React Query | tasks、messages、skills、announcements、admin resources |
| 本地流状态 | Zustand | streaming message、runtime blocks、SSE 状态 |
| 跨刷新偏好 | localStorage | 当前 session、主题、右栏宽度、管理 token |
| 组件瞬时状态 | React state | Dialog、输入内容、选中文件、hover |

`stores/chat.ts` 是 barrel export，不是一个巨型 store。它组合：

- `navigation-store.ts`：`currentSessionId` 与导航动作。
- `session-store.ts`：以 Session ID 为 key 的 `Map`，每个会话保存 messages、streaming、runtime blocks 等。
- `message-store.ts`：消息追加、流式文本更新、结构化块、运行状态和公告相关动作。

这种拆分避免切换会话时清空另一个会话正在流式接收的数据。React Query 仍保存服务端已确认列表；SSE 到达时通过 query-key helpers 做局部 patch/upsert，最终重新对账。

### Conversation 聚合

`use-conversations.ts` 从 Task 与 Session 生成 UI Conversation：

- 单 Agent Task 显示为单聊。
- 多 Session Task 聚合成一个群聊，不在左栏重复显示成员。
- 群聊优先取 Orchestrator Session 为主 Session。
- 排序先按 `pinnedAt` 倒序，再按 `lastActiveAt` 倒序。
- 创建多 Agent 会话时，如果没有 Orchestrator，前端自动加入名为“编排器”的 Orchestrator。

当前 Session 的恢复顺序是 URL search param → localStorage → 空。切换会话使用 replace 更新 URL，避免每次点击都污染浏览器后退历史。

### REST 客户端

`frontend/src/lib/api.ts` 统一以 `API_BASE='/api'` 发起请求。关键职责包括：

- 将非 2xx 响应转为可展示错误。
- 对 task/session/message/path 参数使用 `encodeURIComponent` 或逐段 path 编码。
- 为 admin 请求附加保存的 JWT。
- 将 Backend 的 Task/Session 结构转换成前端 Conversation。
- 为消息分页构造 `limit`、`before`、`session_id`、`mode`、`primary_session_id`。
- 对 run、review、conflict action 使用明确请求类型。
- 对头像和 Skill 使用 multipart/form-data，而不是 JSON。

Vite 开发代理把 `/api` 和兼容的 `/uploads` 转发到 Backend，默认 `127.0.0.1:8080`；`VITE_API_PROXY_TARGET` 优先于 `API_PROXY_TARGET`。

### SSE 客户端

`lib/sse.ts` 封装原生 `EventSource`：

- URL 包含 task、session 和 message 标识，防止消费到另一条消息流。
- 为契约中的每个事件名注册 listener，而不仅依赖默认 `message` 事件。
- JSON 解析失败走错误回调，不把坏数据写入 store。
- `close()` 主动释放连接；原生 EventSource 在允许时自动重连。

`use-chat-stream.ts` 再解决 React 生命周期问题：

- 为每次连接保留 generation/active guard，忽略 close 后队列中迟到的事件。
- init 事件建立消息与 sequence 基线。
- text/runtime_text 追加增量内容。
- tool、planning、coordination、ask-card、artifact 等转换为 runtime block。
- done/error 结束 streaming，更新消息终态并触发服务端缓存对账。
- 组件卸载、Session 切换或新一轮运行时关闭旧连接。

### 消息块归约与渲染

`block-types.ts` 定义判别联合类型，涵盖 text、html-render、image、attachment、diff、preview、plan、plan_review、runtime_status、coordination、ask_agent、task_failure、final_summary、tool_call、tool_result。

`block-reducer.ts` 接收两类输入：

1. 普通流式文本，连续合并为 text block。
2. `aka_yhy` fenced block 或结构化 RuntimeEvent，解析成强类型卡片。

归约器必须支持未闭合 fenced block，因为 token streaming 期间结尾可能尚未到达。完成后 `BlockRenderer` 按 `type` 选择组件；未知或解析失败内容保持为文本，避免整条消息丢失。

### Markdown 与代码

`MarkdownRenderer` 使用 `react-markdown` + `remark-gfm` 支持表格、任务列表等 GFM。代码块由 `CodeBlock` 交给 Shiki JavaScript engine 高亮，并提供行号/复制等 UI。用户输入的 Markdown 预览和 Agent 消息共用语义，但预览不触发 Artifact 或 Workspace 副作用。

### Diff 工作流

`DiffCard` 的数据有两个来源：持久化 `snapshot_id` 或当前 Session Workspace diff。组件流程为：

1. 取 snapshot；没有可用内容时向 `/api/session/:sessionId/diff` 请求实时 diff。
2. `diff-parser.ts` 将 unified diff 拆成文件并计算增删统计。
3. `DiffFileTabs` 切换文件，`DiffFileView` 用 `react-diff-view` 展示。
4. 编辑模式懒加载 CodeMirror，根据扩展名选择语言包。
5. 保存文件调用 Session 文件代理；接受调用 commit，撤销调用 revert。
6. 操作后 PUT snapshot，记录 diff 与 pending/accepted/reverted 状态。

文件路径通过 `encodePathSegments` 逐段编码，既保留目录分隔符，又不会把空格、`#` 等字符误解释成 URL 结构。

### Artifact 与资源卡片

- `HtmlCard` 对新 Artifact 使用 `/api/artifacts/:resourceId/content`，iframe 启用 sandbox；旧路径仍可兼容直接 HTML。
- `ImageCard` 和 `AttachmentCard` 通过 `/api/session/:sessionId/files/:encodedPath` 代理读取 Worktree 文件，其中实际文件路径按段编码。
- `PreviewCard` 展示由 AgentEnd preview server 返回的 URL。
- `ToolCard` 将工具名、输入、结果和状态分区，超大原始负载已在 AgentEnd sanitizer 裁剪。
- `PlanReviewCard` 调 Backend review API，操作由 approve/discuss/modify 表达。
- `RuntimeStatus`、`CoordChannel`、`TaskFailureCard`、`FinalSummaryCard` 负责 Orchestrator 生命周期可视化。

### 管理认证

`stores/admin.ts` 保存认证状态、token、登录 Dialog 和管理头像。`AdminPasswordDialog` 调 `/api/admin/auth` 获取 token。普通页面可访问公开 admin health/avatar；受保护管理 API 通过 `adminFetch` 附加 Bearer token。UI 权限只改善体验，真正权限由 Backend `AdminAuth` 中间件执行。

### 样式与主题

`index.css` 使用 Tailwind CSS 4 与 shadcn 语义 token。`use-theme.ts` 保存 `system|light|dark` 偏好，并把 system 解析为当前实际主题；监听 `prefers-color-scheme` 和跨组件事件。Geist Variable 是主字体。组件使用 `cn()` 合并 clsx 与 tailwind-merge，减少冲突类。

### 前端依赖边界

| 能力 | 依赖 |
|---|---|
| UI 框架 | React 19.2、React DOM 19.2 |
| 路由 | React Router 7.15 |
| 服务端缓存 | TanStack React Query 5.100 |
| 本地状态 | Zustand 5.0 |
| 样式 | Tailwind CSS 4.3、Radix、shadcn、Lucide |
| Markdown | react-markdown 10、remark-gfm 4、Shiki 4 |
| Diff/编辑 | react-diff-view、CodeMirror 6 |
| 大列表 | TanStack Virtual 3.13 |
| 构建与测试 | Vite 8、TypeScript 6、Vitest 4、ESLint 10、Prettier 3 |

### 修改时必须同步的地方

- 新增事件：contracts schema → generate → `lib/sse.ts` listener → `use-chat-stream.ts` → block type/reducer/renderer → tests。
- 新增服务端查询：`lib/api.ts` → query keys → hook → loading/error/empty UI。
- 新增路由：`main.tsx` 或 `ImPage.tsx` → IconSidebar/AdminMenu → page title → lazy/error boundary。
- 新增卡片：block union → reducer → component → renderer → streaming/历史消息双路径测试。
- 修改消息标识：同时检查 Query cache、Zustand session map、EventSource URL 和 Backend pagination。
