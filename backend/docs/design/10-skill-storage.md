# Skill Storage — 技能包私有对象存储与生命周期

## 实现了什么

external Skill 的 ZIP 包存储从数据库 `Content` blob 迁移到 MinIO 私有对象存储（feature-gated，`skill_storage.enabled`）。完整链路包含：multipart 分片直传 + Redis 上传会话（断点确认）、`incoming/` 暂存 → `skills/{name}/{sha256}.zip` 正式对象的原子 Promote、`SkillOperationJob` 持久化 Outbox 补偿（对象删除 / AgentEnd 安装 / 移除 / 迁移 / 对象校验）、`SkillAuditEvent` 只追加审计，以及 `cmd/skill-migrate` / `cmd/skill-reconcile` 两个独立运维工具。未启用 MinIO 时保留 DB blob 兼容路径（临时目录 + `Content` 字段落库）。

数据模型（`SkillHub` / `AgentSkill` / `SkillUploadReceipt` / `SkillOperationJob` / `SkillAuditEvent`）见 [01-models.md](01-models.md)，配置段见 [04-config.md](04-config.md)，路由与 AdminAuth 门控见 [02-handlers.md](02-handlers.md)，装配见 [05-wiring.md](05-wiring.md)。

## 怎么实现的

### Service 接口 (`internal/service/service.go`)

```go
type SkillService interface {
	UploadSkill(ctx context.Context, filename string, zipData []byte) (*ValidationResult, error)
	UploadSkillFile(ctx context.Context, filename, path string, size int64) (*ValidationResult, error)
	SkillUploadLimit() int64
	ConfirmSkill(ctx context.Context, name, description string, fileCount int, totalSize int64, tmpDir string) (*SkillImportResult, error)
	ListSkills() ([]SkillHubItem, error)
	DeleteSkill(ctx context.Context, name string) error
	ImportSkill(ctx context.Context, skillName, sessionID string) (*SkillImportResult, error)
	RemoveSkill(ctx context.Context, skillName, sessionID string) (*SkillImportResult, error)
	ReportBuiltinSkills(skills []BuiltinSkillItem) error
}
```

`UploadSkill` 接收内存字节（传统路径），`UploadSkillFile` 接收已落盘的临时文件（managed 组 `POST /skills/upload` 的 multipart 分片路径）；`SkillUploadLimit` 返回 `max_upload_size` 供 Controller 预检。

### 对象存储抽象 (`pkg/package_store/store.go`)

```go
var (
	ErrNotFound       = errors.New("package object not found")
	ErrIntegrity      = errors.New("package object integrity mismatch")
	ErrTargetConflict = errors.New("package object target conflict")
)

type ObjectInfo struct {
	Key          string
	Size         int64
	SHA256       string
	LastModified time.Time
}

// PackageStore is the private object storage boundary for external Skill packages.
type PackageStore interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, sha256 string) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Stat(ctx context.Context, key string) (*ObjectInfo, error)
	Promote(ctx context.Context, sourceKey, targetKey string, expected ObjectInfo) error
	List(ctx context.Context, prefix, cursor string, limit int) (items []ObjectInfo, nextCursor string, err error)
	Delete(ctx context.Context, key string) error
}
```

- `MinIOStore`（`minio.go`）为生产实现，`memory.go` 为内存 mock；`Put` 要求调用方提供预期 SHA256，`Promote` 校验 `expected`（size + SHA256）后原子换名，目标已存在且内容一致时幂等成功、不一致返回 `ErrTargetConflict`
- 对象键布局：暂存 `incoming/{uploadID}.zip` → 正式 `skills/{name}/{sha256}.zip`（`skill_service.go` 中 `finalKey := "skills/" + name + "/" + actualSHA + ".zip"`），`validateObjectKey` 拒绝空键、绝对路径、反斜杠与 `.` / `..` 段

### Redis 上传会话 (`pkg/skill_upload_session/store.go`)

```go
type Session struct {
	UploadID          string    `json:"upload_id"`
	State             string    `json:"state"`
	OwnerID           string    `json:"owner_id,omitempty"`
	ObjectKey         string    `json:"object_key"`
	Name              string    `json:"name"`
	Description       string    `json:"description,omitempty"`
	SHA256            string    `json:"sha256"`
	FileCount         int       `json:"file_count"`
	TotalSize         int64     `json:"total_size"`
	PackageSize       int64     `json:"package_size"`
	ConfirmedSkillID  uint      `json:"confirmed_skill_id,omitempty"`
	ConfirmLeaseUntil time.Time `json:"confirm_lease_until,omitempty"`
	ConfirmToken      string    `json:"-"`
}

type Options struct {
	TTL             time.Duration // 默认 15m
	Lease           time.Duration // 默认 2m
	ResultRetention time.Duration // 默认 30d
}
```

- 会话键 `skill:upload:{uploadID}`（`Store.Key`）；确认流程用 Lua 脚本实现租约状态机：`BeginConfirm`（领取确认租约 + 发放 ConfirmToken）→ `RenewConfirm`（续租）→ `MarkConfirmed` / `MarkPending` / `MarkFailed`（带 token 校验的状态迁移）
- 确认成功后写 `SkillUploadReceipt`（MySQL），Redis 会话丢失时仍可按 `upload_id` 幂等返回确认结果；过期收据由 `SkillDao.CleanupSkillUploadReceipts` 定期清理（main.go 每小时一批，每批 500，保留期 `receipt_retention` 默认 720h）

### Outbox 补偿 (`internal/dao/skill_operation_dao.go` + `internal/service/impl/skill_operation_worker.go`)

