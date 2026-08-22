import type { RunTaskRequest, RunTaskResponse } from '@/generated/agent-routing'
import type {
  ConflictAction,
  ConflictActionResponse,
  ConflictProjection,
} from '@/generated/conflict-recovery'
import type { AgentType } from '@/generated/request'
import type { SkillConfirmRequest, SkillConfirmResponse, SkillHubItem, SkillUploadResponse } from '@/generated/skill-storage'
import { AGENT_NAMES, AGENT_TYPES, API_BASE } from '@/lib/constants'

export type { SkillHubItem } from '@/generated/skill-storage'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

async function handleResponse<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
  const json = await res.json()
  return (json as { data: T }).data
}

// TODO: 迁移到 contracts/schemas 生成的类型
export interface Task {
  task_id: string
  title: string
  repo_path: string
  status: string
  pinned_at?: string | null
  created_at: string
  updated_at: string
}

// TODO: 迁移到 contracts/schemas 生成的类型
export interface Session {
  id: number
  session_id: string
  task_id: string
  agent_type: AgentType
  agent_name?: string
  route_id?: string
  mention_label?: string
  aliases?: string[]
  avatar_url?: string
  status: string
  created_at: string
  updated_at: string
}

// TODO: 迁移到 contracts/schemas 生成的类型
export interface TaskDetail {
  task: Task
  sessions: Session[]
}

// TODO: 迁移到 contracts/schemas 生成的类型
export interface AgentTypeInfo {
  type: AgentType
  name: string
  description: string
}

export async function fetchTasks(): Promise<Task[]> {
  const res = await fetch(`${API_BASE}/tasks`)
  return handleResponse<Task[]>(res)
}

export async function fetchTask(taskId: string): Promise<TaskDetail> {
  const res = await fetch(`${API_BASE}/tasks/${encodeURIComponent(taskId)}`)
  return handleResponse<TaskDetail>(res)
}

export async function createTask(
  title: string,
  agents?: { type: AgentType; name?: string }[],
  repoPath?: string,
): Promise<Task> {
  const res = await fetch(`${API_BASE}/tasks`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, agents, repo_path: repoPath }),
  })
  return handleResponse<Task>(res)
}

export async function fetchAgentTypes(): Promise<AgentTypeInfo[]> {
  const res = await fetch(`${API_BASE}/agent-types`)
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
  const json = await res.json()
  const data: unknown[] = (json as { data: unknown[] }).data
  return data.map((item) =>
    typeof item === 'string'
      ? { type: item as AgentType, name: item, description: '' }
      : (item as AgentTypeInfo),
  )
}

export interface AgentSessionInfo {
  sessionId: string
  agentType: AgentType
  agentName: string
  routeId: string
  mentionLabel: string
  aliases?: string[]
  avatarUrl?: string
}

// IM 会话 — 跨 Task 的 Session 扁平化视图
export interface Conversation {
  taskId: string
  sessionId: string
  agentType: AgentType
  agentName: string
  title: string
  lastActiveAt: string
  taskTitle: string
  status: string
  avatarUrl?: string
  repoPath?: string
  pinnedAt?: string | null
  isGroupChat?: boolean
  memberCount?: number
  groupAgentTypes?: AgentType[]
  groupAgentNames?: string[]
  groupSessions?: AgentSessionInfo[]
}

const SINGLE_CHAT_TITLE_PREFIX = 'Chat with '

function agentDisplayName(agentName: string | null | undefined, agentType: AgentType): string {
  return agentName || AGENT_NAMES[agentType] || agentType
}

function singleConversationTaskTitle(
  taskTitle: string,
  agentName: string | null | undefined,
  agentType: AgentType,
) {
  if (!taskTitle.startsWith(SINGLE_CHAT_TITLE_PREFIX)) return taskTitle
  return `${SINGLE_CHAT_TITLE_PREFIX}${agentDisplayName(agentName, agentType)}`
}

