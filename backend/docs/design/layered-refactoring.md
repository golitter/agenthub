# 后端分层架构重构计划：handler → controller + service + dao

## 实现了什么

**本方案已全部实施完成。** 当前后端已采用 `internal/controller/impl/`（13 组 Controller）+ `internal/service/impl/`（11 组 Service 实现 + 3 辅助模块：stream_helper、task_route、group_chat_window）+ `internal/dao/gorm/`（GORM 实现）+ `internal/dao/mock/`（测试替身）三层架构。`internal/handler/` 目录已整体删除，所有业务代码已迁移至三层架构中。`StreamWriter` 通过构造函数注入 `dao.MessageDao`、`dao.SessionDao`、`dao.DiffSnapshotDao` 接口访问数据库。本文档作为历史参考保留，记录了重构的全过程和设计决策。

## Context

当前后端 `internal/handler/` 一层包揽了 HTTP 参数绑定、业务逻辑、数据库操作三重职责，此外 `internal/stream/writer.go` 也包含 10 处直调。业务层生产代码共 101 处 `db.GetDB()` 直调（handler 层 86 处 + service 层 5 处 + stream 层 10 处；不统计 `cmd/server/main.go` 启动迁移用的 1 处和测试注释），不符合目标的 controller → service → dao 分层架构。本次重构将按三层模式拆分，每层 interface 先行、impl 实现，使代码可测试、可维护。

## 怎么实现的

重构按「目标架构 → 14 阶段分步执行 → main.go 接线模式」层层推进，下文逐段展开历史决策与实施过程（本方案已全部实施完成，本文档作为历史参考保留）。

## 目标架构

```
internal/
├── controller/           ← HTTP 层（原 handler/，重命名）
│   ├── controller.go     ← 各 Controller interface（RegisterRoutes 模式）
│   └── impl/             ← Gin handler 实现（参数绑定 + 响应，调 service）
├── service/              ← 业务逻辑层
│   ├── service.go        ← 各 Service interface
│   └── impl/             ← 业务编排（调 dao，不含 HTTP/DB 细节）
├── dao/                  ← 数据访问层
│   ├── dao.go            ← 各 Dao interface
│   ├── gorm/             ← GORM 实现（业务层唯一允许出现 db.GetDB() 的地方）
│   └── mock/             ← 测试 mock
├── model/                ← 不变
├── stream/               ← Hub/Writer 底层能力保留，DB 操作迁移至 DAO
├── middleware/            ← 不变
├── vo/                   ← 不变
└── generated/            ← 不变
```

**依赖方向：** controller → service → dao → model，严禁反向依赖。

### 错误处理策略

Service 层返回业务错误，Controller 层统一映射为 HTTP 响应。Service / DAO 层禁止依赖 `gin.Context` 或 `vo` 包。

```go
// service/bizerr.go — 全局业务错误类型
type BizError struct {
    Code    int    // HTTP status code（400 / 404 / 409 / 500 ...）
    Message string // 用户可见提示
}

func (e *BizError) Error() string { return e.Message }

// 语义化构造函数
func ErrNotFound(msg string) *BizError { return &BizError{Code: 404, Message: msg} }
func ErrBadRequest(msg string) *BizError { return &BizError{Code: 400, Message: msg} }
func ErrInternal(msg string) *BizError { return &BizError{Code: 500, Message: msg} }
```

Controller 层统一 error 分发：

```go
func handleBizError(c *gin.Context, err error) {
    var biz *service.BizError
    if errors.As(err, &biz) {
        switch biz.Code {
        case 400: vo.BadRequest(c, biz.Message)
        case 404: vo.NotFound(c, biz.Message)
        case 500: vo.InternalError(c, biz.Message)
        default:  vo.InternalError(c, biz.Message)
        }
        return
    }
    // 未知错误统一 500
    vo.InternalError(c, err.Error())
}
```

### 事务管理原则

