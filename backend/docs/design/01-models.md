# Models — 数据模型

## 实现了什么

使用 GORM 定义了十五个数据模型。其中十二个为核心业务模型（Task、Session、Message、DiffSnapshot、SessionAgent、AdminSetting、Announcement、ContactGroup、ContactGroupItem、SkillHub、AgentSkill、Artifact），构成 Task 1:N Session、Session 1:N Message 的层级关系，支撑多 Agent 会话管理、Diff 快照持久化、Agent 关联存储、管理面板配置、任务公告、联系人分组、技能仓库系统和 AgentEnd 内置资源（Artifact）托管；另三个（SkillUploadReceipt、SkillOperationJob、SkillAuditEvent）服务于技能对象存储（MinIO）迁移、补偿型后台任务和生命周期审计（见下文「技能存储迁移模型」）。

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

- `TaskID`：后端通过 `google/uuid` v4 生成，唯一索引；所有 API/service 入参校验上限与模型 `size:36` 对齐
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
	Status      string    `gorm:"size:32;default:idle" json:"status"`
	SettledDiff string    `gorm:"type:longtext" json:"settled_diff,omitempty"`
	DiffStatus  string    `gorm:"size:32" json:"diff_status,omitempty"`
	SoulMD      string    `gorm:"size:300" json:"soul_md,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
```

- `TaskID`：索引字段，关联 Task
- `AgentType`：Agent 类型（claude-code / opencode / orchestrator / codex / pi）
- `AgentName` / `AvatarURL`：Agent 的显示名称和头像，通过 `PUT /api/sessions/:sessionId` 更新
- `Status`：遵循 `contracts/schemas/session-state.yaml`，取值为 `idle` / `running` / `awaiting_review` / `completed` / `interrupted` / `error` / `inactive`
- `SettledDiff` / `DiffStatus`：工作区 Diff 结算信息
- `SoulMD`：Agent 灵魂描述（最多 300 字符），通过 `PUT /api/sessions/:sessionId/soul` 更新

### Message — 消息记录 (`internal/model/message.go`)

Message 从属于 Session，记录用户和 Agent 的每条消息。流式场景下 `status` 为 `streaming`，结束后变为 `completed` 或 `failed`。

```go
type Message struct {
	ID                uint      `gorm:"primarykey" json:"id"`
	MessageID         string    `gorm:"uniqueIndex;size:36" json:"message_id"`
	TaskID            string    `gorm:"index;size:36" json:"task_id"`
	SessionID         string    `gorm:"index:idx_session_id;index:idx_session_status,size:128" json:"session_id"`
	Role              string    `gorm:"size:16" json:"role"`
	Content           string    `gorm:"type:longtext" json:"content"`
	Status            string    `gorm:"size:16;default:completed;index:idx_session_status" json:"status"`
	LastSeq           string    `gorm:"size:64;default:''" json:"last_seq"`
	AgentType         string    `gorm:"size:64" json:"agent_type,omitempty"`
	AgentName         string    `gorm:"size:128" json:"agent_name,omitempty"`
	GroupID           string    `gorm:"column:group_id;size:64;index" json:"group_id,omitempty"`
	RunID             string    `gorm:"column:run_id;size:36;index" json:"run_id,omitempty"`
	RunKey            *string   `gorm:"column:run_key;size:36;uniqueIndex" json:"-"`
	RunRequestHash    string    `gorm:"column:run_request_hash;size:64" json:"-"`
	TerminationReason string    `gorm:"column:termination_reason;size:64" json:"termination_reason,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}