export async function fetchConversations(): Promise<Conversation[]> {
  const tasks = await fetchTasks()
  // 用 allSettled 容错：单个会话详情拉取失败（如该 task 已被清理、临时 500）
  // 不应让整个会话列表 reject，否则用户将看不到任何会话。
  const settled = await Promise.allSettled(tasks.map((t) => fetchTask(t.task_id)))
  const details = settled
    .filter((r): r is PromiseFulfilledResult<TaskDetail> => r.status === 'fulfilled')
    .map((r) => r.value)
  const convos: Conversation[] = []
  for (const detail of details) {
    const sessions = detail.sessions
    if (sessions.length === 0) continue

    // 群聊：task 拥有多个 session → 使用 orchestrator 作为单个会话展示
    if (sessions.length > 1) {
      const orchestrator = sessions.find((s) => s.agent_type === AGENT_TYPES.Orchestrator)
      const primary = orchestrator ?? sessions[0]
      convos.push({
        taskId: detail.task.task_id,
        sessionId: primary.session_id,
        agentType: primary.agent_type,
        agentName: primary.agent_name ?? '',
        title: detail.task.title,
        lastActiveAt: primary.updated_at,
        taskTitle: detail.task.title,
        status: primary.status,
        avatarUrl: primary.avatar_url || undefined,
        repoPath: detail.task.repo_path || undefined,
        pinnedAt: detail.task.pinned_at || undefined,
        isGroupChat: true,
        memberCount: sessions.length,
        groupAgentTypes: sessions.map((s) => s.agent_type),
        groupAgentNames: sessions.map((s) => s.agent_name || s.agent_type),
        groupSessions: sessions.map((s) => ({
          sessionId: s.session_id,
          agentType: s.agent_type,
          agentName: s.agent_name || s.agent_type,
          routeId: s.route_id || s.agent_name || s.agent_type,
          mentionLabel: s.mention_label || s.route_id || s.agent_name || s.agent_type,
          aliases: s.aliases,
          avatarUrl: s.avatar_url || undefined,
        })),
      })
    } else {
      // 单 Agent：作为独立会话展示
      const s = sessions[0]
      const displayName = agentDisplayName(s.agent_name, s.agent_type)
      convos.push({
        taskId: s.task_id,
        sessionId: s.session_id,
        agentType: s.agent_type,
        agentName: s.agent_name ?? '',
        title: displayName,
        lastActiveAt: s.updated_at,
        taskTitle: singleConversationTaskTitle(detail.task.title, s.agent_name, s.agent_type),
        status: s.status,
        avatarUrl: s.avatar_url || undefined,
        repoPath: detail.task.repo_path || undefined,
        pinnedAt: detail.task.pinned_at || undefined,
      })
    }
  }
  convos.sort((a, b) => {
    const aPinned = a.pinnedAt ? 1 : 0
    const bPinned = b.pinnedAt ? 1 : 0
    if (aPinned !== bPinned) return bPinned - aPinned
    if (aPinned && bPinned && a.pinnedAt && b.pinnedAt) {
      const pinDiff = new Date(b.pinnedAt).getTime() - new Date(a.pinnedAt).getTime()
      if (pinDiff !== 0) return pinDiff
    }
    return new Date(b.lastActiveAt).getTime() - new Date(a.lastActiveAt).getTime()
  })
  return convos
}

