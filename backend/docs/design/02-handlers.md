# Controllers / Services / DAOs — 三层架构

## 实现了什么

基于 Gin 框架实现了 **Controller → Service → DAO 三层架构**，涵盖 14 组业务模块。Controller 仅负责参数绑定和 HTTP 响应；Service 封装纯业务逻辑（无 Gin 依赖）；DAO 封装纯数据访问（接口可 Mock 替换）。通过 `BizError` 统一业务错误码，Controller 层 `handleBizError` 自动映射为 HTTP 状态码。

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
    vo.InternalError(c, err.Error())
}
```

## Controller 层 (`internal/controller/impl/`)

每个 Controller 通过构造函数创建 DAO → 组装 Service → 注入自身，实现 `RegisterRoutes(rg *gin.RouterGroup)` 自注册路由。

### TaskController (`task_controller.go`)

```go
type TaskController struct {
    service     service.TaskService
    agentClient *agentend_client.Client
}
```

- `NewTaskController(agentClient)` — 内部创建 TaskDao + SessionDao + MessageDao + DiffDao → TaskService
- 路由：

```
POST   /tasks                    CreateTask
GET    /tasks                    ListTasks
GET    /tasks/:taskId            GetTask
DELETE /tasks/:taskId            DeleteTask
DELETE /tasks/:taskId/leave      LeaveTask
PATCH  /tasks/:taskId            PatchTask
POST   /tasks/:taskId/run        RunTask
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

### SessionController (`session_controller.go`)

```go
type SessionController struct {
    service service.SessionService
}
```

- 路由：`PATCH /sessions/:sessionId`

### AgentController (`agent_controller.go`)

- 路由：`GET /agent-types`（返回硬编码四种 Agent 类型）

### StreamController (`stream_controller.go`)

```go
type StreamController struct {
    service service.StreamService
}
```

- 路由：`GET /tasks/:taskId/stream`（SSE 流式订阅）

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
DELETE /contact-groups/:groupId/items/:taskId RemoveItem
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
DELETE /admin/sessions     DeleteSessions
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
| `DiffSnapshotService` | Diff 快照 Upsert（终态保护） |
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
- `RunTask` — Agent 路由选择（direct / group / broadcast）→ 创建 Message → 后台 goroutine 调用 AgentEnd → 返回 202
- `ReviewTask` — Orchestrator 规划审查的 approve/discuss/modify
- `DeleteTask` — 级联删除（调用 DAO cascade）
- `LeaveTask` — 软删除（标记 inactive）

**TaskRoute** (`service/impl/task_route.go`)：
- Agent 路由策略：direct（直接发送）、group（群聊）、broadcast（广播）
- 根据 `MessageRoute` 决定消息分发目标

**MessageService** (`service/impl/message_service.go`)：
- `ListMessages` — cursor 分页 + session_id 过滤 + mode 可见性控制
- `WindowMessages` — 群聊窗口消息（聚合同 Task 其他 Session 消息）

**SkillService** (`service/impl/skill_service.go`)：
- `UploadSkill` — ZIP 校验 + 解压到临时目录
- `ConfirmSkill` — 从临时目录读取内容，存入 DB blob
- `ImportSkill` / `RemoveSkill` — Session ↔ Skill 关联管理
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

`DeleteTaskCascade` 在事务中按依赖顺序删除：Message → SessionAgent → DiffSnapshot → Session → Announcement → ContactGroupItem → Task。

### Mock 实现 (`dao/mock/`)

用于 Service 层单元测试，当前提供 `SessionDao` 和 `DiffSnapshotDao` 的 mock。
