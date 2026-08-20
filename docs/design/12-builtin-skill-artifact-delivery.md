# 12 — 内置 Skill 资源脱离 SSE 第一阶段实施规划

> **状态**：第一阶段实现已落地（目标自动化验证通过；全量 Go 测试另有 2 个既有 IPv6 sandbox listener 测试受环境权限阻断；真实 MinIO/端到端联调待部署环境执行）
> **日期**：2026-08-12
> **范围**：AgentEnd、Backend、Frontend、Contracts、MinIO、Docker
> **关联文档**：[SSE 流式输出架构](sse-streaming-architecture.md)、[Skills ZIP MinIO 迁移](10-skills-minio-storage-migration.md)、[头像 MinIO 迁移](11-avatar-qiniu-to-minio-migration.md)

当前代码已经覆盖契约、Backend Artifact Store/API、AgentEnd SSE 脱敏与进程环境注入、
`render html-render` 上传、Frontend `resourceId` 卡片和 Docker/MinIO 初始化。功能默认关闭；
启用前仍需按本文第 11、12、13 节完成真实 MinIO 联调、鉴权部署验证和灰度观测。

### 本轮复核补强

- 上传与 Task 删除共用 Agent Message 行锁；消息级 Artifact 配额在同一事务中计数，避免并发超额和删除竞态。
- 删除流程改为数据库级联成功后再清理 MinIO；`pending` 上传不在任务删除的即时清理范围内，等上传状态收敛后再回收，避免“删除先完成、上传后写入”的孤儿对象；失败对象保留元数据，由 cleanup worker 重试。
- `Put` 返回未知结果、`Stat` 校验失败或元数据最终提交异常时，都会按不可变 object key 做补偿删除；row 保留时再由 cleanup worker 兜底，避免网络异常留下无法追踪的对象。
- AgentEnd 错误事件增加 UTF-8 有界裁剪；MinIO 健康探测改用 `GetBucketLocation`，运行账号不需要 `ListBucket`。
- 首次 Orchestrator 运行可安全创建缺失共享目录，同时拒绝 symlink 和路径穿越；Frontend 资源字段解析对缩进和重复字段保持严格校验。
- 已存在的 Agent workspace 在再次运行前也会刷新受管 Skill；Artifact capability 密钥不得复用 JWT、管理员或任一 MinIO 应用密钥。
- 第一阶段 Artifact 上传上限固定为 25MiB，与当前受限内存缓冲实现和 `render` 客户端上限一致；提高上限前必须先切换专用临时文件方案。

### 本轮复核仍保留的边界

- `MarkReady` 已提交但 AgentEnd 在收到响应前崩溃时，Artifact 仍是合法的 `ready` 对象，当前无法仅凭数据库区分“尚未被消息引用”的孤儿资源；任务删除可以回收，长期存活任务需要第二阶段增加引用确认或 ready 反向对账。
- 开启完整普通用户 Auth 时，iframe 仍不能附加 Bearer；读取票据/Cookie 会话尚未实现。真实 MinIO、单 Agent/Orchestrator 1MiB 联调和灰度观测仍属于发布前验收项。

## 1. 背景

AgentEnd 当前向 Agent 工作区供给两个内置 Skill：

- `taskctl`：读取共享计划和记忆、写入私有记忆、合并 Agent 分支。
- `render`：生成 HTML、图片、附件、Diff 和 Preview 卡片标记。

外部 Agent CLI 会把工具参数和工具结果转换为 `tool_call` / `tool_result` 事件。对于
`render html-render`，完整 HTML 可能依次出现在：

1. Bash、command execution 或 heredoc 的工具参数中。
2. `render` stdout 对应的工具结果中。
3. Agent 按 `SkillRule` 要求复制到最终回复的文本事件中。
4. Claude CLI 的 `done.text` 最终结果中。

这些事件经过 AgentEnd SSE、Backend RuntimeHub、Redis Stream 和浏览器 EventSource。Frontend
虽然不展示工具参数与结果，仍然需要下载和解析完整 JSON。最终回复中的内联 HTML还会进入
`streamingContent`；`MessageList` 在流式更新时反复对累计全文执行 `reduceEventToBlocks()`，
大内容会产生接近 O(n²) 的重复扫描，并在闭合后进入 `iframe.srcDoc`。

`taskctl summary/common-memory/sub-memory` 也可能产生大 stdout，但这些内容属于 Agent 内部
上下文，不是用户可见产物。Agent CLI 必须获得完整结果，Frontend 不应接收或持久化副本。