export async function createConversation(
  agents: { type: AgentType; name: string }[],
  repoPath?: string,
  title?: string,
): Promise<Conversation> {
  // 校验：不允许单独使用 orchestrator
  const hasOrchestrator = agents.some((a) => a.type === AGENT_TYPES.Orchestrator)
  const hasNonOrchestrator = agents.some((a) => a.type !== AGENT_TYPES.Orchestrator)
  if (hasOrchestrator && !hasNonOrchestrator) {
    throw new Error('Orchestrator 不能单独成群，请添加至少一个非 Orchestrator 的 Agent')
  }

  // 当选中多个 agent 时自动注入 orchestrator
  const allAgents = hasOrchestrator
    ? agents
    : agents.length >= 2
      ? [{ type: AGENT_TYPES.Orchestrator as AgentType, name: '编排器' }, ...agents]
      : agents

  const names = agents.map((a) => a.name || a.type).join(' + ')
  const taskTitle = title ?? (allAgents.length > 1 ? `群聊: ${names}` : `Chat with ${names}`)
  const task = await createTask(
    taskTitle,
    allAgents.map((a) => ({ type: a.type, name: a.name })),
    repoPath,
  )
  const detail = await fetchTask(task.task_id)
  // 主 session：群聊取 orchestrator，单聊取第一个 session
  const orchestrator = detail.sessions.find((s) => s.agent_type === AGENT_TYPES.Orchestrator)
  const primary = orchestrator ?? detail.sessions[0]
  if (!primary) throw new Error('Backend failed to create session')
  const isGroup = allAgents.length > 1
  return {
    taskId: task.task_id,
    sessionId: primary.session_id,
    agentType: primary.agent_type,
    agentName: primary.agent_name ?? '',
    title: task.title,
    lastActiveAt: primary.updated_at,
    taskTitle: task.title,
    status: primary.status,
    avatarUrl: primary.avatar_url || undefined,
    repoPath: task.repo_path || undefined,
    isGroupChat: isGroup || undefined,
    memberCount: isGroup ? detail.sessions.length : undefined,
    groupAgentTypes: isGroup ? detail.sessions.map((s) => s.agent_type) : undefined,
    groupAgentNames: isGroup ? detail.sessions.map((s) => s.agent_name || s.agent_type) : undefined,
    groupSessions: isGroup
      ? detail.sessions.map((s) => ({
          sessionId: s.session_id,
          agentType: s.agent_type,
          agentName: s.agent_name || s.agent_type,
          routeId: s.route_id || s.agent_name || s.agent_type,
          mentionLabel: s.mention_label || s.route_id || s.agent_name || s.agent_type,
          aliases: s.aliases,
          avatarUrl: s.avatar_url || undefined,
        }))
      : undefined,
  }
}

// 任务消息
export interface TaskMessage {
  id: number
  message_id?: string
  task_id: string
  session_id: string
  role: 'user' | 'agent'
  content: string
  status?: string
  last_seq?: string
  agent_type?: string
  agent_name?: string
  group_id?: string
  run_id?: string
  termination_reason?: string
  created_at: string
}

export async function cancelAgentRun(
  taskId: string,
  messageId: string,
): Promise<{ run_id: string; state: string; accepted: boolean }> {
  const res = await fetch(
    `${API_BASE}/tasks/${encodeURIComponent(taskId)}/messages/${encodeURIComponent(messageId)}/run/cancel`,
    { method: 'POST' },
  )
  const json = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(json.msg || `HTTP ${res.status}`)
  return json.data
}

export type { ConflictAction, ConflictActionResponse, ConflictProjection }

export async function fetchConflict(
  taskId: string,
  conflictId: string,
): Promise<ConflictProjection> {
  const res = await fetch(
    `${API_BASE}/tasks/${encodeURIComponent(taskId)}/conflicts/${encodeURIComponent(conflictId)}`,
  )
  return handleResponse<ConflictProjection>(res)
}

export async function applyConflictAction(
  taskId: string,
  conflictId: string,
  request: {
    action: ConflictAction
    session_id: string
    root_run_id: string
    expected_attempt: number
    confirmation?: boolean
    idempotency_key?: string
    resolver_agent?: string
  },
): Promise<ConflictActionResponse> {
  const res = await fetch(
    `${API_BASE}/tasks/${encodeURIComponent(taskId)}/conflicts/${encodeURIComponent(conflictId)}/actions`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...request, conflict_id: conflictId }),
    },
  )
  return handleResponse<ConflictActionResponse>(res)
}

