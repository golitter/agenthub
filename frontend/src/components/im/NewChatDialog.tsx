import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { type AgentEntry, AgentSelectList } from '@/components/im/AgentSelectList'
import { RepoPathInput } from '@/components/im/RepoPathInput'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { AgentType } from '@/generated/request'
import { useCreateConversation } from '@/hooks/use-conversations'
import { fetchAgentTypes } from '@/lib/api'
import { AGENT_DESCRIPTIONS, AGENT_TYPES } from '@/lib/constants'
import { UI_ACTIONS, UI_ERRORS, UI_LABELS, UI_PLACEHOLDERS, UI_STATUS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'
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
    : (
        [
          AGENT_TYPES.ClaudeCode,
          AGENT_TYPES.Opencode,
          AGENT_TYPES.Orchestrator,
          AGENT_TYPES.Codex,
        ] as AgentType[]
      ).map((t) => ({
        type: t,
        name: t,
        description: AGENT_DESCRIPTIONS[t] ?? '',
      }))

  const hasOrchestrator = agents.some((a) => a.type === AGENT_TYPES.Orchestrator)
  const hasNonOrchestrator = agents.some((a) => a.type !== AGENT_TYPES.Orchestrator)
  const orchestratorAlone = hasOrchestrator && !hasNonOrchestrator
  const needsGroupTitle = agents.length >= 2
  const groupTitleReady = !needsGroupTitle || groupTitle.trim().length > 0

  const canSubmit =
    agents.length > 0 &&
    repoPathValidated &&
    !createMutation.isPending &&
    !orchestratorAlone &&
    groupTitleReady
  const submitLabel = createMutation.isPending
    ? UI_STATUS.CREATING
    : agents.length > 1
      ? `${UI_ACTIONS.CREATE_GROUP_CHAT}（${agents.length} 个 Agent）`
      : agents.length === 1
        ? UI_ACTIONS.START_CHAT
        : UI_ERRORS.ADD_AGENT

  const handleSubmit = () => {
    if (agents.length >= 2 && !groupTitle.trim()) {
      setGroupTitleError(true)
      return
    }
    if (orchestratorAlone) {
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
          <DialogTitle className="text-foreground">{UI_LABELS.NEW_CHAT}</DialogTitle>
          <DialogDescription className="sr-only">
            {UI_LABELS.NEW_CHAT} - 选择 Agent
          </DialogDescription>
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
              placeholder={UI_PLACEHOLDERS.GROUP_NAME}
              className={cn(
                'w-full rounded-md border bg-background px-2 py-1.5 text-xs text-foreground outline-none transition-[border-color,box-shadow] focus:ring-2 focus:ring-primary/15',
                groupTitleError ? 'border-destructive' : 'border-border',
                groupTitleError && 'animate-[shake_0.4s_ease]',
              )}
              aria-invalid={groupTitleError || undefined}
              onChange={(e) => {
                setGroupTitle(e.target.value)
                setGroupTitleError(false)
              }}
              onAnimationEnd={() => setGroupTitleError(false)}
              onKeyDown={(e) => {
                if (e.nativeEvent.isComposing) return
                if (e.key === 'Enter') handleSubmit()
              }}
            />
            {groupTitleError && (
              <p className="mt-1 text-xs text-destructive">{UI_ERRORS.GROUP_NAME_REQUIRED}</p>
            )}
          </div>
        )}

        {orchestratorAlone && (
          <p className="mb-3 rounded-md border border-destructive/30 bg-destructive/5 px-2.5 py-1.5 text-xs text-destructive">
            Orchestrator 不能单独成群，请添加至少一个非 Orchestrator 的 Agent
          </p>
        )}

        <button
          type="button"
          className={cn(
            'w-full rounded-md py-2 text-sm font-medium transition-[background,transform,opacity] active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
            canSubmit
              ? 'bg-primary text-primary-foreground hover:bg-primary/90'
              : 'cursor-not-allowed bg-muted text-muted-foreground',
            createMutation.isPending && 'opacity-60',
          )}
          onClick={handleSubmit}
          disabled={!canSubmit}
        >
          {submitLabel}
        </button>
      </DialogContent>
    </Dialog>
  )
}
