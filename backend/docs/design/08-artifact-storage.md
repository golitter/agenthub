# Artifact Storage — AgentEnd 内置资源私有对象存储

## 实现了什么

为 AgentEnd 执行产物（当前为渲染 HTML 等内置资源）提供一条**控制面签发能力 + 数据面直传**的私有对象存储链路：Backend 用独立 `capability_secret` 签发短期 JWT capability token，AgentEnd 凭 token 直接把对象 PUT 到私有 Artifact MinIO 桶，元数据落在 `Artifact` 表；前端通过 Backend 代理读取内容。它与头像存储（`pkg/storage`）和 Skill 包存储（`pkg/package_store`）保持独立配置段和独立 Bucket，但允许复用同一个 MinIO 应用账号。

整套链路 feature-gated：`cfg.ArtifactStorage.Enabled=false` 时不创建 store、不装配 `ArtifactService`、`ArtifactController` 不挂任何路由。

## 怎么实现的

### 数据模型 (`internal/model/artifact.go`)

`Artifact` 是 message 与私有桶中一个不可变对象之间的可信元数据链接：

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

- `Kind`：资源类型，目前仅 `html`（`ArtifactKindHTML`）
- `Status`：`pending` → `ready` / `failed`；删除路径 `deleting` → `deleted`
- `(MessageID, IdempotencyKey)`：复合唯一索引，支持 AgentEnd 重传幂等
- `ObjectKey` / `SHA256`：私有桶对象键与完整性校验值，**不暴露给前端**

### 对象存储抽象 (`pkg/artifact_store/store.go`)

```go
type Store interface {
    Put(ctx context.Context, key string, body io.Reader, size int64, options PutOptions) error
    Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
    Stat(ctx context.Context, key string) (ObjectInfo, error)
    Delete(ctx context.Context, key string) error
    Health(ctx context.Context) error
}
```

- `MinIOStore`（`minio.go`）为生产实现；`MemoryStore`（`memory.go`）用于单测
- 接口刻意不含公开 URL 或 Bucket 管理职责——所有读取经 Backend 代理
- `ValidateObjectKey` 拒绝空、绝对路径、控制字符、`.` / `..` 段，防止逃逸

### DAO 接口 (`internal/dao/artifact_dao.go`)

```go
var ErrArtifactQuota = errors.New("artifact quota exceeded")

type ArtifactDao interface {
    CreatePending(artifact *model.Artifact, maxObjects int64) error
    MarkReady(resourceID string, size int64, sha256 string) error
    MarkFailed(resourceID, message string) error
    FindReadyByResourceID(resourceID string) (*model.Artifact, error)
    FindByResourceID(resourceID string) (*model.Artifact, error)
    FindByMessageAndIdempotency(messageID, key string) (*model.Artifact, error)
    CountByMessageID(messageID string) (int64, error)
    ListObjectKeysByTaskID(taskID string) ([]model.Artifact, error)
    MarkDeletingByTaskID(taskID string) ([]model.Artifact, error)
    MarkDeleteFailed(resourceID, message string) error
    DeleteRow(resourceID string) error
    ListStalePendingOrFailed(before time.Time, limit int) ([]model.Artifact, error)
}
```

`CreatePending` 在事务中计数该 message 的 artifact 数量，超过 `maxObjects` 返回 `ErrArtifactQuota`，实现每消息配额。

### Service 层 (`internal/service/service.go` + `internal/service/impl/artifact_service.go`)

```go
// 能力签发：TaskService 在 Run 流程中据此为 AgentEnd 签发短期 token
type ArtifactCapabilityIssuer interface {
    IssueUploadToken(taskID, sessionID, messageID string) (string, error)
}

type ArtifactService interface {
    Upload(ctx context.Context, token, kind, filename string, body io.Reader, size int64) (*ArtifactInfo, error)
    Get(resourceID string) (*model.Artifact, error)
    Open(ctx context.Context, resourceID string) (io.ReadCloser, *model.Artifact, error)
}

// Controller 在解析大 body 前用以提前拒绝无效 capability（关闭 TOCTOU 窗口）
type ArtifactCapabilityValidator interface {
    ValidateUploadCapability(ctx context.Context, token string) error
}

// 幂等上传：Upload 之后由 Controller 探测此接口
type IdempotentArtifactUploader interface {
    UploadWithIdempotency(ctx context.Context, token, kind, filename, idempotencyKey string, body io.Reader, size int64) (*ArtifactInfo, error)
}
```

**Capability token** — 用 `golang-jwt/jwt/v5` 以 `capability_secret`（≥32 字符）签发的 HS256 JWT：

```go
claims := jwt.MapClaims{
    "aud":         "artifact-upload",
    "jti":         jti,           // 随机 UUID，单次使用
    "task_id":     taskID,
    "session_id":  sessionID,
    "message_id":  messageID,
    "max_bytes":   svc.maxBytes,
    "max_objects": svc.maxPerMessage,
    "iat":         now.Unix(),
    "exp":         now.Add(svc.tokenTTL).Unix(),  // 默认 30m
}
```

