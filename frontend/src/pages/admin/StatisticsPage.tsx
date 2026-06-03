import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useState } from 'react'

import { getAdminStatistics, type StatisticsResponse } from '@/lib/api'
import { cn } from '@/lib/utils'

export function StatisticsPage() {
  const { data, isLoading, refetch, isRefetching } = useQuery<StatisticsResponse>({
    queryKey: ['admin-statistics'],
    queryFn: getAdminStatistics,
    staleTime: 30_000,
  })
  const [viewMode, setViewMode] = useState<'daily' | 'weekly'>('daily')

  const sessions = viewMode === 'daily' ? (data?.dailySessions ?? []) : (data?.weeklySessions ?? [])
  const maxCount = Math.max(...sessions.map((s) => s.count), 1)

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold text-foreground">数据统计</h2>
        <button
          onClick={() => refetch()}
          disabled={isLoading}
          className="flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-[13px] text-text-secondary transition-[transform,opacity]"
          onMouseEnter={(e) => {
            e.currentTarget.style.background = 'var(--bg-hover)'
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.background = 'transparent'
          }}
        >
          <RefreshCw
            className={cn('h-3.5 w-3.5', isRefetching && 'animate-spin')}
            strokeWidth={1.25}
          />
          刷新
        </button>
      </div>

      {/* Message total */}
      <div className="mb-6 rounded-lg border border-border bg-card p-4">
        <div className="text-center">
          <div className="text-3xl font-bold text-foreground">{data?.totalMessages ?? 0}</div>
          <div className="text-[13px] text-tertiary">消息总量</div>
        </div>
        {data && data.messagesByAgent.length > 0 && (
          <div className="mt-3 flex items-center justify-center gap-4">
            {data.messagesByAgent.map((m) => {
              const pct =
                data.totalMessages > 0 ? Math.round((m.count / data.totalMessages) * 100) : 0
              return (
                <div key={m.agentType} className="flex items-center gap-1.5 text-[12px]">
                  <span className="text-text-secondary">{m.agentType}</span>
                  <span className="font-medium text-foreground">{pct}%</span>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Session trend */}
      <div className="mb-6 rounded-lg border border-border bg-card p-4">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-[14px] font-medium text-foreground">会话趋势</h3>
          <div className="flex gap-1">
            {(['daily', 'weekly'] as const).map((mode) => (
              <button
                key={mode}
                onClick={() => setViewMode(mode)}
                className="rounded-md px-2.5 py-1 text-[12px] text-text-secondary"
                style={{
                  background: viewMode === mode ? 'var(--primary-soft)' : 'transparent',
                  color: viewMode === mode ? 'var(--color-brand)' : undefined,
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
              <span className="text-[11px] text-tertiary">{s.count}</span>
              <div
                className="w-full rounded-t-sm bg-brand transition-[transform,opacity]"
                style={{
                  height: `${(s.count / maxCount) * 120}px`,
                  minHeight: s.count > 0 ? 4 : 0,
                }}
              />
              <span className="text-[11px] text-tertiary">{s.date.slice(5)}</span>
            </div>
          ))}
        </div>
      </div>

      {/* Storage trend */}
      {data && data.storageDays.length > 0 && (
        <div className="rounded-lg border border-border bg-card p-4">
          <h3 className="mb-3 text-[14px] font-medium text-foreground">存储趋势</h3>
          <div className="flex items-end gap-2" style={{ height: 120 }}>
            {data.storageDays.map((d, i) => {
              const maxStorage = Math.max(...data.storageDays.map((s) => s.size), 1)
              return (
                <div key={i} className="flex flex-1 flex-col items-center gap-1">
                  <span className="text-[11px] text-tertiary">{d.size.toFixed(0)} GB</span>
                  <div
                    className="w-full rounded-t-sm bg-brand/50 transition-[transform,opacity]"
                    style={{
                      height: `${(d.size / maxStorage) * 80}px`,
                      minHeight: 4,
                    }}
                  />
                  <span className="text-[11px] text-tertiary">
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
