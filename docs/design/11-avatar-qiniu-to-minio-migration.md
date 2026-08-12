# 11 — 头像存储改造（移除七牛，MinIO 优先、本地可选）

> **状态**：✅ 已实现（代码、配置模板、Docker 初始化与 Backend 单元测试已接入；真实 MinIO/浏览器手工验收需在部署环境执行）
>
> **日期**：2026-08-11
>
> **关联文档**：[10-skills-minio-storage-migration.md](10-skills-minio-storage-migration.md)、[Docker 部署指南](../guides/docker-deployment.md)

## 实现了什么

本变更删除七牛云实现，在现有本地存储基础上新增 MinIO Provider。MinIO 是默认和主要
写入方案，本地存储继续保留，可通过配置显式作为写入目标，也可在 MinIO 写入模式下继续
提供历史 `/uploads` 文件读取。

当前头像实际保存在 `backend/uploads/`，这些历史文件不复制到 MinIO，数据库中的历史头像
URL 也不做批量回填。

本变更只实现以下结果：

- 默认情况下，新上传的 Agent 和管理员头像写入 MinIO。
- 配置选择本地模式时，继续使用现有 `LocalStorage` 写入 `uploads/`。
- MinIO URL 使用 `/api/assets/avatars/...`；本地模式继续使用 `/uploads/...`。
- 浏览器通过 Backend 匿名只读接口访问头像，不直接访问 MinIO。
- 删除七牛 SDK、配置、环境变量和实现。
- 保留并收紧现有本地磁盘实现，删除的只有七牛 Provider。

## 怎么实现的

使用独立私有 Bucket `agenthub-assets` 保存 MinIO 头像。Backend 使用最小权限账号写入和读取
对象，上传后返回固定的 `/api/assets/avatars/...` URL；读取时由 Backend 从 MinIO 流式代理。
`LocalStorage` 保持现有磁盘写入 + `/uploads` 静态读取方式。配置只决定新头像写入哪个
Provider，两个读取入口可以同时存在。

External Skill ZIP 继续使用现有 `skill-packages` Bucket 和 `PackageStore`，两者不共用 Bucket、
账号或业务接口。

## 1. 范围与明确决策

### 1.1 纳入范围

- `POST /api/agents/avatar` 的新头像上传。
- Agent 头像与管理员头像共用的上传接口。
- `GET`/`HEAD /api/assets/avatars/*path` 匿名只读接口。
- Asset MinIO 配置、Backend 组装、Docker 初始化、健康检查和测试。
- 删除七牛云头像存储实现。
- 保留本地磁盘头像写入与 `/uploads` 静态读取。
- 增加 MinIO Provider，并将其设为默认写入目标。
- 支持在 MinIO/本地之间显式选择写入目标。
- 更新所有仍描述七牛云或“凭据为空自动回退本地”的配置模板和文档。

### 1.2 不纳入范围

- 不读取、下载或迁移任何七牛云对象。
- 不复制 `backend/uploads/` 中的历史文件。
- 不扫描或批量修改 `sessions.avatar_url`、`session_agents.avatar_url`、
  `admin_settings.admin_avatar_url`。
- 不新增迁移记录表、资产对象表或 `avatar-migrate` 命令。
- 不提供七牛影子写、MinIO/本地双写、历史 URL 回填或反向回滚。
- 不改 External Skill ZIP 的 MinIO 存储实现。
- 不处理聊天 `ImageCard` 或 Agent 工作区文件代理。

### 1.3 本地存储的定位

- 本地 Provider 继续按现有方式把新文件写入配置目录，并返回 `/uploads/...` URL。
- `local.enabled` 控制本地 Provider 和 `/uploads` 静态路由是否可用。
- 当 `write_provider=minio` 时仍可保持 `local.enabled=true`，仅用于读取历史本地头像。
- 当 `write_provider=local` 时必须 `local.enabled=true`，本地 Provider 同时负责新写入和读取。
- 历史数据库记录保持原值，不为旧 URL 增加兼容代理。
- 只有 URL 的 host/path 仍能指向当前 Backend 时，旧记录才能从兼容路由受益；不为历史
  `http://localhost:8080/uploads/...` 等绝对 URL 做域名改写。
