# Wiring — 应用组装

## 实现了什么

`main.go` 作为应用入口，完成配置加载、数据库初始化、版本化 schema 迁移、Redis 连接和 HTTP Server 生命周期管理。`internal/app` 集中完成 DAO → Service → Controller 的依赖组装、中间件挂载和路由注册，将所有组件串联为可运行的 HTTP 服务。支持优雅关闭（SIGINT/SIGTERM 信号处理，worker 与 HTTP 排空共享同一有界窗口）。

## 怎么实现的

### 初始化链 (`cmd/server/main.go`)

按依赖顺序依次初始化：配置 → MySQL → 版本化 schema 迁移（`RunMigrations`，MySQL advisory lock 下执行）→ Skill 过期收据清理 → Redis → 清理残留消息 → AgentEnd client → Avatar MinIO/Local Runtime（按配置检查 MinIO Bucket）→ Artifact 私有对象存储（feature-gated，检查 Artifact Bucket）→ MinIO 技能包存储（feature-gated）→ `app.NewRouter` → 启动 Skill 操作 worker、Task 清理 worker、收据/临时目录/Artifact 失败对象定时清理 goroutine。

```go
func main() {
	cfg, err := conf.Load("configs/config.yaml")
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	if err := db.Init(&cfg.MySQL); err != nil {
		slog.Error("init db", "error", err)
		os.Exit(1)
	}

	// 版本化 schema 迁移：GET_LOCK 序列化多副本，逐版本记录 schema_migrations
	migrationCtx, cancelMigration := context.WithTimeout(appCtx, 2*time.Minute)
	if err := gormdao.RunMigrations(migrationCtx); err != nil {
		cancelMigration()
		slog.Error("run database migrations", "error", err)
		os.Exit(1)
	}
	cancelMigration()
	// 清理过期 Skill 上传收据（默认 30 天）
	gormdao.NewSkillDao().CleanupSkillUploadReceipts(time.Now().Add(-receiptRetention), 500)

	if err := redis.Init(&cfg.Redis); err != nil {
		slog.Error("init redis", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := redis.Close(); err != nil {
			slog.Warn("close redis", "error", err)
		}
	}()

	stream.CleanupStaleMessages(gormdao.NewMessageDao())
	stream.Hub.StartClosedKeysCleanup(appCtx)

	agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)
	storageRuntime, err := storage.NewRuntime(&cfg.Storage)
	if err != nil { slog.Error("init storage", "error", err); os.Exit(1) }
	if storageRuntime.MinIO != nil {
		avatarCtx, cancelAvatar := context.WithTimeout(appCtx, 2*time.Minute)
		if err := ensureAvatarStorage(avatarCtx, storageRuntime.MinIO); err != nil { cancelAvatar(); slog.Error("avatar MinIO is not ready", "error", err); os.Exit(1) }
		cancelAvatar()
	}

	// Artifact 私有对象存储（feature-gated）
	var artifactStore artifact_store.Store
	if cfg.ArtifactStorage.Enabled {
		minioStore := artifact_store.NewMinIOStore(...)        // 内置资源 MinIO 实现
		ensureArtifactStorage(ctx, minioStore)
		artifactStore = minioStore
	}

	// Skill 包私有对象存储（feature-gated）
	var skillPackageStore package_store.PackageStore
	var uploadSessionStore *skill_upload_session.Store
	operationDao := gormdao.NewSkillOperationDao()
	if cfg.SkillStorage.Enabled {
		minioStore := package_store.NewMinIOStore(...)        // MinIO 实现
		ensureSkillPackageBucket(ctx, minioStore)
		skillPackageStore = minioStore
		uploadSessionStore = skill_upload_session.New(redis.GetClient(), ...)
	}

	router := app.NewRouter(app.Dependencies{
		Config: cfg, AgentClient: agentClient,
		StorageProvider: storageRuntime.Writer, AssetReader: storageRuntime.AssetReader,
		LocalStorage: storageRuntime.Local,
		PackageStore: skillPackageStore, UploadSessionStore: uploadSessionStore,
		OperationDao: operationDao, ArtifactStore: artifactStore,
	})

	// 后台补偿 worker：处理 SkillOperationJob（install/remove/delete/migrate）
	operationWorker := serviceimpl.NewSkillOperationWorker(operationDao, gormdao.NewSkillDao(), skillPackageStore, agentClient)
	// Task 删除后的 AgentEnd session/workspace/分支清理 outbox worker
	taskCleanupWorker := serviceimpl.NewTaskCleanupWorker(gormdao.NewTaskCleanupDao(), agentClient)
	go operationWorker.Run(workerCtx)
	go taskCleanupWorker.Run(workerCtx)
	go runSkillReceiptCleanup(workerCtx, gormdao.NewSkillDao(), receiptRetention)
	go runSkillTempCleanup(workerCtx, tempRoot)
	// ArtifactStore 启用时，定时清理 failed/pending 超期对象与元数据行
	if artifactStore != nil {
		go runArtifactCleanup(workerCtx, gormdao.NewArtifactDao(), artifactStore, artifactRetention)
	}
	// ... HTTP server + 优雅关闭
}
```

