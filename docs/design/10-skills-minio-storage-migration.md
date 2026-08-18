# 10 — Skills ZIP 从数据库迁移到 MinIO 规划

> **状态**：🚧 核心实现完成（默认功能开关关闭；真实外部服务集成测试代码与迁移/对账工具已补齐，但外部环境执行门禁尚未完成；兼容发布、灰度等运维门禁待执行）
> **日期**：2026-08-09（2026-08-11 更新：校正外部验证状态与测试范围说明）
> **前置**：[07-skills-hub-external-skills.md](07-skills-hub-external-skills.md)、[08-skills-db-migration.md](08-skills-db-migration.md)

## 1. 背景

当前 External Skill 的 ZIP 内容存储在 MySQL `skill_hubs.Content`（`longblob`）中。Backend 上传接口先把 ZIP 解压到系统临时目录，向 Frontend 返回 `tmp_dir`；用户确认后，Backend 重新打包并将完整 ZIP 写入数据库。导入 Skill 时，Backend 从数据库读取 ZIP，再发送给 AgentEnd 安装到目标工作区。

当前链路：

```text
Frontend 上传 ZIP
  → Backend 解压到 /tmp 并校验
  → Frontend 携带 tmp_dir 请求确认
  → Backend 重新打包
  → ZIP 写入 MySQL longblob
  → 导入时从 MySQL 读取 ZIP
  → Backend 将 ZIP 发送给 AgentEnd
```

随着 Skill 数量、版本和压缩包体积增长，把文件内容直接存入业务数据库会增加数据库备份、恢复、主从同步和查询资源消耗。`tmp_dir` 由 Backend 返回给 Frontend 也暴露了服务端实现细节，不利于多 Backend 实例部署。

## 2. 目标与非目标

### 2.1 目标

- MinIO 私有 Bucket 保存 External Skill ZIP。
- MySQL 只保存 Skill 元数据、对象键和完整性信息。
- 保留上传前的 ZIP 安全校验和 `SKILL.md` 格式校验。
- 使用 `upload_id` 替代向 Frontend 暴露服务端 `tmp_dir`。
- 支持多 Backend 实例共享 Skill 包。
- 历史 BLOB 可分批迁移、校验、恢复和回滚。
- MinIO 与 MySQL 操作失败时具备补偿和重试能力。
- 第一阶段保持 AgentEnd 安装接口不变，控制改造范围。

### 2.2 非目标

- 本次不实现同名 Skill 的多版本共存。
- 本次不改变 Builtin Skill 的本地供给机制。
- 本次不向浏览器提供永久公开的 MinIO URL。
- 第一阶段不改造 Backend → AgentEnd 为全链路流式传输。
- 本次不执行应用代码、数据库或历史数据迁移；本文只定义实施规划。

## 3. 现状代码入口

| 文件 | 当前职责 |
|---|---|
| `backend/internal/model/skill.go` | `SkillHub.Content` 保存 ZIP BLOB |
| `backend/internal/service/skill_validator.go` | 解压、路径检查、大小限制、`SKILL.md` 校验与重新打包 |
| `backend/internal/service/impl/skill_service.go` | 上传、确认、导入、删除业务逻辑 |
| `backend/internal/dao/gorm/skill_dao.go` | Skill 元数据与 BLOB 读写 |
| `backend/internal/controller/impl/skill_controller.go` | `/api/skills/*` HTTP 接口 |
| `backend/pkg/storage/` | 头像 MinIO/本地存储抽象；与本 Skill 的 `pkg/package_store/` 独立 |
| `frontend/src/pages/SkillsHubPage.tsx` | 上传校验结果展示和确认请求 |
| `frontend/src/lib/api.ts` | Skill API 手写类型与请求函数 |
| `backend/pkg/agentend_client/client.go` | 将 ZIP 发送给 AgentEnd 安装 |
| `docker/docker-compose.yml` | MySQL、Redis、Backend、Frontend 部署 |

## 4. 目标架构

```text
                         ┌──────────────────┐
Frontend 上传 ZIP ──────▶│ Backend ZIP 校验 │
                         └────────┬─────────┘
                                  │ 校验并规范化打包
                                  ▼
                    MinIO incoming/{upload_id}.zip
                                  │
Frontend 确认 ────────────────────┤
                                  ▼
                    MinIO skills/{name}/{sha256}.zip
                                  │
                     ┌────────────┴────────────┐
                     ▼                         ▼
              MySQL 保存元数据          导入时读取并校验
                                               │
                                               ▼
                                            AgentEnd
```

职责边界：

| 组件 | 职责 |
|---|---|
| Frontend | 上传 ZIP、展示校验结果、提交 `upload_id` 确认 |
| Backend | 校验、对象生命周期、元数据、完整性验证和失败补偿 |
| MinIO | 私有保存临时包和正式包，不承担解压或格式校验 |
| MySQL | Skill 元数据、对象键、哈希、存储状态和 Session 关联 |
| Redis | 保存短生命周期上传会话，避免暴露或依赖本机临时路径 |
| AgentEnd | 接收已校验 ZIP，安装或移除目标工作区中的 Skill |

## 5. 对象存储设计

### 5.1 Bucket 与对象键

使用私有 Bucket `skill-packages`：

```text
skill-packages/
├── incoming/{upload_id}.zip
└── skills/{skill_name}/{sha256}.zip
```

- `incoming/` 保存待确认上传，生命周期建议为 24 小时。
- `skills/` 保存正式包，不设置自动过期。
- `sha256` 作为确定性对象名，便于完整性验证和幂等上传。
- `skill_name` 必须复用 Backend 现有名称规范化和路径安全规则。
- 正式对象视为不可变对象：目标键已存在时不覆盖，必须重新读取并验证大小与 SHA-256；
  一致则幂等复用，不一致则报告存储完整性错误。

### 5.2 私有存储接口

现有 `backend/pkg/storage.Provider` 的语义是上传后返回公开 URL，且缺少读取、复制、查询和删除能力。Skill 包不应直接复用该公开文件接口，建议新增独立的私有对象存储抽象：

```go
type PackageStore interface {
    Put(ctx context.Context, key string, body io.Reader, size int64, sha256 string) error
    Open(ctx context.Context, key string) (io.ReadCloser, error)
    Stat(ctx context.Context, key string) (*ObjectInfo, error)
    Promote(ctx context.Context, sourceKey, targetKey string, expected ObjectInfo) error
    List(ctx context.Context, prefix, cursor string, limit int) (items []ObjectInfo, nextCursor string, err error)
    Delete(ctx context.Context, key string) error
}
```

