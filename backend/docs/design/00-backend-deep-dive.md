# Backend Deep Dive — 后端阅读入口

## 实现了什么

Backend 是 AgentHub 的 Go 控制面：接收前端 API 请求，保存 Task / Session / Message / Skill 等业务状态，决定 Agent 路由，把 AgentEnd 的 SSE 输出中转给前端，并通过 MySQL / Redis 保证刷新、断线和服务重启后的恢复能力。

本文只保留全景地图，避免和 01-07 拆分文档重复维护。具体实现以对应专题文档为准。

## 怎么实现的

### 后端边界

| Backend 负责 | Backend 不负责 |
|--------------|----------------|
| HTTP API、认证、中间件、统一响应 | 直接运行 Claude / OpenCode / Codex CLI |
| Controller → Service → DAO 业务分层 | 在 Controller 中写 GORM 查询细节 |
| Task / Session / Message / Skill / ContactGroup / Admin 持久化 | 维护前端 Zustand / React Query 状态 |
| Agent 路由与 AgentEnd HTTP 调用 | 解析所有 CLI 原始协议细节 |
| SSE 中转、RuntimeHub、Redis Stream、MySQL 刷写 | 直接编辑 Git Worktree 文件 |
| Workspace 文件、diff、commit、preview 代理 | 作为通用 Agent Runtime |

### 启动链路

```text
cmd/server/main.go
  -> internal/conf 读取 config.yaml + .env overlay（含 SkillStorage 安全默认值）
  -> pkg/db 初始化 MySQL
  -> dao/gorm 清理历史 join 表重复数据
  -> GORM AutoMigrate 模型（含 SkillUploadReceipt / SkillOperationJob / SkillAuditEvent / Artifact）
  -> dao/gorm 回填 Skill 存储元数据 + 清理过期上传收据
  -> pkg/redis 初始化 Redis
  -> internal/stream 清理遗留 streaming 消息
  -> pkg/agentend_client 创建 AgentEnd client
  -> pkg/storage 组装 MinIO 默认/本地可选的头像 Runtime，并检查 Asset Bucket
  -> pkg/artifact_store 初始化内置资源私有对象存储（feature-gated，检查 Artifact Bucket）
  -> pkg/package_store + pkg/skill_upload_session 初始化 MinIO 技能包存储（feature-gated）
  -> internal/app.NewRouter 组装 DAO / Service / Controller（含 SkillService / ArtifactService 注入）
  -> 启动 SkillOperationWorker + 收据/临时目录/Artifact 失败对象定时清理 goroutine
  -> Gin 挂载 middleware 与 /api 路由
  -> HTTP server 启动并监听 SIGINT / SIGTERM 优雅关闭（worker 通过 ctx 同步取消）
```

### 分层与专题文档

| 专题 | 文档 | 权威代码 |
|------|------|----------|
| 数据模型 | `01-models.md` | `internal/model/` |
| Controller / Service / DAO | `02-handlers.md` | `internal/controller/`, `internal/service/`, `internal/dao/` |
| SSE 流式中转 | `03-stream.md` | `internal/stream/` |
| 配置加载 | `04-config.md` | `internal/conf/`, `configs/config.yaml` |
| 应用组装 | `05-wiring.md` | `internal/app/`, `cmd/server/main.go` |
| 消息分页 | `06-message-pagination.md` | `internal/service/impl/message_service.go` |
| Admin API | `07-admin-api.md` | `internal/controller/impl/admin_controller.go` |
| Artifact 存储 | `08-artifact-storage.md` | `internal/model/artifact.go`, `pkg/artifact_store/`, `internal/service/impl/artifact_service.go` |
| 分层重构历史 | `layered-refactoring.md` | 仅历史参考 |

### 关键代码入口

```text
backend/
├── cmd/server/main.go
├── cmd/skill-migrate/        # Skill 对象存储迁移独立工具
├── cmd/skill-reconcile/      # Skill 状态对账独立工具
├── internal/app/
├── internal/controller/
├── internal/service/
├── internal/dao/
├── internal/stream/
├── internal/model/
├── internal/middleware/
├── internal/generated/
└── pkg/                      # agentend_client / db / redis / storage / artifact_store / package_store / skill_upload_session
```

读代码建议顺序：

1. `cmd/server/main.go`：看启动顺序和优雅关闭。
2. `internal/app/`：看依赖注入和路由装配。
3. `internal/controller/controller.go`：看 Controller 统一注册接口。
4. `internal/service/service.go`：看业务接口和 DTO。
5. `internal/dao/dao.go`：看 DAO 接口边界。
6. `internal/stream/`：看 AgentEnd SSE 如何转成前端 SSE 与持久化消息。

### 与三端的关系

| 对象 | 关系 |
|------|------|
| Frontend | Backend 暴露 `/api`，前端通过 REST + SSE 消费业务状态 |
| AgentEnd | Backend 调 AgentEnd `/v1/agent/stream`、workspace、skills、resources 等接口 |
| Contracts | Backend 使用 `internal/generated/` 中由 `contracts/schemas/` 生成的类型 |
| MySQL | 业务状态最终恢复来源 |
| Redis | SSE 断线补偿与流式中转辅助通道 |

### 维护规则

1. 后端实现细节只写一次：优先放在 01-07 专题文档。
2. 本文新增内容应是阅读路径或边界说明，不复制模型字段、路由大表、代码片段。
3. API 端点变化时，同步更新 `02-handlers.md` 或未来的 reference API 文档。
4. 分层变更时，同步更新 `05-wiring.md` 和 `layered-refactoring.md` 的历史说明。