// 提交消息并返回用于流式传输的 agent message_id
export async function submitMessage(
  taskId: string,
  body: RunTaskRequest,
): Promise<RunTaskResponse> {
  const res = await fetch(`${API_BASE}/tasks/${encodeURIComponent(taskId)}/run`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new Error(json.msg || `HTTP ${res.status}`)
  }
  const json = await res.json()
  return json.data
}

export async function submitPlanReview(
  taskId: string,
  body: { session_id: string; action: 'approve' | 'discuss' | 'modify'; content?: string },
): Promise<{ status: string }> {
  const res = await fetch(`${API_BASE}/tasks/${encodeURIComponent(taskId)}/review`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new Error(json.msg || `HTTP ${res.status}`)
  }
  const json = await res.json()
  return json.data
}

export interface TaskMessagesResponse {
  data: TaskMessage[]
  has_more: boolean
}

export async function getTaskMessages(
  taskId: string,
  params?: {
    limit?: number
    before?: number
    sessionId?: string
    mode?: 'group'
    primarySessionId?: string
  },
): Promise<TaskMessagesResponse> {
  const searchParams = new URLSearchParams()
  if (params?.limit) searchParams.set('limit', String(params.limit))
  if (params?.before) searchParams.set('before', String(params.before))
  if (params?.sessionId) searchParams.set('session_id', params.sessionId)
  if (params?.mode) searchParams.set('mode', params.mode)
  if (params?.primarySessionId) searchParams.set('primary_session_id', params.primarySessionId)
  const qs = searchParams.toString()
  const url = `${API_BASE}/tasks/${encodeURIComponent(taskId)}/messages${qs ? `?${qs}` : ''}`
  const res = await fetch(url)
  return handleResponse<TaskMessagesResponse>(res)
}

// 头像上传
export async function uploadAvatar(file: File): Promise<string> {
  const formData = new FormData()
  formData.append('avatar', file)
  const res = await fetch(`${API_BASE}/agents/avatar`, {
    method: 'POST',
    body: formData,
  })
  const json = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(json.msg || 'Failed to upload avatar')
  return json.data?.avatar_url
}

// 更新 session（agent 名称 / 头像）
export async function updateSession(
  sessionId: string,
  data: { agent_name?: string; avatar_url?: string },
): Promise<void> {
  const res = await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new Error(json.msg || 'Failed to update session')
  }
}

// 校验仓库路径
export async function validateRepoPath(
  repoPath: string,
): Promise<{ valid: boolean; errors: string[] }> {
  const res = await fetch(`${API_BASE}/validate-repo-path`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo_path: repoPath }),
  })
  if (!res.ok) {
    if (res.status === 503) throw new Error('Agent 服务不可用')
    const errJson = await res.json().catch(() => ({}))
    throw new Error(errJson.msg || 'Validation failed')
  }
  const json = await res.json()
  return json.data
}

// 初始化 git 仓库
export async function initGitRepo(
  repoPath: string,
): Promise<{ success: boolean; errors: string[] }> {
  const res = await fetch(`${API_BASE}/init-git-repo`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo_path: repoPath }),
  })
  if (!res.ok) {
    if (res.status === 503) throw new Error('Agent 服务不可用')
    const errJson = await res.json().catch(() => ({}))
    throw new Error(errJson.msg || 'Git init failed')
  }
  const json = await res.json()
  return json.data
}

// Agent 资料与详情
export interface AgentSkill {
  name: string
  description: string
  builtin: boolean
  source: string
}

export interface AgentProfile {
  agent_name: string
  agent_type: string
  avatar_url?: string
  status: string
  session_id: string
  soul_md?: string
  skills: AgentSkill[]
}

export interface AgentDetail {
  agent_name: string
  agent_type: string
  avatar_url?: string
  status: string
  session_id: string
  task_id: string
  repo_path?: string
  workspace_path?: string
  soul_md?: string
  created_at: string
  message_count: number
  skills: AgentSkill[]
}

