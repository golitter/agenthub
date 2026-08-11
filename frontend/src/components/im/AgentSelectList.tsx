import { X } from 'lucide-react'
import { useId, useState } from 'react'

import { AgentAvatar } from '@/components/chat/AgentAvatar'
import type { AgentType } from '@/generated/request'
import { AGENT_TYPES } from '@/lib/constants'
import { UI_ACTIONS, UI_ERRORS, UI_PLACEHOLDERS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

export interface AgentEntry {
  type: AgentType
  name: string
}

interface AgentSelectListProps {
  types: Array<{ type: string; name: string; description?: string }>
  repoPathValidated: boolean
  disabled: boolean
  onChange: (agents: AgentEntry[]) => void
}

export function AgentSelectList({
  types,
  repoPathValidated,
  disabled,
  onChange,
}: AgentSelectListProps) {
  const [agents, setAgents] = useState<AgentEntry[]>([])
  const [addingType, setAddingType] = useState<AgentType | null>(null)
  const [inputName, setInputName] = useState('')
  const [nameError, setNameError] = useState(false)
  const [ruleError, setRuleError] = useState('')
  const inputId = useId()

  const handleAddAgent = () => {
    const trimmed = inputName.trim()
    if (
      !addingType ||
      !trimmed ||
      agents.some((a) => a.name.trim().toLowerCase() === trimmed.toLowerCase())
    ) {
      setNameError(true)
      setRuleError('')
      return
    }
    if (
      addingType === AGENT_TYPES.Orchestrator &&
      agents.some((a) => a.type === AGENT_TYPES.Orchestrator)
    ) {
      setRuleError(UI_ERRORS.ONE_ORCHESTRATOR)
      setNameError(false)
      return
    }
    const next = [...agents, { type: addingType!, name: trimmed }]
    setAgents(next)
    onChange(next)
    setInputName('')
    setAddingType(null)
    setNameError(false)
    setRuleError('')
  }

  const handleRemoveAgent = (index: number) => {
    const next = agents.filter((_, i) => i !== index)
    setAgents(next)
    onChange(next)
  }

  return (
    <>
      {/* 已添加的 agent 列表 */}
      {agents.length > 0 && (
        <div className="mb-3 flex flex-col gap-1.5">
          <p className="text-xs font-medium text-muted-foreground">已选 Agent（{agents.length}）</p>
          {agents.map((agent, i) => (
            <div
              key={i}
              className="flex items-center gap-2 rounded-lg border border-border bg-background px-2.5 py-2"
            >
              <AgentAvatar agentType={agent.type} status="ready" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-xs font-medium text-foreground">{agent.name}</p>
                <p className="text-[11px] text-muted-foreground">{agent.type}</p>
              </div>
              <button
                type="button"
                className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[background,color,transform] hover:bg-danger-bg hover:text-destructive active:scale-[0.94] disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => handleRemoveAgent(i)}
                disabled={disabled}
                aria-label={UI_ACTIONS.DELETE}
                title={UI_ACTIONS.DELETE}
              >
                <X className="h-3.5 w-3.5" strokeWidth={1.25} />
              </button>
            </div>
          ))}
        </div>
      )}

      {/* 添加 agent 区域 */}
      {repoPathValidated && (
        <div className="mb-3">
          {addingType ? (
            <div className="flex flex-wrap items-center gap-2 rounded-lg border border-primary-border bg-primary-soft px-3 py-2">
              <AgentAvatar agentType={addingType} status="ready" />
              <input
                id={inputId}
                value={inputName}
                placeholder={UI_PLACEHOLDERS.AGENT_NAME_INPUT}
                className={cn(
                  'min-w-0 flex-[1_1_9rem] rounded-md border bg-background px-2 py-1.5 text-xs text-foreground outline-none transition-[border-color,box-shadow] focus:ring-2 focus:ring-primary/15',
                  nameError ? 'border-destructive animate-[shake_0.4s_ease]' : 'border-border',
                )}
                aria-invalid={nameError || undefined}
                aria-label={UI_PLACEHOLDERS.AGENT_NAME_INPUT}
                onChange={(e) => {
                  setInputName(e.target.value)
                  setNameError(false)
                }}
                onAnimationEnd={() => setNameError(false)}
                onKeyDown={(e) => {
                  if (e.nativeEvent.isComposing) return
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    handleAddAgent()
                  }
                }}
              />
              <button
                type="button"
                className="rounded-md bg-primary px-2.5 py-1.5 text-xs font-medium text-primary-foreground transition-[background,transform,opacity] hover:bg-primary/90 active:scale-[0.97] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={handleAddAgent}
                disabled={!inputName.trim()}
              >
                {UI_ACTIONS.ADD}
              </button>
              <button
                type="button"
                className="rounded-md px-2 py-1.5 text-xs text-muted-foreground transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => {
                  setAddingType(null)
                  setInputName('')
                  setNameError(false)
                }}
              >
                {UI_ACTIONS.CANCEL}
              </button>
            </div>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {types.map((agent) => (
                <button
                  key={agent.type}
                  type="button"
                  className="flex items-center gap-1.5 rounded-lg border border-border px-2.5 py-1.5 text-xs text-foreground transition-[background,border-color,transform,opacity] hover:border-primary-border hover:bg-accent active:scale-[0.98] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                  onClick={() => {
                    setAddingType(agent.type as AgentType)
                    setInputName('')
                  }}
                  disabled={disabled}
                >
                  <AgentAvatar agentType={agent.type as AgentType} status="ready" />
                  <span>{agent.name}</span>
                </button>
              ))}
            </div>
          )}
          {nameError && (
            <p className="mt-1 text-xs text-destructive" role="alert">
              {UI_ERRORS.DUPLICATE_NAME}
            </p>
          )}
          {ruleError && (
            <p className="mt-1 text-xs text-destructive" role="alert">
              {ruleError}
            </p>
          )}
        </div>
      )}
    </>
  )
}
