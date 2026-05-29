import { RefreshCw, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'

import { deleteAdminWorkspace, getAdminWorkspaces, type WorkspaceItem } from '@/lib/api'

export function WorkspacePage() {
  const [workspaces, setWorkspaces] = useState<WorkspaceItem[]>([])
  const [stats, setStats] = useState({ total: 0, active: 0, cleaned: 0, totalDisk: 0 })
  const [loading, setLoading] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const res = await getAdminWorkspaces()
      setWorkspaces(res.workspaces ?? [])
      setStats({
        total: res.total,
        active: res.active,
        cleaned: res.cleaned,
        totalDisk: res.totalDisk,
      })
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

  const handleDelete = async (id: string) => {
    if (!confirm('确认清理该工作区？')) return
    try {
      await deleteAdminWorkspace(id)
      load()
    } catch {
      /* ignore */
    }
  }

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
          工作区管理
        </h2>
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
            className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`}
            strokeWidth={1.25}
          />
          刷新
        </button>
      </div>

      <div className="mb-4 grid grid-cols-4 gap-3">
        {[
          { label: '总数', value: stats.total },
          { label: '活跃', value: stats.active },
          { label: '已清理', value: stats.cleaned },
          { label: '磁盘占用', value: `${stats.totalDisk.toFixed(1)} MB` },
        ].map((s) => (
          <div
            key={s.label}
            className="rounded-lg p-3 text-center"
            style={{ border: '1px solid var(--border)', background: 'var(--bg-card)' }}
          >
            <div className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
              {s.value}
            </div>
            <div className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
              {s.label}
            </div>
          </div>
        ))}
      </div>

      <div className="rounded-lg overflow-hidden" style={{ border: '1px solid var(--border)' }}>
        <table className="w-full text-[13px]">
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)', background: 'var(--bg-hover)' }}>
              {['ID', '任务', 'Agent', '磁盘', '状态', '操作'].map((h) => (
                <th
                  key={h}
                  className="px-3 py-2 text-left font-medium"
                  style={{ color: 'var(--text-secondary)' }}
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {workspaces.map((ws) => (
              <tr key={ws.id} style={{ borderBottom: '1px solid var(--border)' }}>
                <td
                  className="px-3 py-2 font-mono text-xs"
                  style={{ color: 'var(--text-tertiary)' }}
                >
                  {ws.id.slice(0, 8)}
                </td>
                <td className="px-3 py-2" style={{ color: 'var(--text-secondary)' }}>
                  {ws.task.slice(0, 8)}
                </td>
                <td className="px-3 py-2" style={{ color: 'var(--text-primary)' }}>
                  {ws.agent}
                </td>
                <td className="px-3 py-2" style={{ color: 'var(--text-secondary)' }}>
                  {ws.disk_mb.toFixed(1)} MB
                </td>
                <td className="px-3 py-2">
                  <span
                    className="inline-block rounded-full px-2 py-0.5 text-[11px]"
                    style={{
                      background:
                        ws.status === 'active' ? 'rgba(34, 197, 94, 0.1)' : 'var(--bg-hover)',
                      color:
                        ws.status === 'active' ? 'var(--color-success)' : 'var(--text-tertiary)',
                    }}
                  >
                    {ws.status}
                  </span>
                </td>
                <td className="px-3 py-2">
                  {ws.status === 'active' && (
                    <button
                      onClick={() => handleDelete(ws.id)}
                      style={{ color: 'var(--color-error)' }}
                    >
                      <Trash2 className="h-3.5 w-3.5" strokeWidth={1.25} />
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {workspaces.length === 0 && (
          <div className="py-8 text-center text-[13px]" style={{ color: 'var(--text-tertiary)' }}>
            暂无工作区
          </div>
        )}
      </div>
    </div>
  )
}