export async function fetchAgentProfile(sessionId: string): Promise<AgentProfile> {
  const res = await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/profile`)
  if (!res.ok) throw new Error(`Failed to fetch agent profile: ${res.status}`)
  const json = await res.json()
  return json.data
}

export async function fetchAgentDetail(sessionId: string): Promise<AgentDetail> {
  const res = await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/detail`)
  if (!res.ok) throw new Error(`Failed to fetch agent detail: ${res.status}`)
  const json = await res.json()
  return json.data
}

export async function fetchAgentSoul(
  sessionId: string,
): Promise<{ soul_md: string; session_id: string }> {
  const res = await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/soul`)
  if (!res.ok) throw new Error(`Failed to fetch soul: ${res.status}`)
  const json = await res.json()
  return json.data
}

export async function updateAgentSoul(sessionId: string, soulMd: string): Promise<void> {
  const res = await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/soul`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ soul_md: soulMd }),
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new Error(json.msg || 'Failed to update soul')
  }
}

// =====================
// 公告
// =====================

export interface Announcement {
  id: number
  task_id: string
  sender_id: string
  sender_name: string
  content: string
  pinned: boolean
  created_at: string
}

export async function fetchAnnouncements(taskId: string): Promise<Announcement[]> {
  const res = await fetch(`${API_BASE}/tasks/${encodeURIComponent(taskId)}/announcements`)
  return handleResponse<Announcement[]>(res)
}