## 2. 第一阶段目标与非目标

### 2.1 目标

- AgentEnd 对外 SSE 不再携带完整工具参数、工具结果和重复的 `done.text`。
- `taskctl` 的完整 stdout 仍可被 Agent CLI 使用，但不进入 Backend 和 Frontend 链路。
- `render html-render` 将 HTML实体写入私有 MinIO Bucket。
- Message正文、Redis Stream 和 Frontend状态只保存小型 `resourceId` 引用。
- HTML卡片通过独立 Backend HTTP接口读取，不把 HTML内容装入 React状态或 `srcDoc`。
- 单 Agent、Orchestrator 子 Agent、消息历史恢复和 workspace 清理后都能读取资源。
- 旧版内联 `html-render` 消息保持可读，发布过程允许新旧格式共存。
- 上传权限限制在当前 task、session 和 message，Agent不接触 MinIO长期凭据。

### 2.2 非目标

- 第一阶段不把 `taskctl` 输出存入 MinIO。
- 第一阶段不迁移 `render image`、`attachment`、`diff` 和 `preview`。
- 第一阶段不启用 `artifact` SSE 事件作为资源主链路。
- 第一阶段不把 Message重构成结构化 Block表。
- 第一阶段不提供浏览器直连 MinIO或永久公开 URL。
- 第一阶段不实现 presigned PUT/GET；资源由 Backend接收和代理读取。
- 第一阶段不做跨资源内容去重、资源版本管理或通用媒体处理。

## 3. 已确认的现状边界

| 位置 | 当前行为 | 第一阶段影响 |
|---|---|---|
| `agentend/src/adapters/claude.py` | `tool_call.args`、`tool_result.result`、`done.text` 可携带大内容 | 对外流统一裁剪 |
| `agentend/src/adapters/codex.py` | command 和 aggregated stdout 分别进入工具事件 | 对外流统一裁剪 |
| `agentend/src/adapters/opencode.py` | 成功输出可能位于 `tool_call.result` | 裁剪时覆盖 OpenCode差异 |
| `agentend/src/api/v1/agent.py` | Adapter事件直接经 trace 后写入 EventSourceResponse | 增加 transport sanitizer |
| `agentend/src/rules/builtin.py` | 要求 Agent把 `render` stdout包含在回复中 | stdout改成轻量引用后保留约定 |
| `backend/internal/stream/writer.go` | 非 TEXT事件原样进入 RuntimeHub和 Redis | 上游瘦身后无需解析资源实体 |
| `frontend/src/hooks/use-chat-stream.ts` | 工具事件只消费名称和完成状态 | 裁剪参数/结果不影响现有 UI |
| `frontend/src/components/chat/MessageList.tsx` | 每次流式更新重新解析累计正文 | 新消息不再包含 HTML实体 |
| `frontend/src/components/cards/HtmlCard.tsx` | 使用 `iframe.srcDoc` | 新格式改用独立 content URL |
| `agentend/src/orchestrator/execution/engine.py` | 子 Agent流只转发 text/done/error | 资源引用继续走小型文本块 |
| `agentend/src/skills/provisioner.py` | 已存在 Skill目录时跳过更新 | 必须解决旧 worktree升级 |

`artifact` 虽已存在于 EventType枚举，但当前 AgentEnd不产生、Backend不持久化、Frontend不消费，
Orchestrator子 Agent转发也会丢弃。因此第一阶段继续使用可穿越现有文本、分组和持久化链路的
小型 `aka_yhy` 引用块。

## 4. 目标架构

```text
Agent 调用 render html-render
          |
          | stdin: HTML
          v
render CLI --POST multipart--> Backend Artifact Upload API
                                   |
                                   | Put + Stat
                                   v
                          private MinIO Bucket
                                   |
render CLI <--resource_id-----------+
          |
          | stdout: 小型 aka_yhy 引用块
          v
Agent 最终回复 -> AgentEnd SSE -> Backend -> Frontend
                                   |
                                   | 仅 resourceId
                                   v
Frontend iframe -> GET /api/artifacts/:id/content -> Backend -> MinIO
```

控制面和数据面分离：

| 链路 | 内容 |
|---|---|
| Agent SSE控制面 | 文本 token、工具状态、`resourceId` 引用 |
| Artifact HTTP数据面 | HTML资源实体 |
| MySQL | Message小型引用、Artifact元数据和对象关联 |
| MinIO | 不可变 HTML实体 |

