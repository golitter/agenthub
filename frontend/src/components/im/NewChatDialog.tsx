import { useQuery } from '@tanstack/react-query'
import { useRef, useState } from 'react'

import { AgentAvatar } from '@/components/chat/AgentAvatar'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { AgentType } from '@/generated/request'
import { useCreateConversation } from '@/hooks/use-conversations'
import { useHoverStyle } from '@/hooks/use-hover-style'
import { fetchAgentTypes, validateRepoPath } from '@/lib/api'
import { AGENT_DESCRIPTIONS } from '@/lib/constants'
import { useChatNav } from '@/stores/chat'

interface NewChatDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function NewChatDialog({ open, onOpenChange }: NewChatDialogProps) {
  const { data: agentTypes } = useQuery({
    queryKey: ['agent-types'],
    queryFn: fetchAgentTypes,
  })
  const createMutation = useCreateConversation()
  const { setCurrentSession } = useChatNav()
  const [expandedAgent, setExpandedAgent] = useState<string | null>(null)
  const nameRef = useRef<HTMLInputElement>(null)
  const repoPathRef = useRef<HTMLInputElement>(null)
  const agentHover = useHoverStyle()

  const [repoPathValidated, setRepoPathValidated] = useState(false)
  const [repoPathError, setRepoPathError] = useState<string | null>(null)
  const [validating, setValidating] = useState(false)

  const handleValidate = async () => {
    const path = repoPathRef.current?.value?.trim()
    if (!path) {
      setRepoPathError('请输入仓库路径')
      setRepoPathValidated(false)
      return
    }
    setValidating(true)
    setRepoPathError(null)
    try {
      const result = await validateRepoPath(path)
      if (result.valid) {
        setRepoPathValidated(true)
        setRepoPathError(null)
      } else {
        setRepoPathValidated(false)
        setRepoPathError(result.errors.join('; '))
      }
    } catch {
      setRepoPathValidated(false)
      setRepoPathError('校验失败，请检查 Agent 服务是否可用')
    } finally {
      setValidating(false)
    }
  }

  const handleRepoPathChange = () => {
    setRepoPathValidated(false)
    setRepoPathError(null)
  }

  const handleSelect = (agentType: AgentType, agentName?: string) => {
    if (!repoPathValidated) return
    createMutation.mutate(
      { agentType, agentName, repoPath: repoPathRef.current?.value?.trim() },
      {
        onSuccess: (conversation) => {
          setCurrentSession(conversation.sessionId)
          onOpenChange(false)
        },
      },
    )
  }

  const types = agentTypes?.length
    ? agentTypes
    : (['claude-code', 'opencode', 'orchestrator', 'codex'] as AgentType[]).map((t) => ({
        type: t,
        name: t,
        description: AGENT_DESCRIPTIONS[t] ?? '',
      }))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm bg-card border-border">
        <DialogHeader>
          <DialogTitle className="text-foreground">新建对话</DialogTitle>
        </DialogHeader>
        <p className="mb-3 text-xs text-tertiary">选择一个 Agent 开始对话</p>
        <div className="mb-3">
          <label className="mb-1 block text-xs font-medium text-muted-foreground">仓库路径</label>
          <div className="flex items-center gap-2">
            <input
              ref={repoPathRef}
              placeholder="/path/to/repo"
              className="flex-1 rounded-md border bg-background px-2 py-1.5 text-xs text-foreground outline-none"
              style={{
                borderColor: repoPathError
                  ? 'var(--destructive)'
                  : repoPathValidated
                    ? 'var(--color-success)'
                    : 'var(--border)',
              }}
              onChange={handleRepoPathChange}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleValidate()
              }}
            />
            <button
              className="shrink-0 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground"
              style={{ opacity: validating ? 0.6 : 1 }}
              onClick={handleValidate}
              disabled={validating}
            >
              {validating ? '校验中...' : '校验'}
            </button>
          </div>
          {repoPathError && <p className="mt-1 text-xs text-destructive">{repoPathError}</p>}
          {repoPathValidated && (
            <p className="mt-1 text-xs" style={{ color: 'var(--color-success)' }}>
              路径校验通过
            </p>
          )}
        </div>
        <div className="flex flex-col gap-2">
          {types.map((agent) => (
            <div key={agent.type}>
              <button
                className="flex w-full items-center gap-3 rounded-lg border border-border px-3 py-3 text-left transition-colors"
                style={{ opacity: !repoPathValidated ? 0.4 : 1 }}
                onClick={() => {
                  if (!repoPathValidated) return
                  if (expandedAgent === agent.type) {
                    handleSelect(agent.type as AgentType, nameRef.current?.value || undefined)
                  } else {
                    setExpandedAgent(agent.type)
                  }
                }}
                onMouseEnter={agentHover.onMouseEnter}
                onMouseLeave={agentHover.onMouseLeave}
                disabled={createMutation.isPending || !repoPathValidated}
              >
                <AgentAvatar agentType={agent.type as AgentType} status="ready" />
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium text-foreground">{agent.name}</p>
                  <p className="mt-0.5 truncate text-xs text-tertiary">
                    {agent.description || AGENT_DESCRIPTIONS[agent.type]}
                  </p>
                </div>
              </button>
              {expandedAgent === agent.type && repoPathValidated && (
                <div className="flex items-center gap-2 px-3 pt-1 pb-2">
                  <input
                    ref={nameRef}
                    placeholder="自定义名称（可选）"
                    className="flex-1 rounded-md border border-border bg-background px-2 py-1.5 text-xs text-foreground outline-none"
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        handleSelect(agent.type as AgentType, nameRef.current?.value || undefined)
                      }
                    }}
                  />
                  <button
                    className="rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground"
                    onClick={() =>
                      handleSelect(agent.type as AgentType, nameRef.current?.value || undefined)
                    }
                    disabled={createMutation.isPending}
                  >
                    开始
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}