```

- `MessageID`：UUID，唯一索引；DAO 创建入口会 trim 并拒绝空值/超过模型 `size:36` 的值
- `TaskID` / `SessionID`：DAO 创建入口会 trim 并拒绝空值/超长值；`TaskID` 上限与模型 `size:36` 对齐，`SessionID` 上限与模型 `size:128` 对齐；`SessionID` 复合索引 `idx_session_id`（单列）和 `idx_session_status`（与 Status 组合），用于按会话过滤和按会话+状态查询
- `Role`：遵循 `contracts/schemas/message.yaml`，只允许 `"user"` 或 `"agent"`；DAO 创建入口会拒绝其他值
- `Content`：`longtext` 类型，Agent 消息由 StreamWriter 批量刷写
- `LastSeq`：Redis Stream 的最后消费位置，用于断线重连时从 MySQL 历史恢复后跳过已消费事件
- `Status`：遵循 `contracts/schemas/message.yaml`，只允许 `streaming`（流式中） / `completed` / `failed`；DAO 创建入口在空值时补默认 `completed`，状态更新入口也会校验白名单
- `GroupID`：编排分组标识，Orchestrator 群聊场景下标记子消息所属分组，带独立索引
- `RunID` / `RunKey` / `RunRequestHash`：Agent Run 生命周期字段（见 [09-run-lifecycle.md](09-run-lifecycle.md)）。`RunKey` 唯一索引实现 run_id 创建幂等；`RunRequestHash` 记录请求体 SHA256，同一 run_id 携带不同请求时返回 409
- `TerminationReason`：Run 终止原因（契约 `AgentRunTerminationReason`），Error 事件携带 `termination_reason` 时由 StreamWriter 持久化

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

- `SnapshotID`：UUID，前端生成的唯一标识；Service 校验上限与模型 `size:36` 对齐
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
- `ID`：GORM 自增 `uint` 主键；删除公告时 Service 会先把路由参数解析为正整数，再传入 DAO
- `SenderID` / `SenderName`：发送者标识
- `Pinned`：是否置顶，列表查询时置顶公告优先排列

### ContactGroup — 联系人分组 (`internal/model/contact_group.go`)

ContactGroup 存储用户自定义的会话分组，支持排序和拖拽排列。

```go
type ContactGroup struct {
    ID        uint      `gorm:"primarykey" json:"id"`
    GroupID   string    `gorm:"uniqueIndex;size:36" json:"group_id"`
    Name      string    `gorm:"size:128;not null" json:"name"`
    SortOrder int       `gorm:"default:0" json:"sort_order"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}
```

- `GroupID`：后端生成 UUID，Service 校验上限与模型 `size:36` 对齐

### ContactGroupItem — 分组项 (`internal/model/contact_group.go`)

ContactGroupItem 是分组与任务的多对多关联表。

```go
type ContactGroupItem struct {
    ID        uint      `gorm:"primarykey" json:"id"`
    GroupID   string    `gorm:"index;uniqueIndex:idx_contact_group_item_group_task;size:36;not null" json:"group_id"`
    TaskID    string    `gorm:"index;uniqueIndex:idx_contact_group_item_group_task;size:36;not null" json:"task_id"`
    SortOrder int       `gorm:"default:0" json:"sort_order"`
    CreatedAt time.Time `json:"created_at"`
}
```

- `(GroupID, TaskID)`：复合唯一索引，防止同一任务在同一分组内重复出现

### SkillHub — 技能仓库 (`internal/model/skill.go`)

SkillHub 统一存储 builtin 和 external 技能。external 技能的 ZIP 包既可落在 `Content` 字段（DB blob 兼容路径），也可托管到 MinIO 对象存储（由 `ObjectKey` + `StorageType` 决定）。

```go
type SkillHub struct {
    ID                 uint      `gorm:"primarykey" json:"id"`
    Name               string    `gorm:"uniqueIndex;size:128;not null" json:"name"`
    Builtin            bool      `gorm:"not null;default:false" json:"builtin"`
    Description        string    `gorm:"type:text" json:"description"`
    FileCount          int       `gorm:"default:0" json:"file_count"`
    TotalSize          int64     `gorm:"default:0" json:"total_size"`
    Content            []byte    `gorm:"type:longblob" json:"-"` // 迁移期兼容字段，external skill 专用
    ObjectKey          string    `gorm:"size:512" json:"-"`
    SHA256             string    `gorm:"size:64" json:"sha256,omitempty"`
    PackageSize        int64     `gorm:"default:0" json:"package_size"`
    StorageType        string    `gorm:"size:16" json:"storage_type"`
    Status             string    `gorm:"size:32;not null;default:ready" json:"status"`
    UploadedBy         string    `gorm:"size:64" json:"uploaded_by,omitempty"`
    FilesJSON          string    `gorm:"type:text" json:"-"`
    ContainsExecutable bool      `gorm:"not null;default:false" json:"contains_executable"`
    ContainsBinary     bool      `gorm:"not null;default:false" json:"contains_binary"`
    CreatedAt          time.Time `json:"created_at"`
    UpdatedAt          time.Time `json:"updated_at"`
}
```

- `Name`：技能名称，唯一索引
- `Builtin`：区分内置/外部技能
- `Content`：external 技能的 ZIP 包二进制数据（`longblob`，DB blob 兼容路径）
- `ObjectKey` / `StorageType` / `SHA256` / `PackageSize`：MinIO 对象存储迁移字段；`StorageType` 取 `db` 或 `minio`
- `Status`：技能状态（`ready` / `migrating` / `deleting` / `storage_error`），驱动后台迁移与清理 worker
- `FilesJSON` / `ContainsExecutable` / `ContainsBinary`：文件清单与内容审计标志（可执行/二进制检测），用于安全策略

### AgentSkill — Agent 技能关联 (`internal/model/skill.go`)

AgentSkill 记录 Session 与 external 技能的多对多关联。

```go
type AgentSkill struct {
    ID         uint      `gorm:"primarykey" json:"id"`
    SessionID  string    `gorm:"uniqueIndex:idx_agent_skill_session_skill;size:128;not null" json:"session_id"`
    SkillName  string    `gorm:"uniqueIndex:idx_agent_skill_session_skill;size:128;not null" json:"skill_name"`
    AgentType  string    `gorm:"size:32;not null" json:"agent_type"`
    Status     string    `gorm:"size:16;not null;default:ready" json:"status"`
    ImportedAt time.Time `json:"imported_at"`
}
```

- 仅 external skills 需要关联记录
- 同一 Session 可导入多个技能，同一技能可被多个 Session 导入
- `(SessionID, SkillName)`：复合唯一索引，防止同一 Session 重复导入同一 external skill
- `Status`：AgentEnd 侧安装状态（`installing` / `ready` / `removing` / `sync_error`），由后台 `SkillOperationWorker` 补偿推进

### Artifact — AgentEnd 内置资源 (`internal/model/artifact.go`)

Artifact 是 message 与私有 artifact 桶中一个不可变对象之间的可信元数据链接。Backend 签发短期 capability token，AgentEnd 凭 token 直传对象，元数据落库；task 删除时级联标记删除。

```go
type Artifact struct {
    ID             uint      `gorm:"primaryKey" json:"id"`
    ResourceID     string    `gorm:"size:36;not null;uniqueIndex" json:"resource_id"`
    TaskID         string    `gorm:"size:36;not null;index" json:"task_id"`
    SessionID      string    `gorm:"size:128;not null;index" json:"session_id"`
    MessageID      string    `gorm:"size:36;not null;index;uniqueIndex:idx_artifact_message_idempotency" json:"message_id"`
    IdempotencyKey *string   `gorm:"size:128;index;uniqueIndex:idx_artifact_message_idempotency" json:"-"`
    Kind           string    `gorm:"size:32;not null" json:"kind"`
    ObjectKey      string    `gorm:"size:512;not null;uniqueIndex" json:"-"`
    Filename       string    `gorm:"size:255;not null" json:"filename"`
    ContentType    string    `gorm:"size:128;not null" json:"content_type"`
    Size           int64     `gorm:"not null" json:"size"`
    SHA256         string    `gorm:"size:64;not null" json:"sha256"`
    Status         string    `gorm:"size:16;not null;index:idx_artifact_status_updated" json:"status"`
    LastError      string    `gorm:"type:text" json:"-"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `gorm:"index:idx_artifact_status_updated" json:"updated_at"`
}
```

- `ResourceID`：UUID，对外暴露的唯一资源标识（`/api/artifacts/:resourceId`）
- `Kind`：资源类型，目前仅 `html`（`ArtifactKindHTML`）
- `Status`：`pending` → `ready` / `failed`；删除路径 `deleting` → `deleted`
- `(MessageID, IdempotencyKey)`：复合唯一索引，支持 AgentEnd 重传幂等
- `ObjectKey` / `SHA256`：私有 artifact 桶对象键与完整性校验值（不暴露给前端）

### 技能存储迁移模型 (`internal/model/skill.go`)

以下三个模型支撑 MinIO 对象存储迁移、补偿型后台任务与审计日志，均通过 AutoMigrate 建表：

**SkillUploadReceipt** — 上传确认幂等收据。Redis 上传会话丢失后仍可按 `upload_id` 幂等返回确认结果：

```go
type SkillUploadReceipt struct {
    UploadID  string    `gorm:"primaryKey;size:64" json:"upload_id"`
    SkillID   uint      `gorm:"not null;index" json:"skill_id"`
    SHA256    string    `gorm:"size:64;not null" json:"sha256"`
    OwnerID   string    `gorm:"size:128" json:"owner_id,omitempty"`
    CreatedAt time.Time `gorm:"index" json:"created_at"`
}
```

**SkillOperationJob** — 对象存储与 AgentEnd 补偿操作的持久化 Outbox。带租约（`LeaseUntil` / `LeaseToken`）和退避（`NextRetryAt` / `Attempts`），由 `SkillOperationWorker` 轮询领取：

```go
type SkillOperationJob struct {
    ID             uint64     `gorm:"primaryKey" json:"id"`
    Operation      string     `gorm:"size:32;not null;index:idx_skill_jobs_due,priority:1" json:"operation"` // delete_object/install/remove/migrate/verify_object
    IdempotencyKey string     `gorm:"size:512;not null;uniqueIndex" json:"idempotency_key"`
    SkillID        *uint      `gorm:"index" json:"skill_id,omitempty"`
    AgentSkillID   *uint      `gorm:"index" json:"-"`
    SkillName      string     `gorm:"size:128" json:"skill_name,omitempty"`
    SessionID      string     `gorm:"size:128" json:"session_id,omitempty"`
    AgentType      string     `gorm:"size:32" json:"agent_type,omitempty"`
    ObjectKey      string     `gorm:"size:512" json:"-"`
    Status         string     `gorm:"size:16;not null;default:pending;index:idx_skill_jobs_due,priority:2" json:"status"` // pending/running/done/failed
    Attempts       int        `gorm:"not null;default:0" json:"attempts"`
    NextRetryAt    *time.Time `gorm:"index:idx_skill_jobs_due,priority:3" json:"next_retry_at,omitempty"`
    LeaseUntil     *time.Time `json:"lease_until,omitempty"`
    LeaseToken     string     `gorm:"size:64" json:"-"`
    LastError      string     `gorm:"type:text" json:"last_error,omitempty"`
    CreatedAt      time.Time  `json:"created_at"`
    UpdatedAt      time.Time  `json:"updated_at"`
}
```

**SkillAuditEvent** — Skill 生命周期动作的只追加审计日志（actor、完整性 hash、内容标志、结果）：

```go
type SkillAuditEvent struct {
    ID                 uint64    `gorm:"primaryKey" json:"id"`
    Action             string    `gorm:"size:32;not null;index" json:"action"`
    Outcome            string    `gorm:"size:16;not null;index" json:"outcome"`
    UploadID           string    `gorm:"size:64;index" json:"upload_id,omitempty"`
    SkillID            *uint     `gorm:"index" json:"skill_id,omitempty"`
    SkillName          string    `gorm:"size:128;index" json:"skill_name"`
    OwnerID            string    `gorm:"size:128;index" json:"owner_id,omitempty"`
    ObjectKey          string    `gorm:"size:512" json:"-"`
    SHA256             string    `gorm:"size:64" json:"sha256,omitempty"`
    FilesJSON          string    `gorm:"type:text" json:"-"`
    ContainsExecutable bool      `gorm:"not null;default:false" json:"contains_executable"`
    ContainsBinary     bool      `gorm:"not null;default:false" json:"contains_binary"`
    Error              string    `gorm:"type:text" json:"error,omitempty"`
    CreatedAt          time.Time `gorm:"index" json:"created_at"`
}
```

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
Task 1:N ContactGroupItem (task_id 关联，通过 ContactGroup 分组)
Session 1:N AgentSkill (session_id 关联，通过 SkillHub 引用技能)

ContactGroup 1:N ContactGroupItem (group_id 关联)
SkillHub 1:N AgentSkill (skill_name 关联)
SkillHub 1:N SkillOperationJob (skill_id 关联，后台补偿任务)
SkillHub 1:N SkillAuditEvent (skill_id 关联，审计日志)
SkillUploadReceipt（按 upload_id 幂等索引，关联 SkillHub.id）

Message 1:N Artifact (message_id 关联，AgentEnd 内置资源元数据)
Task 1:N Artifact (task_id 关联，删除 task 时级联标记删除对象)

AdminSetting（独立 KV 存储，无外键关联）
```
