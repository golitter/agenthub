import { MessageSquare } from 'lucide-react'

import { ChatArea } from '@/components/chat/ChatArea'
import { ConversationList } from '@/components/im/ConversationList'
import { useConversations } from '@/hooks/use-conversations'
import { useChatNav } from '@/stores/chat'

export function ImPage() {
  const { data: conversations } = useConversations()
  const { currentSessionId } = useChatNav()

  const active = conversations?.find((c) => c.sessionId === currentSessionId)

  return (
    <div className="flex h-screen" style={{ backgroundColor: 'var(--bg-canvas)' }}>
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
            <MessageSquare
              className="h-10 w-10"
              style={{ color: 'var(--text-tertiary)' }}
              strokeWidth={1}
            />
            <p className="text-sm" style={{ color: 'var(--text-tertiary)' }}>
              选择一个对话开始聊天
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
