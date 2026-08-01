# Controllers / Services / DAOs — 三层架构

## 实现了什么

基于 Gin 框架实现了 **Controller → Service → DAO 三层架构**，涵盖 13 组业务模块。Controller 仅负责参数绑定和 HTTP 响应；Service 封装纯业务逻辑（无 Gin 依赖）；DAO 封装纯数据访问（接口可 Mock 替换）。通过 `BizError` 统一业务错误码，Controller 层 `handleBizError` 自动映射为 HTTP 状态码。

## 怎么实现的

### 架构概览

```
┌─────────────────────────────────────────────────────┐
│ Controller (impl/)                                   │
│  参数绑定 → Service 调用 → vo 响应 / handleBizError  │
│  每个 Controller 持有一个 Service 接口               │
├─────────────────────────────────────────────────────┤
│ Service (impl/)                                      │
│  纯业务逻辑，接收 DTO，返回业务结果或 BizError        │
│  每个 Service 持有一个或多个 DAO 接口                 │
├─────────────────────────────────────────────────────┤
│ DAO (gorm/)                                          │
│  纯数据访问，GORM 实现接口                            │
│  可被 mock/ 替换用于 Service 单测                     │
└─────────────────────────────────────────────────────┘
```

### 统一错误处理 (`internal/controller/impl/errors.go`)

Service 层通过 `BizError`（Code + Message）表达业务错误，Controller 层通过 `handleBizError` 统一映射：

```go
type BizError struct {
    Code    int
    Message string
}

func handleBizError(c *gin.Context, err error) {
    var bizErr *service.BizError
    if errors.As(err, &bizErr) {
        switch bizErr.Code {
        case 400: vo.BadRequest(c, bizErr.Message)
        case 401: vo.Unauthorized(c, bizErr.Message)
        case 403: vo.Forbidden(c, bizErr.Message)
        case 404: vo.NotFound(c, bizErr.Message)
        case 409: vo.Conflict(c, bizErr.Message)
        case 503: vo.ServiceUnavailable(c, bizErr.Message)
        default:  vo.InternalError(c, bizErr.Message)
        }
        return
    }
    slog.Error("unhandled controller error", "error", err)
    vo.InternalError(c, "internal server error")
}
```

## Controller 层 (`internal/controller/impl/`)

每个 Controller 通过构造函数接收 Service 接口，实现 `RegisterRoutes(rg *gin.RouterGroup)` 自注册路由。DAO → Service → Controller 的组装集中在 `internal/app`。

### TaskController (`task_controller.go`)

```go
type TaskController struct {
    service     service.TaskService
    agentClient *agentend_client.Client
}
```

- `NewTaskController(taskService, agentClient)` — 注入 `TaskService`，Controller 不直接依赖 DAO 实现
- 路由：

```
POST   /tasks                    CreateTask
GET    /tasks                    ListTasks
GET    /tasks/:taskId            GetTask
DELETE /tasks/:taskId            DeleteTask
DELETE /tasks/:taskId/leave      LeaveTask
PATCH  /tasks/:taskId            PatchTask
POST   /tasks/:taskId/run        RunTask（IP 限流 30次/分钟）
POST   /tasks/:taskId/review     ReviewTask
POST   /validate-repo-path       ValidateRepoPath
POST   /init-git-repo             InitGitRepo
```

Controller 方法示例（仅参数绑定 + Service 调用 + 错误处理）：

```go
func (ctrl *TaskController) CreateTask(c *gin.Context) {
    var req service.CreateTaskInput
    if err := c.ShouldBindJSON(&req); err != nil {
        vo.BadRequest(c, "title is required")
        return
    }
    task, err := ctrl.service.CreateTask(req)
    if err != nil {
        handleBizError(c, err)
        return
    }
    vo.Created(c, task)
}
```

### MessageController (`message_controller.go`)

```go
type MessageController struct {
    service service.MessageService
}
```

- 路由：