`RunMigrations`（`internal/dao/gorm/migrations.go`）通过 `SELECT GET_LOCK` 序列化多副本迁移，版本成功后才写入 `schema_migrations` 记录；当前包含两个版本：`baseline_backend_schema`（历史重复 join 行清理 → 基线模型 AutoMigrate → Skill 存储元数据回填）与 `create_task_cleanup_outbox`（TaskCleanupJob 建表）。`cleanupDuplicateJoinRows` 只在旧表已存在时执行，用于在创建 `(group_id, task_id)`、`(session_id, skill_name)` 复合唯一索引前清理历史重复关联；`backfillSkillStorageMetadata` 在 MinIO 迁移期为既有 SkillHub 行补写存储元数据。五个后台 goroutine（`SkillOperationWorker` / `TaskCleanupWorker` / 收据清理 / 临时目录清理 / Artifact 失败对象清理）随服务生命周期运行，收到 SIGINT/SIGTERM 后通过 `workerCtx` 取消。

### 依赖注入

`internal/app.NewRouter(deps Dependencies)` 集中创建 DAO、Service、Controller。`Config`、`agentend_client.Client`、`storage.Provider`、`package_store.PackageStore`、`skill_upload_session.Store`、`dao.SkillOperationDao` 等外部依赖由 `main.go` 构造为 `Dependencies` 结构体传入，路由内部通过 `deps.*` 取用。Controller 构造函数只接收所需的 Service 接口或外部客户端，不再直接依赖 GORM DAO 实现：