- `backend/uploads/` 属于运行数据，代码变更和部署脚本不得自动删除。启用本地 Provider 时
  必须保留并挂载该目录。
- MinIO 不可用时不得在单次请求中自动改写到本地；切换写入目标必须修改配置并重启，避免
  故障期间产生不可预期的混合 URL。
- Frontend 已有头像加载失败后的默认头像回退逻辑；管理员头像等页面仍需纳入手工验收，
  不能假设所有 `<img>` 都具备相同的错误回退行为。

## 2. 实施前基线（已由本变更替换）

实施前调用链：

```text
Frontend
  → POST /api/agents/avatar
  → AvatarController：扩展名 + 2 MiB 限制
  → AvatarService：生成 avatars/{uuid}.{ext}
  → storage.Provider
      ├── 有七牛密钥：QiniuStorage
      └── 无七牛密钥：LocalStorage → backend/uploads/
  → 返回七牛 URL 或 http://localhost:8080/uploads/...
  → Frontend 再调用 Session/Admin 更新接口保存 URL
```

相关代码位置：

| 位置 | 当前职责 | 目标变化 |
|---|---|---|
| `backend/pkg/qiniu/` | 七牛 SDK 上传和 URL 生成 | 删除 |
| `backend/pkg/storage/` | 七牛/本地工厂与 `Provider` | 删除七牛，保留本地并新增 MinIO |
| `backend/internal/service/impl/avatar_service.go` | UUID key、上传、返回 URL | 写入选定 Provider 并接收 Context |
| `backend/internal/controller/impl/avatar_controller.go` | 文件读取和基础校验 | 增加真实图片校验并传递请求 Context |
| `backend/internal/app/app.go` | 注入 Provider、注册 `/uploads` | 注入选定 Writer、MinIO Reader 和 LocalStorage |
| `backend/cmd/server/main.go` | 创建七牛/本地 Provider | 按配置创建 MinIO/本地运行时 |
| `backend/internal/conf/conf.go` | 七牛、本地、Skill MinIO 配置 | 删除七牛，新增 Avatar MinIO 与选择配置 |
| `backend/go.mod` | 同时依赖 MinIO 与七牛 SDK | 删除七牛 SDK，保留 MinIO SDK |

头像上传和保存 URL 仍然是两个 HTTP 请求。上传成功而保存失败时可能产生无引用对象。
本次不引入资产表和自动删除任务，避免在缺少可靠引用关系时误删对象；孤儿清理作为后续独立
需求处理。

## 3. 目标架构

```text
MinIO 写入（默认）：
Browser
  → POST /api/agents/avatar
  → Backend 校验图片、生成 UUID key
  → MinIOStorage 计算 SHA-256 和对象元数据
  → MinIO agenthub-assets/avatars/{uuid}.{canonical_ext}
  → 返回 /api/assets/avatars/{uuid}.{canonical_ext}
  → Session/Admin 更新接口写入 MySQL

读取：
Browser
  → GET /api/assets/avatars/{uuid}.{canonical_ext}
  → Backend AssetController
  → MinIO 私有 Bucket
  → 图片字节流

本地写入（显式选择）：
Browser
  → POST /api/agents/avatar
  → Backend 校验图片、生成 UUID key
  → LocalStorage uploads/avatars/{uuid}.{canonical_ext}
  → 返回 /uploads/avatars/{uuid}.{canonical_ext}
  → Gin 静态路由读取文件
```

`write_provider` 只决定新上传写入 MinIO 还是本地。`minio.enabled` 和 `local.enabled` 分别决定
两条读取链路是否注册，因此可以使用“MinIO 写入 + 同时读取历史本地头像”的推荐组合。

### 3.1 Bucket 与权限

使用独立私有 Bucket：

```text
agenthub-assets/
└── avatars/{uuid}.{jpg|png|gif|webp}
```

