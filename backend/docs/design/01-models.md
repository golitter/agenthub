# Models — 数据模型

## 实现了什么

使用 GORM 定义了七个核心数据模型（Task、Session、Message、DiffSnapshot、SessionAgent、AdminSetting、Announcement），构成 Task 1:N Session、Session 1:N Message 的层级关系，支撑多 Agent 会话管理、Diff 快照持久化、Agent 关联存储、管理面板配置和任务公告系统。

## 怎么实现的

### Task — 顶层任务实体 (`internal/model/task.go`)

Task 是顶层实体，代表一个项目任务。`task_id` 为 UUID，供 AgentEnd 决定 git branch 和 worktree 隔离。

```go
type Task struct {
	ID        uint       `gorm:"primarykey" json:"id"`
	TaskID    string     `gorm:"uniqueIndex;size:36" json:"task_id"`
	Title     string     `gorm:"size:255" json:"title"`
	RepoPath  string     `gorm:"size:512" json:"repo_path"`
	Status    string     `gorm:"size:32;default:active" json:"status"`
	PinnedAt  *time.Time `gorm:"" json:"pinned_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
```

- `TaskID`：后端通过 `google/uuid` v4 生成，唯一索引
- `RepoPath`：仓库路径，运行时注入 AgentRequest
- `Status`：默认 `"active"`
- `PinnedAt`：置顶时间戳，nil 表示未置顶，通过 `PATCH /api/tasks/:taskId` 更新

### Session — Agent 会话 (`internal/model/session.go`)

Session 从属于 Task，代表一个 Agent 的会话。`session_id` 由调用方传入，与 `task_id` 组合映射到 AgentEnd 的 `cli_session_id`。

```go
type Session struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	SessionID   string    `gorm:"uniqueIndex;size:128" json:"session_id"`
	TaskID      string    `gorm:"index;size:36" json:"task_id"`
	AgentType   string    `gorm:"size:64" json:"agent_type"`
	AgentName   string    `gorm:"size:128" json:"agent_name"`
	AvatarURL   string    `gorm:"size:512" json:"avatar_url,omitempty"`
	Status      string    `gorm:"size:32;default:running" json:"status"`
	SettledDiff string    `gorm:"type:longtext" json:"settled_diff,omitempty"`
	DiffStatus  string    `gorm:"size:32" json:"diff_status,omitempty"`
	SoulMD      string    `gorm:"size:300" json:"soul_md,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
```

- `TaskID`：索引字段，关联 Task
- `AgentType`：Agent 类型（claude-code / opencode / orchestrator / codex）
- `AgentName` / `AvatarURL`：Agent 的显示名称和头像，通过 `PUT /api/sessions/:sessionId` 更新
- `Status`：`active` -> `running` -> `completed` / `failed` / `inactive` / `awaiting_review`
- `SettledDiff` / `DiffStatus`：工作区 Diff 结算信息
- `SoulMD`：Agent 灵魂描述（最多 300 字符），通过 `PUT /api/sessions/:sessionId/soul` 更新

### Message — 消息记录 (`internal/model/message.go`)

Message 从属于 Session，记录用户和 Agent 的每条消息。流式场景下 `status` 为 `streaming`，结束后变为 `completed` 或 `failed`。

```go
type Message struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	MessageID string    `gorm:"uniqueIndex;size:36" json:"message_id"`
	TaskID    string    `gorm:"index;size:36" json:"task_id"`
	SessionID string    `gorm:"size:128" json:"session_id"`
	Role      string    `gorm:"size:16" json:"role"`
	Content   string    `gorm:"type:longtext" json:"content"`
	Status    string    `gorm:"size:16;default:completed" json:"status"`
	LastSeq   string    `gorm:"size:64;default:''" json:"last_seq"`
	AgentType string    `gorm:"size:64" json:"agent_type,omitempty"`
	AgentName string    `gorm:"size:128" json:"agent_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
```

- `MessageID`：UUID，唯一索引
- `Role`：`"user"` 或 `"agent"`
- `Content`：`longtext` 类型，Agent 消息由 StreamWriter 批量刷写
- `LastSeq`：Redis Stream 的最后消费位置，用于断线重连时从 MySQL 历史恢复后跳过已消费事件
- `Status`：`streaming`（流式中） / `completed` / `failed`

### DiffSnapshot — Diff 快照 (`internal/model/diff_snapshot.go`)

DiffSnapshot 记录工作区文件变更的快照，由前端 DiffCard 持久化。同一 session 的 pending 快照自动取消。

```go
type DiffSnapshot struct {
    ID          uint      `gorm:"primarykey" json:"id"`
    SnapshotID  string    `gorm:"uniqueIndex;size:36" json:"snapshot_id"`
    SessionID   string    `gorm:"index;size:128" json:"session_id"`
    DiffContent string    `gorm:"type:longtext" json:"diff_content"`
    Status      string    `gorm:"size:16;default:pending" json:"status"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

- `SnapshotID`：UUID，前端生成的唯一标识
- `SessionID`：关联的会话
- `DiffContent`：unified diff 文本（longtext）
- `Status`：`pending` → `committed` / `reverted` / `cancelled`（终态不可变）

### SessionAgent — 会话 Agent 关联 (`internal/model/session_agent.go`)

SessionAgent 将 Agent 信息从 Session 中拆出独立存储，支持同一会话关联多个 Agent。

```go
type SessionAgent struct {
    ID        uint      `gorm:"primarykey" json:"id"`
    SessionID string    `gorm:"uniqueIndex;size:128" json:"session_id"`
    AgentType string    `gorm:"size:64" json:"agent_type"`
    AgentName string    `gorm:"size:128" json:"agent_name"`
    AvatarURL string    `gorm:"size:512" json:"avatar_url,omitempty"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}
```

### AdminSetting — 管理面板配置 (`internal/model/admin_setting.go`)

键值对存储管理面板的持久化配置（如管理员头像 URL）：

```go
type AdminSetting struct {
    Key   string `gorm:"primaryKey;size:64" json:"key"`
    Value string `gorm:"size:1024" json:"value"`
}
```

### Announcement — 任务公告 (`internal/model/announcement.go`)

Announcement 记录任务级别的公告消息，支持置顶排序。

```go
type Announcement struct {
    ID         uint      `gorm:"primarykey" json:"id"`
    TaskID     string    `gorm:"index;size:36;not null" json:"task_id"`
    SenderID   string    `gorm:"size:64;not null" json:"sender_id"`
    SenderName string    `gorm:"size:64;not null" json:"sender_name"`
    Content    string    `gorm:"type:text;not null" json:"content"`
    Pinned     bool      `gorm:"default:false" json:"pinned"`
    CreatedAt  time.Time `json:"created_at"`
}
```

- `TaskID`：所属任务
- `SenderID` / `SenderName`：发送者标识
- `Pinned`：是否置顶，列表查询时置顶公告优先排列

### 实体关系

```
Task 1:N Session 1:N Message
  │         │          │
  ├─ task_id ◄─────────┤ (FK)
  │    (uniqueIndex)   │
  │         │          │
  │    session_id ◄────┤ (FK)
  │    (uniqueIndex)   │
  │                    │
  └────────────────────┘  通过 task_id / session_id 字段关联，
                          未使用 GORM 外键约束（软关联）

Session 1:N SessionAgent (session_id 关联)
Session 1:N DiffSnapshot (session_id 关联)
Task 1:N Announcement (task_id 关联)

AdminSetting（独立 KV 存储，无外键关联）
```
