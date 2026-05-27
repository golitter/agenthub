import { RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'

import { getAdminStatistics, type StatisticsResponse } from '@/lib/api'

export function StatisticsPage() {
  const [data, setData] = useState<StatisticsResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [viewMode, setViewMode] = useState<'daily' | 'weekly'>('daily')

  const load = async () => {
    setLoading(true)
    try {
      setData(await getAdminStatistics())
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

  const sessions = viewMode === 'daily' ? (data?.dailySessions ?? []) : (data?.weeklySessions ?? [])
  const maxCount = Math.max(...sessions.map((s) => s.count), 1)

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
          数据统计
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
          <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
          刷新
        </button>
      </div>

      {/* Message total */}
      <div
        className="mb-6 rounded-lg p-4"
        style={{ border: '1px solid var(--border)', background: 'var(--bg-card)' }}
      >
        <div className="text-center">
          <div className="text-3xl font-bold" style={{ color: 'var(--text-primary)' }}>
            {data?.totalMessages ?? 0}
          </div>
          <div className="text-[13px]" style={{ color: 'var(--text-tertiary)' }}>
            消息总量
          </div>
        </div>
        {data && data.messagesByAgent.length > 0 && (
          <div className="mt-3 flex items-center justify-center gap-4">
            {data.messagesByAgent.map((m) => {
              const pct =
                data.totalMessages > 0 ? Math.round((m.count / data.totalMessages) * 100) : 0
              return (
                <div key={m.agentType} className="flex items-center gap-1.5 text-[12px]">
                  <span style={{ color: 'var(--text-secondary)' }}>{m.agentType}</span>
                  <span className="font-medium" style={{ color: 'var(--text-primary)' }}>
                    {pct}%
                  </span>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Session trend */}
      <div
        className="mb-6 rounded-lg p-4"
        style={{ border: '1px solid var(--border)', background: 'var(--bg-card)' }}
      >
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-[14px] font-medium" style={{ color: 'var(--text-primary)' }}>
            会话趋势
          </h3>
          <div className="flex gap-1">
            {(['daily', 'weekly'] as const).map((mode) => (
              <button
                key={mode}
                onClick={() => setViewMode(mode)}
                className="rounded-md px-2.5 py-1 text-[12px]"
                style={{
                  background: viewMode === mode ? 'var(--primary-soft)' : 'transparent',
                  color: viewMode === mode ? 'var(--color-brand)' : 'var(--text-secondary)',
                }}
              >
                {mode === 'daily' ? '按天' : '按周'}
              </button>
            ))}
          </div>
        </div>
        <div className="flex items-end gap-2" style={{ height: 160 }}>
          {sessions.map((s) => (
            <div key={s.date} className="flex flex-1 flex-col items-center gap-1">
              <span className="text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
                {s.count}
              </span>
              <div
                className="w-full rounded-t-sm transition-all"
                style={{
                  height: `${(s.count / maxCount) * 120}px`,
                  minHeight: s.count > 0 ? 4 : 0,
                  background: 'var(--color-brand)',
                }}
              />
              <span className="text-[10px]" style={{ color: 'var(--text-tertiary)' }}>
                {s.date.slice(5)}
              </span>
            </div>
          ))}
        </div>
      </div>

      {/* Storage trend */}
      {data && data.storageDays.length > 0 && (
        <div
          className="rounded-lg p-4"
          style={{ border: '1px solid var(--border)', background: 'var(--bg-card)' }}
        >
          <h3 className="mb-3 text-[14px] font-medium" style={{ color: 'var(--text-primary)' }}>
            存储趋势
          </h3>
          <div className="flex items-end gap-2" style={{ height: 120 }}>
            {data.storageDays.map((d, i) => {
              const maxStorage = Math.max(...data.storageDays.map((s) => s.size), 1)
              return (
                <div key={i} className="flex flex-1 flex-col items-center gap-1">
                  <span className="text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
                    {d.size.toFixed(0)} GB
                  </span>
                  <div
                    className="w-full rounded-t-sm transition-all"
                    style={{
                      height: `${(d.size / maxStorage) * 80}px`,
                      minHeight: 4,
                      background: 'var(--color-brand)',
                      opacity: 0.5,
                    }}
                  />
                  <span className="text-[10px]" style={{ color: 'var(--text-tertiary)' }}>
                    {data.storageLabels[i]?.slice(5) ?? ''}
                  </span>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