- Bucket 不开启匿名读，不直接暴露到公网。
- Asset 应用账号只允许 `GetBucketLocation` 和 `avatars/*` 的 `GetObject`、`PutObject`、
  `StatObject`。
- 在线 Backend 不授予 `ListBucket` 和 `DeleteObject`。
- 不使用 MinIO Root 凭据，不复用 Skill 应用账号。
- 可以与 Skill Bucket 部署在同一个 MinIO 实例，但权限必须相互隔离。

### 3.2 对象键和元数据

- 对象 key 使用服务端生成的 UUID，不使用客户端文件名。
- 扩展名由真实图片格式映射为 `jpg/png/gif/webp`，`.jpeg` 统一规范为 `.jpg`。
- 上传时设置正确的 `Content-Type`。
- `MinIOStorage` 计算原始字节 SHA-256，并写入对象元数据 `sha256`，用于读取响应 ETag 和
  故障排查；LocalStorage 不需要对象元数据。
- UUID key 不应被覆盖；若极低概率命中已存在 key，则重新生成 UUID，不覆盖旧对象。
- 首期不设置对象生命周期删除规则，避免误删仍被数据库引用的头像。

### 3.3 稳定 URL

MinIO 模式保存：

```text
/api/assets/avatars/550e8400-e29b-41d4-a716-446655440000.webp
```

本地模式保存：

```text
/uploads/avatars/550e8400-e29b-41d4-a716-446655440000.webp
```

使用相对应用 URL 可以避免数据库绑定 MinIO endpoint、部署域名和协议。本地 Vite 已代理
`/api` 与 `/uploads`，Docker Nginx 也已将 `/api/` 与 `/uploads/` 转发到 Backend，因此
Frontend 无需新增 MinIO 地址或凭据配置。

## 4. 配置设计

删除顶层 `qiniu`，将现有 `storage` 扩展为 MinIO/本地双 Provider 配置：

```yaml
storage:
  write_provider: minio # minio | local；默认和推荐值为 minio
  minio:
    enabled: true
    endpoint: 127.0.0.1:19000
    bucket: agenthub-assets
    access_key: ""
    secret_key: ""
    use_ssl: false
    ca_file: ""
    request_timeout: 10s
  local:
    enabled: true         # MinIO 写入时可继续读取历史本地头像
    dir: ./uploads
    url_prefix: /uploads
```

环境变量使用独立前缀，不能与 Skill 的 `MINIO_*` 混用：

```text
AVATAR_STORAGE_WRITE_PROVIDER
ASSET_MINIO_ENABLED
ASSET_MINIO_ENDPOINT
ASSET_MINIO_BUCKET
ASSET_MINIO_ACCESS_KEY
ASSET_MINIO_SECRET_KEY
ASSET_MINIO_USE_SSL
ASSET_MINIO_CA_CERT
ASSET_MINIO_REQUEST_TIMEOUT
LOCAL_STORAGE_ENABLED
LOCAL_STORAGE_DIR
LOCAL_STORAGE_URL_PREFIX
```

配置规则：

- `write_provider` 只能是 `minio` 或 `local`，默认配置和 Docker 部署使用 `minio`。
- `write_provider=minio` 时必须 `minio.enabled=true`，且 endpoint、Bucket、AccessKey、SecretKey
  完整，SecretKey 至少 8 个字符；缺失或过短时 Backend 启动失败。
- `write_provider=local` 时必须 `local.enabled=true`。
- `request_timeout` 必须是正时长。
- MinIO 在生产环境必须 `use_ssl=true`，endpoint 不允许 `http://`。
- `ca_file` 非空时必须能读取并解析证书。
- `minio.enabled=true` 时 Backend 使用有界超时检查 Bucket 和账号权限；失败即启动失败，无论
  当前 Writer 是否为本地。若只要求本地可用，应显式设置 `minio.enabled=false`。
- MinIO 应用账号不创建 Bucket 或修改策略。
- `local.enabled=true` 时沿用现有 `LocalStorage`：确保目录存在、写入文件并注册静态路由。
- `local.url_prefix` 必须是以 `/` 开头的同源路径，首期固定推荐 `/uploads`，不接受外部 host。
- 不自动 fallback、不双写：MinIO 写入失败时请求失败；需要切换本地必须修改
  `write_provider=local` 并重启。