1. **事务永远封装在 DAO 层内部**：DAO 方法自己开 `db.Transaction()`，调用方无需感知事务边界。
2. **跨表事务**：如需在一次事务中操作多张表，在 DAO 层提供一个组合方法（如 `CreateTaskWithSessions`），内部统一管理 tx。
3. **禁止 Service / Controller 层直接操作 `*gorm.DB`**：`*gorm.DB` 只允许出现在 `dao/gorm/` 包内。
4. **DAO 接口不暴露 tx**：interface 方法签名使用业务参数，不传递 `*gorm.DB`。

## 分阶段执行计划

按复杂度递增顺序分 14 个阶段，每个阶段完成后代码可编译可运行。

### Phase 0: 基础脚手架（无行为变更）

补齐已预留空目录中的三层 interface 文件骨架和通用业务错误类型：

| 新建文件 | 内容 |
|---------|------|
| `controller/controller.go` | 空文件，后续各阶段追加 interface |
| `service/service.go` | 空文件，后续各阶段追加 interface |
| `service/bizerr.go` | `BizError` 与 `ErrBadRequest` / `ErrNotFound` / `ErrInternal` 等构造函数 |
| `dao/dao.go` | 空文件，后续各阶段追加 interface |

### Phase 1: Agent 域（0 DB 调用 — 模板建立）

最简单的域，用于确立完整的文件布局模式。

| 操作 | 文件 |
|------|------|
| 新建 | `controller/impl/agent_controller.go` — 从 `handler/agent.go` 迁移 |
| 新建 | `controller/controller.go` 追加 `AgentController` interface |
| 修改 | `cmd/server/main.go` — 切换到 `agentCtrl.RegisterRoutes(api)` |
| 删除 | `handler/agent.go` |

### Phase 2: Session 域（1 DB 调用 — 最简 DAO 链路）

打通完整的 dao → service → controller 链路。

| 操作 | 文件 |
|------|------|
| 新建 | `dao/dao.go` 追加 `SessionDao` interface |
| 新建 | `dao/gorm/session_dao.go` — GORM 实现 |
| 新建 | `dao/mock/session_dao.go` — Mock 实现 |
| 新建 | `service/service.go` 追加 `SessionService` interface |
| 新建 | `service/impl/session_service.go` — 业务逻辑 |
| 新建 | `controller/impl/session_controller.go` |
| 修改 | `cmd/server/main.go` |
| 删除 | `handler/session.go` |

### Phase 3: DiffSnapshot 域（2 DB 调用）

练习 upsert + auto-cancel 逻辑的分层。

| 操作 | 文件 |
|------|------|
| 新建 | `dao/dao.go` 追加 `DiffSnapshotDao` interface、`dao/gorm/diff_snapshot_dao.go`、`dao/mock/diff_snapshot_dao.go` |
| 新建 | `service/service.go` 追加 `DiffSnapshotService` interface、`service/impl/diff_snapshot_service.go` |
| 新建 | `controller/impl/diff_snapshot_controller.go` |
| 删除 | `handler/diff_snapshot.go` |

关键设计：auto-cancel 逻辑属于 service 层，DAO 只提供 `CancelPendingBySession` 原子操作。

### Phase 4: Announcement 域（5 DB 调用，含外部依赖 agentClient）

首次出现跨域读取（需要 Task.RepoPath）和外部服务调用（agentend 通知）。

| 操作 | 文件 |
|------|------|
| 新建 | 完整 dao + service + controller 三层 |
| 删除 | `handler/announcement.go` |

临时方案：AnnouncementDao 内含 `FindTaskRepoPath()` 方法，Phase 12 建立 TaskDao 后清理。

### Phase 5: ContactGroup 域（8 DB 调用，含事务）

练习事务封装在 DAO 内部。

