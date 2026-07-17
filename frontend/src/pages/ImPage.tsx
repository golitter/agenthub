import { LayoutDashboard, MessageSquare } from 'lucide-react'
import { lazy, Suspense, useEffect, useLayoutEffect } from 'react'
import { Link, Navigate, Route, Routes, useParams, useSearchParams } from 'react-router'

import { ChatArea } from '@/components/chat/ChatArea'
import { RightSidebar } from '@/components/chat/RightSidebar'
import { ConversationList } from '@/components/im/ConversationList'
import { AdminMenu } from '@/components/layout/AdminMenu'
import { AdminPasswordDialog } from '@/components/layout/AdminPasswordDialog'
import { IconSidebar } from '@/components/layout/IconSidebar'
import { ErrorBoundary } from '@/components/ui/error-boundary'
import { useConversations } from '@/hooks/use-conversations'
import { useResize } from '@/hooks/use-resize'
import { UI_MESSAGES } from '@/lib/ui-text'
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

const LS_KEY = 'chat-current-session'
const SESSION_QUERY_KEY = 'session'

function RouteLoadingState() {
  return (
    <div className="flex flex-1 items-center justify-center" aria-busy="true" aria-live="polite">
      <div className="h-5 w-5 animate-pulse rounded-md bg-muted" />
      <span className="sr-only">正在加载页面</span>
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

  const menuKey = section && section in ADMIN_PAGES ? (section as AdminMenuKey) : 'dashboard'
  const Page = ADMIN_PAGES[menuKey]
  return <Page />
}

function ChatContent() {
  const { data: conversations } = useConversations()
  const { currentSessionId, setCurrentSession } = useChatNav()
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

    const fromStorage = localStorage.getItem(LS_KEY)
    if (fromStorage) setCurrentSession(fromStorage)
  }, [currentSessionId, searchParams, setCurrentSession])

  useEffect(() => {
    if (!currentSessionId) return

    localStorage.setItem(LS_KEY, currentSessionId)
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

  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      <div className={active ? 'hidden md:block' : 'block'}>
        <ErrorBoundary>
          <ConversationList />
        </ErrorBoundary>
      </div>

      <div className="min-w-0 flex-1">
        {active ? (
          <ErrorBoundary>
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
            />
          </ErrorBoundary>
        ) : (
          <div className="hidden h-full flex-col items-center justify-center gap-3 md:flex">
            <MessageSquare className="h-10 w-10 text-tertiary" strokeWidth={1.25} />
            <p className="text-sm text-tertiary">{UI_MESSAGES.SELECT_CHAT_TO_START}</p>
          </div>
        )}
      </div>

      {active && (
        <div className="hidden h-full xl:block">
          <RightSidebar
            taskId={active.taskId}
            sessionId={active.sessionId}
            isGroupChat={!!active.isGroupChat}
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
  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      <AdminMenu />
      <div className="min-w-0 flex-1 overflow-auto">
        <ErrorBoundary>
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
