import { useQuery } from '@tanstack/react-query'
import { Bot, ChevronDown, ChevronRight, Lock, RefreshCw } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { AdminQueryError } from '@/components/admin/AdminQueryError'
import { useDialogFocusTrap } from '@/hooks/use-dialog-focus-trap'
import { adminAuth, getAdminAgents } from '@/lib/api'
import {
  UI_ACTIONS,
  UI_LABELS,
  UI_MESSAGES,
  UI_PLACEHOLDERS,
  UI_PROFILE,
  UI_STATUS,
} from '@/lib/ui-text'
import { cn } from '@/lib/utils'
import { useAdminStore } from '@/stores/admin'

export function AgentOverviewPage() {
  const {
    data: agents,
    isError,
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
  const setAdminToken = useAdminStore((state) => state.setAdminToken)
  const reauthDialogRef = useRef<HTMLDivElement>(null)

  useDialogFocusTrap(reauthDialogRef, Boolean(reauthTarget))

  useEffect(() => {
    if (!reauthTarget || reauthLoading) return

    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setReauthTarget(null)
    }
    document.addEventListener('keydown', handleEscape)
    return () => document.removeEventListener('keydown', handleEscape)
  }, [reauthLoading, reauthTarget])

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
      const auth = await adminAuth(reauthPassword)
      setAdminToken(auth.token)
      setExpanded((prev) => new Set(prev).add(reauthTarget))
      setReauthTarget(null)
    } catch {
      setReauthError(UI_MESSAGES.PASSWORD_ERROR)
    } finally {
      setReauthLoading(false)
    }
  }

  return (
    <div className="p-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-lg font-semibold text-foreground">Agent 概览</h2>
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
                type="button"
                onClick={() => handleToggle(agent.type)}
                className="flex items-center gap-1 rounded-md border border-border px-2 py-1 text-[12px] text-text-secondary transition-[background,color,transform,opacity] hover:bg-hover hover:text-foreground active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              >
                {expanded.has(agent.type) ? (
                  <ChevronDown className="h-3 w-3" strokeWidth={1.25} />
                ) : (
                  <ChevronRight className="h-3 w-3" strokeWidth={1.25} />
                )}
                {expanded.has(agent.type) ? UI_PROFILE.COLLAPSE_CONFIG : UI_PROFILE.VIEW_CONFIG}
              </button>
            </div>
            {expanded.has(agent.type) && (
              <div className="border-t border-border bg-hover p-4">
                <pre className="max-h-[300px] overflow-auto whitespace-pre-wrap rounded-md bg-bg-canvas p-3 font-mono text-[12px] text-foreground">
                  {agent.configContent || UI_PROFILE.NO_CONFIG}
                </pre>
              </div>
            )}
          </div>
        ))}
        {isLoading &&
          Array.from({ length: 4 }).map((_, index) => (
            <div
              key={index}
              className="h-36 rounded-lg border border-border skeleton-sheen"
              aria-hidden="true"
            />
          ))}
        {!isLoading && !agents?.length && (
          <div className="col-span-full py-8 text-center text-sm text-tertiary">
            {UI_MESSAGES.NO_DATA}
          </div>
        )}
      </div>

      {/* Inline re-auth dialog */}
      {reauthTarget && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="agent-config-auth-title"
          onClick={() => {
            if (!reauthLoading) setReauthTarget(null)
          }}
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
        >
          <div
            ref={reauthDialogRef}
            tabIndex={-1}
            className="mx-4 w-[calc(100%-2rem)] max-w-[340px] rounded-lg border border-border bg-card p-5"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="mb-3 flex items-center gap-2">
              <Lock className="h-4 w-4 text-brand" strokeWidth={1.25} />
              <span
                id="agent-config-auth-title"
                className="text-[14px] font-medium text-foreground"
              >
                {UI_LABELS.SENSITIVE_CONFIRM}
              </span>
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
                className="h-9 rounded-md border border-border bg-bg-canvas px-3 text-sm text-foreground outline-none transition-[border-color,box-shadow] focus:border-primary-border focus:ring-2 focus:ring-primary/15"
                autoFocus
              />
              {reauthError && (
                <p className="text-xs text-error" role="alert">
                  {reauthError}
                </p>
              )}
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => {
                    if (!reauthLoading) setReauthTarget(null)
                  }}
                  disabled={reauthLoading}
                  className="h-9 flex-1 rounded-md border border-border text-[13px] text-text-secondary transition-[background,color,transform] hover:bg-hover hover:text-foreground active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                >
                  {UI_ACTIONS.CANCEL}
                </button>
                <button
                  type="submit"
                  disabled={reauthLoading || !reauthPassword}
                  className="h-9 flex-1 rounded-md bg-brand text-[13px] font-medium text-primary-foreground transition-[background,transform,opacity] hover:bg-primary/90 active:scale-[0.98] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                >
                  {reauthLoading ? UI_STATUS.VERIFYING : UI_ACTIONS.CONFIRM}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
