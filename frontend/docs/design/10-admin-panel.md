# Admin Panel — 管理面板

## 实现了什么

7 模块管理面板，通过 IconSidebar 的 `admin` Tab 进入。进入前需密码验证（JWT），验证后可访问总览仪表盘、会话清理、工作区管理、Agent 概览、服务健康、数据统计、用户管理七个管理页面。

## 怎么实现的

### 布局切换 (`src/pages/ImPage.tsx`)

`ImPage` 通过 `useActiveTab()` 读取 `activeTab` 状态，当值为 `'admin'` 时，中栏切换为 `AdminMenu`（管理菜单），右栏切换为 `AdminContent`（管理页面内容）：

```tsx
{activeTab === 'admin' ? (
  <>
    <AdminMenu />
    <div className="flex-1 overflow-auto">
      <AdminContent />
    </div>
  </>
) : ...}
```

`AdminContent` 检查 `isAuthenticated`，未认证时弹出 `AdminPasswordDialog`，已认证时根据 `activeMenuKey` 渲染对应页面组件。

### Admin Store (`src/stores/admin.ts`)

独立 Zustand store 管理管理面板的认证和菜单状态：

```typescript
export type AdminMenuKey = 'dashboard' | 'sessions' | 'workspaces' | 'agents' | 'services' | 'statistics'

interface AdminStore {
  activeMenuKey: AdminMenuKey
  adminToken: string | null
  isAuthenticated: boolean
  showPasswordDialog: boolean
  passwordDialogPurpose: 'login' | 'reauth'
  // actions...
}
```

暴露两个选择器 hook：`useAdminAuth()`（认证状态）和 `useAdminMenu()`（菜单选择）。`setAdminToken` 同步写入 API 层的 token。

### IconSidebar (`src/components/layout/IconSidebar.tsx`)

56px 宽图标导航栏，最左列。顶部显示用户头像（DiceBear）+ 在线状态灯，中间是 NavTab 切换按钮（聊天/通讯录/管理/设置），底部是设置按钮。通过 `useActiveTab()` 读取和切换导航状态。

### AdminMenu (`src/components/layout/AdminMenu.tsx`)

180px 宽管理菜单，替换聊天模式下的 `ConversationList`。6 个菜单项：总览仪表盘、会话清理、工作区管理、Agent 概览、服务健康、数据统计。选中项使用 `var(--primary-soft)` 背景 + `var(--color-brand)` 文字色。

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

所有管理页面通过 `getAdminXxx` 系列 API 获取数据，使用组件内 `useState` + `useEffect` 管理（不走 React Query）。

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
