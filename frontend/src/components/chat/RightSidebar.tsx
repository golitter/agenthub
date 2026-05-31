import {
  ChevronRight,
  Download,
  FolderOpen,
  FolderSync,
  LogOut,
  PanelRightOpen,
  Pin,
} from 'lucide-react'
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
  /** Resizable width in px (0 = collapsed) */
  width?: number
  /** Whether user is actively dragging */
  isDragging?: boolean
  /** Attach to resize handle */
  onResizeHandleMouseDown?: (e: React.MouseEvent) => void
  /** Callback to expand from collapsed state */
  onExpand?: () => void
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
  width = 280,
  isDragging = false,
  onResizeHandleMouseDown,
  onExpand,
}: RightSidebarProps) {
  const isCollapsed = width === 0
  const [pathsOpen, togglePaths] = useCollapsible('paths', false)

  // Collapsed: show expand tab
  if (isCollapsed) {
    return (
      <div className="flex h-full shrink-0 items-start border-l border-sidebar-border bg-sidebar pt-3">
        <button
          type="button"
          className="flex h-8 w-7 items-center justify-center rounded-l-md text-tertiary transition-colors hover:bg-bg-hover hover:text-foreground"
          onClick={onExpand}
          title="展开侧栏"
        >
          <PanelRightOpen className="h-4 w-4" strokeWidth={1.25} />
        </button>
      </div>
    )
  }

  // Expanded: full sidebar with drag handle
  return (
    <div className="relative flex h-full shrink-0" style={{ width }}>
      {/* Resize handle — left edge */}
      <div
        className="group absolute inset-y-0 -left-[3px] z-10 w-[6px] cursor-col-resize"
        onMouseDown={onResizeHandleMouseDown}
      >
        {/* Visible grip line */}
        <div
          className={`absolute inset-y-0 left-1/2 w-[2px] -translate-x-1/2 transition-colors duration-120 ${
            isDragging ? 'bg-brand' : 'bg-border group-hover:bg-brand'
          }`}
        />
      </div>

      {/* Sidebar content */}
      <aside className="flex h-full min-w-0 flex-1 flex-col overflow-hidden border-l border-sidebar-border bg-sidebar">
        {/* History search */}
        <HistorySearch sessionId={sessionId} />

        {/* Paths */}
        {repoPath && (
          <div className="border-b border-sidebar-border px-4 py-3">
            <button
              type="button"
              className="flex w-full items-center gap-1 text-[11px] font-medium uppercase tracking-wider text-tertiary"
              onClick={togglePaths}
            >
              <ChevronRight
                className={`h-3 w-3 transition-transform ${pathsOpen ? 'rotate-90' : ''}`}
                strokeWidth={1.5}
              />
              路径
            </button>
            {pathsOpen && (
              <div className="mt-2 flex flex-col gap-2">
                {/* Repo path */}
                <p
                  className="flex select-none truncate rounded-md bg-bg-subtle px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-bg-hover hover:text-foreground"
                  title={`${repoPath} — 双击复制`}
                  onDoubleClick={() => {
                    navigator.clipboard.writeText(repoPath)
                    showCopyToast()
                  }}
                >
                  <FolderOpen
                    className="mr-1.5 h-3.5 w-3.5 shrink-0 text-tertiary"
                    strokeWidth={1.25}
                  />
                  <span className="truncate">{repoPath}</span>
                </p>
                {/* Task path */}
                <p
                  className="flex select-none truncate rounded-md bg-bg-subtle px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-bg-hover hover:text-foreground"
                  title={`${repoPath}/worktrees/${taskId} — 双击复制`}
                  onDoubleClick={() => {
                    navigator.clipboard.writeText(`${repoPath}/worktrees/${taskId}`)
                    showCopyToast()
                  }}
                >
                  <FolderSync
                    className="mr-1.5 h-3.5 w-3.5 shrink-0 text-tertiary"
                    strokeWidth={1.25}
                  />
                  <span className="truncate">{`${repoPath}/worktrees/${taskId}`}</span>
                </p>
              </div>
            )}
          </div>
        )}

        {/* Announcements */}
        <AnnouncementsSection taskId={taskId} />

        {/* Members */}
        <MembersSection agentTypes={agentTypes} agentNames={agentNames} sessions={sessions} />

        {/* More actions */}
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
            <Download className="h-3.5 w-3.5 text-muted-foreground" strokeWidth={1.25} />
            导出聊天记录
          </button>
          <button
            type="button"
            className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-xs text-muted-foreground transition-colors hover:bg-bg-hover hover:text-foreground"
          >
            <Pin className="h-3.5 w-3.5 text-muted-foreground" strokeWidth={1.25} />
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
            <LogOut className="h-3.5 w-3.5 text-destructive" strokeWidth={1.25} />
            退出群聊
          </button>
        </div>
      </aside>
    </div>
  )
}

/** Show a lightweight copy-success toast at the bottom-right of the viewport. */
function showCopyToast() {
  const existing = document.getElementById('copy-toast')
  if (existing) existing.remove()

  const el = document.createElement('div')
  el.id = 'copy-toast'
  el.textContent = '✓ 已复制到剪贴板'
  Object.assign(el.style, {
    position: 'fixed',
    bottom: '24px',
    right: '24px',
    padding: '8px 16px',
    borderRadius: '8px',
    fontSize: '13px',
    color: '#fff',
    background: 'rgba(0,0,0,0.75)',
    backdropFilter: 'blur(8px)',
    zIndex: '9999',
    pointerEvents: 'none',
    opacity: '0',
    transition: 'opacity 0.2s ease',
  } satisfies CSSStyleDeclaration)
  document.body.appendChild(el)
  requestAnimationFrame(() => {
    el.style.opacity = '1'
  })
  setTimeout(() => {
    el.style.opacity = '0'
    setTimeout(() => el.remove(), 200)
  }, 1500)
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
