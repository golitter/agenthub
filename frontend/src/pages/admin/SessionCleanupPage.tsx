import { useQuery } from '@tanstack/react-query'
import { RefreshCw, Trash2 } from 'lucide-react'
import { useState } from 'react'

import { type Conversation, deleteAdminSessions, fetchConversations } from '@/lib/api'
import { UI_ACTIONS, UI_CONFIRMS, UI_MESSAGES, UI_STATUS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

const STATUS_CLASSES: Record<string, { bg: string; text: string }> = {
  running: { bg: 'bg-success/10', text: 'text-success' },
  streaming: { bg: 'bg-success/10', text: 'text-success' },
  loading: { bg: 'bg-warning/10', text: 'text-warning' },
  done: { bg: 'bg-primary/10', text: 'text-primary' },
  error: { bg: 'bg-destructive/10', text: 'text-destructive' },
  idle: { bg: 'bg-hover', text: 'text-text-secondary' },
}

export function SessionCleanupPage() {
  const {
    data: sessions,
    isLoading,
    refetch,
    isRefetching,
  } = useQuery<Conversation[]>({
    queryKey: ['admin-sessions'],
    queryFn: fetchConversations,
    staleTime: 30_000,
  })
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [deleting, setDeleting] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [filter, setFilter] = useState('')

  const toggleSelect = (id: string) => {
    const next = new Set(selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setSelected(next)
    setConfirmingDelete(false)
  }

  const handleDelete = async () => {
    if (selected.size === 0) return
    if (!confirmingDelete) {
      setConfirmingDelete(true)
      return
    }
    setDeleting(true)
    try {
      await deleteAdminSessions(Array.from(selected))
      setSelected(new Set())
      setConfirmingDelete(false)
      refetch()
    } catch {
      /* ignore */
    } finally {
      setDeleting(false)
    }
  }

  const allSessions = sessions ?? []
  const filtered = filter ? allSessions.filter((s) => s.agentType === filter) : allSessions
  const agentTypes = [...new Set(allSessions.map((s) => s.agentType))]

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold text-foreground">会话清理</h2>
        <div className="flex items-center gap-2">
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="h-8 rounded-md border border-border bg-card px-2 text-[13px] text-text-secondary"
          >
            <option value="">全部类型</option>
            {agentTypes.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={() => refetch()}
            disabled={isLoading}
            className="flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-[13px] text-text-secondary transition-[background,transform,opacity] hover:bg-hover active:scale-[0.98] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            <RefreshCw
              className={cn('h-3.5 w-3.5', isRefetching && 'animate-spin')}
              strokeWidth={1.25}
            />
            刷新
          </button>
        </div>
      </div>

      {selected.size > 0 && (
        <div className="mb-3 flex flex-wrap items-center gap-2 rounded-[8px] border border-border bg-card/70 px-3 py-2">
          <span className="text-[13px] text-text-secondary">已选 {selected.size} 项</span>
          {confirmingDelete && (
            <span className="text-[12px] text-destructive">
              {UI_CONFIRMS.CLEAN_SESSIONS} {selected.size} 个会话？此操作不可恢复。
            </span>
          )}
          <button
            type="button"
            onClick={handleDelete}
            disabled={deleting}
            className="flex items-center gap-1 rounded-md bg-error px-3 py-1 text-[12px] font-medium text-primary-foreground transition-[background,transform,opacity] hover:bg-error/90 active:scale-[0.97] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            <Trash2 className="h-3 w-3" strokeWidth={1.25} />
            {deleting
              ? UI_STATUS.DELETING
              : confirmingDelete
                ? UI_ACTIONS.CONFIRM
                : UI_ACTIONS.CLEAN_UP}
          </button>
          {confirmingDelete && (
            <button
              type="button"
              className="rounded-md border border-border px-3 py-1 text-[12px] text-text-secondary transition-[background,color,transform] hover:bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              onClick={() => setConfirmingDelete(false)}
            >
              {UI_ACTIONS.CANCEL}
            </button>
          )}
        </div>
      )}

      <div className="rounded-lg overflow-hidden border border-border">
        <table className="w-full text-[13px]">
          <thead>
            <tr className="border-b border-border bg-hover">
              <th className="px-3 py-2 text-left font-medium text-foreground">选择</th>
              <th className="px-3 py-2 text-left font-medium text-foreground">会话 ID</th>
              <th className="px-3 py-2 text-left font-medium text-foreground">Agent</th>
              <th className="px-3 py-2 text-left font-medium text-foreground">类型</th>
              <th className="px-3 py-2 text-left font-medium text-foreground">任务</th>
              <th className="px-3 py-2 text-left font-medium text-foreground">状态</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((s) => {
              const sc = STATUS_CLASSES[s.status] ?? { bg: 'bg-hover', text: 'text-text-secondary' }
              return (
                <tr key={s.sessionId} className="border-b border-border">
                  <td className="px-3 py-2">
                    <input
                      type="checkbox"
                      checked={selected.has(s.sessionId)}
                      onChange={() => toggleSelect(s.sessionId)}
                      className="h-3.5 w-3.5 rounded accent-primary"
                      aria-label={`${UI_ACTIONS.SELECT} ${s.sessionId.slice(0, 8)}`}
                    />
                  </td>
                  <td className="px-3 py-2 font-mono text-xs text-text-secondary">
                    {s.sessionId.slice(0, 8)}
                  </td>
                  <td className="px-3 py-2 text-foreground">{s.agentName || s.agentType}</td>
                  <td className="px-3 py-2 text-text-secondary">{s.agentType}</td>
                  <td className="px-3 py-2 text-text-secondary">{s.taskTitle}</td>
                  <td className="px-3 py-2">
                    <span
                      className={cn(
                        'inline-block rounded-full px-2 py-0.5 text-[11px]',
                        sc.bg,
                        sc.text,
                      )}
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
          <div className="py-8 text-center text-[13px] text-tertiary">
            {UI_MESSAGES.NO_CONVERSATIONS}
          </div>
        )}
      </div>
    </div>
  )
}
