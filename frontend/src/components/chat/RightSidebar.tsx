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
  status?: string
  agentType?: AgentType
  agentName?: string
  avatarUrl?: string
  agentTypes?: AgentType[]
  agentNames?: string[]
  sessions?: AgentSessionInfo[]
  repoPath?: string
  pinnedAt?: string | null
  /** 可调整宽度的像素值（0 = 折叠） */
  width?: number
  /** 用户是否正在拖拽 */
  isDragging?: boolean
  /** 绑定到调整大小手柄 */
  onResizeHandleMouseDown?: (e: React.MouseEvent) => void
  /** 键盘调整大小控制 */
  onResizeHandleKeyDown?: (e: React.KeyboardEvent) => void
  /** 从折叠状态展开的回调 */
  onExpand?: () => void
  /** 在移动端抽屉中占满容器宽度，并禁用拖拽手柄 */
  fluid?: boolean
}

// 为 GitGraphPanel 重新导出
export { useCollapsible } from './useCollapsible'

export function RightSidebar({
  taskId,
  sessionId,
  isGroupChat,
  status,
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
  fluid = false,
}: RightSidebarProps) {
  const isCollapsed = !fluid && width === 0
  const isPinned = !!pinnedAt

  // ── Git 分支状态（在 GitGraph 间共享） ──
  const gitGraphData = useGitGraphData(taskId)
  const [branchSelection, setBranchSelection] = useState<{
    taskId: string
    branch: string
  } | null>(null)
  const currentBranch =
    branchSelection?.taskId === taskId ? branchSelection.branch : gitGraphData.currentBranch
  const setCurrentBranch = (branch: string) => setBranchSelection({ taskId, branch })
  const branchNames = useMemo(
    () => gitGraphData.branches.map((b) => b.name),
    [gitGraphData.branches],
  )
  const selectedBranch =
    currentBranch && branchNames.includes(currentBranch)
      ? currentBranch
      : gitGraphData.currentBranch

  // 从 sessions prop 构建 sessionId → agentName 的映射
  const sessionNameMap = useMemo(
    () => Object.fromEntries(sessions.map((s) => [s.sessionId, s.agentName])),
    [sessions],
  )

  // 构建分支标签映射
  const branchLabels = useMemo(
    () =>
      buildBranchLabels(
        gitGraphData.branches.map((b) => b.name),
        sessionNameMap,
        taskId,
      ),
    [gitGraphData.branches, sessionNameMap, taskId],
  )

  // 折叠状态：显示展开标签
  if (isCollapsed) {
    return (
      <div className="flex h-full shrink-0 items-start border-l border-sidebar-border bg-sidebar pt-3">
        <button
          type="button"
          className="flex h-8 w-7 items-center justify-center rounded-l-md border border-border bg-accent text-muted-foreground transition-[transform,opacity] hover:bg-bg-hover hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          onClick={onExpand}
          title={UI_LABELS.EXPAND_SIDEBAR}
          aria-label={UI_LABELS.EXPAND_SIDEBAR}
        >
          <PanelRightOpen className="h-4 w-4" strokeWidth={1.25} />
        </button>
      </div>
    )
  }

  // 展开状态：带拖拽手柄的完整侧边栏
  return (
    <div className="relative flex h-full shrink-0" style={{ width: fluid ? '100%' : width }}>
      {/* 调整大小手柄 —— 左边缘 */}
      {!fluid && (
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
      )}

      {/* 侧边栏内容 */}
      <aside className="flex h-full min-w-0 flex-1 flex-col overflow-x-hidden overflow-y-auto overscroll-contain border-l border-sidebar-border bg-sidebar">
        {/* 历史搜索 */}
        <HistorySearch sessionId={sessionId} />

        {/* 路径 */}
        {repoPath && <SidebarPathSection repoPath={repoPath} taskId={taskId} />}

        {/* 公告 —— 仅群聊 */}
        {isGroupChat && <AnnouncementsSection taskId={taskId} />}

        {/* 成员 / Agent 信息 */}
        {isGroupChat ? (
          <MembersSection agentTypes={agentTypes} agentNames={agentNames} sessions={sessions} />
        ) : (
          <AgentInfoSection
            agentType={agentType}
            agentName={agentName}
            avatarUrl={avatarUrl}
            sessionId={sessionId}
            status={status}
          />
        )}

        {/* Git 图 */}
        <GitGraphPanel
          data={gitGraphData}
          currentBranch={selectedBranch}
          onBranchChange={setCurrentBranch}
          branchLabels={branchLabels}
        />

        {/* 更多操作 */}
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

// ─── 来自 API 的真实 Git 图数据 ────────────────────────────────

const EMPTY_GIT_DATA: GitGraphData = { commits: [], branches: [], currentBranch: '' }

function useGitGraphData(taskId: string): GitGraphData {
  const [apiData, setApiData] = useState<{ taskId: string; data: GitInfoApiResponse } | null>(null)

  useEffect(() => {
    if (!taskId) return
    let cancelled = false
    let requestId = 0
    let interval: ReturnType<typeof setInterval> | null = null
    const stopPolling = () => {
      if (interval) {
        clearInterval(interval)
        interval = null
      }
    }
    const fetchGitInfo = async () => {
      const currentRequestId = ++requestId
      try {
        const res = await fetch(`${API_BASE}/workspace/task/${encodeURIComponent(taskId)}/git-info`)
        if (!res.ok) {
          // 4xx（如 404 task 已删除）属于不可恢复错误，停止轮询避免每 30s
          // 发送无效请求；5xx 等瞬时错误保持轮询以等待恢复。
          if (res.status >= 400 && res.status < 500) stopPolling()
          return
        }
        const data: GitInfoApiResponse = await res.json()
        if (!cancelled && currentRequestId === requestId) setApiData({ taskId, data })
      } catch {
        // 静默失败 —— 没有 git 数据侧边栏仍可正常工作
      }
    }
    fetchGitInfo()
    interval = setInterval(fetchGitInfo, 30_000)
    return () => {
      cancelled = true
      stopPolling()
    }
  }, [taskId])

  return useMemo(() => {
    if (!apiData || apiData.taskId !== taskId) return EMPTY_GIT_DATA

    const branchNames = apiData.data.branches.map((b) => b.name)
    const agentBranch = branchNames.find((b) => b.startsWith('agent/'))
    const taskBranch = branchNames.find((b) => b.startsWith('task/'))
    const currentBranch = agentBranch ?? taskBranch ?? 'main'

    return {
      repoPath: apiData.data.repoPath,
      commits: apiData.data.commits,
      branches: apiData.data.branches.map((b) => ({
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
  }, [apiData, taskId])
}
