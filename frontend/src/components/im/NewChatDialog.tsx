import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { type AgentEntry, AgentSelectList } from '@/components/im/AgentSelectList'
import { RepoPathInput } from '@/components/im/RepoPathInput'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { AgentType } from '@/generated/request'
import { useCreateConversation } from '@/hooks/use-conversations'
import { fetchAgentTypes } from '@/lib/api'
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

  const [agents, setAgents] = useState<AgentEntry[]>([])
  const [repoPath, setRepoPath] = useState('')
  const [repoPathValidated, setRepoPathValidated] = useState(false)
  const [groupTitle, setGroupTitle] = useState('')
  const [groupTitleError, setGroupTitleError] = useState(false)

  const [prevOpen, setPrevOpen] = useState(open)
  if (prevOpen !== open) {
    setPrevOpen(open)
    if (open) {
      setAgents([])
      setRepoPath('')
      setRepoPathValidated(false)
      setGroupTitle('')
      setGroupTitleError(false)
    }
  }

  const types = agentTypes?.length
    ? agentTypes
    : (['claude-code', 'opencode', 'orchestrator', 'codex'] as AgentType[]).map((t) => ({
        type: t,
        name: t,
        description: AGENT_DESCRIPTIONS[t] ?? '',
      }))

  const canSubmit = agents.length > 0 && repoPathValidated && !createMutation.isPending

  const handleSubmit = () => {
    if (agents.length >= 2 && !groupTitle.trim()) {
      setGroupTitleError(true)
      return
    }
    createMutation.mutate(
      {
        agents,
        repoPath,
        title: agents.length >= 2 ? groupTitle.trim() : undefined,
      },
      {
        onSuccess: (conversation) => {
          setCurrentSession(conversation.sessionId)
          onOpenChange(false)
        },
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm border-border bg-card">
        <DialogHeader>
          <DialogTitle className="text-foreground">新建对话</DialogTitle>
        </DialogHeader>

        <RepoPathInput
          onValidationChange={(path, validated) => {
            setRepoPath(path)
            setRepoPathValidated(validated)
          }}
        />

        <AgentSelectList
          types={types}
          repoPathValidated={repoPathValidated}
          disabled={createMutation.isPending}
          onChange={setAgents}
        />

        {agents.length >= 2 && (
          <div className="mb-3">
            <label className="mb-1 block text-xs font-medium text-muted-foreground">
              群聊名称 <span className="text-destructive">*</span>
            </label>
            <input
              value={groupTitle}
              placeholder="为群聊起个名字"
              className="w-full rounded-md border bg-background px-2 py-1.5 text-xs text-foreground outline-none"
              style={{
                borderColor: groupTitleError ? 'var(--destructive)' : 'var(--border)',
                animation: groupTitleError ? 'shake 0.4s ease' : undefined,
              }}
              onChange={(e) => {
                setGroupTitle(e.target.value)
                setGroupTitleError(false)
              }}
              onAnimationEnd={() => setGroupTitleError(false)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleSubmit()
              }}
            />
            {groupTitleError && <p className="mt-1 text-xs text-destructive">群聊必须填写名称</p>}
          </div>
        )}

        <button
          className="w-full rounded-md py-2 text-sm font-medium transition-colors"
          style={{
            backgroundColor: canSubmit ? 'var(--primary)' : 'var(--muted)',
            color: canSubmit ? 'var(--primary-foreground)' : 'var(--muted-foreground)',
            opacity: createMutation.isPending ? 0.6 : 1,
          }}
          onClick={handleSubmit}
          disabled={!canSubmit}
        >
          {createMutation.isPending
            ? '创建中...'
            : agents.length > 1
              ? `创建群聊（${agents.length} 个 Agent）`
              : agents.length === 1
                ? '开始对话'
                : '请添加 Agent'}
        </button>
      </DialogContent>
    </Dialog>
  )
}