| 操作 | 文件 |
|------|------|
| 新建 | 完整三层，`DeleteGroupWithItems` 在 DAO 内封装事务 |
| 删除 | `handler/contact_group.go` |

### Phase 6: Skill 域（15 DB 调用，已有部分 service）

重构已有 `service/skill_validator.go`（5 处 db.GetDB()）+ `handler/skill.go`（10 处）。**关键：skill_validator.go 混合了文件 I/O 校验和 DB 操作，需要拆分为两层。**

**拆分策略：**

| 原函数 | 文件 I/O（无 DB） | DB 操作 | 迁移目标 |
|--------|------------------|---------|---------|
| `ValidateZip` | zip 解压、frontmatter 解析、路径安全检查 | `Count` builtin 冲突 | I/O 保留为 `SkillValidator.ValidateZip()` 包级工具；DB 检查移入 `SkillService` |
| `ConfirmSkill` | `zipDir` 文件打包 | `Create` 写入 DB | I/O 保留为 `zipDir()` 工具函数；DB 写入移入 `SkillService.Confirm()` |
| `DeleteSkillFromHub` | — | `FirstOrCreate` + `Transaction` 删除 | 整体移入 `SkillService.Delete()` |
| `PackSkillDir` | — | `Select` 查询 | 整体移入 `SkillService.Pack()` |

| 操作 | 文件 |
|------|------|
| 新建 | `dao/dao.go` 追加 `SkillDao` interface、`dao/gorm/skill_dao.go`、`dao/mock/skill_dao.go` |
| 重构 | `service/skill_validator.go` → 保留纯文件 I/O 函数（`ValidateZip` 去掉 DB 查询、`zipDir`、`parseFrontmatter`） |
| 新建 | `service/service.go` 追加 `SkillService` interface、`service/impl/skill_service.go`（注入 SkillDao，承接 DB 操作） |
| 新建 | `controller/impl/skill_controller.go` |
| 删除 | `handler/skill.go` |

**SkillDao interface 核心方法：**
```go
type SkillDao interface {
    CountBuiltinByName(name string) (int64, error)
    CreateSkill(skill SkillHub) error
    FindSkillByName(name string) (*SkillHub, error)
    DeleteSkillCascade(name string) error          // 事务：删 agent_skill + skill_hub
    FindSkillContent(name string) ([]byte, error)  // 查 zip blob
    ListSkills() ([]SkillHub, error)
    // agent_skill 关联
    FindAgentSkillsBySession(sessionID string) ([]AgentSkill, error)
    UpsertAgentSkill(skill AgentSkill) error
    DeleteAgentSkill(name, sessionID string) error
}
```

### Phase 7: Message 域（4 DB 调用，处理跨域依赖）

拆分 Message handler。**注意：`fetchGroupChatWindow` 定义在 `handler/task.go`，被 `RunTask` 和 `WindowMessages` 共同调用。其职责是"为 agent 提供跨会话上下文"，本质属于任务执行流，归入 `TaskService`。**

| 操作 | 文件 |
|------|------|
| 新建 | `dao/dao.go` 追加 `MessageDao` interface、`dao/gorm/message_dao.go`、`dao/mock/message_dao.go` |
| 新建 | `service/service.go` 追加 `MessageService` interface、`service/impl/message_service.go` |
| 新建 | `controller/impl/message_controller.go` |
| 迁移 | `handler/message_test.go` → `controller/impl/message_test.go`（纯函数测试 `TestReverseMessages`，随包迁移） |
| 迁移 | `findPrimaryGroupSessionID`（`handler/message.go`）→ `TaskService`（为路由决策提供会话信息） |
| 迁移 | `fetchGroupChatWindow`（`handler/task.go`）→ `TaskService.FetchGroupChatWindow()` |
| 删除 | `handler/message.go` |