```
GET /tasks/:taskId/messages         ListMessages（cursor 分页 + session_id + mode 过滤）
GET /tasks/:taskId/messages/window  WindowMessages（群聊窗口消息）
```

Service 只接受空 `mode` 或 `mode=group`；session 相关 query 会 trim 并按 128 字符上限校验。

### SessionController (`session_controller.go`)

```go
type SessionController struct {
    service service.SessionService
}
```

- 路由：`PATCH /sessions/:sessionId`
- Service 会 trim 并校验 `session_id`，当前状态值只允许改为 `inactive`。

### AgentController (`agent_controller.go`)

- 路由：`GET /agent-types`（返回硬编码四种 Agent 类型）

### StreamController (`stream_controller.go`)

```go
type StreamController struct {
    service service.StreamService
}
```

- 路由：`GET /tasks/:taskId/stream`（SSE 流式订阅）
- Service 在写出 SSE header 前校验 `session_id` / `message_id`，失败时保持 JSON 错误响应。

### AgentProfileController (`agent_profile_controller.go`)

```go
type AgentProfileController struct {
    service service.AgentProfileService
}
```

- 路由：

```
GET /sessions/:sessionId/profile  GetProfile
GET /sessions/:sessionId/detail   GetDetail
GET /sessions/:sessionId/soul     GetSoul
PUT /sessions/:sessionId/soul     UpdateSoul
```

Service 会统一 trim 并校验 `session_id`，SoulMD 写入前去除空白后按 300 字符上限保存。

### AvatarController (`avatar_controller.go`)

```go
type AvatarController struct {
    service service.AvatarService
}
```

- 路由：

```
POST /agents/avatar            UploadAvatar（multipart 文件上传）
PUT  /sessions/:sessionId      UpdateSession（agent_name + avatar_url）
```

`AvatarService.UpdateSession` 会 trim 字段，并限制 `agent_name` 最大 128 字符、`avatar_url` 最大 512 字节；头像 URL 只允许本地绝对路径（如 `/uploads/...`）或 `http/https` URL，拒绝控制字符、空白、协议相对 URL 和非 HTTP scheme。

### DiffSnapshotController (`diff_snapshot_controller.go`)

```go
type DiffSnapshotController struct {
    service service.DiffSnapshotService
}
```

- 路由：

```
GET /diff-snapshots/:snapshotId  GetDiffSnapshot
PUT /diff-snapshots/:snapshotId  SaveDiffSnapshot
```

### WorkspaceController (`workspace_controller.go`)

```go
type WorkspaceController struct {
    agentClient *agentend_client.Client
    httpClient  *http.Client
}
```

- 路由（任务级别操作）：

```
GET  /workspace/task/:taskId/git-info         TaskGitInfo
POST /workspace/task/:taskId/merge-to-main    MergeTaskToMain
```

- 路由（直接工作区）：

```
GET  /workspace/:id/files/*filepath    ReadFile
PUT  /workspace/:id/files/*filepath    WriteFile
GET  /workspace/:id/diff               GetDiff
POST /workspace/:id/commit             Commit
POST /workspace/:id/revert             Revert
POST /workspace/:id/preview/start      StartPreview
POST /workspace/:id/preview/stop       StopPreview
```

- 路由（Session 级别代理）：先通过 `resolveWorkspaceID` 查询 AgentEnd 获取 workspace ID，再代理。

```
GET  /session/:sessionId/files/*filepath  SessionFileRead
PUT  /session/:sessionId/files/*filepath  SessionFileWrite
GET  /session/:sessionId/diff             SessionGetDiff
POST /session/:sessionId/commit           SessionCommit
POST /session/:sessionId/revert           SessionRevert
```

### AnnouncementController (`announcement_controller.go`)

```go
type AnnouncementController struct {
    service service.AnnouncementService
}
```

- 路由：

```
GET    /tasks/:taskId/announcements    ListAnnouncements
POST   /tasks/:taskId/announcements    CreateAnnouncement
DELETE /tasks/:taskId/announcements/:id DeleteAnnouncement
```

