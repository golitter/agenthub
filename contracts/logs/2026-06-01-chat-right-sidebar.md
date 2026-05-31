# 2026-06-01-chat-right-sidebar

## 变更原因

群聊右侧栏增强：新增群公告、群成员列表、历史消息搜索。公告需要后端 CRUD API，会话置顶需要 `pinned_at` 字段。

## 变更文件

无 schema 文件修改。本次变更为纯 REST API 新增，不涉及 SSE 事件契约。

## 新增后端 API

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/tasks/:taskId/announcements` | GET | 获取公告列表（置顶优先 + 时间倒序） |
| `/api/tasks/:taskId/announcements` | POST | 创建公告 |
| `/api/tasks/:taskId/announcements/:id` | DELETE | 删除公告 |
| `/api/tasks/:taskId` | PATCH | 更新 task 元数据（pinned_at） |

## 新增数据模型

### `task_announcements` 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGINT PK | 自增主键 |
| task_id | VARCHAR(36) | 关联 task |
| sender_id | VARCHAR(64) | 发布者 ID |
| sender_name | VARCHAR(64) | 发布者名称 |
| content | TEXT | 公告内容 |
| pinned | BOOLEAN | 是否置顶 |
| created_at | DATETIME | 创建时间 |

### `tasks` 表新增字段

| 字段 | 类型 | 说明 |
|------|------|------|
| pinned_at | DATETIME NULL | 置顶时间，NULL = 未置顶 |

## 跨端影响

- **Frontend**: 新增 4 个组件（RightSidebar, HistorySearch, AnnouncementsSection, MembersSection）+ api.ts 函数 + chat store 扩展
- **Backend**: 新增 Announcement model/handler + TaskHandler.PatchTask
- **AgentEnd**: 无影响
- **Contracts**: 无 SSE schema 变更，公告走 REST API