export async function createAnnouncement(
  taskId: string,
  data: { sender_id: string; sender_name: string; content: string; pinned?: boolean },
): Promise<Announcement> {
  const res = await fetch(`${API_BASE}/tasks/${encodeURIComponent(taskId)}/announcements`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  return handleResponse<Announcement>(res)
}

export async function deleteAnnouncement(taskId: string, announcementId: number): Promise<void> {
  const res = await fetch(
    `${API_BASE}/tasks/${encodeURIComponent(taskId)}/announcements/${announcementId}`,
    {
    method: 'DELETE',
    },
  )
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
}

export async function updateTaskPin(
  taskId: string,
  pinnedAt: string | null,
): Promise<{ task_id: string }> {
  const res = await fetch(`${API_BASE}/tasks/${encodeURIComponent(taskId)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pinned_at: pinnedAt }),
  })
  return handleResponse<{ task_id: string }>(res)
}

export async function leaveTask(taskId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/tasks/${encodeURIComponent(taskId)}/leave`, {
    method: 'DELETE',
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
}

export interface MergeResult {
  success: boolean
  source_branch: string
  target_branch: string
  conflict_files: string[]
  error: string
  aborted: boolean
}

export async function mergeTaskToMain(taskId: string, repoPath: string): Promise<MergeResult> {
  const res = await fetch(
    `${API_BASE}/workspace/task/${encodeURIComponent(taskId)}/merge-to-main`,
    {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo_path: repoPath }),
    },
  )
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
  const json = await res.json()
  return (json as { data: MergeResult }).data
}

// =====================
// Admin API
// =====================

let _adminToken: string | null = null
let _adminExpiryTimer: ReturnType<typeof setTimeout> | null = null
const adminUnauthorizedListeners = new Set<() => void>()

// 持久化键：仅随标签页生命周期存活（sessionStorage），刷新页面可恢复，
// 关闭标签页即清除，避免长期驻留 admin 凭据。
const ADMIN_TOKEN_STORAGE_KEY = 'admin_token_cache'

interface AdminTokenCache {
  token: string
  // 绝对过期时间戳（毫秒），由 expiresInSeconds 折算。
  expiresAt: number
}

function readTokenCache(): AdminTokenCache | null {
  try {
    const raw = sessionStorage.getItem(ADMIN_TOKEN_STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as AdminTokenCache
    // 已过期的缓存视为不存在。
    if (typeof parsed.expiresAt !== 'number' || parsed.expiresAt <= Date.now()) {
      sessionStorage.removeItem(ADMIN_TOKEN_STORAGE_KEY)
      return null
    }
    return parsed
  } catch {
    return null
  }
}

function writeTokenCache(cache: AdminTokenCache | null) {
  try {
    if (cache) sessionStorage.setItem(ADMIN_TOKEN_STORAGE_KEY, JSON.stringify(cache))
    else sessionStorage.removeItem(ADMIN_TOKEN_STORAGE_KEY)
  } catch {
    // 隐私模式 / 配额满时静默降级为内存态。
  }
}

function scheduleExpiry(token: string | null, expiresInSeconds?: number) {
  if (_adminExpiryTimer) {
    clearTimeout(_adminExpiryTimer)
    _adminExpiryTimer = null
  }
  if (token && expiresInSeconds && expiresInSeconds > 0) {
    // 在真正过期之前触发一次，为 re-auth 流程预留时间。
    const leadSeconds = Math.min(30, Math.floor(expiresInSeconds / 10))
    const delayMs = Math.max(0, (expiresInSeconds - leadSeconds) * 1000)
    _adminExpiryTimer = setTimeout(() => {
      _adminToken = null
      writeTokenCache(null)
      for (const listener of adminUnauthorizedListeners) listener()
    }, delayMs)
  }
}

/**
 * 设置（或清除）admin bearer token。当提供 `expiresInSeconds` 时，
 * 会提前安排一次主动过期，以便在 token 真正失效之前提示用户重新认证，
 * 而不是让后续每个请求都接连遭遇 401 失败。传入 `null` 会清除一切。
 * token 会写入 sessionStorage，刷新页面后可由 restoreAdminToken() 恢复。
 */
export function setAdminToken(token: string | null, expiresInSeconds?: number) {
  _adminToken = token
  if (token && expiresInSeconds && expiresInSeconds > 0) {
    writeTokenCache({ token, expiresAt: Date.now() + expiresInSeconds * 1000 })
  } else if (!token) {
    writeTokenCache(null)
  }
  scheduleExpiry(token, expiresInSeconds)
}

/**
 * 从 sessionStorage 恢复 admin token（页面刷新后调用）。
 * 仅在缓存有效且未过期时恢复，并按剩余时间重新安排过期触发。
 * 返回恢复出的 token（若有），否则返回 null。
 */
export function restoreAdminToken(): string | null {
  const cache = readTokenCache()
  if (!cache) return null
  const remainingMs = cache.expiresAt - Date.now()
  if (remainingMs <= 0) return null
  _adminToken = cache.token
  scheduleExpiry(cache.token, Math.ceil(remainingMs / 1000))
  return cache.token
}

export function onAdminUnauthorized(listener: () => void): () => void {
  adminUnauthorizedListeners.add(listener)
  return () => adminUnauthorizedListeners.delete(listener)
}

function adminHeaders(init?: RequestInit): Headers {
	const headers = new Headers(init?.headers)
	if (!(init?.body instanceof FormData) && !headers.has('Content-Type')) {
		headers.set('Content-Type', 'application/json')
	}
	if (_adminToken) headers.set('Authorization', `Bearer ${_adminToken}`)
	return headers
}

async function adminFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: adminHeaders(init),
  })
  if (res.status === 401) {
    _adminToken = null
    writeTokenCache(null)
    for (const listener of adminUnauthorizedListeners) listener()
    throw new Error('UNAUTHORIZED')
  }
	const json = await res.json().catch(() => ({}))
	if (res.status === 202) {
		throw new ApiError(202, json.msg || '操作正在处理中，请稍后重试')
	}
	if (!res.ok) throw new Error(json.msg || `HTTP ${res.status}`)
	return json.data as T
}

const SKILL_MUTATION_RETRIES = 5