## 5. 跨端契约设计

### 5.1 AgentRequest

修改 `contracts/schemas/agent-request.yaml`，为 Backend → AgentEnd运行请求增加可选字段：

```yaml
message_id:
  type:
    - string
    - "null"
  description: "Backend 为本轮 Agent 回复创建的消息 ID"

artifact_upload_token:
  type:
    - string
    - "null"
  description: "仅允许为当前消息上传 Artifact 的短期能力令牌"
```

`message_id` 当前已在 `TaskService.RunTask` 中生成，并已作为 `buildAgentRequest` 参数传入，
但尚未写入生成的 AgentRequest。第一阶段补齐该关联，避免由 `render` 或 AgentEnd信任
客户端提交的 task/session/message归属。

`artifact_upload_token` 是 Backend与 AgentEnd之间的内部能力，不进入 Frontend消息响应、
日志、Langfuse metadata或错误正文。契约生成后必须检查所有生成类型不包含 `repr`/日志输出
敏感字段的自定义行为。

### 5.2 资源引用块

新 HTML消息使用：

````text
```aka_yhy
type: html-render
resourceId: 6dd9a56e-40b9-4c1d-80bf-2fd19540db88
```
````

兼容规则：

- `html-render + resourceId`：新格式，通过 Artifact内容接口渲染。
- `html-render + inline content`：旧格式，继续通过 `srcDoc` 渲染。
- 同一个块同时存在 `resourceId` 与 inline content 时拒绝解析为新格式，避免来源歧义。
- `resourceId` 必须是规范 UUID，Frontend和 Backend都不得接受任意 object key或 URL。

第一阶段不为引用块新增独立 JSON Schema；它仍是 Message正文中的应用级块格式。后续若
Message升级为结构化 Block，再将其迁入正式契约。

### 5.3 契约变更流程

1. 修改 `contracts/schemas/agent-request.yaml`。
2. 运行 `make generate`。
3. 在 `contracts/logs/` 新增 `YYYY-MM-DD-artifact-upload-context.md`。
4. 检查 Frontend、Backend、AgentEnd生成类型差异。
5. 禁止手改任何 `generated/` 文件。

## 6. Backend 实施规划

### 6.1 配置与凭据

新增独立配置，不复用头像 `storage.Provider` 或 Skill `package_store`：

```yaml
artifact_storage:
  enabled: false
  endpoint: 127.0.0.1:19000
  bucket: agenthub-artifacts
  access_key: ""
  secret_key: ""
  use_ssl: false
  ca_file: ""
  request_timeout: 15s
  max_object_size: 25MiB
  max_artifacts_per_message: 20
  upload_token_ttl: 30m
  failed_retention: 24h
```

另设 Artifact capability签名密钥，禁止复用用户 JWT、管理员 JWT或 MinIO SecretKey。
MinIO应用账号可与 Avatar、Skill 共用，但 Bucket保持独立。配置加载需要支持 `.env` 覆盖、
启动校验和 Docker模板。

建议环境变量前缀：

```text
ARTIFACT_STORAGE_ENABLED
ARTIFACT_MINIO_ENDPOINT
ARTIFACT_MINIO_BUCKET
ARTIFACT_MINIO_ACCESS_KEY
ARTIFACT_MINIO_SECRET_KEY
ARTIFACT_MINIO_USE_SSL
ARTIFACT_MINIO_CA_CERT
ARTIFACT_MINIO_REQUEST_TIMEOUT
ARTIFACT_MAX_OBJECT_SIZE
ARTIFACT_MAX_PER_MESSAGE
ARTIFACT_UPLOAD_TOKEN_TTL
ARTIFACT_CAPABILITY_SECRET
```

启用 Artifact Storage时，缺失 Bucket、凭据或签名密钥必须启动失败；禁止自动回退为 Message
内联 HTML。`/ready` 必须检查 Artifact MinIO可用性。

### 6.2 私有 ArtifactStore

新增目录：

```text
backend/pkg/artifact_store/
├── store.go
├── minio.go
├── memory.go
├── minio_test.go
└── memory_test.go
```

建议接口：

```go
type Store interface {
    Put(ctx context.Context, key string, body io.Reader, size int64, options PutOptions) error
    Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
    Stat(ctx context.Context, key string) (ObjectInfo, error)
    Delete(ctx context.Context, key string) error
    Health(ctx context.Context) error
}
```

约束：

