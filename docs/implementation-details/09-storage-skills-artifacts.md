# 存储、Skills 与 Artifact 超详细实现

## 实现了什么

AgentHub 将结构化业务数据和大对象分开：MySQL 保存可查询元数据；MinIO 可选保存 Skill ZIP、头像和 HTML Artifact。三种对象使用独立 bucket、独立配置和不同授权模型，不能因为共用 MinIO 实例就混用 key 或权限。

## 怎么实现的

### 存储总表

| 类型 | 默认 bucket | 元数据 | 写入者 | 读取者 | 可见性 |
|---|---|---|---|---|---|
| Avatar | `agenthub-assets` | Session/Admin URL | Backend | Backend asset controller | 公开只读代理 |
| Skill package | `skill-packages` | SkillHub/receipt/audit/job | Backend | Backend + AgentEnd install | 私有 |
| Artifact | `agenthub-artifacts` | Artifact | builtin render → Backend | Backend content controller | 私有认证读取 |

Docker `minio-init` 根据三个 feature gate 创建 bucket 和 policy。应用凭据来自 `docker/configs/backend/.env`；浏览器和普通 CLI 不直接持有 MinIO root 凭据。

### 头像 Runtime

`backend/pkg/storage/` 定义 ObjectReader/Writer 等抽象和 MinIO、local、memory 实现。`factory.go` 根据：

- `storage.write_provider` 选择新上传写入位置。
- `storage.minio.enabled` 决定 MinIO reader/writer 可用。
- `storage.local.enabled` 保留本地历史头像读取。

write provider 为 MinIO 时不会在失败后悄悄回落本地，因为静默回落会造成部署节点间不一致。无 MinIO 的开发环境必须显式设置 local。

头像上传限制 2 MiB，只允许 jpg/jpeg/png/gif/webp。Backend 生成受控 object key/URL；公开 `GET/HEAD /api/assets/avatars/*path` 再次解析 key 与扩展名，不接受 bucket。响应设置固定 Content-Type、`nosniff`、强 ETag 和一年 immutable cache；If-None-Match 可返回 304。

### SkillHub 模型

SkillHub 合并 builtin 与 external：builtin 由 AgentEnd 启动上报；external 由管理员上传。external Skill 记录 description、file count、total/package size、SHA-256、object key、storage type、status、文件清单、安全扫描标记。

兼容字段 `Content longblob` 在 MinIO 迁移期保留。`read_preference`、`shadow_write_blob`、`allow_legacy_tmp_confirm` 控制双读写窗口。完成迁移前不能直接删除 BLOB 列。

### Skill 上传阶段

`POST /api/skills/upload` 只做接收和预验证：

1. 获取并发 validation admission，检查 temp volume 空间。
2. 用 `MaxBytesReader` 限制完整 multipart envelope，默认上传 10 MiB 级别，具体读配置。
3. 流式写入受控 temp dir，不信任原始 filename 作为目标路径。
4. 验证 ZIP central directory、entry 名、文件数、单文件/解压后总大小、压缩比。
5. 拒绝绝对路径、`..`、symlink 和不允许的特殊条目。
6. 找到并解析 `SKILL.md`，得到 name/description。
7. 检测 binary/executable，并按 reject 开关决定是否拒绝。
8. 可选执行 content scan command，受独立 timeout 控制。
9. 计算 package SHA-256，写入 incoming object 或上传 session，返回 upload id 与确认信息。

默认配置包括 max package 12 MiB、单文件 10 MiB、解压总量 50 MiB、压缩比 100、文件数 200、并发验证 4、验证超时 2m、temp 最低空间 1 GiB。实际代码解析 human-readable byte size 和 duration。

### Skill 确认阶段

`POST /api/skills/confirm` 使用 upload_id：

- Redis upload session 保存短期阶段状态，默认 TTL 15m。
- DB SkillUploadReceipt 保存最终确认结果，默认保留 720h，用于 Redis 丢失后的幂等返回。
- confirm lease 防止两个请求同时完成同一上传。
- 校验客户端回传 name/file_count/size/hash 与服务器事实一致。
- 在事务中写 SkillHub、receipt、audit 和需要的 operation job。
- MinIO 最终 object key 不从用户路径直接拼接。
- shadow write 开启时同步保留 DB blob 回滚副本。

