import { ChevronDown, Pin, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'

import { MESSAGE_ROLES } from '@/lib/constants'
import {
  UI_ACTIONS,
  UI_CONFIRMS,
  UI_LABELS,
  UI_MESSAGES,
  UI_PLACEHOLDERS,
  UI_STATUS,
} from '@/lib/ui-text'
import { useChatStore } from '@/stores/chat'

import { useCollapsible } from './RightSidebar'

interface AnnouncementsSectionProps {
  taskId: string
}

export function AnnouncementsSection({ taskId }: AnnouncementsSectionProps) {
  const [open, toggleOpen] = useCollapsible('announcements')
  const announcements = useChatStore((s) => s.announcements[taskId]) ?? []
  const loading = useChatStore((s) => s.announcementsLoading[taskId]) ?? false
  const loadAnnouncements = useChatStore((s) => s.loadAnnouncements)
  const addAnnouncement = useChatStore((s) => s.addAnnouncement)
  const removeAnnouncement = useChatStore((s) => s.removeAnnouncement)

  const [showCreateForm, setShowCreateForm] = useState(false)
  const [newContent, setNewContent] = useState('')
  const [newPinned, setNewPinned] = useState(false)
  const [deleteTargetId, setDeleteTargetId] = useState<number | null>(null)

  useEffect(() => {
    loadAnnouncements(taskId)
  }, [taskId, loadAnnouncements])

  const handleCreate = async () => {
    if (!newContent.trim()) return
    await addAnnouncement(taskId, {
      sender_id: MESSAGE_ROLES.USER,
      sender_name: 'You',
      content: newContent.trim(),
      pinned: newPinned,
    })
    setNewContent('')
    setNewPinned(false)
    setShowCreateForm(false)
  }

  const handleDelete = async (id: number) => {
    await removeAnnouncement(taskId, id)
    setDeleteTargetId(null)
  }

  const sorted = [...announcements].sort((a, b) => {
    if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
  })

  return (
    <div className="border-b border-sidebar-border">
      {/* Header */}
      <button
        type="button"
        className="flex w-full items-center justify-between px-4 py-3 pb-2.5 text-left user-select-none"
        onClick={toggleOpen}
      >
        <span className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-text-secondary transition-[transform,opacity] hover:text-foreground">
          {UI_LABELS.ANNOUNCEMENTS}
          <span className="rounded-full bg-accent px-1.5 py-px text-[11px] font-normal tracking-normal text-tertiary">
            {announcements.length}
          </span>
        </span>
        <ChevronDown
          className={`h-3.5 w-3.5 text-tertiary transition-transform ${open ? '' : '-rotate-90'}`}
          strokeWidth={1.25}
        />
      </button>

      {/* Body */}
      <div
        className={`overflow-hidden transition-[max-height] duration-200 ease-out ${open ? 'max-h-[600px] overflow-y-auto' : 'max-h-0'}`}
      >
        <div className="px-4 pb-3.5">
          {loading && announcements.length === 0 && (
            <div className="py-3 text-center text-xs text-tertiary">{UI_STATUS.LOADING}</div>
          )}

          {!loading && announcements.length === 0 && (
            <div className="py-3 text-center text-xs text-tertiary">
              {UI_MESSAGES.NO_ANNOUNCEMENTS}
            </div>
          )}

          {sorted.map((ann) => (
            <div
              key={ann.id}
              className="group relative mb-2 rounded-md border border-border bg-card p-3 transition-[transform,opacity] last:mb-0 hover:border-primary-border hover:bg-bg-hover"
            >
              {/* Delete button — visible on hover */}
              {deleteTargetId !== ann.id && (
                <button
                  type="button"
                  className="absolute right-2 top-2 flex h-6 w-6 items-center justify-center rounded-md text-tertiary opacity-100 transition-[background,color,transform,opacity] hover:bg-danger-bg hover:text-destructive active:scale-[0.94] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring md:opacity-0 md:group-hover:opacity-100"
                  title={UI_LABELS.DELETE_ANNOUNCEMENT}
                  aria-label={UI_LABELS.DELETE_ANNOUNCEMENT}
                  onClick={() => setDeleteTargetId(ann.id)}
                >
                  <Trash2 className="h-3 w-3" strokeWidth={1.25} />
                </button>
              )}
              {ann.pinned && (
                <span className="mb-1.5 inline-flex items-center gap-1 rounded-full bg-warning-soft px-2 py-0.5 text-[11px] font-medium text-color-warning">
                  <Pin className="h-2.5 w-2.5" strokeWidth={1.25} /> 置顶
                </span>
              )}
              <div className="mb-1 flex items-center gap-1.5 text-xs font-medium text-text-secondary">
                <span>{ann.sender_name}</span>
              </div>
              <div className="text-xs leading-relaxed text-foreground">{ann.content}</div>
              <div className="mt-1.5 text-[11px] text-tertiary">
                {new Date(ann.created_at).toLocaleDateString('zh-CN', {
                  month: '2-digit',
                  day: '2-digit',
                })}
              </div>
              {deleteTargetId === ann.id && (
                <div className="mt-2 rounded-[7px] border border-destructive/20 bg-danger-bg px-2.5 py-2">
                  <p className="text-[11px] text-destructive">{UI_CONFIRMS.DELETE_ANNOUNCEMENT}</p>
                  <div className="mt-2 flex justify-end gap-2">
                    <button
                      type="button"
                      className="rounded-[6px] border border-border px-2 py-1 text-[11px] text-text-secondary transition-[background,color,transform] hover:bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                      onClick={() => setDeleteTargetId(null)}
                    >
                      {UI_ACTIONS.CANCEL}
                    </button>
                    <button
                      type="button"
                      className="rounded-[6px] border border-destructive/20 bg-destructive/10 px-2 py-1 text-[11px] font-medium text-destructive transition-[background,transform,opacity] hover:bg-destructive/20 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                      onClick={() => handleDelete(ann.id)}
                    >
                      {UI_ACTIONS.DELETE}
                    </button>
                  </div>
                </div>
              )}
            </div>
          ))}

          {/* Create button */}
          {!showCreateForm ? (
            <button
              type="button"
              className="mt-2 w-full rounded-md border border-dashed border-primary-border py-2 text-xs text-muted-foreground transition-[transform,opacity] hover:border-primary hover:bg-primary-soft hover:text-primary"
              onClick={() => setShowCreateForm(true)}
            >
              {UI_LABELS.NEW_ANNOUNCEMENT}
            </button>
          ) : (
            <div className="mt-2 rounded-md border border-border bg-card p-3">
              <textarea
                className="mb-2 w-full resize-none rounded-md border border-border bg-background p-2 text-xs text-foreground outline-none placeholder:text-tertiary focus:border-ring"
                placeholder={UI_PLACEHOLDERS.ANNOUNCEMENT_INPUT}
                rows={3}
                value={newContent}
                onChange={(e) => setNewContent(e.target.value)}
                autoFocus
              />
              <div className="flex items-center justify-between">
                <label className="flex items-center gap-1.5 text-xs text-text-secondary">
                  <input
                    type="checkbox"
                    checked={newPinned}
                    onChange={(e) => setNewPinned(e.target.checked)}
                    className="rounded"
                  />
                  {UI_ACTIONS.PIN}
                </label>
                <div className="flex gap-2">
                  <button
                    type="button"
                    className="rounded-md px-3 py-1 text-xs text-muted-foreground hover:bg-bg-hover"
                    onClick={() => {
                      setShowCreateForm(false)
                      setNewContent('')
                      setNewPinned(false)
                    }}
                  >
                    {UI_ACTIONS.CANCEL}
                  </button>
                  <button
                    type="button"
                    className="rounded-md bg-primary px-3 py-1 text-xs text-primary-foreground hover:opacity-90 disabled:opacity-40"
                    disabled={!newContent.trim()}
                    onClick={handleCreate}
                  >
                    {UI_ACTIONS.PUBLISH}
                  </button>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
