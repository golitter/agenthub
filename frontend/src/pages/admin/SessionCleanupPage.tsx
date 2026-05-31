import { RefreshCw, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'

import { type Conversation, deleteAdminSessions, fetchConversations } from '@/lib/api'
import { cn } from '@/lib/utils'

const STATUS_COLORS: Record<string, { bg: string; text: string }> = {
  running: { bg: 'rgba(34, 197, 94, 0.1)', text: 'var(--color-success)' },
}

export function SessionCleanupPage() {
  const [sessions, setSessions] = useState<Conversation[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [filter, setFilter] = useState('')

  const load = async () => {
    setLoading(true)
    try {
      const convos = await fetchConversations()
      setSessions(convos)
    } catch {
      /* ignore */
    }
    setLoading(false)
  }

  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    load()
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [])

  const toggleSelect = (id: string) => {
    const next = new Set(selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setSelected(next)
  }

  const handleDelete = async () => {
    if (selected.size === 0) return
    if (!confirm(`确认清理 ${selected.size} 个会话？此操作不可恢复。`)) return
    setDeleting(true)
    try {
      await deleteAdminSessions(Array.from(selected))
      setSelected(new Set())
      load()
    } catch {
      /* ignore */
    }
    setDeleting(false)
  }

  const filtered = filter ? sessions.filter((s) => s.agentType === filter) : sessions
  const agentTypes = [...new Set(sessions.map((s) => s.agentType))]

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
          会话清理
        </h2>
        <div className="flex items-center gap-2">
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="h-8 rounded-md px-2 text-[13px]"
            style={{
              border: '1px solid var(--border)',
              background: 'var(--bg-card)',
              color: 'var(--text-secondary)',
            }}
          >
            <option value="">全部类型</option>
            {agentTypes.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
          <button
            onClick={load}
            disabled={loading}
            className="flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[13px] transition-colors"
            style={{ border: '1px solid var(--border)', color: 'var(--text-secondary)' }}
            onMouseEnter={(e) => {
              e.currentTarget.style.background = 'var(--bg-hover)'
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.background = 'transparent'
            }}
          >
            <RefreshCw
              className={cn('h-3.5 w-3.5', loading && 'animate-spin')}
              strokeWidth={1.25}
            />
            刷新
          </button>
        </div>
      </div>

      {selected.size > 0 && (
        <div className="mb-3 flex items-center gap-2">
          <span className="text-[13px]" style={{ color: 'var(--text-secondary)' }}>
            已选 {selected.size} 项
          </span>
          <button
            onClick={handleDelete}
            disabled={deleting}
            className="flex items-center gap-1 rounded-md px-3 py-1 text-[12px] transition-colors"
            style={{ background: 'var(--color-error)', color: 'var(--primary-foreground)' }}
          >
            <Trash2 className="h-3 w-3" strokeWidth={1.25} />
            {deleting ? '清理中...' : '批量清理'}
          </button>
        </div>
      )}

      <div className="rounded-lg overflow-hidden" style={{ border: '1px solid var(--border)' }}>
        <table className="w-full text-[13px]">
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)', background: 'var(--bg-hover)' }}>
              <th
                className="px-3 py-2 text-left font-medium"
                style={{ color: 'var(--text-secondary)' }}
              >
                选择
              </th>
              <th
                className="px-3 py-2 text-left font-medium"
                style={{ color: 'var(--text-secondary)' }}
              >
                会话 ID
              </th>
              <th
                className="px-3 py-2 text-left font-medium"
                style={{ color: 'var(--text-secondary)' }}
              >
                Agent
              </th>
              <th
                className="px-3 py-2 text-left font-medium"
                style={{ color: 'var(--text-secondary)' }}
              >
                类型
              </th>
              <th
                className="px-3 py-2 text-left font-medium"
                style={{ color: 'var(--text-secondary)' }}
              >
                任务
              </th>
              <th
                className="px-3 py-2 text-left font-medium"
                style={{ color: 'var(--text-secondary)' }}
              >
                状态
              </th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((s) => {
              const sc = STATUS_COLORS[s.status] ?? {
                bg: 'var(--bg-hover)',
                text: 'var(--text-tertiary)',
              }
              return (
                <tr key={s.sessionId} style={{ borderBottom: '1px solid var(--border)' }}>
                  <td className="px-3 py-2">
                    <input
                      type="checkbox"
                      checked={selected.has(s.sessionId)}
                      onChange={() => toggleSelect(s.sessionId)}
                      className="h-3.5 w-3.5 rounded"
                    />
                  </td>
                  <td
                    className="px-3 py-2 font-mono text-xs"
                    style={{ color: 'var(--text-tertiary)' }}
                  >
                    {s.sessionId.slice(0, 8)}
                  </td>
                  <td className="px-3 py-2" style={{ color: 'var(--text-primary)' }}>
                    {s.agentName || s.agentType}
                  </td>
                  <td className="px-3 py-2" style={{ color: 'var(--text-secondary)' }}>
                    {s.agentType}
                  </td>
                  <td className="px-3 py-2" style={{ color: 'var(--text-secondary)' }}>
                    {s.taskTitle}
                  </td>
                  <td className="px-3 py-2">
                    <span
                      className="inline-block rounded-full px-2 py-0.5 text-[11px]"
                      style={{ background: sc.bg, color: sc.text }}
                    >
                      {s.status}
                    </span>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
        {filtered.length === 0 && (
          <div className="py-8 text-center text-[13px]" style={{ color: 'var(--text-tertiary)' }}>
            暂无会话
          </div>
        )}
      </div>
    </div>
  )
}
