import type { AgentType } from '@/generated/request'

export const AGENT_NAMES: Record<AgentType, string> = {
  'claude-code': 'Claude Code',
  opencode: 'OpenCode',
  orchestrator: 'Orchestrator',
  codex: 'Codex',
}

export const AGENT_DESCRIPTIONS: Record<AgentType, string> = {
  'claude-code': 'Anthropic 的 AI 编程助手，擅长代码生成、重构和调试',
  opencode: '开源 AI 编程工具，支持多种模型',
  orchestrator: '多 Agent 协调器，自动分派任务给合适的 Agent',
  codex: 'OpenAI 的 AI 编程助手，内置沙箱安全机制',
}
