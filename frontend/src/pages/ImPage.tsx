import { LayoutDashboard, MessageSquare, Settings, Users } from 'lucide-react'

import { ChatArea } from '@/components/chat/ChatArea'
import { ConversationList } from '@/components/im/ConversationList'
import { IconSidebar } from '@/components/layout/IconSidebar'
import { useConversations } from '@/hooks/use-conversations'
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
      <Icon className="h-12 w-12 text-tertiary" strokeWidth={1} />
      <h3 className="text-base font-medium text-secondary">{title}</h3>
      <p className="text-sm text-tertiary">功能开发中，敬请期待</p>
    </div>
  )
}

const PLACEHOLDER_PAGES: Record<
  string,
  { icon: React.ComponentType<React.SVGProps<SVGSVGElement>>; title: string }
> = {
  contacts: { icon: Users, title: '通讯录' },
  admin: { icon: LayoutDashboard, title: '管理' },
  settings: { icon: Settings, title: '设置' },
}

export function ImPage() {
  const { data: conversations } = useConversations()
  const { currentSessionId } = useChatNav()
  const { activeTab } = useActiveTab()

  const active = conversations?.find((c) => c.sessionId === currentSessionId)
  const placeholder = PLACEHOLDER_PAGES[activeTab]

  return (
    <div className="flex h-screen bg-background">
      <IconSidebar />

      {activeTab === 'chat' ? (
        <>
          <ConversationList />
          <div className="flex-1">
            {active ? (
              <ChatArea
                taskId={active.taskId}
                sessionId={active.sessionId}
                agentType={active.agentType}
                agentName={active.agentName || undefined}
                avatarUrl={active.avatarUrl}
                repoPath={active.repoPath}
              />
            ) : (
              <div className="flex h-full flex-col items-center justify-center gap-3">
                <MessageSquare className="h-10 w-10 text-tertiary" strokeWidth={1} />
                <p className="text-sm text-tertiary">选择一个对话开始聊天</p>
              </div>
            )}
          </div>
        </>
      ) : placeholder ? (
        <PlaceholderPage icon={placeholder.icon} title={placeholder.title} />
      ) : null}
    </div>
  )
}
