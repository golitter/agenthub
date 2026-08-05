import { LayoutDashboard, MessageSquare } from 'lucide-react'
import { lazy, Suspense, useEffect, useLayoutEffect } from 'react'
import { Link, Navigate, Route, Routes, useNavigate, useParams, useSearchParams } from 'react-router'

import { ChatArea } from '@/components/chat/ChatArea'
import { RightSidebar } from '@/components/chat/RightSidebar'
import { ConversationList } from '@/components/im/ConversationList'
import { AdminMenu } from '@/components/layout/AdminMenu'
import { AdminPasswordDialog } from '@/components/layout/AdminPasswordDialog'
import { IconSidebar } from '@/components/layout/IconSidebar'
import { ErrorBoundary } from '@/components/ui/error-boundary'
import { useConversations } from '@/hooks/use-conversations'
import { useResize } from '@/hooks/use-resize'
import { UI_LABELS, UI_MESSAGES } from '@/lib/ui-text'
import type { AdminMenuKey } from '@/stores/admin'
import { useAdminStore } from '@/stores/admin'
import { useChatNav } from '@/stores/chat'

const ContactsPage = lazy(() =>
  import('@/components/im/ContactsPage').then((module) => ({ default: module.ContactsPage })),
)
const SkillsHubPage = lazy(() =>
  import('@/pages/SkillsHubPage').then((module) => ({ default: module.SkillsHubPage })),
)
const DashboardPage = lazy(() =>
  import('@/pages/admin/DashboardPage').then((module) => ({ default: module.DashboardPage })),
)
const SessionCleanupPage = lazy(() =>
  import('@/pages/admin/SessionCleanupPage').then((module) => ({
    default: module.SessionCleanupPage,
  })),
)
const WorkspacePage = lazy(() =>
  import('@/pages/admin/WorkspacePage').then((module) => ({ default: module.WorkspacePage })),
)
const AgentOverviewPage = lazy(() =>
  import('@/pages/admin/AgentOverviewPage').then((module) => ({
    default: module.AgentOverviewPage,
  })),
)
const ServiceHealthPage = lazy(() =>
  import('@/pages/admin/ServiceHealthPage').then((module) => ({
    default: module.ServiceHealthPage,
  })),
)
const StatisticsPage = lazy(() =>
  import('@/pages/admin/StatisticsPage').then((module) => ({ default: module.StatisticsPage })),
)
const UserManagementPage = lazy(() =>
  import('@/pages/admin/UserManagementPage').then((module) => ({
    default: module.UserManagementPage,
  })),
)

const ADMIN_PAGES: Record<AdminMenuKey, React.ComponentType> = {
  dashboard: DashboardPage,
  sessions: SessionCleanupPage,
  workspaces: WorkspacePage,
  agents: AgentOverviewPage,
  services: ServiceHealthPage,
  statistics: StatisticsPage,
  users: UserManagementPage,
}

const ADMIN_SECTIONS: Array<{ key: AdminMenuKey; label: string }> = [
  { key: 'dashboard', label: UI_LABELS.DASHBOARD },
  { key: 'sessions', label: UI_LABELS.SESSION_CLEANUP },
  { key: 'workspaces', label: UI_LABELS.WORKSPACE_MANAGE },
  { key: 'agents', label: UI_LABELS.AGENT_OVERVIEW },
  { key: 'services', label: UI_LABELS.SERVICE_HEALTH },
  { key: 'statistics', label: UI_LABELS.STATISTICS },
  { key: 'users', label: UI_LABELS.USER_MANAGEMENT },
]

function isAdminMenuKey(value: string | undefined): value is AdminMenuKey {
  return value !== undefined && Object.prototype.hasOwnProperty.call(ADMIN_PAGES, value)
}

function AdminMobileNav({ current }: { current: AdminMenuKey }) {
  const navigate = useNavigate()

  return (
    <nav className="border-b border-border bg-card p-3 sm:hidden" aria-label="管理后台导航">
      <label htmlFor="admin-mobile-section" className="sr-only">
        选择管理后台栏目
      </label>
      <select
        id="admin-mobile-section"
        value={current}
        onChange={(event) => navigate(`/admin/${event.target.value}`)}
        className="h-9 w-full rounded-md border border-border bg-bg-canvas px-3 text-sm text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        {ADMIN_SECTIONS.map((item) => (
          <option key={item.key} value={item.key}>
            {item.label}
          </option>
        ))}
      </select>
    </nav>
  )
}

const LS_KEY = 'chat-current-session'
const SESSION_QUERY_KEY = 'session'