`Promote` 不是普通覆盖式 Copy：目标不存在时复制，目标已存在时验证 `Size/SHA256` 后幂等
复用；并发提升相同哈希对象允许收敛到同一内容，任何不一致都返回完整性错误且禁止覆盖。
该语义必须由接口测试覆盖，不能只写在 Service 的调用约定中。

`List` 必须按前缀分页，供 `incoming/` 清理、正式对象反向对账和孤儿检测使用，禁止一次
返回整个 Bucket。`ObjectInfo` 至少包含 Key、Size、LastModified 和对象元数据中的 SHA-256；
元数据哈希仅用于快速筛选，最终完整性判断仍以限量读取后计算的内容哈希为准。

建议目录：

```text
backend/pkg/package_store/
├── store.go
├── minio.go
├── minio_test.go
└── memory.go
```

`memory.go` 用于 Service 单元测试，不依赖真实 MinIO。

请求内执行的 `PackageStore` 调用必须沿用 HTTP 请求的 `context.Context`。实施时同步调整
Controller → Service 的 Skill 方法签名并传入 `c.Request.Context()`，确保客户端取消时能够
中止上传、确认和导入 I/O。异步迁移、删除和孤儿清理 Worker 不得复用已经结束的请求
Context，而应使用由应用生命周期管理、支持优雅关闭并为每次 MinIO 操作设置超时的
Worker Context；两类链路都禁止无超时地使用 `context.Background()`。

## 6. 数据模型

在 `SkillHub` 增加以下字段：

```go
ObjectKey   string `gorm:"size:512" json:"-"`
SHA256      string `gorm:"size:64" json:"sha256,omitempty"`
PackageSize int64  `gorm:"default:0" json:"package_size"`
StorageType string `gorm:"size:16" json:"storage_type"`
Status      string `gorm:"size:32;default:ready" json:"status"`
```

字段语义：

| 字段 | 含义 |
|---|---|
| `ObjectKey` | MinIO 对象键，不保存公开 URL |
| `SHA256` | 规范化 ZIP 的 SHA-256，用于校验和幂等 |
| `PackageSize` | ZIP 压缩包大小 |
| `StorageType` | 迁移期区分 `db` 与 `minio` |
| `Status` | `ready`、`migrating`、`deleting`、`storage_error` |
| `TotalSize` | 保持现有语义：解压后的文件总大小 |
| `Content` | 迁移兼容字段，完成迁移前不得直接删除 |

首期不新增版本表，继续保持 `Name` 唯一。未来需要多版本时，再引入 `skill_versions`，不要复用或改变本次字段语义。

上述对象存储字段只对 External Skill 有效；Builtin Skill 不得因为字段默认值被误判为
数据库 BLOB Skill。扩表发布时必须显式回填现有数据：External Skill 标记为 `db/ready`，
Builtin Skill 的存储类型保持为空。读取逻辑以 `Builtin`、`StorageType` 和 `ObjectKey` 的
组合校验为准，不仅依赖某一个字段是否为空。

迁移期 `Content` 仍可能很大，所有列表、按名称查询和 Session Skill 查询必须使用显式
元数据投影，禁止 `SELECT skill_hubs.*` 或 GORM 默认加载 `Content`。只有 BLOB 回滚读取、
历史迁移和反向回填命令可以显式查询 `Content`；对象读取同样通过独立 DAO 方法获取
`ObjectKey/SHA256/PackageSize`，避免业务列表意外加载 ZIP。

为封闭导入与删除之间的竞态，在 `AgentSkill` 增加安装状态：

```go
Status string `gorm:"size:16;default:ready" json:"status"`
```

允许值为 `installing`、`ready`、`removing`、`sync_error`。删除 Skill 时，任意状态的
`AgentSkill` 记录都视为有效引用。扩表时将历史关联回填为 `ready`；新的导入预约必须在
代码中显式写入 `installing`，不能依赖字段默认值。

对象存储补偿和删除重试不得只依赖进程内 goroutine 或易丢失的 Redis 消息。新增持久化
`skill_operation_jobs`（或复用项目统一 Outbox）记录 `operation`、可空的
`skill_id`/`session_id`/`agent_type`/`object_key`、`status`、`attempts`、`next_retry_at` 和
`last_error`，统一承载对象删除、孤儿清理、历史迁移以及 AgentEnd 安装/移除对账，由幂等
Worker 执行并支持崩溃恢复。
多 Worker 使用任务租约或 `SELECT ... FOR UPDATE SKIP LOCKED` 抢占到期任务；每种操作定义
唯一幂等键，避免两个实例同时执行同一对象删除或迁移。

另外新增轻量 `skill_upload_receipts` 表持久化确认幂等结果：

| 字段 | 含义 |
|---|---|
| `upload_id` | 主键，关联一次上传确认 |
| `skill_id` | 已创建或已复用的 Skill ID |
| `sha256` | 本次确认的规范 ZIP 哈希 |
| `owner_id` | 上传创建者；认证关闭时为空 |
| `created_at` | 确认完成时间，用于审计和定期清理 |

Skill 元数据写入或复用与 receipt 写入必须在同一 MySQL 事务中完成。这样即使 Redis 会话
丢失，Backend 仍能凭确认请求中的 `upload_id` 查询原结果；返回前仍必须校验 receipt 的
`owner_id` 与当前身份一致，不能把 `upload_id` 当作绕过认证的能力令牌。同名同哈希的
不同 `upload_id` 可以各自保存 receipt，而不会受 `SkillHub.Name` 唯一约束影响。

## 7. 上传与确认流程

### 7.1 上传接口

保留接口：

```http
POST /api/skills/upload
Content-Type: multipart/form-data
```

Backend 处理顺序：

1. 校验扩展名和整个 multipart 请求体积，把上传流限量写入权限为 `0600` 的一次性临时
   ZIP 文件；Validator 改为接收文件路径或 `io.ReaderAt + size`，不再要求 `[]byte`。
2. 解析临时 ZIP 的中央目录并执行安全检查。
3. 流式解压到权限为 `0700` 的一次性临时目录。
4. 检查目录结构、`SKILL.md` 和 Front Matter。
5. 把已校验目录重新打包到另一个 `0600` 临时文件；写入过程中同时计算 SHA-256、统计
   压缩包大小并强制规范 ZIP 上限，禁止在内存中构造完整 ZIP。
