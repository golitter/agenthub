import { ChevronRight, ExternalLink, Folder, FolderPlus, Globe, Pin, Search, X } from 'lucide-react'
import { useState } from 'react'
import { useNavigate } from 'react-router'

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
import {
  UI_ACTIONS,
  UI_CONFIRMS,
  UI_ERRORS,
  UI_LABELS,
  UI_MESSAGES,
  UI_MISC,
  UI_PLACEHOLDERS,
  UI_STATUS,
} from '@/lib/ui-text'
import { cn } from '@/lib/utils'
import { useChatNav } from '@/stores/chat'

export function ContactsPage() {
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [newGroupName, setNewGroupName] = useState('')
  const [showNewGroup, setShowNewGroup] = useState(false)
  const [deleteGroupTarget, setDeleteGroupTarget] = useState<string | null>(null)
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})
  const [actionError, setActionError] = useState('')

  const {
    data: conversations,
    isError: conversationsError,
    isLoading: conversationsLoading,
    refetch: refetchConversations,
  } = useConversations()
  const {
    data: groupsData,
    isError: groupsError,
    isLoading: groupsLoading,
    refetch: refetchGroups,
  } = useContactGroups()
  const createGroup = useCreateContactGroup()
  const deleteGroup = useDeleteContactGroup()
  const addItem = useAddToContactGroup()
  const removeItem = useRemoveFromContactGroup()
  const { setCurrentSession } = useChatNav()

  const groups = groupsData?.groups ?? []
  const convMap = buildConvMap(conversations ?? [])
  const query = search.trim()
  const pageLoading = conversationsLoading || groupsLoading

  // 将会话分为置顶和非置顶
  const pinnedConvs = conversations?.filter((c) => c.pinnedAt) ?? []
  const filteredPinned = filterConvs(pinnedConvs, query)
  const groupedTaskIds = new Set(groups.flatMap((group) => group.items.map((item) => item.task_id)))
  const ungroupedConvs = (conversations ?? []).filter(
    (conversation) => !conversation.pinnedAt && !groupedTaskIds.has(conversation.taskId),
  )
  const displayedUngrouped = filterConvs(ungroupedConvs, query)
  const hasSearchMatches =
    filteredPinned.length > 0 ||
    displayedUngrouped.length > 0 ||
    groups.some((group) => {
      const groupConversations = group.items
        .map((item) => convMap.get(item.task_id))
        .filter(Boolean) as Conversation[]
      return filterConvs(groupConversations, query).length > 0
    })

  const toggleGroup = (groupId: string) => {
    setExpandedGroups((prev) => ({ ...prev, [groupId]: !prev[groupId] }))
  }

  const handleCreateGroup = () => {
    if (!newGroupName.trim()) return
    setActionError('')
    createGroup.mutate(newGroupName.trim(), {
      onSuccess: () => {
        setNewGroupName('')
        setShowNewGroup(false)
      },
      onError: () => setActionError(UI_ERRORS.CREATE_GROUP_FAILED),
    })
  }

  const handleDeleteGroup = (groupId: string) => {
    setActionError('')
    deleteGroup.mutate(groupId, {
      onSuccess: () => {
        setDeleteGroupTarget(null)
        setActionError('')
      },
      onError: () => setActionError(UI_ERRORS.DELETE_GROUP_FAILED),
    })
  }

  const handleMoveToGroup = ({ groupId, taskId }: { groupId: string; taskId: string }) => {
    setActionError('')
    addItem.mutate(
      { groupId, taskId },
      { onError: () => setActionError(UI_ERRORS.MOVE_GROUP_FAILED) },
    )
  }

  const handleRemoveFromGroup = ({ groupId, taskId }: { groupId: string; taskId: string }) => {
    setActionError('')
    removeItem.mutate(
      { groupId, taskId },
      { onError: () => setActionError(UI_ERRORS.MOVE_GROUP_FAILED) },
    )
  }

  const openChat = (conv: Conversation) => {
    setCurrentSession(conv.sessionId)
    navigate(`/chat?session=${encodeURIComponent(conv.sessionId)}`)
  }

  return (
    <div className="flex h-full min-w-0 bg-background">
      {/* 左侧：联系人列表 */}
      <section
        className="flex h-full w-full shrink-0 flex-col border-r border-border md:w-[420px]"
        aria-labelledby="contacts-title"
        aria-busy={pageLoading}
      >
        {/* 头部 */}
        <div className="flex items-center justify-between border-b border-border px-5 py-3">
          <div>
            <h1 id="contacts-title" className="text-sm font-semibold text-foreground">
              {UI_LABELS.CONTACTS}
            </h1>
            <p className="mt-0.5 text-[11px] text-tertiary">
              {pageLoading
                ? UI_STATUS.LOADING
                : `${(conversations ?? []).length} ${UI_MISC.CONVERSATION_COUNT_SUFFIX}`}
            </p>
          </div>
        </div>

        {/* 搜索 */}
        <div className="border-b border-border px-4 py-3">
          <div className="flex items-center gap-2 rounded-lg border border-transparent bg-accent px-3 py-1.5 transition-[border-color,box-shadow] focus-within:border-primary-border focus-within:ring-2 focus-within:ring-primary/10">
            <Search
              className="h-3.5 w-3.5 shrink-0 text-tertiary"
              strokeWidth={1.25}
              aria-hidden="true"
            />
            <input
              type="text"
              placeholder={UI_PLACEHOLDERS.SEARCH_CONTACTS}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full bg-transparent text-xs text-foreground outline-none"
              aria-label={UI_PLACEHOLDERS.SEARCH_CONTACTS}
              disabled={pageLoading}
            />
            {search && (
              <button
                type="button"
                className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-tertiary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.94] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => setSearch('')}
                aria-label={UI_ACTIONS.CLEAR_SEARCH}
                title={UI_ACTIONS.CLEAR_SEARCH}
              >
                <X className="h-3.5 w-3.5" strokeWidth={1.25} aria-hidden="true" />
              </button>
            )}
          </div>
          {actionError && (
            <p className="mt-2 text-xs text-destructive" role="alert">
              {actionError}
            </p>
          )}
          {conversationsError && (
            <div
              className="mt-2 flex flex-wrap items-center justify-between gap-2 text-xs text-destructive"
              role="alert"
            >
              {UI_ERRORS.LOAD_CONVERSATIONS_FAILED}
              <button
                type="button"
                className="rounded px-2 py-1 font-medium underline-offset-4 hover:bg-danger-bg hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => refetchConversations()}
              >
                {UI_ACTIONS.RETRY}
              </button>
            </div>
          )}
          {groupsError && (
            <div
              className="mt-2 flex flex-wrap items-center justify-between gap-2 text-xs text-destructive"
              role="alert"
            >
              {UI_ERRORS.LOAD_GROUPS_FAILED}
              <button
                type="button"
                className="rounded px-2 py-1 font-medium underline-offset-4 hover:bg-danger-bg hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => refetchGroups()}
              >
                {UI_ACTIONS.RETRY}
              </button>
            </div>
          )}
        </div>

        {/* 主体 */}
        <div className="relative flex-1 overflow-y-auto px-4 py-3">
          {pageLoading && (
            <div
              className="absolute inset-0 z-10 space-y-2 bg-background px-4 py-4"
              aria-hidden="true"
            >
              {Array.from({ length: 7 }).map((_, index) => (
                <div key={index} className="flex items-center gap-3 rounded-lg px-3 py-2.5">
                  <div className="h-8 w-8 shrink-0 rounded-[9px] skeleton-sheen" />
                  <div className="min-w-0 flex-1 space-y-2">
                    <div className="h-3 w-2/5 rounded skeleton-sheen" />
                    <div className="h-2.5 w-3/5 rounded skeleton-sheen" />
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* 置顶分区 */}
          {filteredPinned.length > 0 && (
            <div className="mb-4">
              <div className="mb-2 flex items-center gap-1.5 px-1 text-[11px] font-semibold uppercase tracking-wider text-tertiary">
                <Pin className="h-3 w-3" aria-hidden="true" />
                {UI_LABELS.PIN_CHAT}
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
                  onMove={handleMoveToGroup}
                  busy={addItem.isPending || removeItem.isPending}
                />
              ))}
            </div>
          )}

          {/* 自定义分组 */}
          {groups.map((group) => {
            const groupConvs = group.items
              .map((item) => convMap.get(item.task_id))
              .filter(Boolean) as Conversation[]
            const filteredGroupConvs = filterConvs(groupConvs, query)
            const isExpanded = query ? true : expandedGroups[group.group_id] !== false // 搜索时强制展开匹配分组
            const groupContentId = `contact-group-${group.group_id}`

            if (query && filteredGroupConvs.length === 0) return null

            return (
              <div key={group.group_id} className="mb-4">
                <div className="group flex items-center justify-between rounded-md px-1 py-1.5">
                  <button
                    type="button"
                    className="flex min-w-0 items-center gap-1.5 rounded-[5px] text-[11px] font-semibold uppercase tracking-wider text-text-secondary transition-colors hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                    onClick={() => toggleGroup(group.group_id)}
                    aria-expanded={isExpanded}
                    aria-controls={groupContentId}
                  >
                    <ChevronRight
                      className={cn('h-3 w-3 transition-transform', isExpanded ? 'rotate-90' : '')}
                      strokeWidth={1.25}
                      aria-hidden="true"
                    />
                    <Folder className="h-3 w-3 shrink-0" strokeWidth={1.25} aria-hidden="true" />
                    <span className="truncate">{group.name}</span>
                    <span className="rounded-full bg-muted px-1.5 text-[10px] font-normal text-tertiary">
                      {groupConvs.length}
                    </span>
                  </button>
                  <div className="flex gap-1 opacity-100 transition-opacity md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100">
                    <button
                      type="button"
                      className="rounded p-1 text-tertiary transition-[background,color,transform] hover:bg-danger-bg hover:text-destructive active:scale-[0.94] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                      onClick={() => setDeleteGroupTarget(group.group_id)}
                      title={UI_ACTIONS.DELETE}
                      aria-label={UI_ACTIONS.DELETE}
                    >
                      <X className="h-3.5 w-3.5" strokeWidth={1.25} aria-hidden="true" />
                    </button>
                  </div>
                </div>
                {deleteGroupTarget === group.group_id && (
                  <div className="mb-2 rounded-[8px] border border-destructive/20 bg-danger-bg px-3 py-2">
                    <p className="text-xs text-destructive">{UI_CONFIRMS.DELETE_GROUP_INLINE}</p>
                    <div className="mt-2 flex justify-end gap-2">
                      <button
                        type="button"
                        className="rounded-[6px] border border-border px-2.5 py-1 text-[11px] text-text-secondary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                        onClick={() => setDeleteGroupTarget(null)}
                      >
                        {UI_ACTIONS.CANCEL}
                      </button>
                      <button
                        type="button"
                        className="rounded-[6px] border border-destructive/20 bg-destructive/10 px-2.5 py-1 text-[11px] font-medium text-destructive transition-[background,transform,opacity] hover:bg-destructive/20 active:scale-[0.97] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                        onClick={() => handleDeleteGroup(group.group_id)}
                        disabled={deleteGroup.isPending}
                      >
                        {UI_ACTIONS.DELETE}
                      </button>
                    </div>
                  </div>
                )}
                <div id={groupContentId} hidden={!isExpanded}>
                  {filteredGroupConvs.length > 0 ? (
                    filteredGroupConvs.map((conv) => (
                      <ContactCard
                        key={conv.taskId}
                        conv={conv}
                        isInGroup={group.group_id}
                        onOpen={openChat}
                        onRemove={handleRemoveFromGroup}
                        busy={addItem.isPending || removeItem.isPending}
                      />
                    ))
                  ) : (
                    <p className="px-3 py-2 text-xs text-tertiary">
                      {UI_MESSAGES.NO_CONVERSATIONS}
                    </p>
                  )}
                </div>
              </div>
            )
          })}

          {/* 未分组 */}
          {displayedUngrouped.length > 0 && (
            <div className="mb-4">
              <div className="mb-2 px-1 text-[11px] font-semibold uppercase tracking-wider text-tertiary">
                {UI_MISC.UNGROUPED}
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
                  onMove={handleMoveToGroup}
                  busy={addItem.isPending || removeItem.isPending}
                />
              ))}
            </div>
          )}

          {query && !hasSearchMatches && (
            <div className="mb-4 rounded-[10px] border border-dashed border-border px-4 py-8 text-center">
              <p className="text-sm font-medium text-foreground">
                {UI_MESSAGES.NO_MATCHING_CONVERSATIONS}
              </p>
              <p className="mt-1 text-xs leading-5 text-tertiary">
                {UI_MESSAGES.CONVERSATION_SEARCH_EMPTY_DESC}
              </p>
              <button
                type="button"
                className="mt-3 rounded-[7px] border border-border px-3 py-1.5 text-xs text-text-secondary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => setSearch('')}
              >
                {UI_ACTIONS.CLEAR_SEARCH}
              </button>
            </div>
          )}

          {/* 新建分组 */}
          <div className="mt-4">
            {showNewGroup ? (
              <div className="flex flex-wrap gap-2">
                <input
                  type="text"
                  value={newGroupName}
                  onChange={(e) => setNewGroupName(e.target.value)}
                  onKeyDown={(e) =>
                    !e.nativeEvent.isComposing && e.key === 'Enter' && handleCreateGroup()
                  }
                  placeholder={UI_PLACEHOLDERS.GROUP_NAME_INPUT}
                  aria-label={UI_PLACEHOLDERS.GROUP_NAME_INPUT}
                  className="min-w-0 flex-[1_1_10rem] rounded-md border border-border bg-code-bg px-3 py-1.5 text-xs text-foreground outline-none transition-[border-color,box-shadow] focus:border-primary focus:ring-2 focus:ring-primary/15"
                  autoFocus
                />
                <button
                  type="button"
                  className="rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-[background,transform,opacity] hover:bg-primary/90 active:scale-[0.97] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                  onClick={handleCreateGroup}
                  disabled={!newGroupName.trim() || createGroup.isPending}
                >
                  {UI_MISC.OK}
                </button>
                <button
                  type="button"
                  className="rounded-md border border-border px-3 py-1.5 text-xs text-text-secondary transition-[background,color,transform] hover:bg-bg-hover hover:text-foreground active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                  onClick={() => {
                    setShowNewGroup(false)
                    setNewGroupName('')
                  }}
                >
                  {UI_ACTIONS.CANCEL}
                </button>
              </div>
            ) : (
              <button
                type="button"
                className="flex w-full items-center gap-2 rounded-md border border-dashed border-border px-3 py-2.5 text-xs text-tertiary transition-[border-color,color,transform] hover:border-primary hover:text-primary active:scale-[0.995] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={() => setShowNewGroup(true)}
              >
                <FolderPlus className="h-3.5 w-3.5" />
                {UI_MISC.NEW_GROUP}
              </button>
            )}
          </div>
        </div>
      </section>

      {/* 右侧：品牌信息面板 */}
      <aside
        className="relative hidden flex-1 flex-col items-center overflow-hidden p-8 pt-[18vh] md:flex"
        aria-label="AgentHub 项目信息"
      >
        {/* GitHub 链接 —— 右上角 */}
        <a
          href={PROJECT_META.GITHUB_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="absolute right-5 top-5 flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs text-tertiary transition-[transform,opacity] hover:border-primary hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          <Globe className="h-4 w-4" aria-hidden="true" />
          GitHub
          <ExternalLink className="h-3 w-3" strokeWidth={1.25} aria-hidden="true" />
        </a>

        {/* Logo */}
        <div className="flex flex-col items-center gap-6">
          <div className="flex items-center gap-3">
            <img src="/favicon.svg" alt={PROJECT_META.NAME} className="h-14 w-14 rounded-xl" />
            <div>
              <h2 className="text-2xl font-bold tracking-tight text-foreground">
                {PROJECT_META.NAME}
              </h2>
              <p className="text-xs text-tertiary">{PROJECT_META.DESCRIPTION_EN}</p>
            </div>
          </div>

          {/* 描述 */}
          <p className="max-w-sm text-center text-sm leading-relaxed text-text-secondary">
            {PROJECT_META.DESCRIPTION_ZH}
          </p>

          {/* 特性标签 */}
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
      </aside>
    </div>
  )
}

