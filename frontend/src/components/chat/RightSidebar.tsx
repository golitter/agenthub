import { useState } from 'react'

import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'

import { AnnouncementsSection } from './AnnouncementsSection'
import { HistorySearch } from './HistorySearch'
import { MembersSection } from './MembersSection'

export interface RightSidebarProps {
  taskId: string
  sessionId: string
  isGroupChat: boolean
  agentTypes: AgentType[]
  agentNames: string[]
  sessions: AgentSessionInfo[]
  repoPath?: string
}

/** Hook for collapsible section state persisted to localStorage. */
export function useCollapsible(key: string, defaultOpen = true): [boolean, () => void] {
  const lsKey = `sidebar-collapse-${key}`
  const [open, setOpen] = useState(() => {
    try {
      const stored = localStorage.getItem(lsKey)
      return stored === null ? defaultOpen : stored !== 'true'
    } catch {
      return defaultOpen
    }
  })
  const toggle = () => {
    setOpen((prev) => {
      try {
        localStorage.setItem(lsKey, String(!prev))
      } catch {
        /* ignore */
      }
      return !prev
    })
  }
  return [open, toggle]
}

export function RightSidebar({
  taskId,
  sessionId,
  agentTypes,
  agentNames,
  sessions,
  repoPath,
}: RightSidebarProps) {
  return (
    <aside className="flex h-full w-[280px] shrink-0 flex-col overflow-hidden border-l border-sidebar-border bg-sidebar">
      {/* History search */}
      <HistorySearch sessionId={sessionId} />

      {/* Repo path */}
      {repoPath && (
        <div className="border-b border-sidebar-border px-4 py-3">
          <h3 className="mb-1.5 text-[11px] font-medium uppercase tracking-wider text-tertiary">
            仓库路径
          </h3>
          <p
            className="cursor-pointer truncate rounded-md bg-bg-subtle px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-bg-hover hover:text-foreground"
            title={repoPath}
            onClick={() => {
              navigator.clipboard.writeText(repoPath)
            }}
          >
            📁 {repoPath}
          </p>
        </div>
      )}

      {/* Announcements */}
      <AnnouncementsSection taskId={taskId} />

      {/* Members */}
      <MembersSection agentTypes={agentTypes} agentNames={agentNames} sessions={sessions} />

      {/* More actions — handled inside MembersSection bottom area */}
      <div className="flex flex-col gap-0.5 px-4 py-3">
        <button
          type="button"
          className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-xs text-muted-foreground transition-colors hover:bg-bg-hover hover:text-foreground"
          onClick={() =>
            exportChatAsMarkdown(
              taskId,
              sessions.map((s) => s.sessionId),
            )
          }
        >
          <span className="inline-flex w-4 justify-center text-sm">📥</span>
          导出聊天记录
        </button>
        <button
          type="button"
          className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-xs text-muted-foreground transition-colors hover:bg-bg-hover hover:text-foreground"
        >
          <span className="inline-flex w-4 justify-center text-sm">📌</span>
          置顶会话
        </button>
        <button
          type="button"
          className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-xs text-destructive transition-colors hover:bg-danger-bg"
          onClick={() => {
            if (confirm('确认退出群聊？退出后将无法查看群聊消息。')) {
              /* TODO: leave group */
            }
          }}
        >
          <span className="inline-flex w-4 justify-center text-sm">✕</span>
          退出群聊
        </button>
      </div>
    </aside>
  )
}

async function exportChatAsMarkdown(taskId: string, sessionIds: string[]) {
  const { useChatStore } = await import('@/stores/chat')
  const store = useChatStore.getState().sessions
  // Only collect messages from sessions belonging to this group chat
  const allMessages = sessionIds
    .flatMap((sid) => store[sid]?.messages ?? [])
    .sort((a, b) => a.timestamp - b.timestamp)
  const lines = allMessages.map(
    (m) =>
      `**${m.role === 'user' ? 'You' : (m.agentName ?? 'Agent')}** (${new Date(m.timestamp).toLocaleString()}):\n${m.content}`,
  )
  const md = `# Chat Export — ${taskId}\n\n${lines.join('\n\n---\n\n')}`
  const blob = new Blob([md], { type: 'text/markdown' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `chat-${taskId}.md`
  a.click()
  URL.revokeObjectURL(url)
}