`DeleteAnnouncement` 的 `:id` 是 Announcement 自增主键，Service 会先解析为正整数；非法、空白、0 或非数字 ID 会在进入 DAO 前返回 400。

### ContactGroupController (`contact_group_controller.go`)

```go
type ContactGroupController struct {
    service service.ContactGroupService
}
```

- 路由：

```
GET    /contact-groups                   ListGroups
POST   /contact-groups                   CreateGroup
PUT    /contact-groups/:groupId          UpdateGroup
DELETE /contact-groups/:groupId          DeleteGroup
POST   /contact-groups/:groupId/items    AddItem
DELETE /contact-groups/:groupId/items/:taskID RemoveItem
```

### SkillController (`skill_controller.go`)

```go
type SkillController struct {
    service service.SkillService
}
```

- 路由：

```
POST   /skills/upload                     Upload（multipart ZIP）
POST   /skills/confirm                    Confirm（确认上传）
GET    /skills                            List
DELETE /skills/:name                      Delete
POST   /skills/:name/import               Import（导入到 Session）
DELETE /skills/:name/sessions/:sessionId  Remove（从 Session 移除）
POST   /internal/builtin-skills           ReportBuiltinSkills（AgentEnd 上报内置技能）
```

### AdminController (`admin_controller.go`)

```go
type AdminController struct {
    service service.AdminService
    cfg     *conf.Config
}
```

- 路由自注册，公开接口与受保护接口分离：

```
POST /admin/auth           Auth（密码认证，IP 限流 5次/分钟）
GET  /admin/health         HealthCheck
GET  /admin/avatar         GetAvatar

--- 以下需要 JWT Bearer Token ---

GET    /admin/resources    GetResources
DELETE /admin/sessions     DeleteSessions（先清理 AgentEnd workspace，再删 DB）
GET    /admin/workspaces   GetWorkspaces
DELETE /admin/workspaces/:id DeleteWorkspace
GET    /admin/agents       GetAgents
GET    /admin/services     GetServices
GET    /admin/statistics   GetStatistics
PUT    /admin/avatar       UpdateAvatar
```

## Service 层 (`internal/service/`)

### 接口定义 (`service.go`)

所有 Service 接口定义在 `internal/service/service.go`，无 Gin 依赖，可独立单测。核心接口：

| 接口 | 职责 |
|------|------|
| `TaskService` | 任务 CRUD + Run（含 Agent 路由选择）+ Review |
| `MessageService` | 消息列表分页 + 群聊窗口消息 |
| `SessionService` | Session 状态管理 |
| `StreamService` | SSE 流式服务 |
| `AgentProfileService` | Agent 档案/详情/灵魂描述 |
| `AvatarService` | 头像上传 + Session 元数据更新 |
| `DiffSnapshotService` | Diff 快照 Upsert（输入白名单、大小限制、终态保护） |
| `AnnouncementService` | 公告 CRUD |
| `ContactGroupService` | 联系人分组管理 |
| `SkillService` | 技能上传/确认/导入/删除 |
| `AdminService` | 管理面板全部功能 |

### DTO 定义 (`service.go`)

Service 层定义了所有 DTO（Data Transfer Object），避免 Controller 直接依赖 model：

- `CreateTaskInput` / `PatchTaskInput` / `RunTaskInput` / `ReviewTaskInput` — 任务相关输入
- `ListMessagesResponse` — 消息列表输出
- `RunTaskResult` — 运行任务结果
- `TaskDetailResponse` / `TaskSessionWithAgent` — 任务详情输出
- `SkillHubItem` / `SkillImportResult` — 技能相关
- `AgentProfileResponse` / `AgentDetailResponse` — Agent 档案
- `AuthResponse` / `ResourceSummary` / `StatisticsResponse` / `WorkspaceSummary` — 管理面板

### Service 实现要点

