import { RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'

import { getAdminServices, type ServiceInfo } from '@/lib/api'

export function ServiceHealthPage() {
  const [services, setServices] = useState<ServiceInfo[]>([])
  const [loading, setLoading] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      setServices(await getAdminServices())
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
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
          服务健康
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

      <div className="grid gap-4 md:grid-cols-3">
        {services.map((svc) => (
          <div
            key={svc.name}
            className="rounded-lg p-4"
            style={{ border: '1px solid var(--border)', background: 'var(--bg-card)' }}
          >
            <div className="mb-3 flex items-center gap-2">
              <span
                className={`h-2.5 w-2.5 rounded-full ${svc.status === 'Running' ? 'animate-pulse' : ''}`}
                style={{
                  background:
                    svc.status === 'Running' ? 'var(--color-success)' : 'var(--color-error)',
                }}
              />
              <span className="text-[14px] font-medium" style={{ color: 'var(--text-primary)' }}>
                {svc.name}
              </span>
            </div>
            <div className="flex flex-col gap-1.5 text-[12px]">
              {[
                {
                  label: '状态',
                  value: svc.status,
                  color: svc.status === 'Running' ? 'var(--color-success)' : 'var(--color-error)',
                },
                { label: '运行时长', value: svc.uptime, color: 'var(--text-secondary)' },
                { label: '版本', value: svc.version, color: 'var(--text-secondary)' },
                { label: '端口', value: String(svc.port), color: 'var(--text-secondary)' },
                { label: '上次检查', value: svc.lastCheck, color: 'var(--text-secondary)' },
              ].map((row) => (
                <div key={row.label} className="flex justify-between">
                  <span style={{ color: 'var(--text-tertiary)' }}>{row.label}</span>
                  <span style={{ color: row.color }}>{row.value}</span>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
