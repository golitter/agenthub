# Phase 5a: 聊天右侧栏增强（群公告 + 群成员 + 历史搜索）

> 目标: 在聊天区右侧新增常驻侧边栏，展示群公告、群成员列表、历史消息搜索。
> 预估: 3-4 天
> 视觉参考: [chat-enhanced-demo.html](chat-enhanced-demo.html)

## 当前状态

聊天区为三栏布局：`IconSidebar(52px)` → `ConversationList(260px)` → `ChatArea(flex-1)`。

ChatArea 已支持群聊（`isGroupChat` + `groupAgentTypes`/`groupAgentNames`），但缺少群信息展示、公告、成员列表和消息搜索。

## 布局变更

```
IconSidebar(52px) → ConvList(260px) → ChatArea(flex-1) → RightSidebar(280px)
```

新增第四栏 `RightSidebar`，仅群聊会话时显示。单聊（1v1 Agent）不显示右侧栏。

## 右侧栏结构（从上到下）

```
┌──────────────────────┐
│ 🔍 搜索历史消息       │  ← 搜索输入框，结果下拉展示
├──────────────────────┤
│ 群公告 (3)        ▼  │  ← 可折叠
│ ┌──────────────────┐ │
│ │ 📌 置顶           │ │
│ │ 田乐檬 · 05-28   │ │
│ │ 认证系统重构任务.. │ │
│ └──────────────────┘ │
│ ┌──────────────────┐ │
│ │ OC · 05-29       │ │
│ │ Phase 1 已完成..  │ │
│ └──────────────────┘ │
│ + 发布新公告          │
├──────────────────────┤
│ 群成员 (5)        ▼  │  ← 可折叠
│ t  Owner        ●    │
│ OC Admin        ●    │
│ CC              ●    │
│ OD              ●    │
│ CX              ○    │
├──────────────────────┤
│ 📥 导出聊天记录       │
│ 📌 置顶会话           │
│ ✕ 退出群聊            │
└──────────────────────┘
```

### 1. 历史消息搜索

**交互**:
- 输入框输入关键词，输入 ≥1 字符后展示下拉结果
- 结果按时间倒序，高亮匹配关键词
- 每条结果显示：Agent 头像 + 名称 + 时间 + 匹配文本摘要
- 点击结果跳转到对应消息位置（滚动到消息并高亮闪烁）
- 失焦或清空时关闭下拉

**实现**: 前端过滤 `chatStore` 中已加载的消息，不需要后端 API。如果后续消息量大（>1000 条），再考虑后端搜索接口。

### 2. 群公告

**交互**:
- 点击标题栏折叠/展开
- 置顶公告带 `📌 置顶` badge
- 每条公告：发布者头像 + 名称 + 内容 + 时间
- 底部「+ 发布新公告」按钮（Owner/Admin 可见）
- 点击公告卡片展开完整内容

**数据来源**: 公告本质是 session 级别的 system message。后端新增 `announcements` 字段，挂在 session 上。

### 3. 群成员

**交互**:
- 点击标题栏折叠/展开
- 每行：头像 + 名称 + 角色 + 在线状态圆点
- 角色区分：Owner（创建者）、Admin（Orchestrator）、Member（Subagent）
- 在线状态：通过 SSE 连接状态判断（有活跃 stream = online）
- 点击成员行可跳转到该成员的独立 1v1 会话

**数据来源**: 直接从 `ChatArea` 现有 props 提取：
- `groupAgentTypes` / `groupAgentNames` → 成员列表
- `groupSessions` → 各成员 session ID（用于跳转）

### 4. 更多操作

- **导出聊天记录**: 将当前会话所有消息导出为 Markdown 文件
- **置顶会话**: 会话列表中置顶显示（`pinned_at` 字段）
- **退出群聊**: 离开群组会话

## 三端改动

### Backend — 新增 API

```
GET  /api/tasks/{taskId}/announcements          # 获取群公告列表
POST /api/tasks/{taskId}/announcements           # 发布群公告
DELETE /api/tasks/{taskId}/announcements/{id}    # 删除公告
PATCH /api/tasks/{taskId}                        # 更新 task 元数据（pinned_at 等）
```

**数据模型** — 新增 `task_announcements` 表：