```go
type SkillOperationDao interface {
	CreateSkillOperationJob(job model.SkillOperationJob) (*model.SkillOperationJob, error)
	ClaimSkillOperationJob(id uint64, now time.Time, lease time.Duration) (*model.SkillOperationJob, error)
	ClaimDueSkillOperationJob(now time.Time, lease time.Duration) (*model.SkillOperationJob, error)
	CompleteSkillOperationJob(id uint64, leaseToken string) error
	RetrySkillOperationJob(id uint64, leaseToken, lastError string, nextRetryAt time.Time) error
	DeleteSkillOperationJob(id uint64, leaseToken string) error
	HasPendingObjectOperation(objectKey string) (bool, error)
}
```

```go
type SkillOperationWorker struct {
	jobs           dao.SkillOperationDao
	skillDao       dao.SkillDao
	packageStore   package_store.PackageStore
	agentClient    skillAgentClient
	poll           time.Duration
	opTimeout      time.Duration
	readPreference string
	zipLimits      service.ZipLimits
	orphanGrace    time.Duration
	incomingTTL    time.Duration
	cleanupEvery   time.Duration
	nextCleanup    time.Time
}
```

- `Run` 以 2 秒轮询调用 `RunOnce`：`ClaimDueSkillOperationJob` 按 `idx_skill_jobs_due` 领取到期任务（租约 + LeaseToken 防多实例重复执行），按 `Operation` 分派五类操作：`delete_object`（删 Skill 后清 MinIO 对象）、`install` / `remove`（AgentEnd 侧安装/卸载补偿，推进 `AgentSkill.Status`）、`migrate`（DB blob → MinIO 迁移）、`verify_object`（完整性复检）
- 失败走 `RetrySkillOperationJob` 退避重试（`retryDelay` 按 `Attempts` 递增）；`AgentSkillID` 字段对 install/remove 重试做围栏，防止命中同 `(session_id, skill_name)` 的后续新关联
- 周期清理（默认每 5 分钟）：`CleanupStaleFormal` 将超过 `orphan_grace_period`（默认 48h）无元数据引用的正式对象登记为 `formal-orphan-delete` outbox 后删除；`CleanupStaleIncoming` 删除超过 `incoming_ttl`（默认 24h）的暂存对象
- Service 侧 `ImportSkill` / `RemoveSkill` / `DeleteSkill` / `ConfirmSkill` 写入 outbox 任务即返回，跨 AgentEnd 与对象存储的最终一致由 worker 收敛；每次动作追加 `SkillAuditEvent`

### 迁移与对账工具（`cmd/`）

**skill-migrate** — DB blob → MinIO 批量迁移：

```go
flag.StringVar(&f.config, "config", "configs/config.yaml", "backend config path")
flag.IntVar(&f.batch, "batch-size", 10, "number of metadata rows per batch")
flag.BoolVar(&f.dryRun, "dry-run", false, "validate and report without writing MinIO or MySQL")
flag.BoolVar(&f.resume, "resume", false, "resume from the cursor file")
flag.StringVar(&f.name, "skill-name", "", "only migrate one Skill name")
flag.BoolVar(&f.verify, "verify-only", false, "verify MinIO objects and database hashes")
flag.BoolVar(&f.reverse, "reverse-to-db", false, "read MinIO objects back into the BLOB column")
flag.BoolVar(&f.clear, "clear-content", false, "clear verified migrated shadow BLOBs (requires explicit acknowledgement)")
flag.StringVar(&f.clearAck, "confirm-clear-content", "", "required acknowledgement: CLEAR-SKILL-BLOBS")
flag.StringVar(&f.cursorFile, "cursor-file", ".skill-migrate.cursor", "cursor file used by --resume")
```

支持 dry-run、断点续跑（cursor 文件）、单 Skill 迁移（`migrateOneWithJob` 走 outbox）与 `--reverse-to-db` 回滚；清空 shadow blob 需显式 `--confirm-clear-content CLEAR-SKILL-BLOBS` 确认。

**skill-reconcile** — 存储与元数据对账：

```go
flag.StringVar(&opt.config, "config", "configs/config.yaml", "backend config path")
flag.IntVar(&opt.batch, "batch-size", 100, "metadata/object page size")
flag.BoolVar(&opt.repair, "repair", false, "delete only confirmed stale orphan/incoming objects")
flag.BoolVar(&opt.verify, "verify", true, "verify referenced object size and SHA-256")
flag.BoolVar(&opt.verifyShadow, "verify-shadow", true, "compare non-empty legacy Content shadow copies with MinIO")
```

`verifyReceipts` 校验收据、`verifyReferenced` 复检引用对象的 size/SHA256（不一致登记 `verify_object` outbox 并标 `storage_error`）、`verifyShadowCopies` 比对双写期 shadow blob、`reconcileObjects` 在 `--repair` 下仅删除确认过期的孤儿/暂存对象并写审计。

### 与服务生命周期的关系

`cmd/server/main.go` 在 `skill_storage.enabled` 时创建 `package_store.MinIOStore`（等待 Bucket 就绪，不创建 Bucket）与 `skill_upload_session.Store`，随 `app.Dependencies` 注入 `SkillService`；`SkillOperationWorker` 作为后台 goroutine 随服务运行，SIGINT/SIGTERM 后经 `workerCtx` 取消（见 [05-wiring.md](05-wiring.md)）。`read_preference=minio|db` 控制读取来源，`shadow_write_blob=true` 时迁移期双写 DB blob 以便回滚；`MinIO_ACCESS_KEY` / `MINIO_SECRET_KEY` 作为共享应用账号会回填留空的 Avatar / Artifact 专用凭据（见 [04-config.md](04-config.md)）。
