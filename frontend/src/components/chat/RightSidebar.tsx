import { PanelRightOpen } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import type { AgentType } from '@/generated/request'
import type { AgentSessionInfo } from '@/lib/api'
import { API_BASE } from '@/lib/constants'
import { UI_LABELS } from '@/lib/ui-text'

import { AgentInfoSection } from './AgentInfoSection'
import { AnnouncementsSection } from './AnnouncementsSection'
import type { GitGraphData, GitInfoApiResponse } from './git-graph-types'
import { buildBranchLabels, getBranchColor } from './git-graph-types'
import { GitGraphPanel } from './GitGraphPanel'
import { HistorySearch } from './HistorySearch'
import { MembersSection } from './MembersSection'
import { SidebarActions } from './SidebarActions'
import { SidebarPathSection } from './SidebarPathSection'

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
  /** Keyboard resize controls */
  onResizeHandleKeyDown?: (e: React.KeyboardEvent) => void
  /** Callback to expand from collapsed state */
  onExpand?: () => void
}

// Re-export for GitGraphPanel
export { useCollapsible } from './useCollapsible'

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
  onResizeHandleKeyDown,
  onExpand,
}: RightSidebarProps) {
  const isCollapsed = width === 0
  const isPinned = !!pinnedAt

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

  // Build branch label mapping
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
          title={UI_LABELS.EXPAND_SIDEBAR}
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
        role="separator"
        aria-label="调整详情侧栏宽度"
        aria-orientation="vertical"
        aria-valuemin={0}
        aria-valuemax={400}
        aria-valuenow={width}
        tabIndex={0}
        onMouseDown={onResizeHandleMouseDown}
        onKeyDown={onResizeHandleKeyDown}
      >
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
        {repoPath && <SidebarPathSection repoPath={repoPath} taskId={taskId} />}

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
        <SidebarActions
          taskId={taskId}
          sessionId={sessionId}
          isGroupChat={isGroupChat}
          sessions={sessions}
          isPinned={isPinned}
        />
      </aside>
    </div>
  )
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
    const interval = setInterval(fetchGitInfo, 30_000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [taskId])

  return useMemo(() => {
    if (!apiData) return EMPTY_GIT_DATA

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