function skillRetryDelayMs(res: Response): number {
  const raw = res.headers.get('Retry-After')?.trim()
  const seconds = raw ? Number.parseFloat(raw) : Number.NaN
  if (Number.isFinite(seconds)) {
    return Math.min(10_000, Math.max(100, Math.round(seconds * 1000)))
  }
  const date = raw ? Date.parse(raw) : Number.NaN
  if (Number.isFinite(date)) {
    return Math.min(10_000, Math.max(100, date - Date.now()))
  }
  return 2_000
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/** Skill mutations use 202 as a lease/in-progress response, not a failure. */
async function skillMutationFetch<T>(url: string, init?: RequestInit, admin = false): Promise<T> {
  for (let attempt = 0; attempt <= SKILL_MUTATION_RETRIES; attempt += 1) {
    const res = await fetch(url, {
      ...init,
      headers: admin ? adminHeaders(init) : init?.headers,
    })
    if (res.status === 202) {
      if (attempt < SKILL_MUTATION_RETRIES) {
        // Release the in-progress response before retrying so a burst of
        // concurrent mutations cannot pin browser HTTP connections.
        if (res.body) await res.body.cancel().catch(() => undefined)
        await sleep(skillRetryDelayMs(res))
        continue
      }
      const json = await res.json().catch(() => ({}))
      throw new ApiError(202, (json as { msg?: string }).msg || '操作仍在处理中，请稍后重试')
    }
    if (admin && res.status === 401) {
      _adminToken = null
      writeTokenCache(null)
      for (const listener of adminUnauthorizedListeners) listener()
      throw new ApiError(401, 'UNAUTHORIZED')
    }
    const json = await res.json().catch(() => ({}))
    if (!res.ok) {
      throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
    }
    return (json as { data: T }).data
  }
  throw new ApiError(202, '操作仍在处理中，请稍后重试')
}

export interface AuthResponse {
  token: string
  expires_in: number
}

export async function adminAuth(password: string): Promise<AuthResponse> {
  const res = await fetch(`${API_BASE}/admin/auth`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  const json = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(json.msg || '密码错误')
  return json.data as AuthResponse
}

export interface ResourceInfo {
  used: number
  total: number
  unit: string
}

export interface ResourcesResponse {
  disk: ResourceInfo
  memory: ResourceInfo
  redis: ResourceInfo
}

export function getAdminResources(): Promise<ResourcesResponse> {
  return adminFetch<ResourcesResponse>(`${API_BASE}/admin/resources`)
}

export function deleteAdminSessions(sessionIds: string[]): Promise<{ deleted: number }> {
  return adminFetch<{ deleted: number }>(`${API_BASE}/admin/sessions`, {
    method: 'DELETE',
    body: JSON.stringify({ session_ids: sessionIds }),
  })
}

export interface WorkspaceItem {
  id: string
  task: string
  agent: string
  branch: string
  disk_mb: number
  status: string
}

export function getAdminWorkspaces(): Promise<{
  workspaces: WorkspaceItem[]
  total: number
  active: number
  cleaned: number
  totalDisk: number
}> {
  return adminFetch(`${API_BASE}/admin/workspaces`)
}

export function deleteAdminWorkspace(id: string): Promise<{ success: boolean }> {
  return adminFetch(`${API_BASE}/admin/workspaces/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export interface AgentInfo {
  type: string
  name: string
  description: string
  configPath: string
  configContent: string
}

export function getAdminAgents(): Promise<AgentInfo[]> {
  return adminFetch<AgentInfo[]>(`${API_BASE}/admin/agents`)
}

export interface ServiceInfo {
  name: string
  status: string
  uptime: string
  version: string
  port: number
  lastCheck: string
}

export function getAdminServices(): Promise<ServiceInfo[]> {
  return adminFetch<ServiceInfo[]>(`${API_BASE}/admin/services`)
}

export interface DailySession {
  date: string
  count: number
}
export interface MessageByAgent {
  agentType: string
  count: number
}
export interface StorageDay {
  date: string
  size: number
}

export interface StatisticsResponse {
  dailySessions: DailySession[]
  weeklySessions: DailySession[]
  labels: string[]
  totalMessages: number
  messagesByAgent: MessageByAgent[]
  storageDays: StorageDay[]
  storageLabels: string[]
}

export function getAdminStatistics(): Promise<StatisticsResponse> {
  return adminFetch<StatisticsResponse>(`${API_BASE}/admin/statistics`)
}

export async function getAdminAvatar(): Promise<{ url: string }> {
  const res = await fetch(`${API_BASE}/admin/avatar`)
  return handleResponse<{ url: string }>(res)
}

// ── 联系人分组 ──

export interface ContactGroup {
  group_id: string
  name: string
  sort_order: number
  items: { task_id: string; sort_order: number }[]
}

export interface ContactGroupsResponse {
  groups: ContactGroup[]
  ungrouped_task_ids: string[]
}

export async function fetchContactGroups(): Promise<ContactGroupsResponse> {
  const res = await fetch(`${API_BASE}/contact-groups`)
  return handleResponse<ContactGroupsResponse>(res)
}

export async function createContactGroup(name: string): Promise<ContactGroup> {
  const res = await fetch(`${API_BASE}/contact-groups`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  return handleResponse<ContactGroup>(res)
}

export async function updateContactGroup(groupId: string, name: string): Promise<void> {
  const res = await fetch(`${API_BASE}/contact-groups/${encodeURIComponent(groupId)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
}

export async function deleteContactGroup(groupId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/contact-groups/${encodeURIComponent(groupId)}`, {
    method: 'DELETE',
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
}

export async function addToContactGroup(groupId: string, taskId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/contact-groups/${encodeURIComponent(groupId)}/items`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ task_id: taskId }),
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
}

export async function removeFromContactGroup(groupId: string, taskId: string): Promise<void> {
  const res = await fetch(
    `${API_BASE}/contact-groups/${encodeURIComponent(groupId)}/items/${encodeURIComponent(taskId)}`,
    { method: 'DELETE' },
  )
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
}

export function updateAdminAvatar(url: string): Promise<{ success: boolean }> {
  return adminFetch<{ success: boolean }>(`${API_BASE}/admin/avatar`, {
    method: 'PUT',
    body: JSON.stringify({ url }),
  })
}

// ── SkillsHub ──

export async function fetchSkills(): Promise<SkillHubItem[]> {
  const res = await fetch(`${API_BASE}/skills`)
  return handleResponse<SkillHubItem[]>(res)
}

export async function uploadSkill(file: File): Promise<SkillUploadResponse> {
  const formData = new FormData()
  formData.append('file', file)
  return adminFetch<SkillUploadResponse>(`${API_BASE}/skills/upload`, {
    method: 'POST',
    body: formData,
  })
}

export async function confirmSkill(data: SkillConfirmRequest): Promise<SkillConfirmResponse> {
  return skillMutationFetch<SkillConfirmResponse>(`${API_BASE}/skills/confirm`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  }, true)
}

export async function deleteSkill(name: string): Promise<void> {
  await skillMutationFetch<{ success: boolean }>(`${API_BASE}/skills/${encodeURIComponent(name)}`, { method: 'DELETE' }, true)
}

export async function importSkill(
  skillName: string,
  sessionId: string,
): Promise<{ success: boolean }> {
  return skillMutationFetch<{ success: boolean }>(`${API_BASE}/skills/${encodeURIComponent(skillName)}/import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionId }),
  })
}

export async function removeSkill(skillName: string, sessionId: string): Promise<void> {
  await skillMutationFetch<{ success: boolean }>(
    `${API_BASE}/skills/${encodeURIComponent(skillName)}/sessions/${encodeURIComponent(sessionId)}`,
    { method: 'DELETE' },
  )
}
