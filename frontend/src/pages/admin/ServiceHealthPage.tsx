import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'

import { AdminQueryError } from '@/components/admin/AdminQueryError'
import { getAdminServices } from '@/lib/api'
import { UI_MESSAGES } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

export function ServiceHealthPage() {
  const {
    data: services,
    isError,
    isLoading,
    refetch,
    isRefetching,
  } = useQuery({
    queryKey: ['admin-services'],
    queryFn: getAdminServices,
    staleTime: 30_000,
  })

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold text-foreground">服务健康</h2>
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
      {isError && <AdminQueryError onRetry={() => refetch()} />}

      <div className="grid gap-4 md:grid-cols-3">
        {isLoading
          ? Array.from({ length: 3 }).map((_, index) => (
              <div
                key={index}
                className="h-40 rounded-lg border border-border skeleton-sheen"
                aria-hidden="true"
              />
            ))
          : (services ?? []).map((svc) => (
              <div key={svc.name} className="rounded-lg border border-border bg-card p-4">
                <div className="mb-3 flex items-center gap-2">
                  <span
                    className={cn(
                      'h-2.5 w-2.5 rounded-full',
                      svc.status === 'Running' && 'animate-pulse',
                    )}
                    style={{
                      background:
                        svc.status === 'Running' ? 'var(--color-success)' : 'var(--color-error)',
                    }}
                  />
                  <span className="text-[14px] font-medium text-foreground">{svc.name}</span>
                </div>
                <div className="flex flex-col gap-1.5 text-[12px]">
                  {[
                    {
                      label: '状态',
                      value: svc.status,
                      color:
                        svc.status === 'Running' ? 'var(--color-success)' : 'var(--color-error)',
                    },
                    { label: '运行时长', value: svc.uptime },
                    { label: '版本', value: svc.version },
                    { label: '端口', value: String(svc.port) },
                    { label: '上次检查', value: svc.lastCheck },
                  ].map((row) => (
                    <div key={row.label} className="flex justify-between">
                      <span className="text-tertiary">{row.label}</span>
                      <span
                        className={row.color ? '' : 'text-text-secondary'}
                        style={row.color ? { color: row.color } : undefined}
                      >
                        {row.value}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            ))}
        {!isLoading && !services?.length && (
          <div className="col-span-full py-8 text-center text-sm text-tertiary">
            {UI_MESSAGES.NO_DATA}
          </div>
        )}
      </div>
    </div>
  )
}
