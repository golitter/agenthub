import { MessageSquare, Plus, Search, X } from 'lucide-react'
import { useState } from 'react'

import { useConversations } from '@/hooks/use-conversations'
import {
  UI_ACTIONS,
  UI_ERRORS,
  UI_LABELS,
  UI_MESSAGES,
  UI_MISC,
  UI_PLACEHOLDERS,
} from '@/lib/ui-text'
import { useChatNav } from '@/stores/chat'

import { ConversationItem } from './ConversationItem'
import { NewChatDialog } from './NewChatDialog'

export function ConversationList() {
  const [search, setSearch] = useState('')
  const [showNewChat, setShowNewChat] = useState(false)
  const { data: conversations, isError, isLoading, refetch } = useConversations()
  const { currentSessionId, setCurrentSession } = useChatNav()
  const query = search.trim().toLowerCase()

  const filtered = conversations?.filter((c) => {
    if (!query) return true
    return [
      c.agentType,
      c.agentName,
      c.title,
      c.taskTitle,
      c.repoPath,
      ...(c.groupAgentNames ?? []),
      ...(c.groupAgentTypes ?? []),
    ].some((value) => value?.toLowerCase().includes(query))
  })
  const conversationCount = conversations?.length ?? 0

  return (
    <div className="flex h-full w-full shrink-0 flex-col border-r border-border bg-sidebar md:w-[280px]">
      {/* 搜索 */}
      <div className="shrink-0 px-3 pb-3 pt-3">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h1 className="text-sm font-semibold text-foreground">{UI_LABELS.CONVERSATIONS}</h1>
            <p className="mt-0.5 text-[11px] text-tertiary">
              {conversationCount} {UI_MISC.CONVERSATION_COUNT_SUFFIX}
            </p>
          </div>
          <button
            type="button"
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-[8px] bg-primary text-primary-foreground shadow-[0_10px_24px_rgba(15,118,110,0.18)] transition-[background,transform,opacity] hover:bg-primary/90 active:scale-[0.95] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            onClick={() => setShowNewChat(true)}
            aria-label={UI_LABELS.NEW_CHAT}
            title={UI_LABELS.NEW_CHAT}
          >
            <Plus className="h-4 w-4" strokeWidth={1.25} />
          </button>
        </div>
        <div className="flex items-center gap-2 rounded-[10px] border border-border/70 bg-accent px-3 py-2 shadow-[inset_0_1px_0_rgba(255,255,255,0.03)] transition-[border-color,box-shadow] focus-within:border-primary-border focus-within:ring-2 focus-within:ring-primary/10">
          <Search className="h-3.5 w-3.5 shrink-0 text-text-secondary" strokeWidth={1.25} />
          <input
            type="text"
            placeholder={UI_PLACEHOLDERS.SEARCH_CONVERSATION}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-transparent text-xs text-foreground outline-none placeholder:text-text-secondary"
            aria-label={UI_PLACEHOLDERS.SEARCH_CONVERSATION}
          />
          {search && (
            <button
              type="button"
              className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.94] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              onClick={() => setSearch('')}
              aria-label={UI_ACTIONS.CLEAR_SEARCH}
              title={UI_ACTIONS.CLEAR_SEARCH}
            >
              <X className="h-3.5 w-3.5" strokeWidth={1.25} />
            </button>
          )}
        </div>
      </div>

      {/* 会话列表 */}
      <div className="flex flex-1 flex-col gap-1.5 overflow-y-auto px-2">
        {isLoading ? (
          <div className="space-y-2 px-1 py-2" aria-hidden="true">
            {Array.from({ length: 6 }).map((_, index) => (
              <div key={index} className="flex items-center gap-3 rounded-xl px-2 py-2.5">
                <div className="h-8 w-8 rounded-[9px] skeleton-sheen" />
                <div className="min-w-0 flex-1 space-y-2">
                  <div className="h-3 w-2/3 rounded-full skeleton-sheen" />
                  <div className="h-2.5 w-5/6 rounded-full skeleton-sheen" />
                </div>
              </div>
            ))}
          </div>
        ) : isError ? (
          <div className="flex flex-col items-center px-4 py-10 text-center" role="alert">
            <p className="text-sm font-medium text-destructive">
              {UI_ERRORS.LOAD_CONVERSATIONS_FAILED}
            </p>
            <button
              type="button"
              className="mt-4 rounded-[7px] border border-border px-3 py-1.5 text-xs text-text-secondary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              onClick={() => refetch()}
            >
              {UI_ACTIONS.RETRY}
            </button>
          </div>
        ) : !filtered?.length ? (
          <div className="flex flex-col items-center px-4 py-10 text-center">
            <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-[12px] border border-border bg-card/70">
              <MessageSquare className="h-5 w-5 text-tertiary" strokeWidth={1.25} />
            </div>
            <p className="text-sm font-medium text-foreground">
              {search
                ? UI_MESSAGES.NO_MATCHING_CONVERSATIONS
                : UI_MESSAGES.CONVERSATION_EMPTY_TITLE}
            </p>
            <p className="mt-1 text-xs leading-5 text-tertiary">
              {search
                ? UI_MESSAGES.CONVERSATION_SEARCH_EMPTY_DESC
                : UI_MESSAGES.CONVERSATION_EMPTY_DESC}
            </p>
            {search ? (
              <button
                type="button"
                className="mt-4 rounded-[7px] border border-border px-3 py-1.5 text-xs text-text-secondary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => setSearch('')}
              >
                {UI_ACTIONS.CLEAR_SEARCH}
              </button>
            ) : (
              <button
                type="button"
                className="mt-4 inline-flex items-center gap-1.5 rounded-[7px] bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-[background,transform,opacity] hover:bg-primary/90 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => setShowNewChat(true)}
              >
                <Plus className="h-3.5 w-3.5" strokeWidth={1.25} />
                {UI_LABELS.NEW_CHAT}
              </button>
            )}
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