- 两个 Provider 可以同时 enabled，但只有 `write_provider` 指定的一个接收新上传。

## 5. Backend 实现规划

### 5.1 Storage Provider (`backend/pkg/storage/`)

保留现有上传接口，使 AvatarService 不感知写入目标：

```go
type Provider interface {
    UploadBytes(ctx context.Context, key string, data []byte) (string, error)
    UploadReader(ctx context.Context, key string, reader io.Reader, size int64) (string, error)
}

type ObjectReader interface {
    Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
    Stat(ctx context.Context, key string) (ObjectInfo, error)
    Health(ctx context.Context) error
}

type ObjectInfo struct {
    Key         string
    Size        int64
    SHA256      string
    ContentType string
    ETag        string
}
```

目标目录：

```text
backend/pkg/storage/
├── storage.go             # Provider / ObjectReader / ObjectInfo
├── factory.go             # 构造两个 Provider 并选择 Writer
├── runtime.go             # Writer + MinIOReader + LocalStorage
├── minio.go
├── minio_test.go
├── local.go               # 保留现有实现
├── local_test.go
└── memory.go              # 单测替身
```

- `MinIOStorage` 实现 `Provider + ObjectReader`，上传返回 `/api/assets/...`。
- `LocalStorage` 保持现有 `Provider` 行为，上传返回 `/uploads/...` 并暴露 `Dir()`。
- `Runtime.Writer` 由 `write_provider` 唯一选择；Runtime 同时保留已启用的 MinIO Reader 和
  LocalStorage，供两种读取路由使用。
- `MemoryStore` 仅用于 Service/Controller 单测。
- 所有 MinIO 调用使用请求 Context 和配置超时。
- `Open` 必须返回可关闭 reader，Controller 负责 `defer Close()`。
- 上传使用 `PutObject` 的 `If-None-Match: *` 做不可覆盖检查，不依赖没有授予的
  `ListBucket` 预检。
- MinIO/S3 ETag 不等于 SHA-256；响应强 ETag 使用上传时保存的 SHA-256 元数据。
- 明确映射 not-found、timeout、permission 和其他存储错误，Controller 不通过字符串匹配错误。
- Factory 不再检查七牛密钥，也不在 MinIO 错误时自动返回 LocalStorage。

### 5.2 AvatarService

将接口改为接收 Context：

```go
UploadAvatar(ctx context.Context, filename string, data []byte) (string, error)
```

处理顺序：

1. 在 2 MiB 硬上限内读取上传内容。
2. 使用 `http.DetectContentType` 和 `image.DecodeConfig` 验证真实图片格式。
3. 只允许 JPEG、PNG、GIF、WebP；拒绝 SVG 和伪装文件。
4. 限制宽、高不超过 4096，总像素不超过 16 MP。
5. 文件扩展名与真实格式不一致时返回 `400`，不静默改写用户上传类型。
6. 生成 UUID key，调用选定 `storage.Provider.UploadBytes`；MinIOStorage 在内部计算 SHA-256
   和对象元数据。
7. 直接返回 Provider 生成的 URL：MinIO 为 `/api/assets/...`，本地为 `/uploads/...`。

禁止继续使用 `context.Background()`；客户端取消请求时应中止 MinIO I/O，本地实现至少在
落盘前检查 Context，避免已取消请求继续创建新文件。

### 5.3 AssetController

新增匿名只读接口：

```text
GET  /api/assets/avatars/*path
HEAD /api/assets/avatars/*path
```

路径只接受：

```text
{uuid}.{jpg|png|gif|webp}
```

Controller 必须：

- 解析 UUID 并重新构造对象 key，不把 wildcard 原文直接传给 MinIO。
- 拒绝反斜线、路径穿越、额外目录、控制字符、双重编码和未知扩展名。
- 返回正确的 `Content-Type`、`Content-Length`、`ETag`。
- 返回 `Cache-Control: public, max-age=31536000, immutable` 和
  `X-Content-Type-Options: nosniff`。
