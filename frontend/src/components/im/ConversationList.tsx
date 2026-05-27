import { MessageSquare, Plus, Search } from 'lucide-react'
import { useState } from 'react'

import { useConversations } from '@/hooks/use-conversations'
import { useHoverStyle } from '@/hooks/use-hover-style'
import { useChatNav } from '@/stores/chat'

import { ConversationItem } from './ConversationItem'
import { NewChatDialog } from './NewChatDialog'

export function ConversationList() {
  const [search, setSearch] = useState('')
  const [showNewChat, setShowNewChat] = useState(false)
  const { data: conversations, isLoading } = useConversations()
  const { currentSessionId, setCurrentSession } = useChatNav()
  const newChatHover = useHoverStyle()

  const filtered = conversations?.filter((c) => {
    if (!search) return true
    const q = search.toLowerCase()
    return c.agentType.includes(q) || c.taskTitle.toLowerCase().includes(q)
  })

  return (
    <div className="flex h-full w-[280px] shrink-0 flex-col border-r border-border bg-sidebar">
      {/* Search */}
      <div className="shrink-0 px-3 py-3">
        <div className="flex items-center gap-2 rounded-lg bg-accent px-3 py-1.5">
          <Search className="h-3.5 w-3.5 shrink-0 text-tertiary" strokeWidth={1.25} />
          <input
            type="text"
            placeholder="搜索对话..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-transparent text-xs text-foreground outline-none"
          />
          <button
            className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors duration-120"
            onClick={() => setShowNewChat(true)}
            {...newChatHover}
          >
            <Plus className="h-4 w-4" strokeWidth={1.25} />
          </button>
        </div>
      </div>

      {/* Conversation list */}
      <div className="flex-1 overflow-y-auto px-2">
        {isLoading ? (
          <div className="px-2 py-4 text-xs text-tertiary">加载中...</div>
        ) : !filtered?.length ? (
          <div className="flex flex-col items-center gap-2 px-2 py-8">
            <MessageSquare className="h-6 w-6 text-tertiary" strokeWidth={1.25} />
            <p className="text-center text-xs text-tertiary">
              {search ? '没有找到对话' : '还没有对话，点击 + 开始新对话'}
            </p>
          </div>
        ) : (
          filtered.map((c) => (
            <ConversationItem
              key={c.sessionId}
              conversation={c}
              isActive={c.sessionId === currentSessionId}
              onClick={() => setCurrentSession(c.sessionId)}
            />
          ))
        )}
      </div>

      <NewChatDialog open={showNewChat} onOpenChange={setShowNewChat} />
    </div>
  )
}
