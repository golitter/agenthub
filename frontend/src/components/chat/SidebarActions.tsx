import { useQueryClient } from '@tanstack/react-query'
import { Download, LogOut, Pin } from 'lucide-react'
import { useState } from 'react'

import { leaveTask, updateTaskPin } from '@/lib/api'
import { MESSAGE_ROLES } from '@/lib/constants'
import { UI_ACTIONS, UI_CONFIRMS, UI_ERRORS, UI_LABELS, UI_MESSAGES } from '@/lib/ui-text'
import { useChatNav } from '@/stores/chat'

interface SidebarActionsProps {
  taskId: string
  sessionId: string
  isGroupChat: boolean
  sessions: { sessionId: string; agentName?: string }[]
  isPinned: boolean
}

export function SidebarActions({
  taskId,
  sessionId,
  isGroupChat,
  sessions,
  isPinned,
}: SidebarActionsProps) {
  const queryClient = useQueryClient()
  const { clearNavigation } = useChatNav()
  const [confirmingLeave, setConfirmingLeave] = useState(false)
  const [pinUpdating, setPinUpdating] = useState(false)
  const [leaving, setLeaving] = useState(false)
  const [actionError, setActionError] = useState('')
  const leaveConfirmMessage = isGroupChat ? UI_CONFIRMS.EXIT_GROUP : UI_CONFIRMS.DELETE_CHAT

  return (
    <div className="flex flex-col gap-0.5 px-4 py-3">
      <button
        type="button"
        className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-xs text-muted-foreground transition-[background,color,transform,opacity] hover:bg-bg-hover hover:text-foreground active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        onClick={() =>
          exportChatAsMarkdown(taskId, isGroupChat ? sessions.map((s) => s.sessionId) : [sessionId])
        }
      >
        <Download className="h-3.5 w-3.5 text-muted-foreground" strokeWidth={1.25} />
        {UI_LABELS.EXPORT_CHAT}
      </button>
      <button
        type="button"
        className={`flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-xs transition-[background,color,transform,opacity] hover:bg-bg-hover active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring ${
          isPinned ? 'text-primary' : 'text-muted-foreground hover:text-foreground'
        }`}
        onClick={async () => {
          if (pinUpdating) return
          setConfirmingLeave(false)
          setActionError('')
          setPinUpdating(true)
          const newPin = isPinned ? null : new Date().toISOString()
          try {
            await updateTaskPin(taskId, newPin)
            await queryClient.invalidateQueries({ queryKey: ['conversations'] })
          } catch {
            setActionError(UI_ERRORS.UPDATE_PIN_FAILED)
          } finally {
            setPinUpdating(false)
          }
        }}
        disabled={pinUpdating}
      >
        <Pin
          className={`h-3.5 w-3.5 ${isPinned ? 'text-primary' : 'text-muted-foreground'}`}
          strokeWidth={1.25}
        />
        {isPinned ? UI_ACTIONS.UNPIN : UI_LABELS.PIN_CHAT}
      </button>
      <button
        type="button"
        className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-xs text-destructive transition-[background,transform,opacity] hover:bg-danger-bg active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        onClick={async () => {
          if (!confirmingLeave) {
            setActionError('')
            setConfirmingLeave(true)
            return
          }
          if (leaving) return
          setLeaving(true)
          try {
            await leaveTask(taskId)
            await queryClient.invalidateQueries({ queryKey: ['conversations'] })
            clearNavigation()
          } catch {
            setActionError(UI_ERRORS.LEAVE_TASK_FAILED)
          } finally {
            setLeaving(false)
          }
        }}
        disabled={leaving}
      >
        <LogOut className="h-3.5 w-3.5 text-destructive" strokeWidth={1.25} />
        {confirmingLeave
          ? UI_ACTIONS.CONFIRM
          : isGroupChat
            ? UI_LABELS.EXIT_GROUP
            : UI_LABELS.DELETE_CHAT}
      </button>
      {confirmingLeave && (
        <div className="rounded-[8px] border border-destructive/20 bg-danger-bg px-3 py-2">
          <p className="text-[11px] leading-5 text-destructive">{leaveConfirmMessage}</p>
          <button
            type="button"
            className="mt-2 rounded-[6px] border border-border px-2.5 py-1 text-[11px] text-text-secondary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            onClick={() => setConfirmingLeave(false)}
          >
            {UI_ACTIONS.CANCEL}
          </button>
        </div>
      )}
      {actionError && (
        <p className="px-2.5 py-1 text-[11px] text-destructive" role="alert">
          {actionError}
        </p>
      )}
    </div>
  )
}

/** 在视口右下角显示一个轻量的复制成功 toast。 */
export function showCopyToast() {
  const existing = document.getElementById('copy-toast')
  if (existing) existing.remove()

  const el = document.createElement('div')
  el.id = 'copy-toast'
  el.textContent = UI_MESSAGES.COPY_SUCCESS
  Object.assign(el.style, {
    position: 'fixed',
    bottom: '24px',
    right: '24px',
    padding: '8px 16px',
    borderRadius: '8px',
    fontSize: '13px',
    color: 'var(--text-primary)',
    background: 'var(--bg-card)',
    backdropFilter: 'blur(8px)',
    zIndex: 'var(--z-toast)',
    pointerEvents: 'none',
    opacity: '0',
    transition: 'opacity 0.2s ease',
  } satisfies Partial<CSSStyleDeclaration>)
  document.body.appendChild(el)
  requestAnimationFrame(() => {
    el.style.opacity = '1'
  })
  setTimeout(() => {
    el.style.opacity = '0'
    setTimeout(() => el.remove(), 200)
  }, 1500)
}

async function exportChatAsMarkdown(taskId: string, sessionIds: string[]) {
  const { useChatStore } = await import('@/stores/chat')
  const store = useChatStore.getState().sessions
  const allMessages = sessionIds
    .flatMap((sid) => store[sid]?.messages ?? [])
    .sort((a, b) => a.timestamp - b.timestamp)
  const lines = allMessages.map(
    (m) =>
      `**${m.role === MESSAGE_ROLES.USER ? 'You' : (m.agentName ?? 'Agent')}** (${new Date(m.timestamp).toLocaleString()}):\n${m.content}`,
  )
  const md = `# Chat Export — ${taskId}\n\n${lines.join('\n\n---\n\n')}`
  const blob = new Blob([md], { type: 'text/markdown' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `chat-${taskId}.md`
  a.click()
  URL.revokeObjectURL(url)
}
