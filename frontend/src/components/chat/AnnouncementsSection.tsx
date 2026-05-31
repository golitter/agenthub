import { useEffect, useState } from 'react'

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

  useEffect(() => {
    loadAnnouncements(taskId)
  }, [taskId, loadAnnouncements])

  const handleCreate = async () => {
    if (!newContent.trim()) return
    await addAnnouncement(taskId, {
      sender_id: 'user',
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
        <span className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-secondary transition-colors hover:text-foreground">
          群公告
          <span className="rounded-full bg-accent px-1.5 py-px text-[11px] font-normal tracking-normal text-tertiary">
            {announcements.length}
          </span>
        </span>
        <svg
          className={`h-3.5 w-3.5 text-tertiary transition-transform ${open ? '' : '-rotate-90'}`}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>

      {/* Body */}
      <div
        className={`overflow-hidden transition-[max-height] duration-200 ease-out ${open ? 'max-h-[600px] overflow-y-auto' : 'max-h-0'}`}
      >
        <div className="px-4 pb-3.5">
          {loading && announcements.length === 0 && (
            <div className="py-3 text-center text-xs text-tertiary">加载中...</div>
          )}

          {!loading && announcements.length === 0 && (
            <div className="py-3 text-center text-xs text-tertiary">暂无公告</div>
          )}

          {sorted.map((ann) => (
            <div
              key={ann.id}
              className="group mb-2 cursor-pointer rounded-md border border-border bg-card p-3 transition-colors last:mb-0 hover:border-primary-border hover:bg-bg-hover"
              onContextMenu={(e) => {
                e.preventDefault()
                if (confirm('确认删除此公告？')) handleDelete(ann.id)
              }}
            >
              {ann.pinned && (
                <span className="mb-1.5 inline-flex rounded-full bg-warning-soft px-2 py-0.5 text-[10px] font-medium text-color-warning">
                  📌 置顶
                </span>
              )}
              <div className="mb-1 flex items-center gap-1.5 text-xs font-medium text-secondary">
                <span>{ann.sender_name}</span>
              </div>
              <div className="text-xs leading-relaxed text-foreground">{ann.content}</div>
              <div className="mt-1.5 text-[10px] text-tertiary">
                {new Date(ann.created_at).toLocaleDateString('zh-CN', {
                  month: '2-digit',
                  day: '2-digit',
                })}
              </div>
            </div>
          ))}

          {/* Create button */}
          {!showCreateForm ? (
            <button
              type="button"
              className="mt-2 w-full rounded-md border border-dashed border-primary-border py-2 text-xs text-muted-foreground transition-colors hover:border-primary hover:bg-primary-soft hover:text-primary"
              onClick={() => setShowCreateForm(true)}
            >
              + 发布新公告
            </button>
          ) : (
            <div className="mt-2 rounded-md border border-border bg-card p-3">
              <textarea
                className="mb-2 w-full resize-none rounded-md border border-border bg-background p-2 text-xs text-foreground outline-none placeholder:text-tertiary focus:border-ring"
                placeholder="输入公告内容..."
                rows={3}
                value={newContent}
                onChange={(e) => setNewContent(e.target.value)}
                autoFocus
              />
              <div className="flex items-center justify-between">
                <label className="flex items-center gap-1.5 text-xs text-secondary">
                  <input
                    type="checkbox"
                    checked={newPinned}
                    onChange={(e) => setNewPinned(e.target.checked)}
                    className="rounded"
                  />
                  置顶
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
                    取消
                  </button>
                  <button
                    type="button"
                    className="rounded-md bg-primary px-3 py-1 text-xs text-primary-foreground hover:opacity-90 disabled:opacity-40"
                    disabled={!newContent.trim()}
                    onClick={handleCreate}
                  >
                    发布
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
