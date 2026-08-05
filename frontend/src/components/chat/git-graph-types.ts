// ─── Git Graph Types ────────────────────────────────────────────

export interface GitCommit {
  hash: string
  fullHash?: string
  msg: string
  author: string
  lane: string
  time: string
  /** Parent commit full hashes — for drawing graph connections */
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

/** API response shape from GET /api/workspace/task/:taskId/git-info */
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
  /** Map raw branch name → display label */
  branchLabels: Record<string, string>
}

// ─── Terminal Types ─────────────────────────────────────────────

export interface TerminalPanelProps {
  currentBranch: string
  availableBranches: string[]
  gitGraphData: GitGraphData
  onBranchChange: (branch: string) => void
  /** Map raw branch name → display label */
  branchLabels: Record<string, string>
}

export type CommandResult = string | '__CLEAR__'

// ─── Shared ─────────────────────────────────────────────────────

/** Branch name → display color */
export const BRANCH_COLORS: Record<string, string> = {
  main: 'var(--text-secondary)',
}
/** Fallback for task/agent branches */
export const TASK_BRANCH_COLOR = 'var(--primary)'

export function getBranchColor(name: string): string {
  if (name === 'main') return BRANCH_COLORS.main
  if (name.startsWith('task/')) return 'var(--color-warning)'
  if (name.startsWith('agent/')) return TASK_BRANCH_COLOR
  return TASK_BRANCH_COLOR
}

/** Author → color mapping for commit node dots */
export const GIT_AUTHOR_COLORS: Record<string, string> = {
  Orchestrator: 'var(--agent-orchestrator)',
  'Claude Code': 'var(--agent-claude)',
  OpenCode: 'var(--agent-opencode)',
  田乐檬: 'var(--agent-codex)',
}

export const ROW_HEIGHT = 28
export const LANE_WIDTH = 120

// ─── Branch Label Mapping ──────────────────────────────────────

/**
 * Build a mapping from raw git branch names to display labels.
 *
 * - `main` → `"main"`
 * - `task/{taskId}` → `"task"`
 * - `agent/{sessionId}/{taskId}` → agent display name from sessions
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
      // agent/{sessionId}/{taskId} → extract sessionId
      const parts = b.split('/')
      // parts = ['agent', sessionId, ...rest]
      const sessionId = parts[1]
      const agentName = sessionNameMap[sessionId]
      labels[b] = agentName ?? `agent/${sessionId ? sessionId.slice(0, 6) : 'unknown'}`
    } else {
      labels[b] = b
    }
  }
  return labels
}