- 支持 `If-None-Match` 和 `304`；`HEAD` 不发送响应体。
- 对象不存在返回 `404`，MinIO 超时返回 `503`，其他内部错误返回 `500`。
- 不支持目录列表、任意 Bucket/key 透传、上传、删除和预签名 URL。

现有 `middleware.AuthWithSkips` 只支持完整路径精确匹配，不能用 wildcard 跳过认证。
AssetController 应在受保护 `/api` Group 挂载 Auth middleware 之前，以单独的公开只读 Group
注册；该 Group 只注册 `GET`/`HEAD`，仍继承 Logger、CORS、Recovery、请求超时和读取限流。

### 5.4 应用组装与健康检查

`backend/cmd/server/main.go`：

- 从 `StorageConfig` 创建 `storage.Runtime`。
- `minio.enabled=true` 时创建 MinIOStorage，并用有界 Context 检查 Bucket 和账号权限。
- `local.enabled=true` 时创建现有 LocalStorage 并确保目录存在。
- 按 `write_provider` 选择 `Runtime.Writer`，配置矛盾时终止启动。
- 将 Writer、MinIOReader 和 LocalStorage 注入 `app.Dependencies`。

`backend/internal/app/app.go`：

- `StorageProvider storage.Provider` 继续作为 AvatarService 的 Writer。
- 新增可选 `AssetReader storage.ObjectReader`，启用 MinIO 时注入 AssetController。
- 保留 `LocalStorage`，在 `local.enabled=true` 时注册静态路由：

  ```go
  r.StaticFS("/uploads", gin.Dir(runtime.Local.Dir(), false))
  ```

  `gin.Dir(..., false)` 禁止目录列表。MinIO 写入模式下，该路由只读取历史本地文件；本地
  写入模式下，它同时读取新文件。
- `/ready` 在 `minio.enabled=true` 时检查 Asset MinIO；只有
  `write_provider=local,minio.enabled=false` 的纯本地模式不依赖 MinIO。Skill MinIO 仍只在其
  功能开启时检查。

## 6. 删除七牛并保留本地实现

实施时删除：

- `backend/pkg/qiniu/`
- `backend/pkg/storage/qiniu.go`
- `storage.NewProvider` 中的七牛自动选择分支；改为显式 `write_provider=minio|local`
- `conf.QiniuConfig`、`Config.Qiniu` 以及 `QINIU_*` 环境变量读取
- `backend/configs/*.yaml` 和 `docker/configs/backend/*.yaml` 中的 `qiniu` 配置；`storage` 改为
  MinIO/本地显式选择结构
- `backend/.env.example`、`docker/configs/backend/.env.example` 中的 `QINIU_*`
- `github.com/qiniu/go-sdk/v7` 依赖及 `go.sum` 残留
- 任何“检测不到 MinIO/七牛凭据就自动切换 Provider”的隐式 fallback

删除后运行 `go mod tidy`，并用全项目搜索确认生产代码、配置模板和当前文档不再引用：

```text
qiniu
QINIU_ACCESS_KEY
QINIU_SECRET_KEY
QiniuStorage
```

历史审计报告可以保留带日期的旧事实，但应注明其已被本变更取代；当前配置、指南、技术栈
和架构文档必须同步更新。本地 `LocalStorage`、`/uploads` 和 MinIO 都属于当前支持能力。
`AGENTS.md` 由文档维护流程单独更新，不在本规划编辑动作中修改。

## 7. Docker 与本地开发

### 7.1 MinIO 初始化

扩展 `docker/minio/init.sh`：

- 保留 `skill-packages` Bucket 和策略；仅在 Skill feature 开启时创建 Skill 应用账号。
- Avatar MinIO 开启时新建私有 `agenthub-assets` Bucket 和 Asset 应用账号；关闭时不创建未使用的 Asset 资源。
- Asset 应用账号绑定只允许 `avatars/*` 读写/Stat 的策略。
- 对已存在 Bucket 再次执行时保持幂等，并显式清除匿名策略。
- Root 凭据只注入 `minio` 和 `minio-init`，不进入 Backend。

