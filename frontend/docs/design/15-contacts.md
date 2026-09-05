# Contacts — 通讯录与联系人分组

## 实现了什么

`/contacts` 路由对应的通讯录页面（`ContactsPage`）：按「置顶 / 自定义分组 / 未分组」三区浏览全部会话，支持创建、删除分组，把会话（按 taskId）移入/移出分组，点击联系人跳转对应聊天。左侧为联系人列表（移动端全宽，桌面端 420px 固定宽），右侧为品牌信息面板（`md` 以上显示，含 Logo、项目描述、特性标签与 GitHub 链接）。

## 怎么实现的

### 页面组件 (`src/components/im/ContactsPage.tsx`)

数据源为两个 React Query 查询 + 四个 mutation（全部来自 `use-contact-groups.ts`），会话列表复用 `useConversations()`：

```tsx
export function ContactsPage() {
  const { data: conversations } = useConversations()
  const { data: groupsData } = useContactGroups()
  const createGroup = useCreateContactGroup()
  const deleteGroup = useDeleteContactGroup()
  const addItem = useAddToContactGroup()
  const removeItem = useRemoveFromContactGroup()
  const { setCurrentSession } = useChatNav()

  const groups = groupsData?.groups ?? []
  const convMap = buildConvMap(conversations ?? [])   // taskId → Conversation

  // 将会话分为置顶和非置顶
  const pinnedConvs = conversations?.filter((c) => c.pinnedAt) ?? []
  const groupedTaskIds = new Set(groups.flatMap((group) => group.items.map((item) => item.task_id)))
  const ungroupedConvs = (conversations ?? []).filter(
    (conversation) => !conversation.pinnedAt && !groupedTaskIds.has(conversation.taskId),
  )
  // ...
}
```

三区划分规则：

- **置顶区**：`conversation.pinnedAt` 存在的会话（与 `ConversationItem` 的置顶图钉同一数据源，置顶通过聊天侧的 `updateTaskPin` API 维护）。
- **自定义分组区**：`groups` 顺序渲染；每组的 `items` 仅存 `task_id`，通过 `convMap`（taskId → Conversation）联表得到完整会话。分组可折叠（`aria-expanded` + `aria-controls`，`ChevronRight` 旋转指示；搜索时强制展开）；删除走内联确认条（`UI_CONFIRMS.DELETE_GROUP_INLINE`），而非 Dialog。
- **未分组区**：既未置顶也不在任何分组 `groupedTaskIds` 中的会话。

点击联系人 `openChat`：先 `setCurrentSession(sessionId)` 写入导航 store，再 `navigate('/chat?session=<id>')` 交由 `ChatContent` 的 URL 恢复逻辑接手。

### ContactCard（页内子组件）

单条联系人卡片，左侧可点击区域（头像 + 名称 + 描述行）与右侧操作控件分离，避免点击操作误触导航：

- 头像复用 `AgentAvatar` / `GroupAvatar`（群聊透传 `groupSessions`），状态经 `toAgentDisplayStatus()` 投影为四态显示模型。
- 未分组卡片带「移动到分组」`<select>`（选项来自 `groups`，提交后重置回占位项）；组内卡片带移除按钮（`onRemove`）。两种操作在 `addItem.isPending || removeItem.isPending` 期间禁用（`busy`）。

搜索 `filterConvs` 在 `agentType` / `title` / `agentName` 上做包含匹配，`title` / `agentName` 用 `?? ''` 兜底后端 null，避免 `toLowerCase()` 抛错导致整页白屏。

### 数据 Hooks (`src/hooks/use-contact-groups.ts`)

查询与 mutation 以 `['contact-groups']` 为共享 queryKey，所有 mutation 成功后统一 invalidate：

```typescript
export function useContactGroups() {
  return useQuery({
    queryKey: ['contact-groups'],
    queryFn: fetchContactGroups,
  })
}

export function useAddToContactGroup() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ groupId, taskId }: { groupId: string; taskId: string }) =>
      addToContactGroup(groupId, taskId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['contact-groups'] })
    },
  })
}
```

（`useCreateContactGroup` / `useUpdateContactGroup` / `useDeleteContactGroup` / `useRemoveFromContactGroup` 同构。）

### API 层 (`src/lib/api.ts`)

分组以 task 为成员单位（`items: { task_id; sort_order }[]`）：

```typescript
export interface ContactGroup {
  group_id: string
  name: string
  sort_order: number
  items: { task_id: string; sort_order: number }[]
}

export interface ContactGroupsResponse {
  groups: ContactGroup[]
  ungrouped_task_ids: string[]
}
```

| 函数 | 方法 | 路径 | 说明 |
|------|------|------|------|
| `fetchContactGroups` | GET | `/api/contact-groups` | 分组列表（含 items） |
| `createContactGroup` | POST | `/api/contact-groups` | 创建分组 |
| `updateContactGroup` | PUT | `/api/contact-groups/:id` | 重命名分组 |
| `deleteContactGroup` | DELETE | `/api/contact-groups/:id` | 删除分组 |
| `addToContactGroup` | POST | `/api/contact-groups/:id/items` | 添加 task 到分组 |
| `removeFromContactGroup` | DELETE | `/api/contact-groups/:id/items/:taskID` | 从分组移除 task |

### 状态与错误处理

- 页面级加载态：两个查询任一 loading 时显示 7 行骨架卡（`skeleton-sheen`）覆盖层。
- 两个查询分别渲染可重试错误条（`refetchConversations()` / `refetchGroups()`）；分组操作的失败统一写入页面级 `actionError`（`role="alert"`），展示 `UI_ERRORS.CREATE_GROUP_FAILED / DELETE_GROUP_FAILED / MOVE_GROUP_FAILED`。
- 新建分组为内联输入（`Enter` 提交、`isComposing` 防误发），空名或 pending 时禁用确认按钮。