**MessageDao interface 核心方法：**
```go
type MessageDao interface {
    FindByTaskID(taskID string, limit, offset int) ([]Message, error)
    FindByTaskIDAndSessionID(taskID, sessionID string) ([]Message, error)
    CreateMessage(msg Message) error
    CountBySessionID(sessionID string) (int64, error)
}
```

**跨域依赖说明：** `WindowMessages` handler（message 域）通过注入的 `TaskService` 调用 `FetchGroupChatWindow`，这是合法的 controller → service 跨域调用。

### Phase 8: Avatar 域（2 DB 调用，qiniu 外部依赖）

复用 Phase 2 的 SessionDao（扩展 `FindBySessionID`、`UpdateFields` 方法）。

| 操作 | 文件 |
|------|------|
| 扩展 | `SessionDao` interface 追加方法 |
| 新建 | `service/service.go` 追加 `AvatarService` interface、`service/impl/avatar_service.go` |
| 新建 | `controller/impl/avatar_controller.go` |
| 删除 | `handler/avatar.go` |

### Phase 9: AgentProfile 域（13 DB 调用，多模型关联）

涉及 Session、Task、Message、SkillHub、AgentSkill 共 5 个表。原则上复用已建立 DAO；Task 表查询在本阶段提前抽出 TaskDao 只读子集，Phase 12 再扩展为完整 TaskDao。

**DAO 复用关系：**

| 表 | DAO 来源 | 需追加的方法 |
|----|---------|------------|
| `session` | `SessionDao`（Phase 2） | 已有 `FindBySessionID`（Phase 8 扩展） |
| `task` | `TaskDao` 早期子集 | `FindByTaskID` / `FindRepoPathByTaskID` |
| `message` | `MessageDao`（Phase 7） | 追加 `CountBySessionID`（如 Phase 7 未加） |
| `skill_hub` + `agent_skill` | `SkillDao`（Phase 6） | 追加 `ListSkillsByAgentTypeSession`、`UpsertSkillHub`、`DeleteAgentSkillBySession` |

| 操作 | 文件 |
|------|------|
| 扩展 | `SkillDao` interface 追加方法 |
| 扩展 | `MessageDao` interface 追加 `CountBySessionID`（如未在 Phase 7 添加） |
| 新建 | `service/service.go` 追加 `AgentProfileService` interface、`service/impl/agent_profile_service.go` |
| 新建 | `controller/impl/agent_profile_controller.go` |
| 删除 | `handler/agent_profile.go` |

**临时方案约束：** 如果不提前建立 TaskDao，只允许在代码中标注 TODO 的短期桥接方法；Phase 12 必须清理，不要把 Task 表查询长期塞进 SessionDao。

### Phase 10: Workspace 域（0 DB 调用，纯 agentend 代理）

无需 DAO/Service，直接建 controller。

| 操作 | 文件 |
|------|------|
| 新建 | `controller/impl/workspace_controller.go` |
| 迁移 | `handler/workspace_test.go` → `controller/impl/workspace_test.go`（纯函数测试 `TestSanitizePath`，随包迁移） |
| 删除 | `handler/workspace.go` |

### Phase 11: Stream 域（12 DB 调用：handler 2 + stream/writer 10，SSE + Redis）

**架构决策：** 将 SSE/Redis 编排逻辑抽成 `StreamService`，Controller 只负责 HTTP 参数绑定、响应头和 SSE 连接建立。`stream` 包保持底层能力（Hub、Writer 的 SSE 推送机制），但 writer.go 中的 10 处 `db.GetDB()` DB 操作（消息创建、状态更新、内容查询、DiffSnapshot upsert 等）必须迁移至 DAO 层。`StreamService` 负责编排（读取 MySQL 历史 → 订阅 Hub/Redis → 推送 SSE），`StreamWriter` 通过注入的 DAO 接口访问数据库。

**writer.go 的 10 处 DB 调用分析：**