incoming orphan 使用 grace/TTL 清理，不能刚过 session TTL 就立即删除可能仍在确认的对象。

### Skill 导入与移除

导入 API 创建 AgentSkill 状态 installing，并通过 AgentEnd `/v1/skills/:agentType/:skillName/install` 安装到 Session Workspace 的 Agent 配置目录。成功置 ready；失败置 sync_error 并由 SkillOperationJob 重试。

AgentEnd 安装是原子流程：

1. 校验 agent type、skill name、session 与 Workspace。
2. 限制请求 body，安全解析 ZIP。
3. 解压到同一文件系统 staging。
4. 验证 staging 中存在合法 SKILL.md 且无 symlink/路径逃逸。
5. 将已有目录改名为 backup，再把 staging 原子 rename 到目标。
6. 成功删除 backup；失败恢复 backup。
7. 启动时扫描 stale staging/backup，按规则恢复。

移除使用 AgentSkillID fence：旧 remove job 不能删除后来重新导入的同名 Skill。builtin Skill 不通过 AgentSkill 关系安装。

### Skill 删除与 Outbox

删除 SkillHub 数据和 MinIO 对象不能靠一个跨系统事务。Service 先把数据库状态置 deleting 并写唯一 idempotency SkillOperationJob；worker 再删对象并完成记录。对象服务暂时失败会增加 attempts、next_retry_at 和 last_error，而不是回滚已接受的用户请求或丢失意图。

`cmd/skill-migrate` 将旧 BLOB 搬到 MinIO；`cmd/skill-reconcile` 比对 DB metadata、对象存在性、hash 和状态。两者分别通过 `make skills migrate ARGS="<参数>"` 与 `make skills reconcile ARGS="<参数>"` 使用。

### Artifact 数据模型

Artifact 绑定 resource_id、task_id、session_id、message_id、可选 idempotency key、kind、object key、filename、content type、size、SHA-256 和 status。当前 kind 主要是 html；状态 pending、ready、failed、deleting、deleted。

message_id + idempotency key 的联合唯一约束避免 render 重试产生重复资源。对象 key 与 last_error 不对外 JSON 暴露。

### Artifact capability

Backend 在启动 Run 时签发短期 capability，至少约束：

- task/session/message identity。
- 允许 kind。
- 最大对象大小与每消息对象数量。
- 预期 digest/filename 等请求上下文。
- 过期时间和不可伪造签名。

render 通过环境变量得到上传 URL 与 capability，不得到 MinIO access key。`POST /api/internal/artifacts` 从 Bearer token 解析 capability，先做 token 验证，再解析受限 multipart。

### Artifact 上传验证

Controller 默认最大对象 25 MiB，并给 multipart envelope 额外 1 MiB。它要求恰好一个 kind 和一个 file，拒绝未知 form field。Service 再验证：

- capability 未过期且绑定完全一致。
- feature gate 和 store 可用。
- kind/content type/filename 合法。
- 读取字节数未超过声明限制。
- SHA-256 与声明一致。
- 同 message 的 artifact 数未超限。
- 幂等键已存在时只返回同一事实。

对象上传成功与 DB 状态转换之间的失败使用 failed status、last error 和 retention cleanup 处理。

### Artifact 读取

`GET /api/artifacts/:resourceId` 返回可信元数据；GET/HEAD content 从私有 store 流式读取。HTML 响应设置：

- private immutable cache。
- 禁止 MIME sniff。
- CSP 默认拒绝所有资源，只允许 inline style、data/https image，禁止 script，frame ancestor 仅 self。

Frontend 仍使用 sandbox iframe。CSP 与 sandbox 是不同层的约束，都不能移除。

### 备份与恢复

Docker MinIO 提供 `backup.sh`、`restore.sh`，volume 为 `minio_data`。数据库备份和对象备份必须保持可关联：只恢复 MySQL 会留下 metadata 指向不存在对象；只恢复对象会产生 orphan。Skill reconcile 和 Artifact 状态检查用于发现偏差，但不能替代一致的备份策略。