```go
type Dependencies struct {
	Config             *conf.Config
	AgentClient        *agentend_client.Client
	StorageProvider    storage.Provider
	AssetReader        storage.ObjectReader
	LocalStorage       *storage.LocalStorage
	PackageStore       package_store.PackageStore          // MinIO 技能包存储（feature-gated）
	UploadSessionStore *skill_upload_session.Store          // Redis 上传会话
	OperationDao       dao.SkillOperationDao                // SkillOperationJob outbox
	ArtifactStore      artifact_store.Store                 // 内置资源 Artifact 私有存储（feature-gated）
}

// main.go 中构造外部依赖并注入
agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)
storageRuntime, err := storage.NewRuntime(&cfg.Storage)
router := app.NewRouter(app.Dependencies{
    Config:             cfg,
    AgentClient:        agentClient,
    StorageProvider:    storageRuntime.Writer,
    AssetReader:        storageRuntime.AssetReader,
    LocalStorage:       storageRuntime.Local,
    PackageStore:       skillPackageStore,
    UploadSessionStore: uploadSessionStore,
    OperationDao:       operationDao,
    ArtifactStore:      artifactStore,
})

// NewRouter 内部（deps.* 即上面注入的依赖）
sessionDao := gormdao.NewSessionDao()
taskDao := gormdao.NewTaskDao()
messageDao := gormdao.NewMessageDao()
diffSnapshotDao := gormdao.NewDiffSnapshotDao()

sessionService := impl.NewSessionService(sessionDao)
taskService := impl.NewTaskService(taskDao, sessionDao, messageDao, diffSnapshotDao, deps.AgentClient)
taskService.SetArtifactLifecycle(gormdao.NewArtifactDao(), deps.ArtifactStore)
messageService := impl.NewMessageService(taskDao, sessionDao, messageDao)

// ArtifactService 仅在 ArtifactStore 配置启用时创建，并作为 ArtifactCapabilityIssuer
// 注入 TaskService，使其在 Run 流程中为 AgentEnd 签发短期上传 capability token。
var artifactService service.ArtifactService
if deps.ArtifactStore != nil && cfg.ArtifactStorage.Enabled {
	artifactService = impl.NewArtifactService(gormdao.NewArtifactDao(), messageDao, deps.ArtifactStore, impl.ArtifactServiceConfig{...})
	taskService.SetArtifactCapabilityIssuer(artifactService)
}

// SkillService 在 MinIO 启用时注入 PackageStore / UploadSessionStore / OperationDao，
// 并按 cfg.SkillStorage 配置 ZIP 限制、内容扫描策略、校验并发与超时等
skillService := impl.NewSkillService(skillDao, sessionDao, deps.AgentClient, deps.PackageStore)

taskController := ctrlimpl.NewTaskController(taskService, deps.AgentClient)
agentController := ctrlimpl.NewAgentController()
sessionController := ctrlimpl.NewSessionController(sessionService)
messageController := ctrlimpl.NewMessageController(messageService)
workspaceController := ctrlimpl.NewWorkspaceController(deps.AgentClient)
```

以 `TaskController` 为例，Controller 只保存业务接口：

```go
func NewTaskController(taskService service.TaskService, agentClient *agentend_client.Client) *TaskController {
	return &TaskController{service: taskService, agentClient: agentClient}
}
```

外部依赖说明：

| Controller | 外部依赖 | 说明 |
|------------|---------|------|
| TaskController | `TaskService` + `agentend_client.Client` | run/review 等业务操作走 `TaskService`（内部注入 `agentend_client.Client`）；`validate-repo-path`、`init-git-repo` 直接通过 Controller 持有的 `agentend_client.Client` 转发 |
| AvatarController | `AvatarService`（内部注入 `storage.Provider`） | 头像上传（MinIO 默认、本地显式选择）；上传接收请求 Context，Controller 只持有 Service |
| AssetController | `storage.ObjectReader` | 匿名 GET/HEAD `/api/assets/avatars/*path`，严格解析 UUID/扩展名并代理私有 MinIO 对象 |
| AgentProfileController | `AgentProfileService`（内部注入 `agentend_client.Client`） | Agent 详情 / SOUL.md 读写；Controller 只持有 Service，不直接依赖 agentend_client |
| WorkspaceController | `agentend_client.Client`（直接持有，无 Service 层） | 代理工作区操作到 AgentEnd，并持有 `*http.Client` 用于流式合并预览 |
| AnnouncementController | `AnnouncementService`（内部注入 `agentend_client.Client`） | 公告管理；Controller 只持有 Service，不直接依赖 agentend_client |
| SkillController | `SkillService`（内部注入 `agentend_client.Client`、`package_store.PackageStore`、`skill_upload_session.Store`、`dao.SkillOperationDao`） | 技能上传/确认/导入/删除；MinIO 启用走对象存储，否则 DB blob 兼容路径；写接口在 `require_admin=true` 时叠加 Admin JWT |
| ArtifactController | `ArtifactService`（内部注入 `dao.ArtifactDao`、`dao.MessageDao`、`artifact_store.Store`） | AgentEnd 凭 capability token 直传内置资源；feature-gated，未启用时不挂路由 |
| AdminController | `Config` + `AdminService` | 认证/头像/代理 |
| 其余 Controller | 无 | Session、Message、Agent、Stream、DiffSnapshot、ContactGroup |