| writer.go 位置 | DB 操作 | 迁移目标 |
|---------------|---------|---------|
| `Run` / `EventTypePlanReview` | Update session status to `awaiting_review` | `SessionDao.UpdateStatusBySessionTask` |
| `switchAgent` | Create sub-message on speaker/source switch | `MessageDao.CreateMessage` |
| `shouldForwardTextWithoutPersist` | Select message session_id by task/message | `MessageDao.FindSessionIDByTaskMessage` |
| `persistPlanReviewEvent` | Upsert pending DiffSnapshot | `DiffSnapshotDao.UpsertPending` |
| `appendTextToMessage` | Select message content | `MessageDao.FindMessageContent` |
| `appendTextToMessage` | Update content + last_seq | `MessageDao.UpdateMessageContentAndSeq` |
| `doFlush` | Update content + last_seq | `MessageDao.UpdateMessageContentAndSeq` |
| `updateMessageStatus` | Update message status | `MessageDao.UpdateMessageStatus` |
| `PublishErrorAndFail` | Update message status to failed | `MessageDao.UpdateMessageStatus` |
| `CleanupStaleMessages` | Mark all streaming messages failed at startup | `MessageDao.FailStaleStreamingMessages` |

| 操作 | 文件 |
|------|------|
| 扩展 | `MessageDao` 追加 `CreateMessage`、`FindSessionIDByTaskMessage`、`FindMessageContent`、`UpdateMessageContentAndSeq`、`UpdateMessageStatus`、`FailStaleStreamingMessages` |
| 扩展 | `SessionDao` 追加 `UpdateStatusBySessionTask` |
| 扩展 | `DiffSnapshotDao` 追加 `UpsertPending` |
| 重构 | `stream/writer.go` — 将 10 处 `db.GetDB()` 替换为注入的 DAO 接口调用 |
| 新建 | `service/service.go` 追加 `StreamService` interface、`service/impl/stream_service.go`（依赖 `stream.Hub` + `MessageDao`） |
| 新建 | `controller/impl/stream_controller.go`（薄层：参数绑定 + 调用 StreamService） |
| 迁移 | `handler/stream.go` → `splitContent` 辅助函数迁移至 `service/impl/stream_helper.go`（供 StreamService 使用） |
| 迁移 | `handler/stream_test.go` → `service/impl/stream_test.go`（纯函数测试 `TestSplitContentKeepsUTF8Boundaries`，随 `splitContent` 函数迁移） |
| 删除 | `handler/stream.go` |

**StreamService interface：**
```go
type StreamService interface {
    ServeStream(ctx context.Context, taskID, sessionID, messageID string, writer io.Writer, flusher http.Flusher) error
}
```

这样分层的好处：StreamService 可脱离 gin 单独测试（传入 mock Writer），而不会让 SSE/Redis 编排逻辑成为分层架构中的"例外"。

### Phase 12: Task 域（32 DB 调用 — 最难阶段）

所有跨域依赖已在前序阶段解决。

**关键迁移：**

| 原文件 | 迁移目标 |
|--------|---------|
| `handler/task.go` (业务逻辑) | `service/impl/task_service.go` |
| `handler/task.go` (HTTP 部分) | `controller/impl/task_controller.go` |
| `handler/task_route.go` | `service/impl/task_route.go`（纯业务逻辑） |
| `handler/task_route_test.go` | `service/impl/task_route_test.go` |
| `handler/cascade.go` | `dao/gorm/cascade.go`（共享 helper） |
| `handler/cascade_test.go` | `dao/gorm/cascade_test.go` |
| `handler/task.go` → `runStream` goroutine | 保留在 controller impl（使用 stream 包 + TaskService） |

