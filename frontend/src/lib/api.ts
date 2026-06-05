import type { RunTaskRequest, RunTaskResponse } from '@/generated/agent-routing'
import type { AgentType } from '@/generated/request'
import { AGENT_TYPES, API_BASE } from '@/lib/constants'

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

// TODO: migrate to generated types from contracts/schemas
export interface Task {
  task_id: string
  title: string
  repo_path: string
  status: string
  pinned_at?: string | null
  created_at: string
  updated_at: string
}

// TODO: migrate to generated types from contracts/schemas
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

// TODO: migrate to generated types from contracts/schemas
export interface TaskDetail {
  task: Task
  sessions: Session[]
}

// TODO: migrate to generated types from contracts/schemas
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
  const res = await fetch(`${API_BASE}/tasks/${taskId}`)
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

// IM Conversation — a flattened view of Session across Tasks
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

export async function fetchConversations(): Promise<Conversation[]> {
  const tasks = await fetchTasks()
  const details = await Promise.all(tasks.map((t) => fetchTask(t.task_id)))
  const convos: Conversation[] = []
  for (const detail of details) {
    const sessions = detail.sessions
    if (sessions.length === 0) continue

    // Group chat: task has multiple sessions → show as one conversation using orchestrator
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
      // Single agent: show as individual conversation
      const s = sessions[0]
      convos.push({
        taskId: s.task_id,
        sessionId: s.session_id,
        agentType: s.agent_type,
        agentName: s.agent_name ?? '',
        title: s.agent_name || s.agent_type,
        lastActiveAt: s.updated_at,
        taskTitle: detail.task.title,
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
  // Validate: orchestrator alone is not allowed
  const hasOrchestrator = agents.some((a) => a.type === AGENT_TYPES.Orchestrator)
  const hasNonOrchestrator = agents.some((a) => a.type !== AGENT_TYPES.Orchestrator)
  if (hasOrchestrator && !hasNonOrchestrator) {
    throw new Error('Orchestrator 不能单独成群，请添加至少一个非 Orchestrator 的 Agent')
  }

  // Auto-inject orchestrator when multiple agents are selected
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
  // Primary session: orchestrator for group chats, first session for single
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

// Task messages
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
  created_at: string
}

// Submit a message and get back the agent message_id for streaming
export async function submitMessage(
  taskId: string,
  body: RunTaskRequest,
): Promise<RunTaskResponse> {
  const res = await fetch(`${API_BASE}/tasks/${taskId}/run`, {
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
  const res = await fetch(`${API_BASE}/tasks/${taskId}/review`, {
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
  const url = `${API_BASE}/tasks/${taskId}/messages${qs ? `?${qs}` : ''}`
  const res = await fetch(url)
  return handleResponse<TaskMessagesResponse>(res)
}

// Avatar upload
export async function uploadAvatar(file: File): Promise<string> {
  const formData = new FormData()
  formData.append('avatar', file)
  const res = await fetch(`${API_BASE}/agents/avatar`, {
    method: 'POST',
    body: formData,
  })
  const json = await res.json()
  if (!res.ok) throw new Error(json.msg || 'Failed to upload avatar')
  return json.data.avatar_url
}

// Update session (agent name / avatar)
export async function updateSession(
  sessionId: string,
  data: { agent_name?: string; avatar_url?: string },
): Promise<void> {
  const res = await fetch(`${API_BASE}/sessions/${sessionId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  if (!res.ok) {
    const json = await res.json()
    throw new Error(json.msg || 'Failed to update session')
  }
}

// Validate repo path
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
    const json = await res.json()
    throw new Error(json.msg || 'Validation failed')
  }
  const json = await res.json()
  return json.data
}

// Agent profile & detail
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
  const res = await fetch(`${API_BASE}/sessions/${sessionId}/profile`)
  if (!res.ok) throw new Error(`Failed to fetch agent profile: ${res.status}`)
  const json = await res.json()
  return json.data
}

export async function fetchAgentDetail(sessionId: string): Promise<AgentDetail> {
  const res = await fetch(`${API_BASE}/sessions/${sessionId}/detail`)
  if (!res.ok) throw new Error(`Failed to fetch agent detail: ${res.status}`)
  const json = await res.json()
  return json.data
}

export async function fetchAgentSoul(
  sessionId: string,
): Promise<{ soul_md: string; session_id: string }> {
  const res = await fetch(`${API_BASE}/sessions/${sessionId}/soul`)
  if (!res.ok) throw new Error(`Failed to fetch soul: ${res.status}`)
  const json = await res.json()
  return json.data
}

export async function updateAgentSoul(sessionId: string, soulMd: string): Promise<void> {
  const res = await fetch(`${API_BASE}/sessions/${sessionId}/soul`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ soul_md: soulMd }),
  })
  if (!res.ok) {
    const json = await res.json()
    throw new Error(json.msg || 'Failed to update soul')
  }
}

// =====================
// Announcements
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
  const res = await fetch(`${API_BASE}/tasks/${taskId}/announcements`)
  return handleResponse<Announcement[]>(res)
}

export async function createAnnouncement(
  taskId: string,
  data: { sender_id: string; sender_name: string; content: string; pinned?: boolean },
): Promise<Announcement> {
  const res = await fetch(`${API_BASE}/tasks/${taskId}/announcements`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  return handleResponse<Announcement>(res)
}

export async function deleteAnnouncement(taskId: string, announcementId: number): Promise<void> {
  const res = await fetch(`${API_BASE}/tasks/${taskId}/announcements/${announcementId}`, {
    method: 'DELETE',
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
}

export async function updateTaskPin(
  taskId: string,
  pinnedAt: string | null,
): Promise<{ task_id: string }> {
  const res = await fetch(`${API_BASE}/tasks/${taskId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pinned_at: pinnedAt }),
  })
  return handleResponse<{ task_id: string }>(res)
}

export async function leaveTask(taskId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/tasks/${taskId}/leave`, { method: 'DELETE' })
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
  const res = await fetch(`${API_BASE}/workspace/task/${taskId}/merge-to-main`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ repo_path: repoPath }),
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
  return res.json()
}

// =====================
// Admin API
// =====================

let _adminToken: string | null = null

export function setAdminToken(token: string | null) {
  _adminToken = token
}

function adminHeaders(): HeadersInit {
  const h: HeadersInit = { 'Content-Type': 'application/json' }
  if (_adminToken) h['Authorization'] = `Bearer ${_adminToken}`
  return h
}

async function adminFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: { ...adminHeaders(), ...(init?.headers ?? {}) },
  })
  if (res.status === 401) {
    _adminToken = null
    throw new Error('UNAUTHORIZED')
  }
  const json = await res.json()
  if (!res.ok) throw new Error(json.msg || `HTTP ${res.status}`)
  return json.data as T
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
  const json = await res.json()
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
  return adminFetch(`${API_BASE}/admin/workspaces/${id}`, { method: 'DELETE' })
}

export interface AgentInfo {
  type: string
  name: string
  description: string
  configDir: string
  configFile: string
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

export function getAdminAvatar(): Promise<{ url: string }> {
  return fetch(`${API_BASE}/admin/avatar`)
    .then((res) => res.json())
    .then((json) => json.data)
}

// ── Contact Groups ──

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
  const json = await res.json()
  return (json as { data: ContactGroupsResponse }).data
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
  const res = await fetch(`${API_BASE}/contact-groups/${groupId}`, {
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
  const res = await fetch(`${API_BASE}/contact-groups/${groupId}`, { method: 'DELETE' })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
}

export async function addToContactGroup(groupId: string, taskId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/contact-groups/${groupId}/items`, {
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
  const res = await fetch(`${API_BASE}/contact-groups/${groupId}/items/${taskId}`, {
    method: 'DELETE',
  })
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

export interface SkillHubItem {
  name: string
  builtin: boolean
  description: string
  file_count: number
  total_size: number
  import_count: number
  created_at: string
}

export async function fetchSkills(): Promise<SkillHubItem[]> {
  const res = await fetch(`${API_BASE}/skills`)
  return handleResponse<SkillHubItem[]>(res)
}

export async function uploadSkill(
  file: File,
): Promise<
  AgentSkill & { valid: boolean; errors?: string[]; file_count?: number; total_size?: number }
> {
  const formData = new FormData()
  formData.append('file', file)
  const res = await fetch(`${API_BASE}/skills/upload`, {
    method: 'POST',
    body: formData,
  })
  return handleResponse<
    AgentSkill & { valid: boolean; errors?: string[]; file_count?: number; total_size?: number }
  >(res)
}

export async function confirmSkill(data: {
  name: string
  description: string
  file_count: number
  total_size: number
  tmp_dir: string
}): Promise<{ success: boolean; name: string }> {
  const res = await fetch(`${API_BASE}/skills/confirm`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  return handleResponse<{ success: boolean; name: string }>(res)
}

export async function deleteSkill(name: string): Promise<void> {
  const res = await fetch(`${API_BASE}/skills/${name}`, { method: 'DELETE' })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
}

export async function importSkill(
  skillName: string,
  sessionId: string,
): Promise<{ success: boolean }> {
  const res = await fetch(`${API_BASE}/skills/${skillName}/import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionId }),
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
  return handleResponse<{ success: boolean }>(res)
}

export async function removeSkill(skillName: string, sessionId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/skills/${skillName}/sessions/${sessionId}`, {
    method: 'DELETE',
  })
  if (!res.ok) {
    const json = await res.json().catch(() => ({}))
    throw new ApiError(res.status, (json as { msg?: string }).msg || `HTTP ${res.status}`)
  }
}
