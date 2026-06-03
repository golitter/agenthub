import { useQueryClient } from '@tanstack/react-query'
import {
  ChevronDown,
  ChevronRight,
  Download,
  FolderOpen,
  FolderSync,
  LogOut,
  PanelRightOpen,
  Pin,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'
import { leaveTask, updateTaskPin } from '@/lib/api'
import { AGENT_NAMES, API_BASE, MESSAGE_ROLES } from '@/lib/constants'
import { useChatNav } from '@/stores/chat'

import { AgentHoverCard } from './AgentHoverCard'
import { AnnouncementsSection } from './AnnouncementsSection'
import type { GitGraphData, GitInfoApiResponse } from './git-graph-types'
import { buildBranchLabels, getBranchColor } from './git-graph-types'
import { GitGraphPanel } from './GitGraphPanel'
import { HistorySearch } from './HistorySearch'
import { MembersSection } from './MembersSection'

export interface RightSidebarProps {
  taskId: string
  sessionId: string
  isGroupChat: boolean
  agentType?: AgentType
  agentName?: string
  avatarUrl?: string
  agentTypes?: AgentType[]
  agentNames?: string[]
  sessions?: AgentSessionInfo[]
  repoPath?: string
  pinnedAt?: string | null
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

/** Single-chat agent info section — mirrors MembersSection layout but for one agent. */
function AgentInfoSection({
  agentType,
  agentName,
  avatarUrl,
  sessionId,
}: {
  agentType?: AgentType
  agentName?: string
  avatarUrl?: string
  sessionId: string
}) {
  const [open, toggleOpen] = useCollapsible('agent-info')

  const displayName = agentName ?? (agentType ? AGENT_NAMES[agentType] : 'Agent')
  const typeLabel = agentType ?? ''
  const online = false // TODO: wire to real session status

  return (
    <div className="border-b border-sidebar-border">
      {/* Header */}
      <button
        type="button"
        className="flex w-full items-center justify-between px-4 py-3 pb-2.5 text-left user-select-none"
        onClick={toggleOpen}
      >
        <span className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-text-secondary transition-[transform,opacity] hover:text-foreground">
          Agent 信息
        </span>
        <ChevronDown
          className={`h-3.5 w-3.5 text-tertiary transition-transform ${open ? '' : '-rotate-90'}`}
          strokeWidth={1.25}
        />
      </button>

      {/* Body */}
      <div
        className={`overflow-hidden transition-[max-height] duration-200 ease-out ${open ? 'max-h-[600px] overflow-y-auto' : 'max-h-0'}`}
      >
        <div className="px-4 pb-3.5">
          <div className="flex items-center gap-2.5 rounded-md px-2 py-1.5">
            <AgentHoverCard
              agentType={typeLabel}
              agentName={displayName}
              sessionId={sessionId}
              avatarUrl={avatarUrl}
              status={online ? 'running' : 'offline'}
            />
            <div className="min-w-0 flex-1">
              <div className="text-[13px] font-medium">{displayName}</div>
              <div className="text-[11px] text-tertiary">{typeLabel}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export function RightSidebar({
  taskId,
  sessionId,
  isGroupChat,
  agentType,
  agentName,
  avatarUrl,
  agentTypes = [],
  agentNames = [],
  sessions = [],
  repoPath,
  pinnedAt,
  width = 300,
  isDragging = false,
  onResizeHandleMouseDown,
  onExpand,
}: RightSidebarProps) {
  const isCollapsed = width === 0
  const queryClient = useQueryClient()
  const { clearNavigation } = useChatNav()
  const isPinned = !!pinnedAt
  const [pathsOpen, togglePaths] = useCollapsible('paths', false)

  // ── Git branch state (shared between GitGraph) ──
  const gitGraphData = useGitGraphData(taskId)
  const initialBranch = useMemo(() => gitGraphData.currentBranch, [gitGraphData.currentBranch])
  const [currentBranch, setCurrentBranch] = useState(initialBranch)
  const branchNames = useMemo(
    () => gitGraphData.branches.map((b) => b.name),
    [gitGraphData.branches],
  )
  const selectedBranch =
    currentBranch && branchNames.includes(currentBranch)
      ? currentBranch
      : gitGraphData.currentBranch

  // Build sessionId → agentName mapping from sessions prop
  const sessionNameMap = useMemo(
    () => Object.fromEntries(sessions.map((s) => [s.sessionId, s.agentName])),
    [sessions],
  )

  // Build branch label mapping: agent/{sid}/... → agent name, task/{id} → "task", main → "main"
  const branchLabels = useMemo(
    () =>
      buildBranchLabels(
        gitGraphData.branches.map((b) => b.name),
        sessionNameMap,
        taskId,
      ),
    [gitGraphData.branches, sessionNameMap, taskId],
  )

  // Collapsed: show expand tab
  if (isCollapsed) {
    return (
      <div className="flex h-full shrink-0 items-start border-l border-sidebar-border bg-sidebar pt-3">
        <button
          type="button"
          className="flex h-8 w-7 items-center justify-center rounded-l-md border border-border bg-accent text-muted-foreground transition-[transform,opacity] hover:bg-bg-hover hover:text-foreground"
          onClick={onExpand}
          title="展开侧栏"
        >
          <PanelRightOpen className="h-4 w-4" strokeWidth={1.5} />
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
          className={`absolute inset-y-0 left-1/2 w-[2px] -translate-x-1/2 transition-[transform,opacity] duration-120 ${
            isDragging ? 'bg-brand' : 'bg-border group-hover:bg-brand'
          }`}
        />
      </div>

      {/* Sidebar content */}
      <aside className="flex h-full min-w-0 flex-1 flex-col overflow-x-hidden overflow-y-auto overscroll-contain border-l border-sidebar-border bg-sidebar">
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
              路径信息
            </button>
            {pathsOpen && (
              <div className="mt-2 flex flex-col gap-2">
                {/* Repo path */}
                <div>
                  <span className="mb-0.5 block text-[11px] text-tertiary">仓库路径</span>
                  <p
                    className="flex select-none truncate rounded-md bg-bg-subtle px-2.5 py-1.5 text-xs text-muted-foreground transition-[transform,opacity] hover:bg-bg-hover hover:text-foreground"
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
                </div>
                {/* Task path */}
                <div>
                  <span className="mb-0.5 block text-[11px] text-tertiary">任务路径</span>
                  <p
                    className="flex select-none truncate rounded-md bg-bg-subtle px-2.5 py-1.5 text-xs text-muted-foreground transition-[transform,opacity] hover:bg-bg-hover hover:text-foreground"
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
              </div>
            )}
          </div>
        )}

        {/* Announcements — group chat only */}
        {isGroupChat && <AnnouncementsSection taskId={taskId} />}

        {/* Members / Agent Info */}
        {isGroupChat ? (
          <MembersSection agentTypes={agentTypes} agentNames={agentNames} sessions={sessions} />
        ) : (
          <AgentInfoSection
            agentType={agentType}
            agentName={agentName}
            avatarUrl={avatarUrl}
            sessionId={sessionId}
          />
        )}

        {/* Git Graph */}
        <GitGraphPanel
          data={gitGraphData}
          currentBranch={selectedBranch}
          onBranchChange={setCurrentBranch}
          branchLabels={branchLabels}
        />

        {/* More actions */}
        <div className="flex flex-col gap-0.5 px-4 py-3">
          <button
            type="button"
            className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-xs text-muted-foreground transition-[transform,opacity] hover:bg-bg-hover hover:text-foreground"
            onClick={() =>
              exportChatAsMarkdown(
                taskId,
                isGroupChat ? sessions.map((s) => s.sessionId) : [sessionId],
              )
            }
          >
            <Download className="h-3.5 w-3.5 text-muted-foreground" strokeWidth={1.25} />
            导出聊天记录
          </button>
          <button
            type="button"
            className={`flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-xs transition-[transform,opacity] hover:bg-bg-hover ${
              isPinned ? 'text-primary' : 'text-muted-foreground hover:text-foreground'
            }`}
            onClick={async () => {
              const newPin = isPinned ? null : new Date().toISOString()
              await updateTaskPin(taskId, newPin)
              queryClient.invalidateQueries({ queryKey: ['conversations'] })
            }}
          >
            <Pin
              className={`h-3.5 w-3.5 ${isPinned ? 'text-primary' : 'text-muted-foreground'}`}
              strokeWidth={1.25}
            />
            {isPinned ? '取消置顶' : '置顶会话'}
          </button>
          <button
            type="button"
            className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-xs text-destructive transition-[transform,opacity] hover:bg-danger-bg"
            onClick={async () => {
              const msg = isGroupChat
                ? '确认退出群聊？退出后将彻底删除所有消息和工作区数据，且不可恢复。'
                : '确认删除会话？删除后将清除所有聊天记录，且不可恢复。'
              if (!confirm(msg)) return
              try {
                await leaveTask(taskId)
                queryClient.invalidateQueries({ queryKey: ['conversations'] })
                clearNavigation()
              } catch (err) {
                console.error('leave task failed:', err)
              }
            }}
          >
            <LogOut className="h-3.5 w-3.5 text-destructive" strokeWidth={1.25} />
            {isGroupChat ? '退出群聊' : '删除会话'}
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
    color: 'var(--text-primary)',
    background: 'var(--bg-card)',
    backdropFilter: 'blur(8px)',
    zIndex: '9999',
    pointerEvents: 'none',
    opacity: '0',
    transition: 'opacity 0.2s ease',
  } satisfies Partial<CSSStyleDeclaration>)
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
      `**${m.role === MESSAGE_ROLES.USER ? 'You' : (m.agentName ?? 'Agent')}** (${new Date(m.timestamp).toLocaleString()}):\n${m.content}`,
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

// ─── Real Git Graph Data from API ────────────────────────────────

const EMPTY_GIT_DATA: GitGraphData = { commits: [], branches: [], currentBranch: '' }

function useGitGraphData(taskId: string): GitGraphData {
  const [apiData, setApiData] = useState<GitInfoApiResponse | null>(null)

  useEffect(() => {
    if (!taskId) return
    let cancelled = false
    const fetchGitInfo = async () => {
      try {
        const res = await fetch(`${API_BASE}/workspace/task/${taskId}/git-info`)
        if (!res.ok) return
        const data: GitInfoApiResponse = await res.json()
        if (!cancelled) setApiData(data)
      } catch {
        // Silently fail — sidebar still works without git data
      }
    }
    fetchGitInfo()
    // Refresh every 30s
    const interval = setInterval(fetchGitInfo, 30_000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [taskId])

  return useMemo(() => {
    if (!apiData) return EMPTY_GIT_DATA

    // Determine current branch: prefer the most specific branch (agent > task > main)
    const branchNames = apiData.branches.map((b) => b.name)
    const agentBranch = branchNames.find((b) => b.startsWith('agent/'))
    const taskBranch = branchNames.find((b) => b.startsWith('task/'))
    const currentBranch = agentBranch ?? taskBranch ?? 'main'

    return {
      repoPath: apiData.repoPath,
      commits: apiData.commits,
      branches: apiData.branches.map((b) => ({
        name: b.name,
        color: getBranchColor(b.name),
        headHash: b.headHash,
        headMsg: b.headMsg,
        headAuthor: b.headAuthor,
        headTime: b.headTime,
        exists: b.exists,
      })),
      currentBranch,
    }
  }, [apiData])
}