6. 从临时规范 ZIP 文件上传到 `incoming/{upload_id}.zip`。
7. 在 Redis 写入上传会话并设置约 15 分钟 TTL。
8. 删除原始 ZIP、解压目录和规范 ZIP 等全部本地临时数据。
9. 返回 `upload_id` 和服务端计算的校验结果。

响应示例：

```json
{
  "valid": true,
  "upload_id": "01K2...",
  "name": "reviewer",
  "description": "Review source code",
  "file_count": 5,
  "total_size": 18240,
  "package_size": 5210,
  "sha256": "..."
}
```

Redis 上传会话至少保存：

```json
{
  "upload_id": "01K2...",
  "state": "pending",
  "confirm_lease_until": null,
  "confirm_token": null,
  "owner_id": "current-user-or-admin",
  "object_key": "incoming/01K2....zip",
  "name": "reviewer",
  "description": "Review source code",
  "sha256": "...",
  "file_count": 5,
  "total_size": 18240,
  "package_size": 5210
}
```

上传会话状态为 `pending`、`confirming`、`confirmed`、`failed`。进入 `confirming` 时写入
短租约 `confirm_lease_until` 和随机 `confirm_token`，同时把 Redis TTL 延长到覆盖租约与
结果保留期；Backend 崩溃后，其他实例只有在租约过期且数据库中不存在 receipt 时才能
生成新 token 并原子接管。只有持有当前 token 的请求能续租或更新 Redis 状态，最终幂等性
仍由 MySQL receipt 保证。可重试错误通过 Lua 比较并设置把本请求持有的 `confirming` 恢复
为 `pending`；不可重试冲突标记为 `failed`。启用认证时必须绑定创建该上传的用户或
管理员；确认时校验当前身份一致。Frontend 不再接收或回传服务器 `tmp_dir`。

### 7.2 确认接口

保留接口路径，调整请求：

```http
POST /api/skills/confirm
```

```json
{
  "upload_id": "01K2..."
}
```

Backend 处理顺序：

1. 先按 `upload_id` 和当前所有者查询 MySQL receipt，命中时直接幂等返回；未命中再使用
   Redis Lua 脚本原子校验会话、所有者和 TTL，并将 `pending → confirming`、写入 token 与
   确认租约。处于有效租约的请求返回“处理中”；租约过期且仍无 receipt 时允许其他
   Backend 原子接管。
2. 名称、描述、文件数、大小和哈希只取服务端上传会话，不接受 Frontend 覆盖。
3. 检查同名 Builtin 或 External Skill 冲突；如果已存在记录的名称和 SHA-256 都一致，
   按重复确认幂等返回。
4. 将临时对象复制到 `skills/{name}/{sha256}.zip`。
5. 通过 `Stat` 验证正式对象大小；正式发布前重新读取并验证 SHA-256。
6. 在同一 MySQL 事务中写入或复用 `skill_hubs` 元数据并创建
   `skill_upload_receipts(upload_id, skill_id, sha256, owner_id)`。回滚观察期内同时把规范
   ZIP 写入 `Content` 作为影子副本，MinIO 仍是权威读取源；确认成功审计事件与这两个
   持久化写入同事务提交，审计失败不得返回成功。
7. 将上传会话更新为 `confirmed` 并短期保存 Skill 标识和 SHA-256，随后删除 `incoming`
   对象；删除失败写入持久化补偿任务。

确认接口在契约中固定结果语义：首次或幂等成功返回 `200`；其他实例持有有效租约返回
`202` 并带可重试提示；上传过期返回 `410`；名称/哈希冲突返回 `409`；MinIO、Redis 或
MySQL 暂时不可用返回 `503`。Frontend 不通过错误字符串猜测状态。

对象操作和 MySQL 事务不能组成分布式事务。正式对象键是确定性的，因此数据库写入失败
时不得由当前请求直接删除正式对象：另一个并发确认可能已经成功引用同一对象。失败请求
只回滚自己的上传会话；正式对象交给带限速和宽限期的孤儿清理 Worker。Worker 只处理
创建时间超过 48 小时、没有任何 `skill_hubs.object_key` 引用且不存在待执行补偿任务的
对象；确认租约必须远短于该宽限期。即使 Redis 会话在数据库提交后丢失，重试也能通过
`upload_id` 查询持久化 receipt 并返回已确认结果。

### 7.3 跨端契约

上传响应和确认请求会由 `tmp_dir` 改为 `upload_id`，且确认请求不再回传名称和其他校验
元数据，属于 Frontend 与 Backend 的跨端协议变化。实施时应：

1. 在 `contracts/schemas/` 增加 Skill API Schema。
2. 扩展 `scripts/generate_contracts.py` 的生成映射。
3. 运行 `make generate`。
4. 在 `contracts/logs/` 添加契约变更记录。
5. 删除 `frontend/src/lib/api.ts` 中对应的重复手写类型。

## 8. ZIP 安全校验加固

现有校验器已覆盖路径穿越、符号链接、文件数量、解压总大小和 `SKILL.md` Front Matter。实施 MinIO 前应补齐：

- 上传包大小、单文件大小和解压总大小使用不同限制。
- Controller 在解析 multipart 前使用 `http.MaxBytesReader` 限制整个请求体，并为 multipart
  边界和表单头预留小幅开销；不能只在 `FormFile` 返回后限制文件流。
- 单个文件使用带上限的流式读取，禁止先完整读入内存再检查大小。
- 检查单文件和整体压缩率，防止 ZIP Bomb。
- 拒绝重复或大小写冲突路径。
- 拒绝设备文件、管道等特殊文件类型。
- ZIP 中只允许一个 Skill 根目录。
- `SKILL.md` 必须为 UTF-8。
- 限制 Front Matter 必填字段、类型和长度。
- 保留现有“ZIP 文件名与 `SKILL.md.name` 一致”的规则；如果包内存在顶层 Skill 目录，
  额外要求目录名也一致。只使用服务端解析得到的规范名称生成对象键。
- 重新确认阶段不信任 Frontend 回传的描述、文件数和大小。
- 规范 ZIP 必须可复现：按路径排序、统一 `/` 分隔符、固定时间戳，并把普通文件权限归一为
  `0644`、明确允许执行的脚本归一为 `0755`，排除宿主机所有者等不稳定元数据，保证相同
  文件内容和执行属性得到相同 SHA-256。