**TaskService** (`service/impl/task_service.go` + `task_route.go`)：
- `CreateTask` — 事务中创建 Task + Session + SessionAgent
- `ListTasks` — 默认 50、最大 100 的 cursor 分页；响应体保持任务数组，分页游标放在 header
- `RunTask` — IP 限流 → 校验 message/session/agent_type → Agent 路由选择（direct / orchestrator / unchanged）→ 创建 Message → 后台 goroutine 调用 AgentEnd → 返回 202
- `ReviewTask` — Orchestrator 规划审查的 approve/discuss/modify；进入 AgentEnd 前会确认目标 Session 当前处于 `awaiting_review`，否则返回 409
- `DeleteTask` / `LeaveTask` — best-effort 清理 AgentEnd session/workspace/分支后，级联删除 DB 数据

**TaskRoute** (`service/impl/task_route.go`)：
- Agent 路由策略：`direct`（直接 @mention 单个普通 Agent）、`orchestrator`（多 Agent 或 @Orchestrator 时交给 Orchestrator 协调）、`unchanged`（无路由干预，保持请求 Session）
- 返回 `MessageRoute`（含 Mode / SessionID / AgentType / AgentName / RouteID / AgentMessage / DisplayMessage），由 `RunTask` 据此决定消息分发目标

**MessageService** (`service/impl/message_service.go`)：
- `ListMessages` — cursor 分页 + session_id 过滤 + mode 可见性控制
- `WindowMessages` — 群聊窗口消息（聚合同 Task 其他 Session 消息）

**SkillService** (`service/impl/skill_service.go`)：
- `UploadSkill` — ZIP 校验 + 解压到临时目录
- `ConfirmSkill` — 从临时目录读取内容，存入 DB blob
- `ImportSkill` / `RemoveSkill` — Session ↔ Skill 关联管理；导入 DB 记录失败时回滚 worktree，移除前确认导入关系存在
- `DeleteSkill` — 只允许删除未被任何 Session 导入的 external skill
- `ReportBuiltinSkills` — AgentEnd 上报内置技能列表

**StreamService** (`service/impl/stream_service.go`)：
- `ServeStream` — 三阶段 SSE 分发（MySQL 历史 → Redis 缺口 → Hub 实时）

## DAO 层 (`internal/dao/`)

### 接口定义 (`dao.go`)

| 接口 | 职责 |
|------|------|
| `TaskDao` | Task + Session + SessionAgent 联表操作 |
| `MessageDao` | Message 查询/创建/更新（含群聊窗口） |
| `SessionDao` | Session 状态/字段更新 |
| `DiffSnapshotDao` | DiffSnapshot Upsert + 终态保护 |
| `AnnouncementDao` | Announcement CRUD |
| `ContactGroupDao` | ContactGroup + Item CRUD |
| `SkillDao` | SkillHub + AgentSkill 关联 |
| `AdminDao` | AdminSetting KV + 统计查询 |

### GORM 实现 (`dao/gorm/`)

每个 DAO 通过 `db.GetDB()` 获取 GORM 实例，实现对应接口。构造函数模式：

```go
func NewTaskDao() dao.TaskDao {
    return &taskDao{}
}
```

### 级联删除 (`dao/gorm/cascade.go`)

`DeleteTaskCascade` 在事务中按依赖顺序删除：Message → SessionAgent → DiffSnapshot → AgentSkill → Session → Announcement → ContactGroupItem → Task，避免任务删除后留下分组或技能导入孤儿项。级联 helper 会检查每个 `Pluck` / `Delete` 的错误；任一步失败都会返回 error 并回滚外层事务。

`SessionDao.UpdateStatusByTask` 只接受契约内 Session 状态；更新 0 行时会回查 `(session_id, task_id)`，同值更新算成功，不存在则返回 not found，避免后台流式状态更新静默丢失。

### Mock 实现 (`dao/mock/`)

用于 Service 层单元测试，当前提供 `SessionDao` 和 `DiffSnapshotDao` 的 mock。
