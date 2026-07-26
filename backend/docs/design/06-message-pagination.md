# 消息列表 Cursor 分页 — 后端实现

## 实现了什么

`GET /api/tasks/:taskId/messages` 接口支持 **cursor 分页**、**session_id 过滤** 和 **mode 可见性控制**，前端通过自增 ID 向前翻页加载历史消息，可选按会话过滤和群聊可见性模式。

## 怎么实现的

### 三层架构

```
MessageController.ListMessages()
    │ 参数绑定（limit / before / session_id / mode）
    ▼
MessageService.ListMessages()
    │ 业务逻辑（分页策略、mode 可见性过滤）
    ▼
MessageDao.ListByTask()
    │ GORM 查询
    ▼
MySQL
```

### 响应结构 (`internal/service/service.go`)

```go
type ListMessagesResponse struct {
    Data    []model.Message `json:"data"`
    HasMore bool            `json:"has_more"`
}
```

### Service 接口 (`internal/service/service.go`)

```go
type MessageService interface {
    ListMessages(taskID, sessionID, mode, primarySessionID string, limit int, beforeID *uint64, paginated bool) (*ListMessagesResponse, error)
    WindowMessages(taskID, sessionID string) ([]map[string]interface{}, error)
}
```

### Controller 实现 (`internal/controller/impl/message_controller.go`)

Controller 仅做参数绑定和 Service 调用：

```go
func (ctrl *MessageController) ListMessages(c *gin.Context) {
    taskID := c.Param("taskId")
    sessionID := c.Query("session_id")
    mode := c.Query("mode")
    primarySessionID := c.Query("primary_session_id")

    limitStr := c.Query("limit")
    beforeStr := c.Query("before")

    limit := 20
    paginated := limitStr != "" || beforeStr != ""
    if limitStr != "" {
        if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
            limit = l
        }
    }
    var beforeID *uint64
    if beforeStr != "" {
        if id, err := strconv.ParseUint(beforeStr, 10, 64); err == nil {
            beforeID = &id
        }
    }

    result, err := ctrl.service.ListMessages(taskID, sessionID, mode, primarySessionID, limit, beforeID, paginated)
    if err != nil {
        handleBizError(c, err)
        return
    }
    vo.OK(c, result)
}
```

### DAO 接口 (`internal/dao/dao.go`)

```go
type MessageDao interface {
    ListByTask(taskID, sessionID, mode, primarySessionID string, limit int, beforeID *uint64) ([]model.Message, error)
    // ...
}
```

### 分页策略

| 参数 | 行为 |
|------|------|
| 无 `limit`/`before` | 返回全部消息，`has_more=false` |
| `session_id=xxx` | 按 session 过滤（可与分页组合使用） |
| `mode=group` | 启用群聊可见性过滤；其他非空 mode 会返回 400 |
| `primary_session_id=xxx` | 主 Session ID（群聊 mode 下用于确定可见性范围） |
| `limit=20` | 返回最近 20 条，多查一条判断 `has_more` |
| `limit=20&before=100` | 返回 `id < 100` 的最近 20 条 |
| `before=100` | 默认 `limit=20`，返回 `id < 100` 的最近 20 条 |

Cursor 使用自增主键 `id`（非 `message_id`），保证时间有序且唯一。查询按 `id DESC` 排序（新 → 旧），取出后反转（旧 → 新），前端接收后直接 prepend 到数组头部。

### 路由注册

```
GET /api/tasks/:taskId/messages        -> MessageController.ListMessages
GET /api/tasks/:taskId/messages/window -> MessageController.WindowMessages
```

每个 Controller 通过 `RegisterRoutes(rg *gin.RouterGroup)` 自注册路由。

### 群聊窗口消息

`WindowMessages` 查询同一 Task 下其他 Session 的消息，每条截断至 2000 字符，去重后返回。供跨 Agent 上下文供给使用。

- Service: `MessageService.WindowMessages(taskID, sessionID)`
- DAO: `MessageDao.ListGroupChatWindowMessages(taskID, sessionID, afterCreatedAt)`