- 临时文件和目录必须位于专用可配置目录，而非任意系统 `/tmp` 路径；启动与定时清理器按
  前缀、属主和年龄删除崩溃遗留项，绝不跟随链接或清理不属于 Skill 上传的目录。
- 使用进程级信号量限制并发校验数量，并检查临时卷剩余空间和预留水位；单请求限制无法
  防止大量合法大小的并发上传耗尽磁盘、CPU 或文件描述符。
- 为上传校验设置总超时和取消检查，并对上传/确认接口执行认证、速率限制与审计。
- 增加 ZIP 路径和解压逻辑的 Go fuzz 测试。

建议初始限制：

| 限制 | 建议值 |
|---|---:|
| ZIP 上传大小 | 10 MB |
| 规范 ZIP 大小 | 12 MB |
| 单个解压文件 | 10 MB |
| 解压总大小 | 50 MB |
| 文件数量 | 200 |
| 最大压缩率 | 100:1 |
| 单实例并发校验 | 4 |
| 单次校验超时 | 2 分钟 |
| 临时卷最低预留空间 | 1 GB |

最终限制应进入 Backend 配置，并在 Controller 与 Validator 复用同一配置来源。
AgentEnd 对规范 ZIP 的请求体、文件数、单文件和解压总量使用不低于 Backend 严格度的独立
限制；配置不一致时启动检查应报错，避免 Backend 接受而 AgentEnd 无法安装。

### 8.1 内容信任边界

上述校验只能证明“归档结构可安全解压、格式符合约定、传输内容未损坏”，不能证明 Skill
中的提示词和脚本是可信代码。External Skill 安装后可能被 Agent 读取或执行，因此还必须：

- 上传、确认和删除 External Skill 仅允许管理员或明确授权角色，`UploadedBy` 和 SHA-256
  写入审计记录；普通用户只能导入已经进入 Hub 的 Skill。
- UI 在确认和导入时展示来源、上传者、SHA-256、文件清单以及是否包含脚本/二进制文件，
  不把“ZIP 校验通过”表述为“内容安全”。
- 可配置拒绝未知二进制、超出允许类型的可执行文件，并预留恶意软件扫描接口；扫描失败
  或超时按失败关闭，不能静默放行。
- 生产环境应建立人工审阅或签名发布流程；包签名与多级审批可作为后续独立增强，但不能
  用存储完整性哈希替代发布者身份和内容审查。

## 9. 导入与删除

### 9.1 导入到 Session

导入流程调整为：

```text
事务内锁定 SkillHub 并校验 status=ready
  → 同事务创建 AgentSkill(status=installing) 和 install 操作任务
  → 读取 SkillHub.ObjectKey
  → PackageStore.Open
  → 限量读取 ZIP
  → 校验 PackageSize 和 SHA256
  → AgentEnd InstallSkill
  → 更新 AgentSkill(status=ready)
```

第一阶段保留 `InstallSkill(..., zipData []byte)`。由于上传包大小受限，可将 MinIO 内容限量读入内存，以避免同时改造 AgentEnd。后续 ZIP 上限明显增大时，再把 Backend → AgentEnd 改造成流式传输。

这里的“保留接口”只表示 URL、请求体和 Backend Client 签名不变，不代表 AgentEnd 内部实现
无需加固。为支持安装任务安全重试，AgentEnd 必须先在目标目录的同文件系统临时目录中
限量解压，再次拒绝路径穿越、链接和特殊文件并校验 `SKILL.md`；全部成功后用原子重命名
替换目标目录，失败时保留原安装。替换操作通过备份目录或可恢复的两阶段 rename 处理
“目标已存在”，清理残留临时目录。相同 Session、Skill 和 SHA-256 的重复安装返回成功；
移除时目录不存在也返回成功。不得继续使用“先 `rmtree(dest)`、再直接 `extractall(dest)`”
的非原子流程。

`AgentSkill(status=installing)` 和 install 操作任务必须在调用 AgentEnd 前同事务提交，使
并发删除能够看到引用，并使进程崩溃后的 Worker 能恢复安装。请求可以同步尝试执行该
任务；AgentEnd 安装必须支持同一 Session、Skill 和 SHA-256 的重复调用幂等。明确安装
失败时在事务中删除预约和任务；最终结果未知或 AgentEnd 安装成功但状态更新失败时标记
`sync_error` 并由任务继续对账。对同一 `(session_id, skill_name)` 的重复请求依赖现有唯一
索引幂等拒绝，不允许创建第二个活跃安装任务。

迁移期间的读取顺序由门禁配置控制：

1. `read_preference=minio`：`ObjectKey` 非空时严格读取 MinIO；仅当 `ObjectKey` 为空时读取
   `Content`。对象存在但读取或哈希失败时不得静默回退到 BLOB，以免掩盖权威副本损坏；
   应将 Skill 标记为 `storage_error` 并创建持久化校验/修复任务。
2. `read_preference=db`：`Content` 非空时严格读取 BLOB；只有完成反向回填与对账后才允许
   在生产启用。缺少 `Content` 时直接报错，不静默回退 MinIO，以便暴露不完整回滚。
3. 两种模式都校验实际字节数和 SHA-256；历史 `db` 记录尚无哈希时，首次迁移校验完成后
   回填，不能用空哈希伪装为校验通过。

### 9.2 删除 Skill

删除前继续检查是否仍有 Session 引用。建议状态机：

```text
ready → deleting → 已删除
             └──失败──→ storage_error ──重试──→ 已删除
```

操作顺序：

1. 在数据库事务中使用行锁读取 Skill，拒绝 Builtin，并检查所有状态的 `AgentSkill` 引用。
2. 无引用时以条件更新将 Skill 从 `ready/storage_error → deleting`；`storage_error` 存在其他
   活跃修复任务时先取消或终止该任务。未命中说明存在并发操作，返回冲突。
3. 在同一个数据库事务中创建持久化删除任务后再提交，避免进程在状态提交与任务创建之间
   崩溃，留下永远无人处理的 `deleting` 记录。
4. `storage_type=minio` 时先由请求线程领取并同步执行持久化删除任务；进程崩溃、请求
   超时或租约过期后由 Worker 接管重试。对象不存在视为删除成功。这样既保持接口的
   完成语义，又不会因为请求进程退出而丢失对象删除任务。
   `storage_type=db` 且 `object_key` 为空的历史记录不调用 MinIO，任务直接进入数据库删除阶段。