// ── 子组件 ──

function ContactCard({
  conv,
  groups,
  isInGroup,
  onOpen,
  onMove,
  onRemove,
  busy = false,
}: {
  conv: Conversation
  groups?: { group_id: string; name: string }[]
  isInGroup?: string
  onOpen: (conv: Conversation) => void
  onMove?: (params: { groupId: string; taskId: string }) => void
  onRemove?: (params: { groupId: string; taskId: string }) => void
  busy?: boolean
}) {
  const isGroup = !!conv.isGroupChat
  const displayName = isGroup
    ? conv.title
    : conv.agentName || AGENT_NAMES[conv.agentType] || conv.agentType

  return (
    <div className="group flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-[transform,opacity] hover:bg-bg-hover">
      {/* 可点击区域 —— 导航到聊天 */}
      <button
        type="button"
        className="flex min-w-0 flex-1 items-center gap-3 rounded-[6px] bg-transparent text-left outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
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
              <Pin
                className="h-3 w-3 shrink-0 -rotate-45 text-primary"
                strokeWidth={1.25}
                aria-hidden="true"
              />
            )}
          </div>
          <p className="truncate text-xs text-tertiary">
            {isGroup
              ? `${conv.groupAgentNames?.join(' · ') ?? '群聊'}`
              : `${conv.agentType} · ${conv.status}`}
          </p>
        </div>
      </button>

      {/* 移动到分组 —— 与可点击区域分离 */}
      {!isInGroup && groups && groups.length > 0 && (
        <select
          aria-label={`${UI_MESSAGES.MOVE_TO_GROUP}: ${displayName}`}
          className="max-w-28 shrink-0 cursor-pointer truncate rounded-md border border-border bg-transparent px-2 py-1 text-xs text-tertiary outline-none hover:border-primary hover:text-primary disabled:cursor-not-allowed disabled:opacity-50 sm:max-w-36"
          disabled={busy}
          onChange={(e) => {
            const val = e.target.value
            if (val && onMove) {
              onMove({ groupId: val, taskId: conv.taskId })
            }
            e.target.selectedIndex = 0
          }}
        >
          <option value="">{UI_MESSAGES.MOVE_TO_GROUP}</option>
          {groups.map((g) => (
            <option key={g.group_id} value={g.group_id}>
              {g.name}
            </option>
          ))}
        </select>
      )}

      {/* 从分组移除 */}
      {isInGroup && onRemove && (
        <button
          type="button"
          className="shrink-0 rounded p-1 text-xs text-tertiary transition-opacity hover:bg-bg-hover hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          disabled={busy}
          onClick={() => onRemove({ groupId: isInGroup, taskId: conv.taskId })}
          title={UI_MISC.MOVE_OUT_GROUP}
          aria-label={UI_MISC.MOVE_OUT_GROUP}
        >
          <X className="h-3.5 w-3.5" strokeWidth={1.25} aria-hidden="true" />
        </button>
      )}
    </div>
  )
}

// ── 辅助函数 ──

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
      // 后端可能返回 null（TS 类型声明为 string 但运行时未必），用 ?? 兜底
      // 避免 toLowerCase() 抛错导致整个联系人页白屏。
      c.agentType.toLowerCase().includes(q) ||
      (c.title ?? '').toLowerCase().includes(q) ||
      (c.agentName ?? '').toLowerCase().includes(q),
  )
}
