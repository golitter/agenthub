# 2026-06-03 Backend 删除 pinned announcement 通知 agentend

**类型**: backend + agentend（新增内部通信端点，无跨端契约变更）

## 变更说明

Backend 删除 pinned announcement 时，新增异步通知 agentend 机制，使 Orchestrator 感知约束取消事件。

### 变更内容

1. **Agentend**: 新增 `POST /v1/pin/announcement-unpin` 端点，接收 `{shared_dir, content, sender_name}`，写入 unpin SystemMessage 到 ConversationMemoryStore
2. **Backend**: `agentend_client` 新增 `NotifyAnnouncementUnpin` 方法
3. **Backend**: `DeleteAnnouncement` handler 先查公告再删，若 `pinned=true` 则异步调 agentend 写入 unpin 事件

### 数据流

```
前端 DELETE /api/tasks/:taskId/announcements/:id
  → Backend DeleteAnnouncement
    ├── MySQL DELETE announcement
    └── (if pinned) goroutine → agentend POST /v1/pin/announcement-unpin
         └── ConversationMemoryStore.save_messages([SystemMessage("[公告约束已取消] ...")])
```

## 影响范围

- **Agentend**: 新增端点，不影响现有 API
- **Backend**: handler 构造函数签名变更（需传 agentClient），内部逻辑增强
- **Frontend**: 无影响
- **contracts/schemas/**: 无变更