5. 成功后在数据库事务中依次删除关联的 receipt、当前任务和 `SkillHub`。
6. 任一步失败则将 Skill 标记为 `storage_error`，更新任务重试时间并指数退避。

不要采用“数据库先永久删除、对象删除失败只记日志”的方案，否则对象失去可追踪的元数据。
导入只能处理 `ready` Skill；因此一旦删除事务提交 `deleting`，新的导入无法再创建预约，
而删除事务又会把已存在的 `installing` 预约视为引用，从而封闭导入与删除的竞态窗口。

### 9.3 从 Session 移除

新增 `AgentSkill.Status` 后，移除流程必须同时落地，不能继续沿用“先调用 AgentEnd、失败后
仅返回错误”的隐式状态：

1. 在事务中锁定关联记录，将 `ready/sync_error → removing` 并创建唯一 remove 操作任务；
   重复移除幂等返回处理中。
2. 提交后请求线程可以同步执行任务，Worker 负责进程崩溃后的接管。调用 AgentEnd 删除
   工作区文件时，目标文件不存在视为成功。
3. 成功后在事务中删除 `AgentSkill` 和任务。只有能确认 AgentEnd 在修改前拒绝请求时才
   恢复为 `ready` 并删除任务；
   网络超时、连接中断或服务端错误都视为最终状态未知，标记 `sync_error` 并更新现有任务
   进入持久化对账重试，不能主观假定工作区文件仍然存在。
4. Skill Hub 删除把 `removing` 和 `sync_error` 都视为引用，直到关联被可靠清理。

## 10. 历史 BLOB 迁移

### 10.1 迁移策略

采用“MinIO 权威写入、观察期影子写 BLOB、旧数据双读、历史数据分批搬迁”：

```text
新上传（回滚观察期）：写 MinIO，并向 Content 写入影子副本
新上传（观察期结束）：只写 MinIO
读取：优先 MinIO；ObjectKey 为空时读取 Content
旧数据：后台迁移任务按批次上传并回填元数据
```

单条迁移流程：

1. 查询 `builtin = false AND content IS NOT NULL AND object_key = ''`。
2. 在同一数据库事务中创建或抢占唯一 migrate 任务，并把记录从 `ready → migrating`；
   其他 Worker 跳过租约未过期的任务，租约过期后允许接管。
3. 验证 BLOB 是有效 ZIP，并重新执行 Skill 格式校验。
4. 计算 SHA-256 和压缩包大小。
5. 幂等上传到 `skills/{name}/{sha256}.zip`。
6. 使用 `Stat` 和重新读取验证大小与哈希。
7. 在数据库事务中回填对象字段、`storage_type=minio` 和 `status=ready`。
8. 首轮迁移不清空 `Content`。上传或校验失败时把 Skill 恢复为 `db/ready`，错误和退避状态
   记录在持久化迁移任务中，使原 BLOB 在重试期间仍可正常导入；不得永久停留在
   `migrating` 或把非权威副本写入失败误报为不可用的 `storage_error`。
9. 全量验证和观察期结束后，再分批将 `Content` 置空。

批量迁移先按主键游标只查询 ID 和轻量元数据，再逐条、限并发读取 `Content`；禁止使用
`SELECT *` 一次加载一批 BLOB，也不使用大 OFFSET 翻页。`--batch-size` 表示每轮认领的任务
数，不表示同时把这些 ZIP 放入内存。清空 `Content` 同样按主键小批事务执行并限速，监控
MySQL undo、binlog、复制延迟和磁盘回收，不能用一次全表 UPDATE 完成。

迁移工具建议支持：

```text
--dry-run
--batch-size
--resume
--skill-name
--verify-only
--reverse-to-db
```

`--reverse-to-db` 只处理 `object_key` 非空的记录，从 MinIO 限量读取、校验大小和 SHA-256
后逐条回填 `Content`；重复执行必须幂等，且不得覆盖一份哈希不同的现有 BLOB。

### 10.2 回滚

- 回滚观察期内，新上传和历史 Skill 都保留 `Content`，可通过配置把读取优先级切回 BLOB。
- 停止影子写 BLOB 是一次明确的不可逆切换门禁；执行前必须确认历史迁移全量通过、观察期
  无存储错误且没有仅存在于 MinIO 的 Skill。
- 如果停止影子写后仍需回滚，必须先运行 MinIO → BLOB 反向回填命令，完成数量、大小和
  SHA-256 对账后才能切回 BLOB；禁止直接切换读取优先级。
- 清空 BLOB 后：必须由 MinIO 备份恢复，不再保证数据库独立回滚。
- 未完成全量校验和观察期前，不删除 `Content` 列。
- 删除 `Content` 列属于后续独立变更，不与首次上线合并。

### 10.3 对账与修复

提供默认只读的对账命令或定时任务，至少检查：

- `storage_type=minio` 且 `status=ready` 的每条数据库记录都存在正式对象，大小和 SHA-256
  一致；不一致时标记 `storage_error`，不回退读取旧 BLOB。
- 正式对象反向关联到现有 `SkillHub` 或活跃操作任务；超过孤儿宽限期的无引用对象只生成
  候选报告，显式启用 `--repair` 后才删除。
- receipt 引用的 Skill 存在且哈希一致，操作任务没有超过租约长期卡在处理中。
- 观察期内 MinIO 与影子 BLOB 的数量、大小和 SHA-256 完全一致。

对账使用主键/对象键游标和限速，不执行全量内存聚合。所有自动修复必须幂等并写审计日志；
哈希冲突、双边内容都存在但不一致等不可判定场景只告警，禁止自动选择一边覆盖另一边。

## 11. 配置与 Docker 部署

Backend 配置建议增加：

```yaml
skill_storage:
  enabled: true                # 总开关；为 false 时仍走 DB-BLOB 兼容路径
  type: minio
  endpoint: minio:9000
  bucket: skill-packages
  use_ssl: false
  upload_session_ttl: 15m
  # Go duration syntax; 30 days = 720h.
  receipt_retention: 720h
  read_preference: minio
  shadow_write_blob: true
  allow_legacy_tmp_confirm: true
  confirm_lease: 2m
  orphan_grace_period: 48h
  incoming_ttl: 24h            # incoming/ 临时对象过期阈值，由 reconcile Worker 清理
  temp_dir: /var/lib/agenthub/skill-tmp
  min_temp_free_bytes: 1GiB
  max_upload_size: 10MiB
  max_package_size: 12MiB
  max_file_size: 10MiB
  max_unpacked_size: 50MiB
  max_compression_ratio: 100
  max_file_count: 200
  max_concurrent_validations: 4
  validation_timeout: 2m
  # 可选内容信任策略（详见 §8.1）：
  # require_admin: true
  # reject_binaries: false
  # reject_executables: false
  # content_scan_command: ""
  # content_scan_timeout: 2m
  # ca_file: /etc/agenthub/minio-ca.crt
```