**TaskDao interface 核心方法：**
```go
type TaskDao interface {
    CreateTaskWithSessions(task, sessions, sessionAgents) error  // 事务
    ListTasks() ([]Task, error)
    FindByTaskID(taskID) (*Task, error)
    DeleteTaskCascade(taskID) error                              // 事务
    PatchTask(taskID, updates) (int64, error)
    FindSessionsByTaskID(taskID) ([]Session, error)
    FindSessionAgentsBySessionIDs(ids) ([]SessionAgent, error)
    EnsureSession(sessionID, taskID, agentType) (created bool, err error)
    UpdateSessionStatus(sessionID, taskID, status) error
    GetSessionSoulMD(sessionID) (string, error)
    CreateUserMessage(msg) error
    CreateAgentMessage(msg) (string, error)
}
```

### Phase 13: Admin 域（7 DB 调用，8 个 admin 文件）

Admin handler 已有 `RegisterRoutes` 模式，拆分较自然。cascade helper 复用 Phase 12 的 `dao/gorm/cascade.go`。

| 操作 | 文件 |
|------|------|
| 新建 | 完整三层 |
| 删除 | `handler/admin*.go`（8 个文件：admin, admin_agent, admin_avatar, admin_health, admin_resource, admin_session, admin_stats, admin_workspace） |

### Phase 14: 清理

1. 删除 `internal/handler/` 目录
2. 清理临时跨域方法（Phase 4 的 `FindTaskRepoPath` → 用 TaskDao 替换）
3. 确认无 `db.GetDB()` 残留在 controller / service / stream 层
4. 更新 `backend/AGENTS.md` 文档反映新架构

## main.go 最终接线模式

**原则：** 每个 Controller 提供一个 `New*Controller(外部依赖) *Controller` 构造函数，在函数内部组装 DAO → Service → Controller 完整链路。main.go 只调 13 个构造函数 + 注册路由，不暴露 DAO/Service 实例，也不把 `*gorm.DB` 继续传入 controller/service 层。启动迁移仍可在 `cmd/server/main.go` 使用 `db.GetDB().AutoMigrate(...)`。

```go
// 外部依赖
agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)
storageProvider, err := storage.NewProvider(&cfg.Qiniu, &cfg.Storage)

// Controller 层（内部组装 DAO → Service → Controller）
agentCtrl := ctrlimpl.NewAgentController()
sessionCtrl := ctrlimpl.NewSessionController()
diffSnapshotCtrl := ctrlimpl.NewDiffSnapshotController()
announcementCtrl := ctrlimpl.NewAnnouncementController(agentClient)
contactGroupCtrl := ctrlimpl.NewContactGroupController()
skillCtrl := ctrlimpl.NewSkillController(agentClient)
messageCtrl := ctrlimpl.NewMessageController()
avatarCtrl := ctrlimpl.NewAvatarController(storageProvider)
agentProfileCtrl := ctrlimpl.NewAgentProfileController(agentClient)
workspaceCtrl := ctrlimpl.NewWorkspaceController(agentClient)
streamCtrl := ctrlimpl.NewStreamController()
taskCtrl := ctrlimpl.NewTaskController(agentClient)
adminCtrl := ctrlimpl.NewAdminController(cfg, storageProvider, agentClient)

// 注册路由
agentCtrl.RegisterRoutes(api)
sessionCtrl.RegisterRoutes(api)
diffSnapshotCtrl.RegisterRoutes(api)
announcementCtrl.RegisterRoutes(api)
contactGroupCtrl.RegisterRoutes(api)
skillCtrl.RegisterRoutes(api)
messageCtrl.RegisterRoutes(api)
avatarCtrl.RegisterRoutes(api)
agentProfileCtrl.RegisterRoutes(api)
workspaceCtrl.RegisterRoutes(api)
streamCtrl.RegisterRoutes(api)
taskCtrl.RegisterRoutes(api)
adminCtrl.RegisterRoutes(api)
```

**构造函数内部示例（session_controller.go）：**
```go
func NewSessionController() *SessionController {
    dao := gormdao.NewSessionDao()
    svc := svcimpl.NewSessionService(dao)
    return &SessionController{svc: svc}
}
```

