import { useQuery } from '@tanstack/react-query'
import { Bot, ChevronDown, ChevronRight, Lock, RefreshCw } from 'lucide-react'
import { useRef, useState } from 'react'

import { AdminQueryError } from '@/components/admin/AdminQueryError'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { adminAuth, getAdminAgents } from '@/lib/api'
import {
  UI_ACTIONS,
  UI_LABELS,
  UI_MESSAGES,
  UI_PLACEHOLDERS,
  UI_PROFILE,
  UI_STATUS,
} from '@/lib/ui-text'
import { cn, isFocusableTarget } from '@/lib/utils'
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
  const reauthTriggerRef = useRef<HTMLButtonElement>(null)

  const handleToggle = (agentType: string, trigger: HTMLButtonElement) => {
    if (expanded.has(agentType)) {
      setExpanded((prev) => {
        const n = new Set(prev)
        n.delete(agentType)
        return n
      })
      return
    }
    // 展开前需要重新认证
    reauthTriggerRef.current = trigger
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
      setAdminToken(auth.token, auth.expires_in)
      setExpanded((prev) => new Set(prev).add(reauthTarget))
      setReauthTarget(null)
    } catch {
      setReauthError(UI_MESSAGES.PASSWORD_ERROR)
    } finally {
      setReauthLoading(false)
    }
  }

  return (
    <div className="p-4 sm:p-6">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold text-foreground">Agent 概览</h2>
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
          刷新
        </Button>
      </div>
      {isError && <AdminQueryError onRetry={() => refetch()} />}

      <div className="grid gap-4 md:grid-cols-2">
        {(agents ?? []).map((agent) => (
          <div key={agent.type} className="rounded-lg border border-border bg-card">
            <div className="flex items-start gap-3 p-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary-soft">
                <Bot className="h-5 w-5 text-brand" strokeWidth={1.25} />
              </div>
              <div className="min-w-0 flex-1">
                <h3 className="break-words text-[14px] font-medium text-foreground">
                  {agent.name}
                </h3>
                <p className="break-words text-[12px] text-tertiary">{agent.description}</p>
                <p className="mt-1 break-all text-[11px] text-tertiary">{agent.configPath}</p>
              </div>
              <button
                type="button"
                onClick={(event) => handleToggle(agent.type, event.currentTarget)}
                aria-expanded={expanded.has(agent.type)}
                aria-controls={`agent-config-${agent.type}`}
                className="flex items-center gap-1 rounded-md border border-border px-2 py-1 text-[12px] text-text-secondary transition-[background,color,transform,opacity] hover:bg-bg-hover hover:text-foreground active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
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
              <div
                id={`agent-config-${agent.type}`}
                className="border-t border-border bg-hover p-4"
              >
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

      <Dialog
        open={Boolean(reauthTarget)}
        onOpenChange={(open) => {
          if (!open && !reauthLoading) setReauthTarget(null)
        }}
      >
        <DialogContent
          showCloseButton={!reauthLoading}
          className="max-w-[340px] border-border bg-card shadow-[var(--shadow-popup)]"
          onCloseAutoFocus={(event) => {
            const trigger = reauthTriggerRef.current
            if (!isFocusableTarget(trigger)) return
            event.preventDefault()
            trigger.focus()
          }}
        >
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-[14px]">
              <Lock className="h-4 w-4 text-brand" strokeWidth={1.25} aria-hidden="true" />
              {UI_LABELS.SENSITIVE_CONFIRM}
            </DialogTitle>
            <DialogDescription className="sr-only">查看配置文件需要再次验证密码</DialogDescription>
          </DialogHeader>
          <p className="text-[13px] text-text-secondary">查看配置文件需要再次验证密码</p>
          <form onSubmit={handleReauthSubmit} className="flex flex-col gap-3">
            <label htmlFor="agent-config-password" className="sr-only">
              {UI_LABELS.ENTER_PASSWORD}
            </label>
            <input
              id="agent-config-password"
              type="password"
              value={reauthPassword}
              onChange={(e) => {
                setReauthPassword(e.target.value)
                setReauthError('')
              }}
              placeholder={UI_PLACEHOLDERS.PASSWORD}
              className="h-9 rounded-md border border-border bg-bg-canvas px-3 text-sm text-foreground outline-none transition-[border-color,box-shadow] focus:border-primary-border focus:ring-2 focus:ring-primary/15"
              aria-invalid={Boolean(reauthError) || undefined}
              aria-describedby={reauthError ? 'agent-config-password-error' : undefined}
              autoComplete="current-password"
              autoFocus
            />
            {reauthError && (
              <p id="agent-config-password-error" className="text-xs text-error" role="alert">
                {reauthError}
              </p>
            )}
            <div className="flex gap-2">
              <Button
                type="button"
                variant="secondary"
                size="md"
                className="flex-1"
                onClick={() => setReauthTarget(null)}
                disabled={reauthLoading}
              >
                {UI_ACTIONS.CANCEL}
              </Button>
              <Button
                type="submit"
                size="md"
                className="flex-1"
                loading={reauthLoading}
                disabled={!reauthPassword}
              >
                {reauthLoading ? UI_STATUS.VERIFYING : UI_ACTIONS.CONFIRM}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
