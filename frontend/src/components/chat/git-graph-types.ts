// ─── Git 图类型 ────────────────────────────────────────────

export interface GitCommit {
  hash: string
  fullHash?: string
  msg: string
  author: string
  lane: string
  time: string
  /** 父提交的完整哈希 —— 用于绘制图中的连线 */
  parentHashes?: string[]
}

export interface GitBranchConfig {
  name: string
  color: string
  headHash?: string
  headMsg?: string
  headAuthor?: string
  headTime?: string
  exists?: boolean
}

export type GitBranchInfo = GitBranchConfig

export interface GitGraphData {
  repoPath?: string
  commits: GitCommit[]
  branches: GitBranchConfig[]
  currentBranch: string
}

/** GET /api/workspace/task/:taskId/git-info 的 API 响应结构 */
export interface GitInfoApiResponse {
  repoPath: string
  branches: {
    name: string
    headHash: string
    headMsg: string
    headAuthor: string
    headTime: string
    exists?: boolean
  }[]
  commits: {
    hash: string
    fullHash: string
    msg: string
    author: string
    lane: string
    time: string
    parentHashes?: string[]
  }[]
}

export interface GitGraphPanelProps {
  data: GitGraphData
  currentBranch: string
  onBranchChange: (branch: string) => void
  /** 原始分支名 → 显示标签 的映射 */
  branchLabels: Record<string, string>
}

// ─── 终端类型 ─────────────────────────────────────────────

export interface TerminalPanelProps {
  currentBranch: string
  availableBranches: string[]
  gitGraphData: GitGraphData
  onBranchChange: (branch: string) => void
  /** 原始分支名 → 显示标签 的映射 */
  branchLabels: Record<string, string>
}

export type CommandResult = string | '__CLEAR__'

// ─── 共享 ─────────────────────────────────────────────────────

/** 分支名 → 显示颜色 的映射 */
export const BRANCH_COLORS: Record<string, string> = {
  main: 'var(--text-secondary)',
}
/** task/agent 分支的兜底颜色 */
export const TASK_BRANCH_COLOR = 'var(--primary)'

export function getBranchColor(name: string): string {
  if (name === 'main') return BRANCH_COLORS.main
  if (name.startsWith('task/')) return 'var(--color-warning)'
  if (name.startsWith('agent/')) return TASK_BRANCH_COLOR
  return TASK_BRANCH_COLOR
}

/** 提交者 → 颜色 的映射，用于提交节点圆点 */
export const GIT_AUTHOR_COLORS: Record<string, string> = {
  Orchestrator: 'var(--agent-orchestrator)',
  'Claude Code': 'var(--agent-claude)',
  OpenCode: 'var(--agent-opencode)',
  田乐檬: 'var(--agent-codex)',
}

export const ROW_HEIGHT = 28
export const LANE_WIDTH = 120

// ─── 分支标签映射 ──────────────────────────────────────

/**
 * 构建从原始 git 分支名到显示标签的映射。
 *
 * - `main` → `"main"`
 * - `task/{taskId}` → `"task"`
 * - `agent/{sessionId}/{taskId}` → 来自 sessions 的 Agent 显示名
 */
export function buildBranchLabels(
  branches: string[],
  sessionNameMap: Record<string, string>,
  taskId: string,
): Record<string, string> {
  const labels: Record<string, string> = {}
  for (const b of branches) {
    if (b === 'main') {
      labels[b] = 'main'
    } else if (b === `task/${taskId}`) {
      labels[b] = 'task'
    } else if (b.startsWith('agent/')) {
      // agent/{sessionId}/{taskId} → 提取 sessionId
      const parts = b.split('/')
      // parts = ['agent', sessionId, ...其余部分]
      const sessionId = parts[1]
      const agentName = sessionNameMap[sessionId]
      labels[b] = agentName ?? `agent/${sessionId ? sessionId.slice(0, 6) : 'unknown'}`
    } else {
      labels[b] = b
    }
  }
  return labels
}
