import { useEffect, useState } from "react"
import { getBackups } from "../api"
import type { BackupEntry, ProfileId } from "../schema"

interface Props {
  profile: ProfileId
  refreshKey: string
  disabled: boolean
  hasDraft: boolean
  onRestore: (backup: BackupEntry) => Promise<void>
}

export function BackupManager({ profile, refreshKey, disabled, hasDraft, onRestore }: Props) {
  const [backups, setBackups] = useState<BackupEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [restoring, setRestoring] = useState("")
  const [error, setError] = useState("")
  const [expanded, setExpanded] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError("")
    setExpanded(false)
    getBackups(profile)
      .then((items) => { if (!cancelled) setBackups(items) })
      .catch((cause: unknown) => { if (!cancelled) setError(cause instanceof Error ? cause.message : "备份列表加载失败") })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [profile, refreshKey])

  async function restore(backup: BackupEntry) {
    const draftWarning = hasDraft ? " 未提交草稿将被丢弃。" : ""
    if (!window.confirm(`恢复 ${backup.path} 到所选备份？当前文件会先自动备份。${draftWarning}`)) return
    setRestoring(backup.id)
    setError("")
    try {
      await onRestore(backup)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "恢复失败")
    } finally {
      setRestoring("")
    }
  }

  return (
    <section className="backup-manager">
      <div className="panel-heading"><div><p className="eyebrow">Recovery</p><h3>配置备份</h3></div><span className="mono">{backups.length}</span></div>
      {loading && <div className="compact-skeleton" aria-label="正在加载备份"><span /><span /><span /></div>}
      {!loading && backups.length === 0 && !error && <p className="muted">提交配置后，最近备份会出现在这里。</p>}
      {error && <p className="inline-panel-error" role="alert">{error}</p>}
      {!loading && (expanded ? backups : backups.slice(0, 5)).map((backup) => (
        <div className="backup-row" key={backup.id}>
          <div><code>{backup.path}</code><time>{new Date(backup.createdAt).toLocaleString("zh-CN")}</time></div>
          <button className="button ghost compact" type="button" disabled={disabled || Boolean(restoring)} onClick={() => void restore(backup)}>{restoring === backup.id ? "恢复中" : "恢复"}</button>
        </div>
      ))}
      {!loading && backups.length > 5 && <button className="button ghost compact backup-expand" type="button" onClick={() => setExpanded((value) => !value)}>{expanded ? "收起" : `查看全部 ${backups.length} 份`}</button>}
    </section>
  )
}
