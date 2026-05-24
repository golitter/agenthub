import { useState } from 'react'

import type { AgentType } from '@/generated/request'
import { useChatStream } from '@/hooks/use-chat-stream'
import { validateRepoPath } from '@/lib/api'
import { AGENT_NAMES } from '@/lib/constants'

import { AgentAvatar } from './AgentAvatar'
import { AgentEditDialog } from './AgentEditDialog'
import { MessageInput } from './MessageInput'
import { MessageList } from './MessageList'

interface ChatAreaProps {
  taskId: string
  sessionId: string
  agentType?: AgentType
  agentName?: string
  avatarUrl?: string
  repoPath?: string
}

export function ChatArea({
  taskId,
  sessionId,
  agentType = 'claude-code',
  agentName,
  avatarUrl,
  repoPath,
}: ChatAreaProps) {
  const { state, sendMessage } = useChatStream(taskId)
  const isStreaming = ['loading', 'streaming', 'tool_running'].includes(state.status)
  const [editOpen, setEditOpen] = useState(false)
  const [validationError, setValidationError] = useState<string | null>(null)
  const [validating, setValidating] = useState(false)

  const handleSend = async (message: string) => {
    setValidationError(null)

    if (repoPath) {
      setValidating(true)
      try {
        const result = await validateRepoPath(repoPath)
        if (!result.valid) {
          setValidationError(result.errors.join('; '))
          setValidating(false)
          return
        }
      } catch {
        setValidationError('路径校验失败，请检查 Agent 服务是否可用')
        setValidating(false)
        return
      }
      setValidating(false)
    }

    sendMessage(message, sessionId, agentType)
  }

  const displayName = agentName ?? AGENT_NAMES[agentType] ?? agentType

  return (
    <div className="flex h-full flex-col" style={{ backgroundColor: 'var(--bg-canvas)' }}>
      {/* Header — IM style: agent avatar + name */}
      <div
        className="flex h-12 shrink-0 items-center gap-3 border-b px-6"
        style={{ borderColor: 'var(--divider)' }}
      >
        <button className="cursor-pointer" onClick={() => setEditOpen(true)}>
          <AgentAvatar
            agentType={agentType}
            status={isStreaming ? 'running' : 'ready'}
            size={28}
            avatarUrl={avatarUrl}
            agentName={agentName}
          />
        </button>
        <button className="cursor-pointer" onClick={() => setEditOpen(true)}>
          <h2 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            {displayName}
          </h2>
        </button>
        {isStreaming && (
          <p className="text-[11px]" style={{ color: 'var(--color-brand)' }}>
            正在回复...
          </p>
        )}
      </div>

      {/* Validation error banner */}
      {validationError && (
        <div
          className="shrink-0 px-4 py-2 text-xs"
          style={{ backgroundColor: '#FEF2F2', color: '#EF4444' }}
        >
          {validationError}
          <button className="ml-2 underline" onClick={() => setValidationError(null)}>
            关闭
          </button>
        </div>
      )}

      {/* Messages */}
      {state.messages.length === 0 && !isStreaming ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2">
          <AgentAvatar
            agentType={agentType}
            status="ready"
            size={48}
            avatarUrl={avatarUrl}
            agentName={agentName}
          />
          <p className="mt-2 text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            {displayName}
          </p>
          <p className="text-xs" style={{ color: 'var(--text-tertiary)' }}>
            发送消息开始对话
          </p>
        </div>
      ) : (
        <MessageList
          messages={state.messages}
          streamingContent={state.streamingContent}
          streamingAgentType={state.streamingAgentType}
          isStreaming={isStreaming}
          avatarUrl={avatarUrl}
          agentName={agentName}
        />
      )}

      {/* Input */}
      <MessageInput
        onSend={handleSend}
        disabled={isStreaming || validating}
        placeholder={validating ? '校验路径中...' : `发消息给 ${displayName}...`}
      />

      {/* Edit dialog */}
      <AgentEditDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        sessionId={sessionId}
        agentName={displayName}
        avatarUrl={avatarUrl}
      />
    </div>
  )
}