Compose 默认设置 `AVATAR_STORAGE_WRITE_PROVIDER=minio`，并为 Backend 注入独立的
`ASSET_MINIO_*` 应用凭据。默认 Docker 部署应等待 `minio-init` 成功；Skill MinIO 的 feature
gate 语义保持不变。

Docker 启用 `local.enabled` 时挂载独立 uploads volume：MinIO 写入、本地仅用于历史读取时可
只读挂载（Compose 使用 `LOCAL_STORAGE_VOLUME_MODE=ro`）；`write_provider=local` 时必须设置
`LOCAL_STORAGE_VOLUME_MODE=rw` 并读写挂载。目录禁止包含指向挂载外部的符号链接。
Docker Backend 同时只读挂载 `docker/certs/` 到 `/etc/agenthub/certs/`；启用 MinIO TLS
时，`ASSET_MINIO_CA_CERT` / `MINIO_CA_CERT` 必须填写容器内证书路径。

### 7.2 本地开发

推荐本机运行也使用 MinIO，配置示例给出宿主机 endpoint；Docker Backend 使用
`minio:9000`。没有 MinIO 的纯本地开发可显式设置 `write_provider=local`、
`minio.enabled=false`、`local.enabled=true`，继续使用现有 `backend/uploads/` 行为。禁止根据
凭据是否为空自动选择本地。

## 8. 发布与回滚

本变更没有资源迁移阶段，按以下顺序直接发布：

1. 创建 `agenthub-assets` Bucket、策略和 Asset 应用账号。
2. 验证应用凭据可以 Put/Get/Stat `avatars/*`，且不能访问 `skill-packages`。
3. 设置推荐组合：`write_provider=minio`、`minio.enabled=true`、`local.enabled=true`，保留历史
   `/uploads` 读取。
4. 发布包含 MinIO Provider、AssetController 和保留 LocalStorage 的新 Backend。
5. 验证 `/ready`、两种读取路径、MinIO 上传、缓存头和前端展示。
6. 确认默认模式的新头像 URL 均为 `/api/assets/avatars/...`。
7. 在非生产环境额外切换 `write_provider=local`，确认新文件写入 uploads 并返回 `/uploads/...`。

存储切换只改配置和重启，**不搬运对象**：

- MinIO 上传异常时可以把 `write_provider` 切为 `local`；这只影响后续上传，不复制已有对象。
- 切到本地写入时建议仍保持 `minio.enabled=true`，使已有 `/api/assets/...` URL 继续可读。
- MinIO 本身完全不可用时，切换本地还必须设置 `minio.enabled=false`，否则启用组件的启动
  检查会失败；此时已有 MinIO 头像不可读，但本地上传和 `/uploads` 可工作。
- 不删除 MinIO 中已经写入的头像对象。
- 不恢复七牛凭据或七牛 SDK。
- 从本地切回 MinIO 后，已有 `/uploads` URL 继续由 `local.enabled=true` 保持可读。

## 9. 测试计划

### 9.1 单元测试

- 真实 MIME、扩展名、大小、宽高、像素总量和伪装文件校验。
- UUID key 生成和稳定 URL 构造。
- Asset URL 路径解析、路径穿越、反斜线和双重编码拒绝。
- MinIO not-found、timeout、permission、reader close 的错误映射。
- `GET`、`HEAD`、ETag、`If-None-Match`、缓存头和 `nosniff`。
- Asset 公共路由匿名可读，但 `POST/PUT/DELETE` 不存在且其他 `/api` 路由仍受 Auth 保护。
- `local.enabled=false` 时 `/uploads` 为 `404`；启用时禁止目录列表和路径穿越。
- `write_provider=minio` 时本地目录不产生新文件；`write_provider=local` 时正常写入并返回
  `/uploads/...`。
- 非法配置组合、未知 Provider，以及 MinIO 失败不自动 fallback 的测试。
- AvatarService 使用请求 Context，取消时 Store 收到 cancellation。