### 中间件

```go
r := gin.New()
r.Use(middleware.Logger())
r.Use(middleware.CORS(cfg.CORS.AllowOrigins))
r.Use(gin.Recovery())

// Serve local uploads when local reading is enabled, including historical
// files while MinIO is the active writer.
if local := storageRuntime.Local; local != nil {
    r.StaticFS(local.URLPrefix(), gin.Dir(local.Dir(), false))
}

publicAssets := r.Group("/api/assets")
publicAssets.Use(middleware.NewIPRateLimiter(120, time.Minute).Middleware())
assetController.RegisterRoutes(publicAssets)
```

`/api` 路由组额外挂载 `middleware.JSONBodyLimit(1<<20, "/api/workspace")`：普通 JSON / `+json` 请求体最大 1MB；workspace 代理路由跳过该限制，继续使用代理层自己的 25MB 上限。

当 `cfg.Auth.Enabled` 为 true 时，`/api` 路由组还会挂载 `AuthWithSkips`。普通业务 API 需要 Bearer JWT；`/api/admin/auth`、`/api/admin/health`、`/api/admin/avatar` 保持公开，其中受保护的 admin 写接口仍由 `AdminAuth` 二次校验。只有 `GET .../stream` SSE 路由可以通过 `access_token` query 传递同一 JWT，兼容浏览器 EventSource。

CORS 配置从 `config.yaml` 的 `cors.allow_origins` 字段加载，默认允许 `http://localhost:5173`：

```go
func CORS(origins []string) gin.HandlerFunc {
	if len(origins) == 0 {
		origins = []string{"http://localhost:5173"}
	}
	cfg := cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	for _, origin := range origins {
		if origin == "*" {
			cfg.AllowAllOrigins = true
			cfg.AllowCredentials = false
			return cors.New(cfg)
		}
	}
	cfg.AllowOrigins = origins
	return cors.New(cfg)
}
```

当 `cors.allow_origins` 配置为 `*` 时，后端使用 allow-all origins 模式并关闭 credentials；配置为具体 origin 白名单时才允许 credentials。

### 路由注册

每个 Controller 通过 `RegisterRoutes(api)` 自注册路由，替代旧版 main.go 中的手动注册：