> 如果跨域 Controller 间需要共享 Service（如 TaskController 需要 MessageService），则将共享 Service 提升为 main.go 层级注入。

## 每阶段验证方式

1. `go build ./...` 编译通过
2. `go test ./...` 测试通过（迁移到新位置后）
3. `make run-backend` 启动正常，手动 curl 验证涉及接口
4. 无 `db.GetDB()` 残留在 controller / service / stream 生产代码中
5. `rg -n 'db\.GetDB\(\)' internal/controller internal/service internal/stream --glob '!**/*_test.go'` 返回空

### 回滚策略

- **每个 Phase 一个独立分支**：`refactor/phase-N-<domain>`，合并到 main 后保留分支不删除。
- **单 Phase 回滚**：`git revert <merge-commit>` 即可回退单个阶段，因为每个 Phase 只涉及新增文件 + 删除旧文件，冲突概率低。
- **全量回滚**：回退到 `refactor/phase-0-scaffold` 分支状态，此时所有代码仍在 `handler/` 中。
- **禁止跨 Phase 修改旧域**：已完成拆分的域，后续 Phase 只能通过 interface 扩展（追加方法），不允许修改已有方法签名。

### 集成测试策略

当前项目缺少 API 级别的集成测试。建议在 Phase 0 搭建基础框架，后续每 Phase 同步补充：

**Phase 0 新增：**

| 文件 | 内容 |
|------|------|
| `tests/integration/setup.go` | 测试 DB 初始化（Docker MySQL 或 sqlmock DAO 注入）、Router 构造 |
| `tests/integration/helpers.go` | HTTP 请求封装（`GET`/`POST`/`PUT`/`DELETE` + 断言） |

**每 Phase 补充：**

| Phase | 测试文件 | 覆盖接口 |
|-------|---------|---------|
| 1 | `tests/integration/agent_test.go` | `GET /agent-types` |
| 2 | `tests/integration/session_test.go` | `PATCH /sessions/:id` |
| 3 | `tests/integration/diff_snapshot_test.go` | `GET/PUT /diff-snapshots/:id` |
| ... | 依此类推 | 每个 Phase 至少覆盖 CRUD 快乐路径 |

**最小化要求：** 每个 Phase 至少补充快乐路径（200 响应）和 1 个错误路径（400/404）测试。

## 预估工作量

| 阶段 | 域 | DB 调用数 | 预估时间 |
|------|-----|----------|---------|
| 0 | 脚手架 + 集成测试框架 + BizError 定义 | 0 | 1h |
| 1 | Agent | 0 | 1h |
| 2 | Session | 1 | 1.5h |
| 3 | DiffSnapshot | 2 | 1.5h |
| 4 | Announcement | 5 | 2h |
| 5 | ContactGroup | 8 | 2h |
| 6 | Skill | 15 | 4h |
| 7 | Message | 4 | 2h |
| 8 | Avatar | 2 | 1.5h |
| 9 | AgentProfile | 13 | 3h |
| 10 | Workspace | 0 | 1h |
| 11 | Stream | 12 | 4h |
| 12 | Task | 32 | 5-6h |
| 13 | Admin | 7 | 3h |
| 14 | 清理 | 0 | 2h |
| **合计** | | **~101** | **~34.5-35.5h** |

> 相比初版 +4.5-5.5h，主要用于：Phase 0 补充 BizError 类型定义 + 集成测试框架（+0.5h）、Skill 域拆分策略更细致（+1h）、AgentProfile DAO 复用关系梳理（+0.5h）、Stream 增加 StreamService + writer.go 当前 DB 触点迁移（+2h）、每 Phase 同步补充集成测试（+1-2h 分摊）。DB 调用总数经逐文件复核为 101 处（handler 86 + service 5 + stream 10），初版统计的 86 处遗漏了 admin handler 调用，且完全忽略了 stream/writer.go 的 10 处直调。