`read_preference`、`shadow_write_blob` 和 `allow_legacy_tmp_confirm` 是迁移期门禁配置：阶段 C
使用上面的值；旧请求清空后关闭 `allow_legacy_tmp_confirm`；回滚观察期通过后再关闭
`shadow_write_blob`。生产环境不得在没有完成反向回填和对账的情况下把
`read_preference` 切换为 `db`。

阶段 A 中 MinIO 还不是权威存储，MinIO 初始化失败不能让现有 BLOB 读写链路停止服务；
此时只降低 Skill Storage readiness 并禁止开启 MinIO 功能开关。阶段 B 的全部 Backend
升级完成后，才把 MinIO 健康检查提升为切换门禁。健康端点应区分进程存活状态与 MinIO
readiness，避免对象存储短暂故障触发无意义的 Backend 重启循环。

敏感信息由环境变量注入：

```dotenv
# 仅供 MinIO 服务初始化和管理员应急使用，不注入 Backend
MINIO_ROOT_USER=minio-admin
MINIO_ROOT_PASSWORD=replace-with-admin-secret

# Backend 使用的 Bucket 最小权限凭据
MINIO_ACCESS_KEY=agenthub
MINIO_SECRET_KEY=replace-with-strong-secret
```

Bucket 初始化任务使用 Root 凭据创建专用用户和最小权限策略；Backend 容器只注入应用凭据，
不得获得 Root 凭据。生产环境优先通过 Secret 管理机制挂载，不在 Compose 或仓库中保存明文。

本地 Docker 可以使用 `use_ssl: false`；生产必须启用 TLS、校验服务端证书和自定义 CA，禁止
明文传输 Skill 包与凭据。MinIO Client 应配置连接、读取和单次操作超时；自动重试只用于
`Stat`、读取以及具备确定幂等语义的 `Put/Promote/Delete`，并使用有上限的指数退避。

`docker/docker-compose.yml` 增加：

- MinIO 服务和持久卷。
- MinIO 健康检查。
- 初始化私有 Bucket 的一次性任务。
- Backend 的 MinIO 健康门禁：Compose 不把 MinIO 静态列为 Backend 启动依赖，功能开关关闭时保留 DB-BLOB 可用；开关开启后由 Backend 在启动阶段以有界超时检查私有 Bucket readiness。

生产部署要求：

- MinIO API 不直接暴露到公网。
- MinIO Console 仅管理员可访问。
- Bucket 禁止匿名读写。
- Access Key 使用最小权限，只允许目标 Bucket。
- 对象数据和 MySQL 分别制定备份、恢复与演练策略。
- 备份必须记录同一恢复点的 MySQL 快照标识和 MinIO 对象版本/清单；恢复后先运行只读对账，
  再开放确认、导入和删除写流量，避免两套独立备份时间点不一致。

## 12. 实施与发布阶段

以下阶段是开发与发布门禁，不是可以任意拆开的功能上线批次。特别是 Backend 切换上传
协议前，确认、导入、删除、补偿和双读能力必须全部就绪；禁止上线一个只能创建
`incoming` 对象却不能完成确认或导入的中间版本。

开发可以在功能开关关闭的前提下拆成以下可独立审查和回滚的合并单元：

| 单元 | 主要内容 | 合并门禁 |
|---|---|---|
| 1. 扩展模型 | 新字段、receipt、operation job、显式回填 | 旧行为与旧 API 回归通过 |
| 2. 存储基础 | PackageStore、MinIO、TLS、Docker、内存实现 | 接口契约和真实 MinIO 测试通过 |
| 3. 安全打包 | multipart 落盘、限量解压、规范 ZIP、临时清理 | fuzz、资源上限和确定性哈希测试通过 |
| 4. 上传确认 | upload session、租约、receipt、Promote、兼容旧确认 | 并发与故障注入测试通过，开关仍关闭 |
| 5. Session 同步 | 导入/移除预约、AgentEnd 原子安装、对账任务 | 三类 Agent 回归与崩溃恢复通过 |
| 6. 删除与修复 | 删除状态机、Worker、孤儿清理、双向对账 | Worker 重启和误删保护测试通过 |
| 7. 前端切换 | 生成契约、`upload_id` UI、状态展示和权限提示 | 全链路测试通过后才允许开启开关 |
| 8. 历史迁移 | dry-run、游标迁移、反向回填、BLOB 清理 | 灰度、对账和回滚演练逐级通过 |

### 阶段 A：扩展式基础设施发布

- 增加 Skill API 契约和生成类型。
- 增加 `SkillHub` 对象存储字段。
- 显式回填现有 External/Builtin Skill 的存储类型，不依赖 GORM 默认值推断。
- 增加 `PackageStore` 和 MinIO 实现。
- 增加配置加载、环境变量和启动健康检查。
- 在 Docker Compose 增加 MinIO。
- 仅扩展数据库和依赖注入，不切换现有上传、确认和读取行为。

### 阶段 B：Backend 完整兼容能力

- 加固 ZIP 流式解压和安全限制。
- 上传成功后写入 MinIO `incoming/`。
- Redis 保存带所有者和状态的上传会话，并原子确认。
- 完成正式对象确认、MinIO/BLOB 双读、影子写 BLOB、导入预约、删除状态机和持久化补偿。
- Backend 在短期兼容窗口内继续接受旧 `tmp_dir` 确认，仅用于升级前已上传且尚未确认的请求。
- 增加过期上传与临时对象清理。
- 所有新逻辑先以功能开关关闭的状态滚动到全部 Backend；确认没有旧版本实例后再统一开启，
  禁止新旧 Service 的导入/删除事务语义同时处理同一 Skill。

### 阶段 C：Frontend 切换与整批发布

- Frontend 使用 `upload_id` 替代 `tmp_dir`，确认请求只发送 `upload_id`。
- Backend 与 Frontend 作为同一切换批次发布；滚动升级期间不得让旧 Backend 处理
  MinIO-only 或 `upload_id` 请求，可通过先完成全部 Backend 升级再切 Frontend 实现。