- Bucket私有，禁止匿名策略。
- `Put` 使用请求 Context和独立超时。
- 对象键由 Backend生成，上传请求不能提交对象键。
- 正式对象不可覆盖；UUID键冲突直接失败。
- `ObjectInfo` 至少包含 Size、ContentType、ETag和 SHA-256 metadata。
- MinIO错误映射复用项目现有 timeout/not-found/permission语义，但不复用头像业务接口。

对象键：

```text
artifacts/{task_id}/{message_id}/{resource_id}
```

用户文件名只存在 MySQL元数据和 `Content-Disposition` 中，不进入对象键。

### 6.3 Artifact 数据模型与 DAO

新增 `backend/internal/model/artifact.go`：

```go
type Artifact struct {
    ID          uint
    ResourceID  string
    TaskID      string
    SessionID   string
    MessageID   string
    IdempotencyKey *string
    Kind        string
    ObjectKey   string
    Filename    string
    ContentType string
    Size        int64
    SHA256      string
    Status      string
    LastError   string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

状态：

```text
pending -> ready
pending -> failed
ready   -> deleting -> deleted
failed  -> deleting -> deleted
```

索引与约束：

- `resource_id` 唯一索引。
- `task_id`、`session_id`、`message_id` 普通索引。
- `status + updated_at` 清理索引。
- `object_key` 唯一索引。
- `kind` 第一阶段只允许 `html`。
- `message_id` 必须指向当前存在且归属 task/session的 Agent Message。

DAO至少支持：

```text
CreatePending
MarkReady
MarkFailed
FindReadyByResourceID
CountByMessageID
ListObjectKeysByTaskID
MarkDeletingByTaskID
DeleteRow
ListStalePendingOrFailed
```

把 Artifact加入 Backend `AutoMigrate` 列表。发布前必须通过迁移测试确认空表创建不会修改
现有 Message、Skill和头像表。

### 6.4 Capability Token

Backend在创建 Agent Message后生成短期 HMAC capability。Claims至少包含：

```json
{
  "aud": "artifact-upload",
  "jti": "uuid",
  "task_id": "...",
  "session_id": "...",
  "message_id": "...",
  "max_bytes": 26214400,
  "max_objects": 20,
  "iat": 1786531200,
  "exp": 1786533000
}
```

验证要求：

- 固定算法，拒绝算法协商和 `none`。
- 严格验证 `aud`、`exp`、必填 claim及 UUID格式。
- 根据数据库重新验证 Message归属，不只信任 token claim。
- 上传数量和大小由服务端配置与 token限制取更小值。
- Token只允许调用 Artifact上传接口，不能读取、删除或访问其他 `/api` 路由。
- 错误响应和日志不得输出 token。

### 6.5 上传接口

注册独立路由组，避免混用用户 Auth：

```http
POST /api/internal/artifacts
Authorization: Bearer <artifact capability>
Content-Type: multipart/form-data
```

请求字段：

```text
kind=html
filename=preview.html
file=<HTML bytes>
```

处理顺序：

1. 在解析 multipart前限制整个请求体，给 boundary/header预留小幅开销。
2. 验证专用 capability并读取 task/session/message归属。
3. 校验当前消息 Artifact数量配额。
4. 只接受一个文件 part和受支持字段，拒绝未知或重复字段。
5. 第一阶段只允许 `kind=html` 和 UTF-8 HTML。
6. 限量读取并计算 size 和 SHA-256；当前 25MiB 第一阶段实现使用受限内存缓冲，后续若提高对象上限再切换为 `0600` 专用临时文件。
7. 生成 `resource_id` 和 object key，写入 `pending` 元数据。
8. 从临时文件上传 MinIO，`Stat`校验 size、Content-Type和 SHA-256 metadata。
9. 更新 Artifact为 `ready`，删除临时文件并返回引用信息。
10. 任一步失败时关闭 reader、清理临时文件并标记 `failed`；已写入但未关联的对象由清理流程回收。

成功响应：

```json
{
  "data": {
    "resource_id": "6dd9a56e-40b9-4c1d-80bf-2fd19540db88",
    "kind": "html",
    "filename": "preview.html",
    "content_type": "text/html; charset=utf-8",
    "size": 182430,
    "sha256": "..."
  }
}
```

接口必须幂等处理网络重试。`render` 每次调用生成 `Idempotency-Key`，Backend按消息范围
记录该键；同一 key 的已完成上传直接返回原 `resource_id`，进行中的上传返回 409，避免
网络重试自动产生多个资源。

### 6.6 读取接口

Frontend使用：

```http
GET  /api/artifacts/:resourceId
GET  /api/artifacts/:resourceId/content
HEAD /api/artifacts/:resourceId/content
```

元数据接口不返回 object key。内容接口：

- 仅允许读取 `ready` 对象。
- 使用 Artifact记录中的受信任 Content-Type，不反射请求参数或任意 MinIO metadata。
- HTML响应设置 `Content-Type: text/html; charset=utf-8`。
- 设置 `X-Content-Type-Options: nosniff`。
- 设置适合 sandbox iframe的 Content-Security-Policy。
- 使用 ETag、Content-Length和 `private, max-age=31536000, immutable`。
- 支持 `If-None-Match` 和 HEAD。
- 读取失败映射为 404、503或 500，响应提交后失败则中断连接。

第一阶段 Frontend主 API当前没有稳定的普通用户 Bearer会话，内容接口沿用现有 `/api`部署
模式。若后续启用完整用户鉴权，需要在 iframe无法附加 Authorization Header的约束下引入
短期读取票据或 Cookie会话；不得临时把 Bucket改成公开。

### 6.7 生命周期与删除

Task删除不能先删除 Artifact行再尝试 MinIO，否则会丢失 object key。第一阶段至少实现：

1. 删除 Task前锁定 Agent Message；已完成/失败的关联 Artifact标记为 `deleting`。仍在上传的 `pending` 行暂不删除，避免清理与 MinIO `Put` 竞态。
2. 按数据库记录逐个删除对象，不依赖 MinIO ListBucket权限。
3. 删除成功后删除 Artifact行。
4. 失败项保留 `deleting + last_error`，由周期任务重试。
5. `MarkReady` 复用 Agent Message 行锁；若任务已删除则上传转为 `failed`，由周期任务清理；周期任务先把超期 `pending` / `failed` 行原子认领为 `deleting`，再删除对象和记录，避免超长上传与回收竞态。
6. Bucket配置生命周期规则，回收长时间无数据库关联的失败上传作为最后兜底。

第一阶段可以使用轻量 Artifact cleanup worker，不必复用 Skill Operation Job表。Worker必须
有应用生命周期 Context、优雅关闭、并发上限、重试退避和操作超时。

## 7. AgentEnd 实施规划

### 7.1 Transport Sanitizer

在 `agentend/src/api/v1/agent.py` 的流式路径增加公共 sanitizer，位置为 Adapter/可观测性处理
之后、`EventSourceResponse` yield之前。它只改变对外传输副本，不修改 Adapter交给 Agent CLI
的内部输出，也不影响非流式 `/execute` 的结果聚合。

裁剪规则：

| 事件 | 删除 | 保留/补充 |
|---|---|---|
| `tool_call` | `args`、`result`、`raw` | `tool`、`status`、`input_size`、`output_size` |
| `tool_result` | `result`、`raw` | `tool`、`status`、`exit_code`、`output_size` |
| `done` | `text`、`raw` | `usage`、终态字段 |
| `text` | 不裁剪 | Agent最终用户可见文本 |
| `error` | 不删除必要错误 | 错误内容仍经过既有隐私过滤和长度上限 |

要求：

- 大小使用 UTF-8 byte数，不把内容复制多次计算。
- Sanitizer不得解析任意 Bash命令或猜测是否调用 `taskctl/render`。
- 所有工具统一裁剪，避免其他工具重现同类性能问题。
- 不直接丢弃 `tool_result` 事件，Frontend仍依赖它结束 `tool_running` 状态。
- OpenCode成功输出位于 `tool_call.result` 的情况必须有专门测试。
- Claude `done.text` 删除后，已流式到达的 `text` 仍是唯一用户可见内容来源。
- Langfuse继续使用现有有界、脱敏的捕获策略；Transport Sanitizer不放宽观测内容上限。

### 7.2 子进程环境注入

`_execute_stream` 从 AgentRequest读取 `message_id` 和 `artifact_upload_token`，构造每次运行专用
环境：

```text
AGENTHUB_ARTIFACT_ENDPOINT=<settings.backend.url>/api/internal/artifacts
AGENTHUB_ARTIFACT_TOKEN=<scoped token>
AGENTHUB_MESSAGE_ID=<message_id>
```

四个 CLI Adapter 接收显式 `process_env`，调用 `asyncio.create_subprocess_exec(..., env=...)`；
Orchestrator 的 `run_skill` 子进程也使用同一请求范围的环境上下文。
环境保留 CLI/system 运行所需变量，同时过滤 AgentEnd 自身的数据库、存储、JWT、LLM 和
Langfuse 配置前缀；再叠加三项请求范围 Artifact 变量，不把 AgentEnd `.env` 中无关密钥整体
暴露给 Agent CLI。至少保留 PATH、HOME/用户运行目录、locale以及 CLI明确需要的变量；敏感
变量策略需要回归现有 Agent启动行为。

Token不会写入 workspace文件、Skill目录、session mapping、日志或 trace。Agent进程具备命令
执行能力，因此可以读取自身环境；安全边界依赖能力令牌的最小权限、TTL和配额，而不是假设
Agent看不到 token。

### 7.3 SkillProvisioner 升级

当前 Provisioner遇到已存在的内置 Skill目录会跳过，新 `render` 无法覆盖活动 worktree中的
旧二进制。第一阶段必须选择并实现以下策略之一：

**推荐：受管文件原子刷新**

- manifest中的 Builtin名称视为保留名称。
- 对 manifest声明的 `SKILL.md` 和二进制计算源文件 SHA-256。
- 不一致时复制到同目录临时文件，保持权限后 `os.replace` 原子替换。
- 只更新 manifest声明文件，不删除未知文件。
- 更新失败保持旧文件完整，并让本次 workspace准备失败，不允许半更新运行。
- 增加 provisioner并发与重复执行测试。

若暂不改 Provisioner，发布操作必须强制回收全部现有 worktree并重新创建；该策略会影响正在
执行的会话，不作为推荐默认方案。

## 8. render Skill 实施规划

### 8.1 `html-render` 新行为

`render html-render` 保持现有参数/stdin输入方式和 HTML基础校验，校验通过后：

1. 从环境读取 Artifact endpoint和 token。
2. 缺失配置时明确失败，不回退输出内联 HTML。
3. 使用 Go标准库构造限量 multipart上传。
4. 设置 `kind=html`、固定安全文件名和随机 `Idempotency-Key`。
5. 设置连接、响应头和总请求超时。
6. 校验 Backend状态码、响应 Content-Type和 `resource_id`。
7. stdout只输出轻量 `aka_yhy`引用块。
8. 错误详情写 stderr，但不得包含 token、HTML正文或完整 Backend响应。

输出：

````text
```aka_yhy
type: html-render
resourceId: 6dd9a56e-40b9-4c1d-80bf-2fd19540db88
```
````

`render` 不接受任意 endpoint参数，避免 Agent把 capability发送到其他地址；endpoint只从
AgentEnd注入的环境读取。对 endpoint进行 URL校验，禁止 userinfo、fragment和非 HTTP(S)
scheme。

### 8.2 Skill说明与 Rule

更新 `render/SKILL.md`：

- 说明 HTML由平台上传并返回引用。
- Agent仍应把 stdout引用块原样包含在最终回复中。
- 禁止 Agent自行读取 MinIO、拼接资源 URL或把 HTML再次复制进回复。
- 上传失败时不要伪造 resourceId。

更新 `SkillRule` 注入文本，明确 `render` stdout是平台资源引用，不应展开、改写或重复 HTML。

## 9. Frontend 实施规划

### 9.1 Block 类型与解析

将 `html-render` 表达为兼容联合语义：

```ts
type HtmlRenderBlock =
  | { type: 'html-render'; id: string; content: string; streaming?: boolean }
  | { type: 'html-render'; id: string; resourceId: string }
