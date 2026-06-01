# 消息列表 Cursor 分页 — 后端实现

## 实现了什么

`GET /api/tasks/:taskId/messages` 接口支持 **cursor 分页** 和 **session_id 过滤**，前端通过自增 ID 向前翻页加载历史消息，可选按会话过滤。

## 怎么实现的

### 响应结构 (`internal/handler/message.go`)

```go
type ListMessagesResponse struct {
    Data    []model.Message `json:"data"`
    HasMore bool            `json:"has_more"`
}
```

### Handler 实现

```go
func (h *MessageHandler) ListMessages(c *gin.Context) {
    taskID := c.Param("taskId")

    // 1. 验证 task 存在
    var task model.Task
    if err := db.GetDB().Where("task_id = ?", taskID).First(&task).Error; err != nil {
        vo.NotFound(c, "task not found")
        return
    }

    limitStr := c.Query("limit")
    beforeStr := c.Query("before")
    sessionID := c.Query("session_id")

    // 2. 无分页参数：返回全部消息，has_more=false
    if limitStr == "" && beforeStr == "" {
        query := db.GetDB().Where("task_id = ?", taskID)
        if sessionID != "" {
            query = query.Where("session_id = ?", sessionID)
        }
        var messages []model.Message
        query.Order("created_at ASC").Order("id ASC").Find(&messages)
        vo.OK(c, ListMessagesResponse{Data: messages, HasMore: false})
        return
    }

    // 3. 有分页参数
    limit := 20
    if limitStr != "" {
        if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
            limit = l
        }
    }

    query := db.GetDB().Where("task_id = ?", taskID)
    if sessionID != "" {
        query = query.Where("session_id = ?", sessionID)
    }

    if beforeStr != "" {
        if beforeID, err := strconv.ParseUint(beforeStr, 10, 64); err == nil {
            query = query.Where("id < ?", beforeID)
        }
    }

    // 4. 多查一条判断 has_more
    var messages []model.Message
    query.Order("id DESC").Limit(limit + 1).Find(&messages)

    hasMore := len(messages) > limit
    if hasMore {
        messages = messages[:limit]
    }
    reverseMessages(messages)

    vo.OK(c, ListMessagesResponse{Data: messages, HasMore: hasMore})
}
```

### 分页策略

| 参数 | 行为 |
|------|------|
| 无 `limit`/`before` | 返回全部消息，`has_more=false` |
| `session_id=xxx` | 按 session 过滤（可与分页组合使用） |
| `limit=20` | 返回最近 20 条，多查一条判断 `has_more` |
| `limit=20&before=100` | 返回 `id < 100` 的最近 20 条 |
| `before=100` | 默认 `limit=20`，返回 `id < 100` 的最近 20 条 |

Cursor 使用自增主键 `id`（非 `message_id`），保证时间有序且唯一。查询按 `id DESC` 排序（新 -> 旧），取出后通过 `reverseMessages` 原地反转为时间升序（旧 -> 新），前端接收后直接 prepend 到数组头部。

### 路由注册 (`cmd/server/main.go`)

```
GET /api/tasks/:taskId/messages        -> MessageHandler.ListMessages
GET /api/tasks/:taskId/messages/window -> MessageHandler.WindowMessages
```
