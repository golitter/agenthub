import type {
  ApplyResult,
  ConfigResponse,
  BackupEntry,
  Draft,
  Profile,
  ProfileId,
  SaveResult,
  RestoreResult,
  ValidationResult,
} from "./schema"

let token = ""

interface ApiErrorPayload {
  error?: string
  message?: string
  issues?: unknown[]
}

export class ApiError extends Error {
  status: number
  payload: ApiErrorPayload

  constructor(status: number, payload: ApiErrorPayload) {
    super(payload.message || payload.error || `Request failed with status ${status}`)
    this.status = status
    this.payload = payload
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  if (init?.body) headers.set("Content-Type", "application/json")
  if (token) headers.set("X-Config-Center-Token", token)
  const response = await fetch(path, { ...init, headers, cache: "no-store" })
  const payload = (await response.json()) as T & ApiErrorPayload
  if (!response.ok) throw new ApiError(response.status, payload)
  return payload
}

export async function bootstrap(): Promise<void> {
  const result = await request<{ token: string }>("/api/session")
  token = result.token
}

export async function getProfiles(): Promise<Profile[]> {
  return (await request<{ profiles: Profile[] }>("/api/profiles")).profiles
}

export function getConfig(profile: ProfileId): Promise<ConfigResponse> {
  return request(`/api/config?profile=${profile}`)
}

function draftPayload(profile: ProfileId, draft: Draft) {
  return { profile, files: draft }
}

export function validateConfig(profile: ProfileId, draft: Draft): Promise<ValidationResult> {
  return request("/api/config/validate", { method: "POST", body: JSON.stringify(draftPayload(profile, draft)) })
}

export function saveConfig(profile: ProfileId, draft: Draft): Promise<SaveResult> {
  return request("/api/config/save", { method: "POST", body: JSON.stringify(draftPayload(profile, draft)) })
}

export function applyProfile(profile: ProfileId): Promise<ApplyResult> {
  return request<ApplyResult>("/api/apply", {
    method: "POST",
    body: JSON.stringify({ profile }),
  }).catch((error: unknown) => {
    if (error instanceof ApiError && error.status === 422 && typeof (error.payload as ApplyResult).exitCode === "number") {
      return error.payload as ApplyResult
    }
    throw error
  })
}

export async function getBackups(profile: ProfileId): Promise<BackupEntry[]> {
  return (await request<{ backups: BackupEntry[] }>(`/api/backups?profile=${profile}`)).backups
}

export function restoreBackup(profile: ProfileId, fileId: string, backupId: string, revision: string): Promise<RestoreResult> {
  return request("/api/backups/restore", {
    method: "POST",
    body: JSON.stringify({ profile, fileId, backupId, revision }),
  })
}
