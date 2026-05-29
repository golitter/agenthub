import { RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'

import { getAdminResources, type ResourcesResponse } from '@/lib/api'

function ProgressBar({ used, total, unit }: { used: number; total: number; unit: string }) {
  const pct = total > 0 ? Math.round((used / total) * 100) : 0
  const barColor =
    pct > 80 ? 'var(--color-error)' : pct > 60 ? 'var(--color-warning)' : 'var(--color-success)'

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between text-[13px]">
        <span style={{ color: 'var(--text-secondary)' }}>
          {used.toFixed(1)} / {total.toFixed(1)} {unit}
        </span>
        <span className="font-medium" style={{ color: 'var(--text-primary)' }}>
          {pct}%
        </span>
      </div>
      <div className="h-2 w-full rounded-sm" style={{ background: 'var(--border)' }}>
        <div
          className="h-full rounded-sm transition-all"
          style={{ width: `${pct}%`, background: barColor }}
        />
      </div>
    </div>
  )
}

export function DashboardPage() {
  const [data, setData] = useState<ResourcesResponse | null>(null)
  const [loading, setLoading] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const res = await getAdminResources()
      setData(res)
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

  return (
    <div className="p-6">
      <div className="mb-6 flex items-center justify-between">
        <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
          总览仪表盘
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

      <div className="grid gap-6 md:grid-cols-3">
        {data ? (
          <>
            {(['disk', 'memory', 'redis'] as const).map((key) => {
              const labels = { disk: '磁盘', memory: '内存', redis: 'Redis' }
              return (
                <div
                  key={key}
                  className="rounded-lg p-4"
                  style={{ border: '1px solid var(--border)', background: 'var(--bg-card)' }}
                >
                  <h3
                    className="mb-3 text-[13px] font-medium"
                    style={{ color: 'var(--text-secondary)' }}
                  >
                    {labels[key]}
                  </h3>
                  <ProgressBar {...data[key]} />
                </div>
              )
            })}
          </>
        ) : (
          <div
            className="col-span-3 py-8 text-center text-sm"
            style={{ color: 'var(--text-tertiary)' }}
          >
            {loading ? '加载中...' : '暂无数据'}
          </div>
        )}
      </div>
    </div>
  )
}