```

`block-reducer.ts`：

- 先读取规范 `resourceId`。
- 存在合法 resourceId时产生资源块，不读取 inline content。
- 不存在 resourceId时沿用旧 inline解析和 streaming占位逻辑。
- 非法、缺失或冲突字段按普通文本降级，不发起任意 URL请求。
- Block ID稳定性和 coalesce行为不得因 resourceId格式产生重复卡片。

### 9.2 HtmlCard

`HtmlCard` 接收 `content` 或 `resourceId`：

- 旧格式继续使用 `srcDoc`。
- 新格式使用 `/api/artifacts/{resourceId}/content` 作为 iframe `src`。
- 保持 `sandbox=""`，不增加 `allow-scripts`、`allow-same-origin`。
- 加载中显示轻量占位；失败时显示重试或错误卡片。
- 放大预览复用相同资源 URL，不重复下载到 React state。
- 不先 fetch HTML再转回 `srcDoc`，否则会重新引入大字符串内存问题。

### 9.3 历史兼容

- 不迁移历史 Message正文。
- 旧内联 HTML继续渲染。
- 新 Message只保存 resourceId块。
- workspace被 AgentEnd清理后，新资源仍由 MinIO读取。
- Artifact对象不可变，因此可使用稳定 URL和长期 private cache；删除 Task后 URL返回 404。

## 10. Docker 与 MinIO

扩展 MinIO初始化：

- 创建私有 `agenthub-artifacts` Bucket。
- 创建 Artifact Bucket；应用账号可复用 Skill/Avatar 的 MinIO 用户。
- 策略只允许 `artifacts/*` 的 Put/Get/Stat/Delete和必要健康探测。
- 独立账号部署时，各账号只访问对应 Bucket；共享账号部署时为同一用户累计绑定三个 Bucket策略。
- Root凭据不进入 Backend容器。
- 初始化脚本幂等，重复执行不得打开匿名访问。

更新：

```text
docker/docker-compose.yml
docker/minio/init.sh
backend/configs/config.example.yaml
backend/.env.example
docker/configs/backend/.env.example
docs/guides/docker-deployment.md
```

默认发布前保持 `artifact_storage.enabled=false`，完成 Bucket、凭据和 Backend验证后再开启。

## 11. 实施顺序

### 里程碑 A：契约和 Backend 基础设施

- [x] AgentRequest增加 `message_id` 和 `artifact_upload_token`
- [x] 运行 `make generate` 并新增契约日志
- [x] 新增 Artifact配置、校验、环境变量和 `/ready`检查
- [x] 新增 Artifact模型、DAO、Store和 AutoMigrate
- [x] 新增 capability签发与验证
- [x] 新增上传、metadata、GET/HEAD content接口
- [x] 新增任务删除和 stale artifact最小清理流程
- [x] 完成 Backend Artifact capability、上传、幂等、补偿删除与 HTTP 控制器单元测试
- [ ] 建立并执行真实 MinIO 集成测试入口

### 里程碑 B：Frontend 前向兼容

- [x] Block parser支持 `html-render.resourceId`
- [x] HtmlCard支持 iframe `src`读取 Artifact
- [x] 保留并测试旧 inline content路径
- [ ] 增加加载失败、404和放大预览测试

Frontend必须先于新版 `render` 发布，避免新引用到达时显示原始 fence。

### 里程碑 C：AgentEnd 传输和运行上下文

- [x] 实现公共 Transport Sanitizer
- [x] 覆盖 Claude、Codex、OpenCode事件差异测试
- [x] 将 Backend URL、token和 message ID注入每次 CLI运行环境
- [x] 避免 token进入日志、trace和持久化文件
- [x] 实现 Builtin Skill受管文件原子刷新

### 里程碑 D：render HTML 资源化

- [x] 改造 `render html-render` 上传流程
- [x] stdout只输出 resourceId块
- [x] 更新 `render/SKILL.md` 和 `SkillRule`
- [x] 重新执行 `make skills build`
- [x] 验证活动和新建 worktree都获得新版二进制

### 里程碑 E：三端联调和灰度

- [ ] 单 Agent 1MB HTML端到端测试
- [ ] Orchestrator子 Agent 1MB HTML端到端测试
- [ ] `taskctl` 大 stdout传输裁剪测试
- [ ] 历史刷新与 workspace清理测试
- [ ] Task删除与 MinIO清理测试
- [ ] 观察 SSE事件大小、Redis写入量、前端长任务和错误率
- [ ] 灰度开启 `artifact_storage.enabled`

## 12. 测试计划

### 12.1 AgentEnd 单元测试

- Claude tool_call的大 args被裁剪，工具名保留。
- Claude tool_result的大 result被裁剪，exit code保留。
- Claude done.text被裁剪，usage保留。
- Codex command/aggregated_output不进入对外事件。
- OpenCode `tool_call.result`被裁剪。
- TEXT事件完全不被 sanitizer改写。
- `/execute` 非流式聚合不因 transport sanitizer丢失最终文本。
- 环境变量按请求隔离；并发 Session不串 token或 message ID。
- 日志和 trace不出现 capability token。
- Provisioner原子刷新成功、失败回滚和并发幂等。

### 12.2 Backend 单元测试

- Artifact capability正常、过期、篡改、错误 audience、错误 message归属。
- multipart总大小、单文件、重复字段、未知 kind和非法 UTF-8。
- 每消息数量配额和 token/服务端限制取最小值。
- pending → ready、pending → failed状态转换。
- MinIO timeout、permission、not-found和完整性不一致映射。
- metadata接口不泄露 object key。
- Content GET/HEAD、ETag、304、CSP、nosniff和 Content-Length。
- Task删除失败时保留可重试对象记录。
- 幂等 key相同内容复用、不同内容冲突。

### 12.3 render 单元测试

- 参数和 stdin输入保持兼容。
- HTML校验失败时不请求 Backend。
- 缺失 endpoint/token时失败且不输出 inline HTML。
- 成功响应只输出 resourceId块。
- 4xx、5xx、超时、无效 JSON和缺少 resource_id均失败。
- stderr、stdout和错误对象不包含 token或 HTML正文。
- 请求体受大小限制且上传不会无限阻塞。

### 12.4 Frontend 单元测试

- 旧 inline html-render正常解析。
- 新 resourceId html-render正常解析。
- 非 UUID resourceId不发起资源请求。
- 新 HtmlCard使用 iframe `src`，不设置 `srcDoc`。
- 旧 HtmlCard仍使用 `srcDoc`。
- sandbox属性没有放宽。
- 加载失败和放大预览正常。

### 12.5 集成与性能测试

使用至少 1MB HTML和 1MB `taskctl` stdout验证：

- Agent仍能读取完整 taskctl输出。
- AgentEnd → Backend事件不包含完整 tool args/result/done text。
- Redis Stream不包含测试 HTML实体或 taskctl正文。
- MySQL Message只包含小型 resourceId引用和正常回复文本。
- MinIO对象 Size/SHA-256与源 HTML一致。
- Frontend不把新 HTML放入 `streamingContent` 或 `srcDoc`。
- 页面流式期间无明显长任务、输入阻塞或卡片 fence闪现。
- 刷新页面、Backend重启和 AgentEnd workspace清理后资源仍可读取。
- Orchestrator子 Agent的 resourceId引用能进入最终分组消息。

## 13. 发布与回滚

### 13.1 发布顺序

1. 创建 Artifact Bucket、账号和策略，保持功能关闭。
2. 发布 Backend模型、Store、API和读取能力。
3. 发布支持 resourceId的 Frontend。
4. 发布 AgentEnd sanitizer、环境注入和 Provisioner升级。
5. 发布新版 `render` 二进制与 SKILL.md。
6. 验证所有现有 worktree已刷新 Builtin Skill。
7. 开启 Artifact Storage并灰度执行 HTML资源化。

### 13.2 回滚

- 新版 Frontend兼容旧 inline格式，可以先保留不回滚。
- 回滚 `render` 会恢复内联 HTML，可能重新出现性能问题，只作为紧急手段。
- 已生成的 resourceId消息依赖 Backend读取接口；存在这类消息后不得先回滚或删除读取 API。
- 关闭新上传前先停止/回滚 `render`，再关闭 `artifact_storage`写入能力。
- 已有 MinIO对象和 Artifact元数据在观察期内保留，不在回滚时删除。
- Transport Sanitizer与资源存储解耦，可独立保留，优先不回滚。

## 14. 完成标准

第一阶段满足以下全部条件才可标记完成：

- 1MB `taskctl`输出对 Agent可见、对 Browser SSE不可见。
- 1MB HTML资源实体只经过 Artifact HTTP和 MinIO数据面。
- 任一 AgentEnd → Backend工具或 done事件都不携带完整大参数/结果。
- 新 Message和 Redis Stream中不存在 HTML实体，只存在规范 resourceId引用。
- 新 HtmlCard不把 HTML加载到 React state或 `srcDoc`。
- 单 Agent和 Orchestrator子 Agent都能实时显示资源卡片。
- 历史刷新、服务重启和 workspace清理后资源可读。
- 旧内联 HTML消息无回归。
- Capability越权、过期、配额和内容限制测试通过。
- Task删除和失败上传不会留下无法追踪的永久对象。
- Docker MinIO Bucket保持隔离；应用账号可以共享并累计绑定所需策略。
- 三端测试、契约生成检查和文档同步完成。

## 15. 第二阶段候选项

第一阶段稳定后再评估：

- `render image` 和 `attachment` 迁移到 ArtifactStore。
- 大文件 presigned PUT/GET。
- 规范化 `artifact` SSE事件及 Orchestrator透传。
- Message结构化 Block表和资源顺序模型。
- Artifact删除 Outbox、完整 MinIO反向对账和管理面板。
- Range请求、视频/音频和其他媒体类型。
- 资源级访问票据与完整普通用户鉴权。
