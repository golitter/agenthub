import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useState } from 'react'

import { AdminQueryError } from '@/components/admin/AdminQueryError'
import { Button } from '@/components/ui/button'
import { getAdminStatistics, type StatisticsResponse } from '@/lib/api'
import { UI_ACTIONS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

export function StatisticsPage() {
  const { data, isError, isLoading, refetch, isRefetching } = useQuery<StatisticsResponse>({
    queryKey: ['admin-statistics'],
    queryFn: getAdminStatistics,
    staleTime: 30_000,
  })
  const [viewMode, setViewMode] = useState<'daily' | 'weekly'>('daily')

  const sessions = viewMode === 'daily' ? (data?.dailySessions ?? []) : (data?.weeklySessions ?? [])
  const maxCount = Math.max(...sessions.map((s) => s.count), 1)
  const maxStorage = Math.max(...(data?.storageDays ?? []).map((s) => s.size), 1)

  return (
    <div className="p-4 sm:p-6">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold text-foreground">数据统计</h2>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          onClick={() => refetch()}
          disabled={isLoading}
        >
          <RefreshCw
            className={cn('h-3.5 w-3.5', isRefetching && 'animate-spin')}
            strokeWidth={1.25}
          />
          {UI_ACTIONS.REFRESH}
        </Button>
      </div>
      {isError && <AdminQueryError onRetry={() => refetch()} />}

      {/* 消息总数 */}
      <div className="mb-6 rounded-lg border border-border bg-card p-4">
        <div className="text-center">
          <div className="text-3xl font-bold text-foreground">{data?.totalMessages ?? 0}</div>
          <div className="text-[13px] text-tertiary">消息总量</div>
        </div>
        {data && data.messagesByAgent.length > 0 && (
          <div className="mt-3 flex flex-wrap items-center justify-center gap-x-4 gap-y-2">
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

      {/* 会话趋势 */}
      <div className="mb-6 rounded-lg border border-border bg-card p-4">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-[14px] font-medium text-foreground">会话趋势</h3>
          <div className="flex gap-1" role="group" aria-label="会话趋势范围">
            {(['daily', 'weekly'] as const).map((mode) => (
              <button
                key={mode}
                type="button"
                onClick={() => setViewMode(mode)}
                aria-pressed={viewMode === mode}
                className={cn(
                  'rounded-md px-2.5 py-1 text-[12px] transition-[background,color,transform] active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
                  viewMode === mode
                    ? 'bg-primary-soft text-brand'
                    : 'text-text-secondary hover:bg-bg-hover hover:text-foreground',
                )}
              >
                {mode === 'daily' ? '按天' : '按周'}
              </button>
            ))}
          </div>
        </div>
        <div
          className="overflow-x-auto"
          role="region"
          aria-label="会话趋势图"
          aria-describedby="sessions-trend-summary"
          tabIndex={0}
        >
          <p id="sessions-trend-summary" className="sr-only">
            {sessions.length > 0
              ? `${viewMode === 'daily' ? '按天' : '按周'}会话数量趋势，最高值为 ${maxCount}。`
              : '暂无会话趋势数据。'}
          </p>
          <div
            className="flex min-w-[560px] items-end gap-2"
            style={{ height: 160 }}
            aria-hidden="true"
          >
            {data &&
              sessions.map((s) => (
                <div key={s.date} className="flex min-w-8 flex-1 flex-col items-center gap-1">
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
          <SessionsTrendTable sessions={sessions} />
        </div>
      </div>

      {/* 存储趋势 */}
      {data && data.storageDays.length > 0 && (
        <div className="rounded-lg border border-border bg-card p-4">
          <h3 className="mb-3 text-[14px] font-medium text-foreground">存储趋势</h3>
          <div
            className="overflow-x-auto"
            role="region"
            aria-label="存储趋势图"
            aria-describedby="storage-trend-summary"
            tabIndex={0}
          >
            <p id="storage-trend-summary" className="sr-only">
              存储趋势，最高值为 {maxStorage.toFixed(0)} GB。
            </p>
            <div
              className="flex min-w-[560px] items-end gap-2"
              style={{ height: 120 }}
              aria-hidden="true"
            >
              {data.storageDays.map((d, i) => {
                return (
                  <div key={i} className="flex min-w-8 flex-1 flex-col items-center gap-1">
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
          <StorageTrendTable days={data.storageDays} labels={data.storageLabels} />
        </div>
      )}
    </div>
  )
}

function SessionsTrendTable({ sessions }: { sessions: StatisticsResponse['dailySessions'] }) {
  return (
    <table className="sr-only">
      <caption>会话趋势数据</caption>
      <thead>
        <tr>
          <th scope="col">日期</th>
          <th scope="col">会话数</th>
        </tr>
      </thead>
      <tbody>
        {sessions.map((session) => (
          <tr key={session.date}>
            <th scope="row">{session.date}</th>
            <td>{session.count}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function StorageTrendTable({
  days,
  labels,
}: {
  days: StatisticsResponse['storageDays']
  labels: string[]
}) {
  return (
    <table className="sr-only">
      <caption>存储趋势数据</caption>
      <thead>
        <tr>
          <th scope="col">日期</th>
          <th scope="col">存储量（GB）</th>
        </tr>
      </thead>
      <tbody>
        {days.map((day, index) => (
          <tr key={day.date}>
            <th scope="row">{labels[index] ?? day.date}</th>
            <td>{day.size.toFixed(2)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
