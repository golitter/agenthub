import { useState } from 'react'

import { AgentAvatar } from '@/components/chat/AgentAvatar'
import type { AgentType } from '@/generated/request'

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

  const handleAddAgent = () => {
    const trimmed = inputName.trim()
    if (!trimmed || agents.some((a) => a.name === trimmed)) {
      setNameError(true)
      return
    }
    const next = [...agents, { type: addingType!, name: trimmed }]
    setAgents(next)
    onChange(next)
    setInputName('')
    setAddingType(null)
    setNameError(false)
  }

  const handleRemoveAgent = (index: number) => {
    const next = agents.filter((_, i) => i !== index)
    setAgents(next)
    onChange(next)
  }

  return (
    <>
      {/* Added agents list */}
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
                className="text-xs text-muted-foreground hover:text-destructive"
                onClick={() => handleRemoveAgent(i)}
              >
                ✕
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Add agent section */}
      {repoPathValidated && (
        <div className="mb-3">
          {addingType ? (
            <div className="flex items-center gap-2 rounded-lg border border-primary-border bg-primary-soft px-3 py-2">
              <AgentAvatar agentType={addingType} status="ready" />
              <input
                value={inputName}
                placeholder="输入 Agent 名称"
                className="flex-1 rounded-md border bg-background px-2 py-1.5 text-xs text-foreground outline-none"
                style={{
                  borderColor: nameError ? 'var(--destructive)' : 'var(--border)',
                  animation: nameError ? 'shake 0.4s ease' : undefined,
                }}
                onChange={(e) => {
                  setInputName(e.target.value)
                  setNameError(false)
                }}
                onAnimationEnd={() => setNameError(false)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    handleAddAgent()
                  }
                }}
              />
              <button
                className="rounded-md bg-primary px-2.5 py-1.5 text-xs font-medium text-primary-foreground"
                onClick={handleAddAgent}
              >
                添加
              </button>
              <button
                className="text-xs text-muted-foreground hover:text-foreground"
                onClick={() => {
                  setAddingType(null)
                  setInputName('')
                  setNameError(false)
                }}
              >
                取消
              </button>
            </div>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {types.map((agent) => (
                <button
                  key={agent.type}
                  className="flex items-center gap-1.5 rounded-lg border border-border px-2.5 py-1.5 text-xs text-foreground transition-colors hover:bg-accent"
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
          {nameError && <p className="mt-1 text-xs text-destructive">请输入不重复的 Agent 名称</p>}
        </div>
      )}
    </>
  )
}