`capability_secret` 不得复用 `jwt.secret` / `admin.password` / 头像或 Skill 的 `secret_key`，启动校验强制隔离。

**Upload 流程** — `ValidateUploadCapability` → `ArtifactDao.CreatePending`（含每消息配额）→ `store.Put`（写入私有桶）→ 读回 size/SHA256 → `MarkReady`。命中 `(message_id, idempotency_key)` 或 `Idempotency-Key` header 时直接返回既有结果，避免 AgentEnd 重传产生重复对象。

### Controller 层 (`internal/controller/impl/artifact_controller.go`)

```go
type ArtifactController struct {
    service       service.ArtifactService
    maxObjectSize int64
}
```

路由分两组挂载，上传刻意放在用户 JWT 组之外，只接受 capability token：

```
--- RegisterUploadRoutes（/api/internal，Bearer capability token）---
POST   /artifacts                    Upload（multipart：kind + 单文件，body 上限 maxObjectSize+1MiB）

--- RegisterRoutes（/api）---
GET    /artifacts/:resourceId         GetMetadata（元数据，不含 ObjectKey 等内部字段）
GET    /artifacts/:resourceId/content GetContent（inline 内容，带 CSP / nosniff / immutable ETag）
HEAD   /artifacts/:resourceId/content GetContent（仅元数据头）
```

- `Upload` 先用 `MaxBytesReader` 限定整个 multipart 请求大小，单文件部分由 Service 读出后复核 size 与 SHA256
- `GetContent` 设置严格 CSP（`default-src 'none'; style-src 'unsafe-inline'; img-src data: https:; script-src 'none'; frame-ancestors 'self'`）、`X-Content-Type-Options: nosniff`、`Cache-Control: private, max-age=31536000, immutable`，并支持 `If-None-Match` ETag 协商
- 对外只投影 `ArtifactInfo`（ResourceID / Kind / Filename / ContentType / Size / SHA256 / CreatedAt），`ObjectKey` 等存储内部字段永不返回

### 装配与生命周期 (`internal/app/app.go` + `cmd/server/main.go`)

```go
// app.NewRouter：仅在 ArtifactStore 启用时创建 Service，并作为 capability issuer 注入 TaskService
taskService.SetArtifactLifecycle(gormdao.NewArtifactDao(), deps.ArtifactStore)
if deps.ArtifactStore != nil && cfg.ArtifactStorage.Enabled {
    artifactService = impl.NewArtifactService(gormdao.NewArtifactDao(), messageDao, deps.ArtifactStore, impl.ArtifactServiceConfig{...})
    taskService.SetArtifactCapabilityIssuer(artifactService)
}
```

- 启动时 `ensureArtifactStorage` 仅等待 Bucket 与应用凭据就绪，**绝不创建或修改 Bucket**（由部署层 init job 负责）
- `/ready` 在 `artifact_storage.enabled` 时探测 `ArtifactStore.Health`（3s 超时），未就绪返回 503
- task 删除时由 `TaskService.SetArtifactLifecycle` 钩子调 `ArtifactDao.MarkDeletingByTaskID`，对象清理由后台 `runArtifactCleanup` goroutine（15 分钟轮询，按 `failed_retention` 清理 stale pending/failed）异步完成，不在 `DeleteTaskCascade` 事务内

### 配置 (`internal/conf/conf.go`，`artifact_storage` 段)

```go
type ArtifactStorageConfig struct {
    Enabled            bool   `yaml:"enabled"`
    Endpoint           string `yaml:"endpoint"`
    Bucket             string `yaml:"bucket"`
    AccessKey          string `yaml:"access_key"`
    SecretKey          string `yaml:"secret_key"`
    UseSSL             bool   `yaml:"use_ssl"`
    CAFile             string `yaml:"ca_file"`
    RequestTimeout     string `yaml:"request_timeout"`      // 默认 15s
    MaxObjectSize      string `yaml:"max_object_size"`      // 上限 25MiB
    MaxArtifactsPerMsg int    `yaml:"max_artifacts_per_message"` // 默认 20，上限 1000
    UploadTokenTTL     string `yaml:"upload_token_ttl"`     // 默认 30m
    CapabilitySecret   string `yaml:"capability_secret"`    // ≥32 字符
    FailedRetention    string `yaml:"failed_retention"`     // 默认 24h
}
```

环境变量覆盖见 [04-config.md](04-config.md) 的 `ARTIFACT_*` / `ARTIFACT_MINIO_*` 表。生产模式（`APP_ENV=production` 或 `GIN_MODE=release`）下启用时强制 `use_ssl=true` 且 endpoint 不得使用 `http://`。