- 开启 MinIO 权威读取和 BLOB 影子写，观察确认、导入、删除和补偿指标。
- 验证没有旧 `tmp_dir` 在途请求后，移除旧确认兼容分支。

### 阶段 D：历史迁移

- 实现可恢复、可验证的批量迁移命令。
- 先执行 `--dry-run`，再小批量迁移。
- 全量校验对象数量、大小和哈希。
- 完成回滚演练，包括把 MinIO 对象反向回填到 BLOB。

### 阶段 E：收尾

- 通过门禁后停止新上传影子写 BLOB。
- 再观察一个周期，确认不存在 MinIO-only 数据回滚需求后分批清空 `Content`。
- 移除数据库 BLOB 读取逻辑和反向回填临时代码。
- 评估是否在独立变更中删除 `Content` 列。
- 同步 Backend、Docker、测试和部署文档。

## 13. 测试计划

### 13.1 单元测试

- ZIP 路径穿越、符号链接、特殊文件和重复路径。
- ZIP Bomb、单文件超限、文件数超限和总大小超限。
- 相同文件在不同临时目录、时间戳和权限来源下生成完全相同的规范 ZIP 与 SHA-256。
- `SKILL.md` 缺失、编码错误、Front Matter 错误和名称不一致。
- MinIO Put、Promote、Open、Stat、分页 List、Delete 失败。
- 正式对象已存在且内容一致时幂等复用，大小或哈希不一致时拒绝覆盖。
- Redis 上传会话原子状态转换、所有者校验和确认结果幂等。
- 确认租约过期接管、token fencing、有效租约拒绝并发接管和 receipt 优先返回。
- Redis 丢失后的 receipt 查询仍校验所有者，跨用户 `upload_id` 返回拒绝。
- 未授权角色不能上传、确认或删除 External Skill，审计记录包含上传者和 SHA-256。
- 可执行/二进制策略与恶意软件扫描失败、超时均按失败关闭。
- 确认时数据库失败后不误删其他请求已引用的确定性对象。
- 导入时大小或哈希不一致。
- 导入预约创建、安装失败回滚和 `sync_error` 补偿。
- AgentEnd 临时目录解压、原子替换、残留恢复、重复安装和重复移除幂等。
- Session 移除的 `removing` 状态、AgentEnd 文件不存在和对账补偿。
- 删除失败进入 `storage_error` 并成功重试。
- BLOB-only 历史 Skill 删除不调用 MinIO。
- 持久化补偿 Worker 重启恢复、指数退避和重复执行幂等。
- 请求取消会中止同步 MinIO I/O，Worker 关闭会取消后台 I/O。
- 多个合法大小上传并发时受信号量、临时卷预留水位和超时限制，崩溃遗留临时目录可被
  安全清理且不会误删其他文件。
- 迁移期间 `read_preference=minio/db` 的严格读取与禁止静默回退行为。
- 列表、按名称和 Session Skill 元数据查询不会选择或加载 `Content`。

### 13.2 集成测试

- 使用真实 MinIO 容器完成上传、确认、导入和删除闭环。
- Backend 重启后仍可使用 `upload_id` 完成确认。
- 数据库提交后 Redis 会话丢失，重复确认仍可通过 receipt 返回原结果。
- Backend 在 `confirming` 状态崩溃后，其他实例可在租约过期后继续确认。
- 两个 Backend 实例可读取同一 Skill 包。
- 两个 Backend 并发确认同一个 `upload_id` 时只产生一条记录且不会误删正式对象。
- 两个不同 `upload_id` 并发确认同名同哈希 Skill 时结果幂等且正式对象仍然存在。
- 导入与删除并发时，要么保留有效引用并拒绝删除，要么先进入删除态并拒绝导入。
- MinIO 临时不可用时不产生可用状态的数据库记录。
- Redis 写会话失败时 `incoming` 对象最终被生命周期规则或清理 Worker 删除。
- 孤儿清理不会删除宽限期内对象、已引用对象或存在待执行补偿任务的对象。
- 迁移命令中断后可继续执行，重复执行保持幂等。
- 迁移按游标逐条读取 BLOB，批次大小增长不会使内存按全部 ZIP 总和增长。
- 只读对账能发现缺失、损坏、孤儿对象和悬空 receipt；修复模式不会覆盖不可判定冲突。
- 新旧数据在回滚演练中均可由 MinIO 反向回填并切回 BLOB 读取。

### 13.3 回归测试

- Builtin Skill 上报和供给不受影响。
- External Skill 列表、导入计数和 Session 关联不受影响。
- Claude Code、OpenCode、Codex、Pi 四类 Agent 均可安装和移除 Skill。
- AgentEnd 在替换目录前后模拟崩溃均不会破坏上一份完整安装，重试后可收敛。
- Orchestrator 继续禁止导入 External Skill。

### 13.4 真实外部服务集成测试

`backend/internal/service/impl/skill_fullchain_integration_test.go` 用真实 MinIO + Redis +
MySQL 串起完整 External Skill 生命周期，AgentEnd 使用可控 fake client 记录安装/移除调用并注入
故障；“重启”通过构造全新的进程内 `SkillService` 实例模拟。该测试代码已具备，但本仓库当前尚未
在一次性真实外部环境执行，因此不能将结果标记为已验证。测试默认跳过，仅当显式启用时才连接
外部服务，避免污染开发者本机数据库：

```bash
# 在 backend/ 目录下，注入本机 MySQL/Redis 连接信息与 MinIO 凭据后运行
export SKILL_E2E=1
export SKILL_E2E_MYSQL_HOST=127.0.0.1 SKILL_E2E_MYSQL_PORT=3307 \
       SKILL_E2E_MYSQL_USER=root SKILL_E2E_MYSQL_PASSWORD=... SKILL_E2E_MYSQL_DBNAME=agenthub
export SKILL_E2E_REDIS_HOST=127.0.0.1 SKILL_E2E_REDIS_PORT=6380 \
       SKILL_E2E_REDIS_PASSWORD=... SKILL_E2E_REDIS_DB=0
export SKILL_E2E_MINIO_ENDPOINT=127.0.0.1:19000 SKILL_E2E_MINIO_BUCKET=e2e-skill-packages \
       SKILL_E2E_MINIO_ACCESS_KEY=... SKILL_E2E_MINIO_SECRET_KEY=...
go test ./internal/service/impl/ -run TestE2E -count=1 -timeout 600s
```

包含的用例：

