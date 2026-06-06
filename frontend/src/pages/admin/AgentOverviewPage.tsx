import { useQuery } from '@tanstack/react-query'
import { Bot, ChevronDown, ChevronRight, Lock, RefreshCw } from 'lucide-react'
import { useState } from 'react'

import { adminAuth, getAdminAgents } from '@/lib/api'
import { UI_PLACEHOLDERS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

export function AgentOverviewPage() {
  const {
    data: agents,
    isLoading,
    refetch,
    isRefetching,
  } = useQuery({
    queryKey: ['admin-agents'],
    queryFn: getAdminAgents,
    staleTime: 30_000,
  })
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [reauthTarget, setReauthTarget] = useState<string | null>(null)
  const [reauthPassword, setReauthPassword] = useState('')
  const [reauthError, setReauthError] = useState('')
  const [reauthLoading, setReauthLoading] = useState(false)

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
        <h2 className="text-lg font-semibold text-foreground">Agent 概览</h2>
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

      <div className="grid gap-4 md:grid-cols-2">
        {(agents ?? []).map((agent) => (
          <div key={agent.type} className="rounded-lg border border-border bg-card">
            <div className="flex items-start gap-3 p-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary-soft">
                <Bot className="h-5 w-5 text-brand" strokeWidth={1.25} />
              </div>
              <div className="flex-1">
                <h3 className="text-[14px] font-medium text-foreground">{agent.name}</h3>
                <p className="text-[12px] text-tertiary">{agent.description}</p>
                <p className="mt-1 text-[11px] text-tertiary">{agent.configPath}</p>
              </div>
              <button
                onClick={() => handleToggle(agent.type)}
                className="flex items-center gap-1 rounded-md border border-border px-2 py-1 text-[12px] text-text-secondary transition-[transform,opacity]"
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
              <div className="border-t border-border bg-hover p-4">
                <pre className="max-h-[300px] overflow-auto whitespace-pre-wrap rounded-md bg-bg-canvas p-3 font-mono text-[12px] text-foreground">
                  {agent.configContent || '无配置内容'}
                </pre>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Inline re-auth dialog */}
      {reauthTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="w-[340px] rounded-lg border border-border bg-card p-5">
            <div className="mb-3 flex items-center gap-2">
              <Lock className="h-4 w-4 text-brand" strokeWidth={1.25} />
              <span className="text-[14px] font-medium text-foreground">敏感操作确认</span>
            </div>
            <p className="mb-3 text-[13px] text-text-secondary">查看配置文件需要再次验证密码</p>
            <form onSubmit={handleReauthSubmit} className="flex flex-col gap-3">
              <input
                type="password"
                value={reauthPassword}
                onChange={(e) => {
                  setReauthPassword(e.target.value)
                  setReauthError('')
                }}
                placeholder={UI_PLACEHOLDERS.PASSWORD}
                className="h-9 rounded-md border border-border bg-bg-canvas px-3 text-sm text-foreground outline-none"
                autoFocus
              />
              {reauthError && <p className="text-xs text-error">{reauthError}</p>}
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => setReauthTarget(null)}
                  className="h-9 flex-1 rounded-md border border-border text-[13px] text-text-secondary"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={reauthLoading || !reauthPassword}
                  className="h-9 flex-1 rounded-md bg-brand text-[13px] font-medium text-primary-foreground disabled:opacity-50"
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
