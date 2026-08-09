export type ProfileId = "local" | "docker"

export interface Profile {
  id: ProfileId
  title: string
  description: string
  available: boolean
  missing: string[]
  fileCount: number
}

export interface ConfigFile {
  id: string
  path: string
  examplePath: string
  title: string
  kind: "env" | "yaml" | "json"
  section: string
  exists: boolean
  revision: string
  exampleContent: string
  actualContent: string
  sameContent: boolean
}

export interface ConfigResponse {
  profile: ProfileId
  files: ConfigFile[]
}

export interface Issue {
  severity: "error" | "warning"
  code: string
  message: string
  fileId: string
}

export interface ValidationResult {
  ok: boolean
  issues: Issue[]
  changes: Array<{ fileId: string; path: string; changes: unknown[] }>
}

export interface SaveResult {
  saved: string[]
  backups: string[]
  warnings: Issue[]
  ignored: string[]
  revisions: Record<string, string>
}

export interface BackupEntry {
  id: string
  fileId: string
  path: string
  size: number
  createdAt: string
  currentRevision: string
}

export interface RestoreResult {
  restored: string
  revision: string
  previousBackup: string | null
}

export interface ApplyResult {
  ok: boolean
  profile: ProfileId
  commands: string[][]
  exitCode: number
  output: string
}

export interface FileDraft {
  revision: string
  content: string
}

export type Draft = Record<string, FileDraft>
