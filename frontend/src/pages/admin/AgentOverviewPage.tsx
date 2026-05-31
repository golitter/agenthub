import { Bot, ChevronDown, ChevronRight, Lock, RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'

import { adminAuth, type AgentInfo, getAdminAgents } from '@/lib/api'
import { cn } from '@/lib/utils'

export function AgentOverviewPage() {
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [reauthTarget, setReauthTarget] = useState<string | null>(null)
  const [reauthPassword, setReauthPassword] = useState('')
  const [reauthError, setReauthError] = useState('')
  const [reauthLoading, setReauthLoading] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      setAgents(await getAdminAgents())
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

  const handleToggle = (agentType: string) => {
    if (expanded.has(agentType)) {
      setExpanded((prev) => {
        const n = new Set(prev)
        n.delete(agentType)
        return n
      })
      return
    }
    // Need re-auth before expanding
    setReauthTarget(agentType)
    setReauthPassword('')
    setReauthError('')
  }

  const handleReauthSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!reauthPassword || !reauthTarget) return
    setReauthLoading(true)
    setReauthError('')
    try {
      await adminAuth(reauthPassword)
      setExpanded((prev) => new Set(prev).add(reauthTarget))
      setReauthTarget(null)
    } catch {
      setReauthError('密码错误')
    } finally {
      setReauthLoading(false)
    }
  }

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
          Agent 概览
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
          <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} strokeWidth={1.25} />
          刷新
        </button>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        {agents.map((agent) => (
          <div
            key={agent.type}
            className="rounded-lg"
            style={{ border: '1px solid var(--border)', background: 'var(--bg-card)' }}
          >
            <div className="flex items-start gap-3 p-4">
              <div
                className="flex h-10 w-10 items-center justify-center rounded-lg"
                style={{ background: 'var(--primary-soft)' }}
              >
                <Bot
                  className="h-5 w-5"
                  strokeWidth={1.25}
                  style={{ color: 'var(--color-brand)' }}
                />
              </div>
              <div className="flex-1">
                <h3 className="text-[14px] font-medium" style={{ color: 'var(--text-primary)' }}>
                  {agent.name}
                </h3>
                <p className="text-[12px]" style={{ color: 'var(--text-tertiary)' }}>
                  {agent.description}
                </p>
                <p className="mt-1 text-[11px]" style={{ color: 'var(--text-tertiary)' }}>
                  {agent.configDir}
                </p>
              </div>
              <button
                onClick={() => handleToggle(agent.type)}
                className="flex items-center gap-1 rounded-md px-2 py-1 text-[12px] transition-colors"
                style={{ border: '1px solid var(--border)', color: 'var(--text-secondary)' }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.background = 'var(--bg-hover)'
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.background = 'transparent'
                }}
              >
                {expanded.has(agent.type) ? (
                  <ChevronDown className="h-3 w-3" strokeWidth={1.25} />
                ) : (
                  <ChevronRight className="h-3 w-3" strokeWidth={1.25} />
                )}
                {expanded.has(agent.type) ? '收起配置' : '查看配置'}
              </button>
            </div>
            {expanded.has(agent.type) && (
              <div
                className="p-4"
                style={{ borderTop: '1px solid var(--border)', background: 'var(--bg-hover)' }}
              >
                <pre
                  className="max-h-[300px] overflow-auto whitespace-pre-wrap rounded-md p-3 font-mono text-[12px]"
                  style={{ background: 'var(--bg-canvas)', color: 'var(--text-primary)' }}
                >
                  {agent.configContent || '无配置内容'}
                </pre>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Inline re-auth dialog */}
      {reauthTarget && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center"
          style={{ background: 'rgba(0,0,0,0.5)' }}
        >
          <div
            className="w-[340px] rounded-lg p-5"
            style={{ background: 'var(--bg-card)', border: '1px solid var(--border)' }}
          >
            <div className="mb-3 flex items-center gap-2">
              <Lock
                className="h-4 w-4"
                strokeWidth={1.25}
                style={{ color: 'var(--color-brand)' }}
              />
              <span className="text-[14px] font-medium" style={{ color: 'var(--text-primary)' }}>
                敏感操作确认
              </span>
            </div>
            <p className="mb-3 text-[13px]" style={{ color: 'var(--text-secondary)' }}>
              查看配置文件需要再次验证密码
            </p>
            <form onSubmit={handleReauthSubmit} className="flex flex-col gap-3">
              <input
                type="password"
                value={reauthPassword}
                onChange={(e) => {
                  setReauthPassword(e.target.value)
                  setReauthError('')
                }}
                placeholder="请输入密码"
                className="h-9 rounded-md px-3 text-sm outline-none"
                style={{
                  border: '1px solid var(--border)',
                  background: 'var(--bg-canvas)',
                  color: 'var(--text-primary)',
                }}
                autoFocus
              />
              {reauthError && (
                <p className="text-xs" style={{ color: 'var(--color-error)' }}>
                  {reauthError}
                </p>
              )}
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => setReauthTarget(null)}
                  className="h-9 flex-1 rounded-md text-[13px]"
                  style={{ border: '1px solid var(--border)', color: 'var(--text-secondary)' }}
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={reauthLoading || !reauthPassword}
                  className="h-9 flex-1 rounded-md text-[13px] font-medium disabled:opacity-50"
                  style={{ background: 'var(--color-brand)', color: 'var(--primary-foreground)' }}
                >
                  {reauthLoading ? '验证中...' : '确认'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
