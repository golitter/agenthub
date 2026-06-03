import { ChevronRight, ExternalLink, FolderPlus, Globe, Pin, Search } from 'lucide-react'
import { useState } from 'react'

import { AgentAvatar } from '@/components/chat/AgentAvatar'
import { GroupAvatar } from '@/components/chat/GroupAvatar'
import {
  useAddToContactGroup,
  useContactGroups,
  useCreateContactGroup,
  useDeleteContactGroup,
  useRemoveFromContactGroup,
} from '@/hooks/use-contact-groups'
import { useConversations } from '@/hooks/use-conversations'
import type { Conversation } from '@/lib/api'
import { AGENT_NAMES, PROJECT_META } from '@/lib/constants'
import { cn } from '@/lib/utils'
import { useActiveTab, useChatNav } from '@/stores/chat'

export function ContactsPage() {
  const [search, setSearch] = useState('')
  const [newGroupName, setNewGroupName] = useState('')
  const [showNewGroup, setShowNewGroup] = useState(false)
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})

  const { data: conversations } = useConversations()
  const { data: groupsData } = useContactGroups()
  const createGroup = useCreateContactGroup()
  const deleteGroup = useDeleteContactGroup()
  const addItem = useAddToContactGroup()
  const removeItem = useRemoveFromContactGroup()
  const { setCurrentSession } = useChatNav()
  const { setActiveTab } = useActiveTab()

  const groups = groupsData?.groups ?? []
  const _ungroupedIds = groupsData?.ungrouped_task_ids ?? []
  const convMap = buildConvMap(conversations ?? [])

  // Split conversations into pinned and non-pinned
  const pinnedConvs = conversations?.filter((c) => c.pinnedAt) ?? []

  const filteredPinned = filterConvs(pinnedConvs, search)

  const toggleGroup = (groupId: string) => {
    setExpandedGroups((prev) => ({ ...prev, [groupId]: !prev[groupId] }))
  }

  const handleCreateGroup = () => {
    if (!newGroupName.trim()) return
    createGroup.mutate(newGroupName.trim(), {
      onSuccess: () => {
        setNewGroupName('')
        setShowNewGroup(false)
      },
    })
  }

  const handleDeleteGroup = (groupId: string, name: string) => {
    if (!confirm(`确认删除分组「${name}」？成员将移至未分组。`)) return
    deleteGroup.mutate(groupId)
  }

  const openChat = (conv: Conversation) => {
    setCurrentSession(conv.sessionId)
    setActiveTab('chat')
  }

  return (
    <div className="flex h-full bg-background">
      {/* Left: Contacts list */}
      <div className="flex h-full w-[420px] shrink-0 flex-col border-r border-border">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-3">
          <h2 className="text-sm font-semibold text-foreground">通讯录</h2>
        </div>

        {/* Search */}
        <div className="border-b border-border px-4 py-3">
          <div className="flex items-center gap-2 rounded-lg bg-accent px-3 py-1.5">
            <Search className="h-3.5 w-3.5 shrink-0 text-tertiary" strokeWidth={1.25} />
            <input
              type="text"
              placeholder="搜索会话..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full bg-transparent text-xs text-foreground outline-none"
            />
          </div>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-4 py-3">
          {/* Pinned section */}
          {filteredPinned.length > 0 && (
            <div className="mb-4">
              <div className="mb-2 flex items-center gap-1.5 px-1 text-[11px] font-semibold uppercase tracking-wider text-tertiary">
                <Pin className="h-3 w-3" />
                置顶会话
                <span className="ml-1 rounded-full bg-muted px-1.5 text-[10px] font-normal">
                  {filteredPinned.length}
                </span>
              </div>
              {filteredPinned.map((conv) => (
                <ContactCard
                  key={conv.taskId}
                  conv={conv}
                  groups={groups}
                  onOpen={openChat}
                  onMove={addItem.mutate}
                />
              ))}
            </div>
          )}

          {/* Custom groups */}
          {groups.map((group) => {
            const groupConvs = group.items
              .map((item) => convMap.get(item.task_id))
              .filter(Boolean) as Conversation[]
            const filteredGroupConvs = filterConvs(groupConvs, search)
            const isExpanded = expandedGroups[group.group_id] !== false // default expanded

            return (
              <div key={group.group_id} className="mb-4">
                <div className="group flex items-center justify-between rounded-md px-1 py-1.5">
                  <button
                    type="button"
                    className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-text-secondary"
                    onClick={() => toggleGroup(group.group_id)}
                  >
                    <ChevronRight
                      className={cn('h-3 w-3 transition-transform', isExpanded ? 'rotate-90' : '')}
                      strokeWidth={1.5}
                    />
                    📁 {group.name}
                    <span className="rounded-full bg-muted px-1.5 text-[10px] font-normal text-tertiary">
                      {groupConvs.length}
                    </span>
                  </button>
                  <div className="flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                    <button
                      type="button"
                      className="rounded p-1 text-tertiary transition-[transform,opacity] hover:bg-bg-hover hover:text-foreground"
                      onClick={() => handleDeleteGroup(group.group_id, group.name)}
                      title="删除分组"
                    >
                      ×
                    </button>
                  </div>
                </div>
                {isExpanded && (
                  <div>
                    {filteredGroupConvs.length > 0 ? (
                      filteredGroupConvs.map((conv) => (
                        <ContactCard
                          key={conv.taskId}
                          conv={conv}
                          isInGroup={group.group_id}
                          onOpen={openChat}
                          onRemove={removeItem.mutate}
                        />
                      ))
                    ) : (
                      <p className="px-3 py-2 text-xs text-tertiary">暂无会话</p>
                    )}
                  </div>
                )}
              </div>
            )
          })}

          {/* Ungrouped */}
          {(() => {
            // Collect all task IDs that are already in a group
            const groupedTaskIds = new Set(groups.flatMap((g) => g.items.map((i) => i.task_id)))

            // Show non-pinned conversations that are NOT in any custom group
            const ungroupedConvs = (conversations ?? []).filter(
              (c) => !c.pinnedAt && !groupedTaskIds.has(c.taskId),
            )
            const displayedUngrouped = filterConvs(ungroupedConvs, search)
            if (displayedUngrouped.length === 0) return null

            return (
              <div className="mb-4">
                <div className="mb-2 px-1 text-[11px] font-semibold uppercase tracking-wider text-tertiary">
                  未分组
                  <span className="ml-1.5 rounded-full bg-muted px-1.5 text-[10px] font-normal">
                    {displayedUngrouped.length}
                  </span>
                </div>
                {displayedUngrouped.map((conv) => (
                  <ContactCard
                    key={conv.taskId}
                    conv={conv}
                    groups={groups}
                    onOpen={openChat}
                    onMove={addItem.mutate}
                  />
                ))}
              </div>
            )
          })()}

          {/* New group */}
          <div className="mt-4">
            {showNewGroup ? (
              <div className="flex gap-2">
                <input
                  type="text"
                  value={newGroupName}
                  onChange={(e) => setNewGroupName(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleCreateGroup()}
                  placeholder="输入分组名称..."
                  className="flex-1 rounded-md border border-border bg-code-bg px-3 py-1.5 text-xs text-foreground outline-none transition-colors focus:border-primary"
                  autoFocus
                />
                <button
                  type="button"
                  className="rounded-md bg-primary px-3 py-1.5 text-xs text-primary-foreground"
                  onClick={handleCreateGroup}
                >
                  确定
                </button>
                <button
                  type="button"
                  className="rounded-md border border-border px-3 py-1.5 text-xs text-text-secondary"
                  onClick={() => {
                    setShowNewGroup(false)
                    setNewGroupName('')
                  }}
                >
                  取消
                </button>
              </div>
            ) : (
              <button
                type="button"
                className="flex w-full items-center gap-2 rounded-md border border-dashed border-border px-3 py-2.5 text-xs text-tertiary transition-[transform,opacity] hover:border-primary hover:text-primary"
                onClick={() => setShowNewGroup(true)}
              >
                <FolderPlus className="h-3.5 w-3.5" />
                新建分组
              </button>
            )}
          </div>
        </div>
      </div>

      {/* Right: Branding panel */}
      <div className="relative flex flex-1 flex-col items-center overflow-hidden p-8 pt-[18vh]">
        {/* GitHub link — top-right corner */}
        <a
          href={PROJECT_META.GITHUB_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="absolute right-5 top-5 flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs text-tertiary transition-[transform,opacity] hover:border-primary hover:text-primary"
        >
          <Globe className="h-4 w-4" />
          GitHub
          <ExternalLink className="h-3 w-3" strokeWidth={1.5} />
        </a>

        {/* Logo */}
        <div className="flex flex-col items-center gap-6">
          <div className="flex items-center gap-3">
            <img src="/favicon.svg" alt={PROJECT_META.NAME} className="h-14 w-14 rounded-xl" />
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-foreground">
                {PROJECT_META.NAME}
              </h1>
              <p className="text-xs text-tertiary">{PROJECT_META.DESCRIPTION_EN}</p>
            </div>
          </div>

          {/* Description */}
          <p className="max-w-sm text-center text-sm leading-relaxed text-text-secondary">
            {PROJECT_META.DESCRIPTION_ZH}
          </p>

          {/* Feature pills */}
          <div className="flex flex-wrap justify-center gap-2">
            {['多 Agent 协作', '实时流式通信', '工作区隔离', '技能供给'].map((tag) => (
              <span
                key={tag}
                className="rounded-full border border-border bg-accent px-3 py-1 text-[11px] text-text-secondary"
              >
                {tag}
              </span>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

// ── Sub-components ──

function ContactCard({
  conv,
  groups,
  isInGroup,
  onOpen,
  onMove,
  onRemove,
}: {
  conv: Conversation
  groups?: { group_id: string; name: string }[]
  isInGroup?: string
  onOpen: (conv: Conversation) => void
  onMove?: ReturnType<typeof useAddToContactGroup>['mutate']
  onRemove?: ReturnType<typeof useRemoveFromContactGroup>['mutate']
}) {
  const isGroup = !!conv.isGroupChat
  const displayName = isGroup
    ? conv.title
    : conv.agentName || AGENT_NAMES[conv.agentType] || conv.agentType

  return (
    <div className="group flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-[transform,opacity] hover:bg-bg-hover">
      {/* Clickable area — navigate to chat */}
      <button
        type="button"
        className="flex min-w-0 flex-1 items-center gap-3 bg-transparent text-left outline-none"
        onClick={() => onOpen(conv)}
      >
        {isGroup && conv.groupAgentTypes && conv.groupAgentNames ? (
          <GroupAvatar agentTypes={conv.groupAgentTypes} agentNames={conv.groupAgentNames} />
        ) : (
          <AgentAvatar
            agentType={conv.agentType}
            status={conv.status === 'running' ? 'running' : 'ready'}
            avatarUrl={conv.avatarUrl}
            agentName={conv.agentName || undefined}
            sessionId={conv.sessionId}
          />
        )}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-sm font-medium text-foreground">{displayName}</span>
            {conv.pinnedAt && (
              <Pin className="h-3 w-3 shrink-0 -rotate-45 text-primary" strokeWidth={1.5} />
            )}
          </div>
          <p className="truncate text-xs text-tertiary">
            {isGroup
              ? `${conv.groupAgentNames?.join(' · ') ?? '群聊'}`
              : `${conv.agentType} · ${conv.status}`}
          </p>
        </div>
      </button>

      {/* Move to group — separate from clickable area */}
      {!isInGroup && groups && groups.length > 0 && (
        <select
          className="shrink-0 cursor-pointer rounded-md border border-border bg-transparent px-2 py-1 text-xs text-tertiary outline-none hover:border-primary hover:text-primary"
          onChange={(e) => {
            const val = e.target.value
            if (val && onMove) {
              onMove({ groupId: val, taskId: conv.taskId })
            }
            e.target.selectedIndex = 0
          }}
        >
          <option value="">移至分组</option>
          {groups.map((g) => (
            <option key={g.group_id} value={g.group_id}>
              {g.name}
            </option>
          ))}
        </select>
      )}

      {/* Remove from group */}
      {isInGroup && onRemove && (
        <button
          type="button"
          className="shrink-0 rounded p-1 text-xs text-tertiary transition-opacity hover:bg-bg-hover hover:text-foreground"
          onClick={() => onRemove({ groupId: isInGroup, taskId: conv.taskId })}
          title="移出分组"
        >
          ×
        </button>
      )}
    </div>
  )
}

// ── Helpers ──

function buildConvMap(convs: Conversation[]): Map<string, Conversation> {
  const map = new Map<string, Conversation>()
  for (const c of convs) map.set(c.taskId, c)
  return map
}

function filterConvs(convs: Conversation[], search: string): Conversation[] {
  if (!search) return convs
  const q = search.toLowerCase()
  return convs.filter(
    (c) =>
      c.agentType.toLowerCase().includes(q) ||
      c.title.toLowerCase().includes(q) ||
      c.agentName.toLowerCase().includes(q),
  )
}