| 用例 | 覆盖的验收点 |
|---|---|
| `TestE2E_FullChainMinIO` | upload → confirm（含 incoming→内容寻址对象、receipt、影子写）→ import（MinIO 读回校验）→ remove → delete（同步删对象 + 级联） |
| `TestE2E_ConfirmIdempotent_ReceiptSurvivesRedisLoss` | 同 `upload_id` 幂等；MySQL 提交后 Redis 会话丢失仍能凭 receipt 返回已确认结果 |
| `TestE2E_ConcurrentConfirmSameUploadID` | 并发确认同一 `upload_id` 只产生一条记录，不误删确定性正式对象 |
| `TestE2E_RestartAcrossInstances` | 一个实例确认、全新实例（无共享内存）导入/移除，证明状态完全持久化 |
| `TestE2E_FaultImportMissingObject` | 正式对象被篡改/删除时导入按失败关闭，不调用 AgentEnd、不静默回退 |
| `TestE2E_ObservationShadowBlob_DualReadRollback` | 观察期影子写 BLOB 与 MinIO 对象 SHA-256 一致；`read_preference=minio` 与 `db` 双读均可导入（回滚演练） |

每个用例使用唯一 Skill 名与 Session ID 并在 `t.Cleanup` 中清理数据库行、MinIO 对象和 Redis
键。配套的 `backend/internal/dao/gorm/skill_storage_integration_test.go`（`MYSQL_INTEGRATION=1`）
还显式断言 `GetSkillContentLimited`/`GetSkillContentByIDLimited` 的限量读取扫描路径，回归
此前 `LEFT(content, ?)` 投影被 `database/sql` 误扫为单字节 `uint8` 的缺陷。

## 14. 可观测性

建议增加以下结构化日志和指标：

- 上传、确认、导入、删除耗时。
- MinIO 请求成功率和错误码。
- `incoming` 对象数量和过期清理数量。
- 正式对象与数据库记录不一致数量。
- 哈希校验失败次数。
- BLOB 待迁移、已迁移和失败数量。
- `storage_error` 数量和重试结果。

日志只记录 `upload_id`、Skill 名称、对象键和错误类型，不记录 MinIO 密钥、ZIP 内容或预签名地址。

## 15. 验收标准

- 回滚观察期结束后，新上传 External Skill 的 ZIP 不再写入 MySQL BLOB。
- MinIO Bucket 保持私有，Frontend 不接触永久对象 URL。
- Frontend 与 API 不再传递服务器 `tmp_dir`。
- 同一个 `upload_id` 或同名同哈希 Skill 的并发确认不会产生重复记录或误删正式对象。
- Redis 会话丢失或确认实例崩溃后，确认可依靠 receipt 和租约安全重试。
- receipt 幂等查询不会绕过上传所有者校验。
- 不合法 ZIP 无法进入 `skills/` 正式对象前缀。
- 确认失败不会产生 `ready` 状态的孤立数据库记录。
- 导入前会验证对象大小和 SHA-256。
- 导入和删除并发不会产生“对象已删除但 Session 新增关联”的状态。
- Session 移除失败具有可诊断状态和持久化补偿，不留下不可追踪的工作区/数据库差异。
- MinIO 读取失败或对象损坏时禁止安装并提供可诊断错误。
- 对象删除与补偿任务在 Backend/Worker 重启后能够继续，重复执行保持幂等。
- 历史 BLOB 迁移支持试运行、中断恢复、幂等执行和全量验证。
- 停止影子写前完成切回 BLOB 的回滚演练；停止后只能经反向回填和对账再回滚。
- 本地开发与 Docker 环境均可完成上传、确认、导入和删除闭环。
- 相关契约、设计、部署和测试文档保持同步。

## 16. 实施清单

- [x] 新增 Skill API Schema、生成类型和契约变更日志
- [x] 新增 `SkillHub` 对象存储字段
- [x] 增加 `AgentSkill` 安装状态、`skill_upload_receipts` 和持久化 `skill_operation_jobs`/Outbox
- [x] 将列表和元数据 DAO 改为显式投影，禁止普通查询加载 `Content`
- [x] 新增 `PackageStore` 接口及 MinIO 实现
- [x] 增加 MinIO 配置、TLS、最小权限凭据、超时和健康检查
- [x] 在 Docker Compose 中部署并初始化 MinIO
- [x] 加固 multipart 限流、ZIP 校验、磁盘流式解压、并发配额和崩溃临时目录清理
- [x] 增加 External Skill 管理权限、来源审计、文件清单展示和可选内容扫描
- [x] 上传接口返回 `upload_id` 并写入 `incoming/`
- [x] Redis 保存带所有者/状态/租约的上传会话，receipt 优先查询，确认时原子抢占并幂等返回
- [x] 确认接口复制正式对象并写入元数据，孤儿对象由引用感知 Worker 清理
- [x] 导入流程创建安装预约，再从 MinIO 读取并校验对象
- [x] AgentEnd 安装改为限量解压、原子替换和幂等重试，移除不存在目录视为成功
- [x] Session 移除流程接入 `removing` 状态和持久化对账补偿
- [x] 删除流程增加行锁、条件状态转换、持久化补偿和重试
- [x] Frontend 主流程移除 `tmp_dir` 并接入 `upload_id`（兼容窗口仍受配置门禁控制）
- [x] 实现历史 BLOB 迁移与验证命令
- [x] 实现双向只读对账、显式修复和审计输出
- [x] 实现观察期 BLOB 影子写与 MinIO → BLOB 反向回填命令
- [x] 提供观察期结束后的显式确认、逐条校验、可恢复 BLOB 清理命令
- [x] 增加单元、显式门控的 MinIO/Redis/MySQL 基础设施测试、回归与 fuzz 测试
- [ ] 在一次性真实外部服务环境完成上传→确认→导入→移除→删除全链路、多实例/重启和故障注入集成测试（测试代码已具备；`SKILL_E2E=1` 的真实环境执行待完成；详见 [13.4](#134-真实外部服务集成测试)）
- [ ] 回滚演练、观察期影子写与 BLOB 清理命令经真实 MinIO+MySQL 验证（命令已实现；`skill-migrate --reverse-to-db/--clear-content`、`skill-reconcile` 的外部执行与输出留存待完成）
- [ ] 完成兼容发布与灰度上线（运维发布活动，依赖生产滚动升级与观察期监控；非代码门禁）
- [x] 同步 Backend、Docker、部署和测试文档
