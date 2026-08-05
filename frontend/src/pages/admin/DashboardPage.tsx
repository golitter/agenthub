import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'

import { AdminQueryError } from '@/components/admin/AdminQueryError'
import { getAdminResources, type ResourcesResponse } from '@/lib/api'
import { UI_MESSAGES } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

function ProgressBar({ used, total, unit }: { used: number; total: number; unit: string }) {
  const pct = total > 0 ? Math.round((used / total) * 100) : 0
  const barColor =
    pct > 80 ? 'var(--color-error)' : pct > 60 ? 'var(--color-warning)' : 'var(--color-success)'

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between text-[13px]">
        <span className="text-text-secondary">
          {used.toFixed(1)} / {total.toFixed(1)} {unit}
        </span>
        <span className="font-medium text-foreground">{pct}%</span>
      </div>
      <div className="h-2 w-full rounded-sm bg-border">
        <div
          className="h-full rounded-sm transition-[transform,opacity]"
          style={{ width: `${pct}%`, background: barColor }}
        />
      </div>
    </div>
  )
}

export function DashboardPage() {
  const { data, isError, isLoading, refetch, isRefetching } = useQuery<ResourcesResponse>({
    queryKey: ['admin-resources'],
    queryFn: getAdminResources,
    staleTime: 30_000,
  })

  return (
    <div className="p-6">
      <div className="mb-6 flex items-center justify-between">
        <h2 className="text-lg font-semibold text-foreground">总览仪表盘</h2>
        <button
          type="button"
          onClick={() => refetch()}
          disabled={isLoading}
          className="flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-[13px] text-text-secondary transition-[background,transform,opacity] hover:bg-bg-hover active:scale-[0.98] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          <RefreshCw
            className={cn('h-3.5 w-3.5', isRefetching && 'animate-spin')}
            strokeWidth={1.25}
          />
          刷新
        </button>
      </div>
      {isError && <AdminQueryError onRetry={() => refetch()} />}

      <div className="grid gap-6 md:grid-cols-3">
        {data ? (
          <>
            {(['disk', 'memory', 'redis'] as const).map((key) => {
              const labels = { disk: '磁盘', memory: '内存', redis: 'Redis' }
              return (
                <div key={key} className="rounded-lg border border-border bg-card p-4">
                  <h3 className="mb-3 text-[13px] font-medium text-text-secondary">
                    {labels[key]}
                  </h3>
                  <ProgressBar {...data[key]} />
                </div>
              )
            })}
          </>
        ) : (
          <>
            {isLoading ? (
              Array.from({ length: 3 }).map((_, index) => (
                <div
                  key={index}
                  className="h-32 rounded-lg border border-border skeleton-sheen"
                  aria-hidden="true"
                />
              ))
            ) : (
              <div className="col-span-full py-8 text-center text-sm text-tertiary">
                {UI_MESSAGES.NO_DATA}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