function RouteLoadingState() {
  const rows = Array.from({ length: 5 })

  return (
    <div className="flex min-h-0 min-w-0 flex-1 bg-background" aria-busy="true" aria-live="polite">
      <div className="hidden w-[280px] shrink-0 border-r border-border bg-sidebar p-3 md:block">
        <div className="mb-4 h-10 rounded-[10px] skeleton-sheen" />
        <div className="space-y-2">
          {rows.map((_, index) => (
            <div
              key={index}
              className="flex items-center gap-3 rounded-xl border border-transparent px-2 py-2.5"
            >
              <div className="h-8 w-8 rounded-[9px] skeleton-sheen" />
              <div className="min-w-0 flex-1 space-y-2">
                <div className="h-3 w-3/5 rounded-full skeleton-sheen" />
                <div className="h-2.5 w-4/5 rounded-full skeleton-sheen" />
              </div>
            </div>
          ))}
        </div>
      </div>
      <div className="chat-canvas flex min-w-0 flex-1 flex-col">
        <div className="flex h-14 shrink-0 items-center gap-3 border-b border-border bg-background/90 px-6">
          <div className="h-8 w-8 rounded-[9px] skeleton-sheen" />
          <div className="space-y-2">
            <div className="h-3 w-32 rounded-full skeleton-sheen" />
            <div className="h-2.5 w-48 rounded-full skeleton-sheen" />
          </div>
        </div>
        <div className="mx-auto flex w-full max-w-[78rem] flex-1 flex-col justify-end px-6 py-8">
          <div className="space-y-4">
            <div className="h-24 w-2/3 rounded-[12px] skeleton-sheen" />
            <div className="ml-auto h-16 w-1/2 rounded-[16px] skeleton-sheen" />
            <div className="h-32 w-3/4 rounded-[12px] skeleton-sheen" />
          </div>
        </div>
      </div>
      <span className="sr-only">正在加载页面</span>
    </div>
  )
}

function NoChatSelectedState() {
  return (
    <div className="chat-canvas hidden h-full flex-col items-center justify-center px-6 text-center md:flex">
      <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-[14px] border border-primary-border bg-primary-soft shadow-[0_18px_42px_rgba(15,118,110,0.10)]">
        <MessageSquare className="h-6 w-6 text-primary" strokeWidth={1.25} />
      </div>
      <h1 className="text-base font-semibold text-foreground text-balance">
        {UI_MESSAGES.WORKBENCH_READY_TITLE}
      </h1>
      <p className="mt-2 max-w-[24rem] text-sm leading-6 text-tertiary text-pretty">
        {UI_MESSAGES.WORKBENCH_READY_DESC}
      </p>
    </div>
  )
}

function AdminContent() {
  const { section } = useParams<{ section: string }>()
  const isAuthenticated = useAdminStore((state) => state.isAuthenticated)
  const showLoginDialog = useAdminStore((state) => state.showLoginDialog)

  useEffect(() => {
    if (!isAuthenticated) showLoginDialog()
  }, [isAuthenticated, showLoginDialog])

  if (!isAuthenticated) {
    return (
      <div className="flex min-h-full flex-col items-center justify-center gap-3">
        <LayoutDashboard className="h-12 w-12 text-tertiary" strokeWidth={1.25} />
        <p className="text-sm text-tertiary">{UI_MESSAGES.PLEASE_AUTH}</p>
      </div>
    )
  }

  const menuKey = isAdminMenuKey(section) ? section : 'dashboard'
  const Page = ADMIN_PAGES[menuKey]
  return (
    <>
      <AdminMobileNav current={menuKey} />
      <Page />
    </>
  )
}