```sql
CREATE TABLE task_announcements (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  task_id    BIGINT NOT NULL,
  sender_id  VARCHAR(64) NOT NULL,    -- 发布者 session_id
  sender_name VARCHAR(64) NOT NULL,
  content    TEXT NOT NULL,
  pinned     BOOLEAN DEFAULT FALSE,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_task_id (task_id)
);
```

**task 表新增字段**:

```sql
ALTER TABLE tasks ADD COLUMN pinned_at DATETIME DEFAULT NULL;
```

### Frontend — 新增组件

```
frontend/src/components/chat/
├── RightSidebar.tsx              # 🆕 右侧栏容器
├── HistorySearch.tsx             # 🆕 历史消息搜索
├── AnnouncementsSection.tsx      # 🆕 群公告区块
├── MembersSection.tsx            # 🆕 群成员区块
└── ChatArea.tsx                  # ✏️ 集成 RightSidebar
```

**ImPage.tsx 布局变更**:
```tsx
<div className="flex flex-1 overflow-hidden">
  <ConversationList ... />
  <ChatArea ... />
  {isGroupChat && <RightSidebar ... />}   {/* 仅群聊显示 */}
</div>
```

**RightSidebar props**:
```typescript
interface RightSidebarProps {
  taskId: string;
  isGroupChat: boolean;
  agentTypes: string[];
  agentNames: string[];
  sessions: Array<{ sessionId: string; agentType: string; agentName: string }>;
}
```

**AnnouncementsSection**:
- `GET /api/tasks/{taskId}/announcements` 获取列表
- `POST` 发布（Owner/Admin 角色）
- 按时间倒序，置顶排最前
- 可折叠，状态持久化到 localStorage

**MembersSection**:
- 从 props 提取成员数据，无需额外 API
- 在线状态：根据各成员 session 的 SSE 连接状态推断
- 点击跳转：切换 `activeSessionId` 到对应成员 session

**HistorySearch**:
- 前端过滤 `useChatStore(state => state.sessions[sessionId].messages)`
- 匹配 `message.content` 和 `block.content`
- 结果高亮用 `<mark>` 标签
- 点击结果 → 滚动到消息 ID → 添加高亮闪烁动画（800ms 后消失）

### AgentEnd — 无改动

公告由用户或 Orchestrator 通过 Backend API 直接创建，不经过 Agent 端。

## 文件清单

```
Backend:
├── internal/model/announcement.go         # ✅ 公告数据模型
├── internal/controller/impl/announcement_controller.go  # ✅ 公告 CRUD controller
├── internal/service/impl/announcement_service.go        # ✅ 公告业务逻辑
├── internal/controller/impl/task_controller.go + internal/service/impl/task_service.go  # ✏️ 新增 pinned_at 更新
├── cmd/server/main.go                     # ✏️ 注册路由

Frontend:
├── src/components/chat/RightSidebar.tsx   # 🆕 右侧栏容器
├── src/components/chat/HistorySearch.tsx  # 🆕 历史搜索
├── src/components/chat/AnnouncementsSection.tsx  # 🆕 群公告
├── src/components/chat/MembersSection.tsx # 🆕 群成员
├── src/components/chat/ChatArea.tsx       # ✏️ 集成右侧栏
├── src/pages/ImPage.tsx                   # ✏️ 四栏布局
├── src/stores/chat.ts                     # ✏️ 公告状态 + 搜索辅助方法
├── src/lib/api.ts                         # ✏️ 公告 API + pin/unpin

Contracts:
└── (无改动 — 公告走 REST API，不走 SSE 事件)
```

## 验证流程

```bash
# 1. 启动三端
make run-backend && make run-agentend && make run-frontend

# 2. 创建群聊会话
# 预期: 右侧栏自动显示（单聊不显示）

# 3. 群公告
# - 发布新公告 → 列表实时更新
# - 置顶公告 → 显示 📌 badge，排在最前
# - 折叠/展开 → 状态持久化到 localStorage

# 4. 群成员
# - 显示所有成员 + 角色 + 在线状态
# - 点击成员 → 跳转到该成员的独立会话
# - 折叠/展开 → 状态持久化

# 5. 历史搜索
# - 输入关键词 → 下拉展示匹配结果
# - 结果高亮匹配文本
# - 点击结果 → 滚动到对应消息并闪烁高亮
# - 清空输入 → 关闭下拉

# 6. 更多操作
# - 置顶会话 → 会话列表中置顶显示
# - 导出 → 下载 Markdown 文件
# - 退出群聊 → 返回会话列表
```
