# 设计文档：通讯录 + 置顶会话 + 退出群聊

> 状态: 📋 设计完成，待实现
> 日期: 2026-06-03
> Phase: Phase 7 交付打磨

## 背景

当前 IM 系统的会话列表（ConversationList）平铺展示所有会话，缺乏分组管理和排序控制。RightSidebar 中的"置顶会话"和"退出群聊"按钮均为 TODO 状态。IconSidebar 中"通讯录"按钮已存在但 disabled。需要实现三个功能来补全 IM 体验。

**核心约束**：单人项目，务实优先，不过度设计。尽量复用已有 API 和组件。

---

## Phase 1：置顶会话（约 0.5 天）

完全复用已有的 `updateTaskPin` API 和 `Task.pinned_at` 字段。

### 1.1 Backend — ListTasks 排序调整

**文件**: `backend/internal/service/impl/task_service.go` → `ListTasks`

修改排序为 `pinned_at DESC NULLS LAST, created_at DESC`，让置顶会话排到前面：

```go
db.GetDB().Order("pinned_at DESC NULLS LAST").Order("created_at DESC").Find(&tasks)
```

### 1.2 Frontend — 接口和排序

**文件**: `frontend/src/lib/api.ts`

- `Task` 接口添加 `pinned_at?: string | null`
- `Conversation` 接口添加 `pinnedAt?: string | null`
- `fetchConversations` 中映射 `pinnedAt: detail.task.pinned_at || undefined`
- 在排序逻辑中：置顶优先（按 pinnedAt desc），然后按 lastActiveAt desc

### 1.3 Frontend — ConversationItem 置顶图标

**文件**: `frontend/src/components/im/ConversationItem.tsx`

- 导入 `Pin` from lucide-react
- 名称旁添加置顶小图标（`pinnedAt` 存在时显示）

### 1.4 Frontend — RightSidebar 置顶按钮接入

**文件**: `frontend/src/components/chat/RightSidebar.tsx`

- Props 新增 `pinnedAt?: string | null`
- 置顶按钮 onClick：调用 `updateTaskPin(taskId, isPinned ? null : new Date().toISOString())`，然后 `invalidateQueries(['conversations'])`
- 按钮文案动态显示"置顶会话" / "取消置顶"

### 1.5 Frontend — ImPage 传递 pinnedAt

**文件**: `frontend/src/pages/ImPage.tsx`

将当前会话的 `pinnedAt` 传递给 RightSidebar。

---

## Phase 2：退出群聊 — 彻底删除（约 1 天）

### 2.1 AgentEnd — 暴露 task 级清理 API

**文件**: `agentend/src/api/v1/workspace.py`

新增端点 `DELETE /v1/workspace/task/{task_id}`，封装已有的 `WorkspaceManager.cleanup_by_task()`：

```python
@router.delete("/task/{task_id}")
async def cleanup_task(task_id: str, mgr=Depends(get_workspace_manager)):
    count = await mgr.cleanup_by_task(task_id)
    return {"cleaned": count}
```

该方法已处理：删除所有 agent worktree → 删除 task-base worktree → 删除 `task/{id}` 和 `agent/{sid}/{tid}` 分支。

### 2.2 Backend — AgentEnd Client 新增方法

**文件**: `backend/pkg/agentend_client/client.go`

新增两个方法（best-effort，失败不阻断）：

- `DestroySession(sessionID)` → `DELETE /v1/session/{id}`
- `CleanupByTask(taskID)` → `DELETE /v1/workspace/task/{id}`

### 2.3 Backend — LeaveTask Handler

**文件**: `backend/internal/controller/impl/task_controller.go`

新增 `LeaveTask` 方法 `DELETE /api/tasks/:taskId/leave`，按序执行：

1. 查询 task 下所有 sessionID
2. 逐一调用 AgentEnd `DestroySession`（终止进程，best-effort）
3. 调用 AgentEnd `CleanupByTask`（清理 worktree + 分支，best-effort）
4. 数据库事务删除：Task → Session → SessionAgent → Message → Announcement → DiffSnapshot → ContactGroupItem

> 注意：即使 AgentEnd 调用失败也继续数据库删除，因为 worktree 清理有后台兜底。

### 2.4 Backend — 路由注册

**文件**: `backend/cmd/server/main.go`

```go
api.DELETE("/tasks/:taskId/leave", taskHandler.LeaveTask)
```

### 2.5 Frontend — API + UI 接入

**文件**: `frontend/src/lib/api.ts`

新增 `leaveTask(taskId)` → `DELETE /tasks/{id}/leave`

**文件**: `frontend/src/components/chat/RightSidebar.tsx`

退出群聊按钮 onClick：
1. `confirm('确认退出群聊？退出后将彻底删除所有消息和工作区数据。')` 二次确认
2. 调用 `leaveTask(taskId)`
3. `clearNavigation()` 清除当前选中
4. `invalidateQueries(['conversations'])` 刷新列表

---

## Phase 3：通讯录（约 1.5 天）

### 3.1 Backend — 数据模型

**新文件**: `backend/internal/model/contact_group.go`