function ChatContent() {
  const { data: conversations, isLoading: conversationsLoading } = useConversations()
  const { currentSessionId, setCurrentSession, clearNavigation } = useChatNav()
  const [searchParams, setSearchParams] = useSearchParams()
  const {
    width: sidebarWidth,
    isDragging,
    handleMouseDown,
    handleKeyDown,
    expand,
  } = useResize({ storageKey: 'right-sidebar' })

  useLayoutEffect(() => {
    if (currentSessionId) return

    const fromQuery = searchParams.get(SESSION_QUERY_KEY)
    if (fromQuery) {
      setCurrentSession(fromQuery)
      return
    }

    let fromStorage: string | null = null
    try {
      fromStorage = localStorage.getItem(LS_KEY)
    } catch {
      // 在隐私受限的浏览器环境中 storage 可能不可用。
    }
    if (fromStorage) setCurrentSession(fromStorage)
  }, [currentSessionId, searchParams, setCurrentSession])

  useEffect(() => {
    if (!currentSessionId) return

    try {
      localStorage.setItem(LS_KEY, currentSessionId)
    } catch {
      // 当 storage 不可用时，URL 仍可保持当前聊天可寻址。
    }
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        next.set(SESSION_QUERY_KEY, currentSessionId)
        return next
      },
      { replace: true },
    )
  }, [currentSessionId, setSearchParams])

  const active = conversations?.find((conversation) => conversation.sessionId === currentSessionId)

  useEffect(() => {
    // 等待会话列表稳定后再判断会话是否消失 —— 否则不在首页（或仍在加载中）
    // 的目标会话会被清除导航，导致用户被退回到空白状态。
    if (!conversations || !currentSessionId || conversationsLoading) return
    if (conversations.some((conversation) => conversation.sessionId === currentSessionId)) return
    clearNavigation()
    try {
      localStorage.removeItem(LS_KEY)
    } catch {
      // 忽略 storage 失败；查询字符串会在下方清除。
    }
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        next.delete(SESSION_QUERY_KEY)
        return next
      },
      { replace: true },
    )
  }, [clearNavigation, conversations, conversationsLoading, currentSessionId, setSearchParams])

  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      <div className={active ? 'hidden md:block' : 'block'}>
        <ErrorBoundary>
          <ConversationList />
        </ErrorBoundary>
      </div>

      <div className="min-w-0 flex-1">
        {active ? (
          <ErrorBoundary key={active.sessionId}>
            <ChatArea
              taskId={active.taskId}
              sessionId={active.sessionId}
              agentType={active.agentType}
              agentName={active.agentName || undefined}
              avatarUrl={active.avatarUrl}
              repoPath={active.repoPath}
              isGroupChat={active.isGroupChat}
              groupTitle={active.isGroupChat ? active.title : undefined}
              groupAgentTypes={active.groupAgentTypes}
              groupAgentNames={active.groupAgentNames}
              groupSessions={active.groupSessions}
              onBack={clearNavigation}
            />
          </ErrorBoundary>
        ) : (
          <NoChatSelectedState />
        )}
      </div>

      {active && (
        <div className="hidden h-full xl:block">
          <RightSidebar
            taskId={active.taskId}
            sessionId={active.sessionId}
            isGroupChat={!!active.isGroupChat}
            status={active.status}
            agentType={active.agentType}
            agentName={active.agentName || undefined}
            avatarUrl={active.avatarUrl}
            agentTypes={active.groupAgentTypes}
            agentNames={active.groupAgentNames}
            sessions={active.groupSessions}
            repoPath={active.repoPath}
            pinnedAt={active.pinnedAt}
            width={sidebarWidth}
            isDragging={isDragging}
            onResizeHandleMouseDown={handleMouseDown}
            onResizeHandleKeyDown={handleKeyDown}
            onExpand={expand}
          />
        </div>
      )}
    </div>
  )
}

function AdminRoute() {
  const { section } = useParams<{ section: string }>()

  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      <AdminMenu />
      <div className="min-w-0 flex-1 overflow-auto">
        <ErrorBoundary key={section}>
          <AdminContent />
        </ErrorBoundary>
      </div>
    </div>
  )
}

function NotFoundPage() {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
      <p className="font-mono text-xs text-tertiary">404</p>
      <h1 className="text-lg font-semibold text-foreground">页面不存在</h1>
      <Link
        to="/chat"
        className="rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        返回聊天
      </Link>
    </div>
  )
}

export function ImPage() {
  return (
    <div className="flex h-dvh min-h-dvh overflow-hidden bg-background">
      <a
        href="#main-content"
        className="fixed left-3 top-3 z-50 -translate-y-20 rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground transition-transform focus:translate-y-0"
      >
        跳到主要内容
      </a>
      <IconSidebar />
      <AdminPasswordDialog />

      <main id="main-content" className="flex min-h-0 min-w-0 flex-1" tabIndex={-1}>
        <Suspense fallback={<RouteLoadingState />}>
          <Routes>
            <Route index element={<Navigate to="/chat" replace />} />
            <Route path="chat" element={<ChatContent />} />
            <Route
              path="contacts"
              element={
                <ErrorBoundary>
                  <ContactsPage />
                </ErrorBoundary>
              }
            />
            <Route
              path="skills"
              element={
                <ErrorBoundary>
                  <SkillsHubPage />
                </ErrorBoundary>
              }
            />
            <Route path="admin" element={<Navigate to="/admin/dashboard" replace />} />
            <Route path="admin/:section" element={<AdminRoute />} />
            <Route path="*" element={<NotFoundPage />} />
          </Routes>
        </Suspense>
      </main>
    </div>
  )
}
