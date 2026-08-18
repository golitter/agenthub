# Admin Panel — 管理面板

## 实现了什么

7 模块管理面板，通过 `IconSidebar` 的 `admin` NavLink（跳转到 `/admin/...` 路由）进入。进入前需密码验证（JWT），验证后可访问总览仪表盘、会话清理、工作区管理、Agent 概览、服务健康、数据统计、用户管理七个管理页面。AdminMenu 侧栏包含 7 个导航项。

## 怎么实现的

### 布局切换 (`src/pages/ImPage.tsx`)

`ImPage` 内部用嵌套路由处理：访问 `/admin/:section` 时渲染 `AdminRoute`（`AdminMenu` + `<AdminContent />`）。菜单项的 `key` 直接来自 URL `:section` 参数，而非 store 状态：

```tsx
<Route path="admin" element={<Navigate to="/admin/dashboard" replace />} />
<Route path="admin/:section" element={<AdminRoute />} />
```

`AdminRoute` 渲染左栏 `AdminMenu` + 右栏 `<AdminContent />`（外层 `<ErrorBoundary>`，以 `section` 为 key 保证切换页面时重置）。`AdminContent` 检查 `useAdminStore` 的 `isAuthenticated`，未认证时调用 `showLoginDialog()`（由常驻的 `AdminPasswordDialog` 接管），已认证时先在顶部渲染 `AdminMobileNav`（移动端 `<select>` 下拉，`sm:hidden`，桌面端用 `AdminMenu`），再根据 `useParams<{ section }>()` 在 `ADMIN_PAGES` 映射表中选出对应页面组件（非法值回退到 `dashboard`）。

### Admin Store (`src/stores/admin.ts`)

独立 Zustand store 管理管理面板的**认证状态**与头像（菜单选择由 URL `:section` 负责，不进 store）：

```typescript
export type AdminMenuKey = 'dashboard' | 'sessions' | 'workspaces' | 'agents' | 'services' | 'statistics' | 'users'

interface AdminStore {
  adminToken: string | null
  isAuthenticated: boolean
  showPasswordDialog: boolean
  passwordDialogPurpose: 'login' | 'reauth'
  adminAvatarUrl: string
  setAdminToken: (token: string | null, expiresInSeconds?: number) => void
  setIsAuthenticated: (val: boolean) => void
  showLoginDialog: () => void
  showReauthDialog: () => void
  hidePasswordDialog: () => void
  logout: () => void
  setAdminAvatarUrl: (url: string) => void
}
```

暴露选择器 hook `useAdminAuth()`（认证状态 + `setAdminToken` + `logout`）。`setAdminToken` 同步写入 API 层的 token（`setAdminToken` from `lib/api.ts`）。`adminAvatarUrl` 默认使用 DiceBear 生成的头像（`https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede`），运行时由 `useAdminAvatar` hook（`src/hooks/use-admin.ts`）走 React Query 共享 `['admin-avatar']` 缓存统一获取与刷新（`IconSidebar` / `AdminMenu` / `UserManagementPage` 均复用此 hook，避免头像多次重复请求）。不存在 `useAdminMenu()` —— 菜单选中态由 `AdminMenu` 的 `NavLink` 自身根据当前 URL 判断。

### IconSidebar (`src/components/layout/IconSidebar.tsx`)

56px 宽图标导航栏，最左列。顶部显示用户头像（DiceBear）+ 在线状态灯，中间是 4 个 `NavLink` 路由按钮（聊天 / 通讯录 / Skills / 管理，分别指向 `/chat`、`/contacts`、`/skills`、`/admin`），底部是设置 `Popover`（内嵌 `SettingsPanel`）和 GitHub 链接。激活态由 React Router 的 `NavLink` 根据 URL 自动判断（`bg-primary-soft text-primary`）。

### AdminMenu (`src/components/layout/AdminMenu.tsx`)

180px 宽管理菜单（`hidden ... sm:flex`，移动端隐藏），在 `/admin/:section` 路由下替换聊天模式下的 `ConversationList`。7 个菜单项（与 `AdminMenuKey` 一一对应）：总览仪表盘、会话清理、工作区管理、Agent 概览、服务健康、数据统计、用户管理。每项为 `<NavLink to={/admin/${key}}>`，选中态由 URL 驱动：`bg-primary-soft text-brand`，非选中 `text-text-secondary hover:bg-bg-hover`。顶部同样展示管理员头像（`useAdminStore` + `getAdminAvatar`）。

### AdminPasswordDialog (`src/components/layout/AdminPasswordDialog.tsx`)

shadcn Dialog 弹窗，支持两种用途：首次进入管理面板的登录验证（`purpose: 'login'`）和敏感操作的二次确认（`purpose: 'reauth'`）。调用 `adminAuth(password)` API 获取 JWT token。

### 管理页面 (`src/pages/admin/`)

| 页面 | 文件 | 功能 |
|------|------|------|
| 总览仪表盘 | `DashboardPage.tsx` | 磁盘/内存/Redis 用量进度条，颜色按阈值变化（>80% 红、>60% 黄） |
| 会话清理 | `SessionCleanupPage.tsx` | 会话列表 + Agent 类型筛选 + 批量勾选删除 |
| 工作区管理 | `WorkspacePage.tsx` | 工作区列表 + 删除操作 |
| Agent 概览 | `AgentOverviewPage.tsx` | Agent 列表与状态 |
| 服务健康 | `ServiceHealthPage.tsx` | 后端/Agent 端服务状态监控 |
| 数据统计 | `StatisticsPage.tsx` | 系统运行统计 |
| 用户管理 | `UserManagementPage.tsx` | 管理员头像上传与更新 |

所有管理页面通过 `getAdminXxx` 系列 API 获取数据，统一使用 TanStack React Query 的 `useQuery` / `useMutation` 管理请求状态、缓存与失效（每个页面以独立的 queryKey 缓存）。

### Admin API (`src/lib/api.ts`)

管理 API 使用 JWT token 认证，`setAdminToken` 在请求头中注入 token：

| 函数 | 方法 | 路径 | 说明 |
|------|------|------|------|
| `adminAuth` | POST | `/api/admin/auth` | 密码验证，返回 JWT token |
| `getAdminResources` | GET | `/api/admin/resources` | 磁盘/内存/Redis 用量 |
| `deleteAdminSessions` | DELETE | `/api/admin/sessions` | 批量删除会话 |
| `getAdminWorkspaces` | GET | `/api/admin/workspaces` | 工作区列表 |
| `deleteAdminWorkspace` | DELETE | `/api/admin/workspaces/:id` | 删除工作区 |
| `getAdminAgents` | GET | `/api/admin/agents` | Agent 列表 |
| `getAdminServices` | GET | `/api/admin/services` | 服务健康状态 |
| `getAdminStatistics` | GET | `/api/admin/statistics` | 统计数据 |
| `getAdminAvatar` | GET | `/api/admin/avatar` | 获取管理面板头像 |
| `updateAdminAvatar` | PUT | `/api/admin/avatar` | 更新管理面板头像 |