```go
// /api/assets 公开读取组（不走 JWT，IP 限流 120/分钟）
publicAssets := r.Group("/api/assets")
publicAssets.Use(middleware.NewIPRateLimiter(120, time.Minute).Middleware())
assetController.RegisterRoutes(publicAssets)

// /api/internal 服务间组：AgentEnd 凭 capability token 上传 artifact，
// 以及 task/stream/announcement/message/skill 的内部读写端点。
// AgentEnd service auth 启用时叠加 ServiceAuth（校验 BACKEND_SERVICE_TOKEN）。
internalArtifacts := r.Group("/api/internal")
artifactController.RegisterUploadRoutes(internalArtifacts)
internalRuns := r.Group("/api/internal")
if cfg.AgentEnd.ServiceAuthEnabled {
	internalRuns.Use(middleware.ServiceAuth(os.Getenv("BACKEND_SERVICE_TOKEN")))
}
taskController.RegisterInternalRoutes(internalRuns)
streamController.RegisterInternalRoutes(internalRuns)
announcementController.RegisterInternalReadRoutes(internalRuns)
messageController.RegisterInternalReadRoutes(internalRuns)
skillController.RegisterInternalRoutes(internalRuns)

api := r.Group("/api")
api.Use(middleware.JSONBodyLimit(1<<20, "/api/workspace"))
if cfg.Auth.Enabled {
    api.Use(middleware.AuthWithSkips(
        cfg.JWT.Secret,
        "/api/admin/auth",
        "/api/admin/health",
        "/api/admin/avatar",
    ))
}
{
	taskController.RegisterRoutes(api)
	streamController.RegisterRoutes(api)
	messageController.RegisterRoutes(api)

	agentController.RegisterRoutes(api)

	announcementController.RegisterRoutes(api)

	sessionController.RegisterRoutes(api)
	avatarController.RegisterRoutes(api)
	agentProfileController.RegisterRoutes(api)

	diffSnapshotController.RegisterRoutes(api)

	contactGroupController.RegisterRoutes(api)

	// require_admin=true 时，Skill 写接口（upload/confirm/delete）叠加 AdminAuth；
	// 读/导入/移除/内置上报仍走普通 /api 路由
	if cfg.SkillStorage.RequireAdmin {
		skillController.RegisterRoutesWithManagerAuth(api, middleware.AdminAuth(cfg.JWT.Secret))
	} else {
		skillController.RegisterRoutes(api)
	}
	workspaceController.RegisterRoutes(api)
}

adminController.RegisterRoutes(api)
artifactController.RegisterRoutes(api)   // GET /artifacts/:resourceId[/content]；service 为 nil（未启用）时内部直接不挂路由
```

健康检查端点：

```go
r.GET("/ping", func(c *gin.Context) {
	vo.OK(c, gin.H{"message": "pong"})
})

r.GET("/health", func(c *gin.Context) {
	vo.OK(c, gin.H{"status": "ok"})
})

// /ready 探测各 feature-gated 存储：Avatar AssetReader.Health、ArtifactStore.Health、
// Skill PackageStore.Health（各 3s 超时）；任一未就绪返回 503，供部署层判断流量就绪条件。
r.GET("/ready", func(c *gin.Context) {
	if cfg.Storage.MinIO.Enabled {
		deps.AssetReader.Health(ctx) // avatar 存储
	}
	if cfg.ArtifactStorage.Enabled {
		deps.ArtifactStore.Health(ctx)
	}
	if cfg.SkillStorage.Enabled {
		checker.(interface{ Health(context.Context) error }).Health(ctx)
	}
	vo.OK(c, gin.H{"status": "ready"})
})
```

HTTP 服务监听端口来自 `config.yaml` 的 `server.port`，也可用 `SERVER_PORT` 覆盖；未配置时默认 `8080`。

### 优雅关闭

使用 `signal.NotifyContext` + `http.Server` 实现 15 秒优雅关闭：先停止后台 worker 领取新任务，再排空 HTTP 请求，最后等待 worker 退出（与 HTTP 排空共享同一个 15 秒窗口）：

```go
appCtx, stopApp := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stopApp()

addr := ":" + fmt.Sprint(cfg.Server.Port)
srv := &http.Server{Addr: addr, Handler: r,
	ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 1 << 20}

serverErr := make(chan error, 1)
go func() {
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		serverErr <- err
	}
}()

var runErr error
select {
case <-appCtx.Done():
case err := <-serverErr:
	runErr = fmt.Errorf("serve http: %w", err)
}
slog.Info("shutting down server...")
// 在排空 HTTP 请求前先停止领取新的持久化操作；进行中的存储/AgentEnd
// 调用收到取消信号，worker 与 HTTP server 共享同一有界关闭窗口。
stopWorker()

// 给未完成的请求 15 秒处理时间，然后强制关闭
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()
if err := srv.Shutdown(ctx); err != nil {
	slog.Error("server forced to shutdown", "error", err)
}

workersDone := make(chan struct{})
go func() {
	workerWG.Wait()
	close(workersDone)
}()
select {
case <-workersDone:
case <-ctx.Done():
	slog.Warn("background workers did not stop before server shutdown deadline")
}

slog.Info("server exited")
```
