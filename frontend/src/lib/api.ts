import type { AgentType } from '@/generated/request'
import { API_BASE } from '@/lib/constants'

// TODO: migrate to generated types from contracts/schemas
export interface Task {
  task_id: string
  title: string
  repo_path: string
  status: string
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
  const json = await res.json()
  return json.data
}

export async function fetchTask(taskId: string): Promise<TaskDetail> {
  const res = await fetch(`${API_BASE}/tasks/${taskId}`)
  const json = await res.json()
  return json.data
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
  const json = await res.json()
  return json.data
}

export async function fetchAgentTypes(): Promise<AgentTypeInfo[]> {
  const res = await fetch(`${API_BASE}/agent-types`)
  const json = await res.json()
  const data: unknown[] = json.data
  return data.map((item) =>
    typeof item === 'string'
      ? { type: item as AgentType, name: item, description: '' }
      : (item as AgentTypeInfo),
  )
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
}

export async function fetchConversations(): Promise<Conversation[]> {
  const tasks = await fetchTasks()
  const details = await Promise.all(tasks.map((t) => fetchTask(t.task_id)))
  const convos: Conversation[] = []
  for (const detail of details) {
    for (const s of detail.sessions) {
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
      })
    }
  }
  convos.sort((a, b) => new Date(b.lastActiveAt).getTime() - new Date(a.lastActiveAt).getTime())
  return convos
}

export async function createConversation(
  agentType: AgentType,
  agentName?: string,
  title?: string,
  repoPath?: string,
): Promise<Conversation> {
  const taskTitle = title ?? `Chat with ${agentName || agentType}`
  const task = await createTask(taskTitle, [{ type: agentType, name: agentName }], repoPath)
  const detail = await fetchTask(task.task_id)
  const session = detail.sessions[0]
  if (!session) throw new Error('Backend failed to create session')
  return {
    taskId: task.task_id,
    sessionId: session.session_id,
    agentType,
    agentName: agentName ?? '',
    title: agentName || agentType,
    lastActiveAt: session.updated_at,
    taskTitle: task.title,
    status: session.status,
    avatarUrl: session.avatar_url || undefined,
    repoPath: task.repo_path || undefined,
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
  created_at: string
}

// Submit a message and get back the agent message_id for streaming
export async function submitMessage(
  taskId: string,
  body: { message: string; session_id: string; agent_type?: string },
): Promise<{ message_id: string; status: string }> {
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

export interface TaskMessagesResponse {
  data: TaskMessage[]
  has_more: boolean
}

export async function getTaskMessages(
  taskId: string,
  params?: { limit?: number; before?: number },
): Promise<TaskMessagesResponse> {
  const searchParams = new URLSearchParams()
  if (params?.limit) searchParams.set('limit', String(params.limit))
  if (params?.before) searchParams.set('before', String(params.before))
  const qs = searchParams.toString()
  const url = `${API_BASE}/tasks/${taskId}/messages${qs ? `?${qs}` : ''}`
  const res = await fetch(url)
  const json = await res.json()
  return json.data
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
  return adminFetch<{ url: string }>(`${API_BASE}/admin/avatar`)
}

export function updateAdminAvatar(url: string): Promise<{ success: boolean }> {
  return adminFetch<{ success: boolean }>(`${API_BASE}/admin/avatar`, {
    method: 'PUT',
    body: JSON.stringify({ url }),
  })
}