```go
type ContactGroup struct {
    ID        uint      `gorm:"primarykey" json:"id"`
    GroupID   string    `gorm:"uniqueIndex;size:36" json:"group_id"`
    Name      string    `gorm:"size:128;not null" json:"name"`
    SortOrder int       `gorm:"default:0" json:"sort_order"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type ContactGroupItem struct {
    ID        uint      `gorm:"primarykey" json:"id"`
    GroupID   string    `gorm:"index;size:36;not null" json:"group_id"`
    TaskID    string    `gorm:"index;size:36;not null" json:"task_id"`
    SortOrder int       `gorm:"default:0" json:"sort_order"`
    CreatedAt time.Time `json:"created_at"`
}
```

单用户系统，无需 user_id。未出现在 contact_group_item 中的 task 隐式属于"未分组"。

### 3.2 Backend — AutoMigrate

**文件**: `backend/cmd/server/main.go`

AutoMigrate 中追加 `&model.ContactGroup{}, &model.ContactGroupItem{}`

### 3.3 Backend — ContactGroup Handler

**文件**: `backend/internal/controller/impl/contact_group_controller.go`

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/contact-groups` | 返回所有分组 + 每组的 task 列表 + 未分组 task IDs |
| POST | `/api/contact-groups` | 创建分组 `{name}` |
| PUT | `/api/contact-groups/:groupId` | 重命名 `{name}` |
| DELETE | `/api/contact-groups/:groupId` | 删除分组（项移至未分组） |
| POST | `/api/contact-groups/:groupId/items` | 添加 task 到分组 `{task_id}` |
| DELETE | `/api/contact-groups/:groupId/items/:taskID` | 从分组移除 task |

### 3.4 Backend — 路由注册

**文件**: `backend/cmd/server/main.go`

```go
cgHandler := handler.NewContactGroupHandler()
api.GET("/contact-groups", cgHandler.ListGroups)
api.POST("/contact-groups", cgHandler.CreateGroup)
api.PUT("/contact-groups/:groupId", cgHandler.UpdateGroup)
api.DELETE("/contact-groups/:groupId", cgHandler.DeleteGroup)
api.POST("/contact-groups/:groupId/items", cgHandler.AddItem)
api.DELETE("/contact-groups/:groupId/items/:taskID", cgHandler.RemoveItem)
```

### 3.5 Frontend — API 层

**文件**: `frontend/src/lib/api.ts`

新增 ContactGroup 接口和 6 个 API 函数（fetchContactGroups, createContactGroup, updateContactGroup, deleteContactGroup, addToContactGroup, removeFromContactGroup）

### 3.6 Frontend — Hooks

**新文件**: `frontend/src/hooks/use-contact-groups.ts`

useContactGroups (query) + useCreateContactGroup / useDeleteContactGroup / useAddToContactGroup / useRemoveFromContactGroup (mutations)

### 3.7 Frontend — ContactsPage 组件

**新文件**: `frontend/src/components/im/ContactsPage.tsx`

布局：

```
ContactsPage
├── SearchBar（复用 ConversationList 的搜索栏模式）
├── UngroupedSection（未分组的会话卡片列表）
├── GroupSection[0..N]（自定义分组，可折叠）
│   ├── GroupHeader（名称 + 编辑/删除/添加成员操作）
│   └── ContactCard[]（会话卡片）
└── NewGroupButton（新建分组）
```

ContactCard：展示头像 + 名称 + 最后活跃时间，点击进入会话，右键/下拉菜单分配到分组。

### 3.8 Frontend — 启用通讯录 Tab

**文件**: `frontend/src/components/layout/IconSidebar.tsx`

移除 contacts NavItem 的 `disabled` 属性。

**文件**: `frontend/src/pages/ImPage.tsx`

将 contacts 的 PlaceholderPage 替换为 `<ContactsPage />`。

---

## 契约层

这三个功能不需要修改 `contracts/schemas/*.yaml`：
- pinned_at 已存在于 Task 模型
- 通讯录分组纯 Backend↔Frontend，不涉及 AgentEnd
- 退出群聊复用已有 AgentEnd 端点

在 `contracts/logs/` 添加变更记录即可。

---

## 关键文件索引

| 端 | 文件 | 变更类型 |
|----|------|----------|
| Backend | `cmd/server/main.go` | 路由注册 + AutoMigrate |
| Backend | `internal/controller/impl/task_controller.go` | ListTasks 排序 + LeaveTask |
| Backend | `internal/controller/impl/contact_group_controller.go` | ContactGroup CRUD |
| Backend | `internal/model/contact_group.go` | **新增** |
| Backend | `pkg/agentend_client/client.go` | DestroySession + CleanupByTask |
| AgentEnd | `src/api/v1/workspace.py` | DELETE /task/{id} 端点 |
| Frontend | `src/lib/api.ts` | 接口 + API 函数 |
| Frontend | `src/hooks/use-contact-groups.ts` | **新增** |
| Frontend | `src/components/im/ContactsPage.tsx` | **新增** |
| Frontend | `src/components/im/ConversationItem.tsx` | 置顶图标 |
| Frontend | `src/components/chat/RightSidebar.tsx` | 置顶 + 退群按钮接入 |
| Frontend | `src/components/layout/IconSidebar.tsx` | 启用通讯录 |
| Frontend | `src/pages/ImPage.tsx` | 路由 + Props 传递 |

---

## 验证方式

### Phase 1 验证
1. 创建 3-4 个会话，侧栏点击"置顶会话" → 会话列表中置顶的排到最前，带 Pin 图标
2. 再次点击"取消置顶" → 恢复原位

### Phase 2 验证
1. 创建群聊会话，发送几条消息
2. `make status` 确认 agentend 有活跃 session 和 worktree
3. 点击"退出群聊" → 确认对话框 → 会话从列表消失
4. 检查：数据库 task/session/message 已清空，agentend 无残留 worktree/分支（`git worktree list`）

### Phase 3 验证
1. 点击左侧"通讯录"图标 → 进入通讯录页面
2. 创建分组"工作组"，将 2 个会话分配进去
3. 验证未分组区域只显示剩余会话
4. 删除分组 → 会话回到未分组区域
5. 点击通讯录中的会话卡片 → 跳转到对应聊天