### 9.2 集成测试

- 真实 MinIO：上传 → 返回稳定 URL → Backend 读取 → `304`。
- 本地 Provider：上传 → 返回 `/uploads/...` → 静态路由读取。
- MinIO Writer 与 Local Reader 同时启用时，两类历史 URL 均可读取。
- Backend 重启后已有 MinIO 头像仍可读取。
- `write_provider=minio` 时 MinIO 不可用或凭据错误导致启动/ready 失败，不写入本地。
- `write_provider=local,minio.enabled=false` 时不依赖 MinIO，上传和读取正常。
- Asset 账号无法读取 `skill-packages`，Skill 账号无法读取 `agenthub-assets`。
- Docker 环境经 Frontend Nginx 的 `/api/assets/...` 读取头像，浏览器不直连 MinIO。

### 9.3 手工验收

- 新建单 Agent 会话并上传头像。
- 编辑 Agent 头像后，会话列表、聊天消息、Hover Card 和 Agent 详情页显示一致。
- 群聊叠加头像显示正常。
- 管理员头像上传和显示正常。
- 分别验收 MinIO 写入和本地写入，新头像 URL 与所选 Provider 一致。
- MinIO 写入 + 本地读取同时开启时，MinIO 新头像和历史 `/uploads` 头像均可读。
- MinIO 临时不可用时不自动落盘；切换配置到本地并重启后，新上传可用。

## 10. 实施任务清单

### 配置与基础设施

- [x] 扩展 `StorageConfig`：`write_provider`、MinIO、本地配置及环境变量覆盖测试
- [x] 创建 `agenthub-assets` 私有 Bucket、独立账号和最小权限策略
- [x] 更新 Docker Compose、MinIO 初始化、配置示例和部署指南
- [x] MinIO enabled 时纳入 Backend 启动检查和 `/ready`，纯本地模式不依赖 MinIO

### Backend

- [x] 保留 `pkg/storage.LocalStorage`，新增 MinIOStorage、ObjectReader、Runtime 和测试
- [x] 改造 AvatarService：Context、真实图片校验、UUID key 和稳定 URL
- [x] 新增匿名只读 AssetController 和严格路径/响应头处理
- [x] 在独立公共路由组注册 Asset GET/HEAD，不扩大 Auth skip
- [x] 工厂删除七牛分支，按 `write_provider` 显式选择 MinIO 或 Local Writer
- [x] 删除七牛配置、环境变量和 qiniu-go-sdk 依赖
- [x] 相对 `avatar_url` 只允许 `/api/assets/avatars/{uuid}.{ext}` 或 `/uploads/avatars/{uuid}.{ext}`；
  外部 `http/https` URL 继续按现有规则支持

### 文档与验收

- [x] 更新 Backend 技术栈、配置、组装、环境搭建和 Docker 文档
- [x] 全项目确认当前说明为“MinIO 默认、本地可选、无自动 fallback”
- [x] 完成两种 Provider 的单元测试；真实 MinIO 集成测试和手工头像验收需在部署环境执行
- [x] 确认没有任何资源迁移命令、迁移表或历史 URL 回填逻辑

## 11. 完成标准

- 默认 `write_provider=minio`，新头像写入 `agenthub-assets/avatars/*` 并返回
  `/api/assets/avatars/...`。
- 显式选择 `write_provider=local` 时沿用现有 LocalStorage，写入 uploads 并返回 `/uploads/...`。
- Backend 不再包含七牛 SDK、`pkg/qiniu`、`QINIU_*` 或七牛配置。
- MinIO 与本地可以同时启用读取，但只有一个 Writer；不存在自动 fallback 或双写。
- Asset 和 Skill 使用不同 Bucket、账号和最小权限策略。
- MinIO Bucket 保持私有，浏览器只经 Backend 读取头像。
- 上传校验、匿名读取、缓存、错误映射和鉴权边界测试通过。
- 当前配置模板和文档统一描述 MinIO 默认、本地可选的头像存储策略。
- 未实现对象复制、数据库 URL 回填或任何形式的资源迁移。
