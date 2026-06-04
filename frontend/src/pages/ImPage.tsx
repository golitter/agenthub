import { LayoutDashboard, MessageSquare, Settings, Users } from 'lucide-react'
import { useEffect, useLayoutEffect } from 'react'

import { ChatArea } from '@/components/chat/ChatArea'
import { RightSidebar } from '@/components/chat/RightSidebar'
import { ContactsPage } from '@/components/im/ContactsPage'
import { ConversationList } from '@/components/im/ConversationList'
import { AdminMenu } from '@/components/layout/AdminMenu'
import { AdminPasswordDialog } from '@/components/layout/AdminPasswordDialog'
import { IconSidebar } from '@/components/layout/IconSidebar'
import { ErrorBoundary } from '@/components/ui/error-boundary'
import { useConversations } from '@/hooks/use-conversations'
import { useResize } from '@/hooks/use-resize'
import { AgentOverviewPage } from '@/pages/admin/AgentOverviewPage'
import { DashboardPage } from '@/pages/admin/DashboardPage'
import { ServiceHealthPage } from '@/pages/admin/ServiceHealthPage'
import { SessionCleanupPage } from '@/pages/admin/SessionCleanupPage'
import { StatisticsPage } from '@/pages/admin/StatisticsPage'
import { UserManagementPage } from '@/pages/admin/UserManagementPage'
import { WorkspacePage } from '@/pages/admin/WorkspacePage'
import { SkillsHubPage } from '@/pages/SkillsHubPage'
import { useAdminMenu, useAdminStore } from '@/stores/admin'
import { useActiveTab, useChatNav } from '@/stores/chat'

function PlaceholderPage({
  icon: Icon,
  title,
}: {
  icon: React.ComponentType<React.SVGProps<SVGSVGElement>>
  title: string
}) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3">
      <Icon className="h-12 w-12 text-tertiary" strokeWidth={1.25} />
      <h3 className="text-base font-medium text-text-secondary">{title}</h3>
      <p className="text-sm text-tertiary">功能开发中，敬请期待</p>
    </div>
  )
}

const PLACEHOLDER_PAGES: Record<
  string,
  { icon: React.ComponentType<React.SVGProps<SVGSVGElement>>; title: string }
> = {
  contacts: { icon: Users, title: '通讯录' },
  settings: { icon: Settings, title: '设置' },
}

const ADMIN_PAGES: Record<string, React.ComponentType> = {
  dashboard: DashboardPage,
  sessions: SessionCleanupPage,
  workspaces: WorkspacePage,
  agents: AgentOverviewPage,
  services: ServiceHealthPage,
  statistics: StatisticsPage,
  users: UserManagementPage,
}

function AdminContent() {
  const { activeMenuKey } = useAdminMenu()
  const isAuthenticated = useAdminStore((s) => s.isAuthenticated)
  const showLoginDialog = useAdminStore((s) => s.showLoginDialog)

  useEffect(() => {
    if (!isAuthenticated) {
      showLoginDialog()
    }
  }, [isAuthenticated, showLoginDialog])

  if (!isAuthenticated) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3">
        <LayoutDashboard className="h-12 w-12 text-tertiary" strokeWidth={1.25} />
        <p className="text-sm text-tertiary">请先验证身份</p>
      </div>
    )
  }

  const Page = ADMIN_PAGES[activeMenuKey] ?? DashboardPage
  return <Page />
}

const LS_KEY = 'chat-current-session'

export function ImPage() {
  const { data: conversations } = useConversations()
  const { currentSessionId, setCurrentSession } = useChatNav()
  const { activeTab } = useActiveTab()

  const {
    width: sidebarWidth,
    isDragging,
    handleMouseDown,
    expand,
  } = useResize({
    storageKey: 'right-sidebar',
  })

  // Restore session from localStorage on mount (before browser paint)
  useLayoutEffect(() => {
    if (!currentSessionId) {
      const stored = localStorage.getItem(LS_KEY)
      if (stored) {
        setCurrentSession(stored)
      }
    }
  }, [currentSessionId, setCurrentSession])

  // Persist session to localStorage when it changes
  useEffect(() => {
    if (currentSessionId) {
      localStorage.setItem(LS_KEY, currentSessionId)
    }
  }, [currentSessionId])

  const active = conversations?.find((c) => c.sessionId === currentSessionId)
  const placeholder = PLACEHOLDER_PAGES[activeTab]

  return (
    <div className="flex h-screen bg-background">
      <IconSidebar />
      <AdminPasswordDialog />

      {activeTab === 'chat' ? (
        <>
          <ErrorBoundary>
            <ConversationList />
          </ErrorBoundary>
          <div className="flex-1">
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
              <div className="flex h-full flex-col items-center justify-center gap-3">
                <MessageSquare className="h-10 w-10 text-tertiary" strokeWidth={1.25} />
                <p className="text-sm text-tertiary">选择一个对话开始聊天</p>
              </div>
            )}
          </div>
          {active && (
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
              onExpand={expand}
            />
          )}
        </>
      ) : activeTab === 'admin' ? (
        <>
          <AdminMenu />
          <div className="flex-1 overflow-auto">
            <ErrorBoundary>
              <AdminContent />
            </ErrorBoundary>
          </div>
        </>
      ) : activeTab === 'contacts' ? (
        <div className="flex-1">
          <ErrorBoundary>
            <ContactsPage />
          </ErrorBoundary>
        </div>
      ) : activeTab === 'skills' ? (
        <div className="flex-1">
          <ErrorBoundary>
            <SkillsHubPage />
          </ErrorBoundary>
        </div>
      ) : placeholder ? (
        <PlaceholderPage icon={placeholder.icon} title={placeholder.title} />
      ) : null}
    </div>
  )
}
