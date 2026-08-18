import { type AgentType, AgentTypeValues } from '@/generated/request'

export const API_BASE = '/api'

// 重新导出 agent type 值，便于访问而无需使用字符串字面量
export const AGENT_TYPES = AgentTypeValues

export const AGENT_COLORS: Record<AgentType, string> = {
  'claude-code': 'var(--agent-claude)',
  opencode: 'var(--agent-opencode)',
  orchestrator: 'var(--agent-orchestrator)',
  codex: 'var(--agent-codex)',
  pi: 'var(--agent-pi)',
}

export const AGENT_NAMES: Record<AgentType, string> = {
  'claude-code': 'Claude Code',
  opencode: 'OpenCode',
  orchestrator: 'Orchestrator',
  codex: 'Codex',
  pi: 'Pi',
}

export const AGENT_DESCRIPTIONS: Record<AgentType, string> = {
  'claude-code': 'Anthropic 的 AI 编程助手，擅长代码生成、重构和调试',
  opencode: '开源 AI 编程工具，支持多种模型',
  orchestrator: '多 Agent 协调器，自动分派任务给合适的 Agent',
  codex: 'OpenAI 的 AI 编程助手，内置沙箱安全机制',
  pi: '支持多模型、Skills 和会话恢复的 AI 编程助手',
}

// 消息角色常量 — 消除魔法字符串
export const MESSAGE_ROLES = {
  USER: 'user',
  AGENT: 'agent',
  SYSTEM: 'system',
} as const

// 聊天状态常量 — 用于可辨识状态的比较
export const CHAT_STATUSES = {
  IDLE: 'idle',
  LOADING: 'loading',
  STREAMING: 'streaming',
  TOOL_RUNNING: 'tool_running',
  DONE: 'done',
  ERROR: 'error',
  RETRYING: 'retrying',
} as const

// 当前用户的显示名称 — 单一数据源
export const CURRENT_USER_NAME = '田乐檬'

// 项目元数据 — 品牌与信息的单一数据源
export const PROJECT_META = {
  GITHUB_URL: 'https://github.com/golitter/bytedanceai',
  NAME: 'AgentHub',
  DESCRIPTION_EN: 'Multi-Agent Chat Platform',
  DESCRIPTION_ZH:
    '多 Agent 协作聊天平台，支持 Claude Code、OpenCode、Codex CLI、Orchestrator 等多种 Agent，提供实时 SSE 流式通信、会话管理、工作区隔离和技能供给能力。',
} as const

// 表示 active/streaming 状态的状态集合
export const ACTIVE_STATUSES: ReadonlySet<string> = new Set([
  CHAT_STATUSES.LOADING,
  CHAT_STATUSES.STREAMING,
  CHAT_STATUSES.TOOL_RUNNING,
])
