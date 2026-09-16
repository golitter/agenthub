# Go Backend 超详细实现

## 实现了什么

Backend 是 Gin + GORM 的控制面，公开 `/api`，保存业务事实，决定 Agent 路由，代理 Workspace，并把 AgentEnd SSE 转换为可重连、可持久化的消息流。代码采用 Controller → Service → DAO；Controller 绑定和响应，Service 处理业务和跨系统协调，DAO 隐藏 GORM。

## 怎么实现的

### 启动入口

`backend/cmd/server/main.go` 的关键顺序是：加载配置 → 初始化 MySQL → 执行显式迁移 → 初始化 Redis → 修复遗留 streaming 消息 → 创建 AgentEnd client → 创建头像/Skill/Artifact 存储 → 组装 Router → 启动 SkillOperationWorker 和 TaskCleanupWorker 等后台清理 → 启动 HTTP server → 信号驱动优雅关闭。

顺序有依赖含义：DAO 和迁移必须在 Service 之前；Redis 必须在 stream service 之前；存储 health 与 feature gate 决定 `/ready` 是否通过；worker 与 HTTP server 共用可取消 context。

### 应用组装

`backend/internal/app/app.go` 显式 new 每个 DAO、Service 和 Controller，没有隐藏容器。这样测试可替换 DAO，且构造顺序直接显示依赖。核心组装关系：

- GORM DAO：task、session、message、diff snapshot、announcement、contact group、skill、skill operation、task cleanup、artifact。
- Service：task、session、message、stream、profile、avatar、diff、announcement、contact、skill、admin、artifact。
- 外部依赖：AgentEnd client、Redis、RuntimeHub、对象存储 Runtime、上传 session store。
- Controller：15 组公开/内部路由处理器。

Gin 使用 `gin.New()`，全局顺序是 Logger → CORS → Recovery。`/api` 组再应用 JSON body limit 与可选 JWT。公开头像资源、内部服务 API 和用户 API 分成不同 RouterGroup，避免通配路径或能力令牌端点误入通用认证例外。

### 分层规则

Controller：

- `ShouldBindJSON`、path/query/multipart 参数解析。
- 输入的语法级限制，例如分页 `limit` 1..100、头像 2 MiB、扩展名白名单。
- 调 Service 并把 `BizError` 映射为 HTTP。
- 使用 `vo` 输出统一 envelope，SSE 和二进制内容除外。

Service：

- 业务校验、幂等、事务边界、路由、外部调用。
- 不依赖 Gin，便于纯单元测试。
- 使用 `ErrBadRequest`、not found、conflict、unavailable 等 `BizError`。
- 负责跨 DAO 组合以及 MySQL 与外部副作用之间的补偿策略。

DAO：

- 接口位于 `internal/dao/`，GORM 实现在 `internal/dao/gorm/`。
- 复杂删除集中在 `cascade.go`；迁移集中在 `migrations.go`。
- mock DAO 支持 Service 测试。
- 数据库错误通过 DAO 层分类，不泄漏具体 SQL 到 Controller。

### 数据模型

| 表/模型 | 关键字段 | 约束与用途 |
|---|---|---|
| Task | `task_id,title,repo_path,status,pinned_at` | task_id 唯一；单聊/群聊聚合根 |
| Session | `session_id,task_id,agent_type,agent_name,avatar_url,status,settled_diff,diff_status,soul_md` | session_id 唯一；默认 idle |
| SessionAgent | `session_id,agent_type,agent_name,avatar_url` | Agent 身份快照，session_id 唯一 |
| Message | `message_id,task_id,session_id,role,content,status,last_seq,agent_type,agent_name,group_id,run_id,run_key,run_request_hash,termination_reason` | message_id 唯一；run_key 唯一；游标用数字 ID |
| Announcement | `task_id,sender_id,sender_name,content,pinned` | Task 公告，可筛选 pinned |
| ContactGroup | `group_id,name,sort_order` | 自定义通讯录组 |
| ContactGroupItem | `group_id,task_id,sort_order` | 联合唯一，避免重复加入 |
| DiffSnapshot | `snapshot_id,session_id,diff_content,status` | pending/accepted/reverted 等 UI 操作事实 |
| SkillHub | 元数据、blob 兼容列、object key、sha256、storage/status、安全扫描标记 | builtin/external 统一仓库 |
| AgentSkill | `session_id,skill_name,agent_type,status` | Session 与 external skill 联合唯一关联 |
| SkillUploadReceipt | `upload_id,skill_id,sha256,owner_id` | Redis 丢失后仍可幂等确认 |
| SkillOperationJob | operation、idempotency、lease、retry | 对象删除/安装/移除/迁移/校验 Outbox |
| SkillAuditEvent | actor、hash、文件清单、安全标记、结果 | 追加式审计 |
| Artifact | resource/task/session/message、kind、object、digest、status | 私有不可变对象元数据与消息幂等 |
| TaskCleanupJob | task、repo、session snapshot、lease/retry | Task 删除后的 AgentEnd 清理意图 |
| AdminSetting | key/value | 管理设置，例如头像 URL |

### Task 与路由

Task API 创建聚合根和一个或多个 Session。`task_route.go` 根据请求、群聊成员和目标选择实际 Session；返回的 RunTaskResponse 不只包含 Task ID，还包含实际 `session_id`、`message_id`、Agent 路由和 run 信息，前端必须订阅响应中的实际标识。

`POST /tasks/:taskId/run` 有每 IP 每分钟 30 次限制。Service 先落用户消息，再建立 Agent 消息/Run 关联，构造 AgentEnd request，并启动或复用流。请求指纹用于防止同一 run key 对应不同内容。

取消不是简单删除消息：Backend 查到 run 并调用 AgentEnd cancel，返回 accepted 状态，最终 termination reason 由运行终态确定。计划审查转发 approve/discuss/modify 给 AgentEnd 的 pending review。

### 消息查询

`GET /tasks/:taskId/messages` 支持两种模式：

- 不给 `limit/before`：兼容完整列表响应。
- 给任一分页参数：游标模式，默认 20，最大 100；`before` 必须是正数字主键。

可选 `session_id`、`mode`、`primary_session_id` 控制单聊/群聊窗口。DAO 用稳定数字 ID 查询，Service 负责按前端需要恢复正序并返回 next cursor。`/messages/window` 给 AgentEnd 构造有限上下文，避免拉取整个历史。

### 公开路由表

| 方法 | 路径 | 处理器职责 |
|---|---|---|
| GET | `/ping`,`/health`,`/ready` | 存活、基础健康、依赖就绪 |
| GET/HEAD | `/api/assets/avatars/*path` | 公共不可变头像读取 |
| POST/GET | `/api/tasks` | 创建或列出 Task |
| GET/DELETE/PATCH | `/api/tasks/:taskId` | 读取、删除或更新 Task |
| DELETE | `/api/tasks/:taskId/leave` | 离开并登记清理 |
| POST | `/api/tasks/:taskId/run` | 启动 Agent run |
| GET | `/api/tasks/:taskId/messages/:messageId/run` | 查询 run |
| POST | `/api/tasks/:taskId/messages/:messageId/run/cancel` | 取消 run |
| GET | `/api/tasks/:taskId/conflicts/:conflictId` | 冲突投影 |
| POST | `/api/tasks/:taskId/conflicts/:conflictId/actions` | 冲突动作 |
| POST | `/api/tasks/:taskId/review` | 计划审查 |
| GET | `/api/tasks/:taskId/stream` | SSE |
| GET | `/api/tasks/:taskId/messages`、`/window` | 历史与上下文窗口 |
| GET/POST | `/api/tasks/:taskId/announcements` | 查询或创建公告 |
| DELETE | `/api/tasks/:taskId/announcements/:id` | 删除公告 |
| PATCH/PUT | `/api/sessions/:sessionId` | 状态或展示信息更新 |
| GET/PUT | `/api/sessions/:sessionId/profile|detail|soul` | Agent profile 与 Soul |
| GET | `/api/agent-types` | 五类 Agent 列表 |
| POST | `/api/agents/avatar` | 头像上传 |
| GET/PUT | `/api/diff-snapshots/:snapshotId` | Diff 快照 |
| GET/POST | `/api/contact-groups` | 查询或创建通讯录分组 |
| PUT/DELETE | `/api/contact-groups/:groupId` | 更新或删除分组 |
| POST/DELETE | `/api/contact-groups/:groupId/items`、`/items/:taskID` | 添加或移除分组成员 |
| GET | `/api/skills` | SkillHub 列表 |
| POST | `/api/skills/upload`、`/api/skills/confirm` | 上传与确认 Skill |
| DELETE/POST | `/api/skills/:name`、`/api/skills/:name/import` | 删除或导入 Skill |
| DELETE | `/api/skills/:name/sessions/:sessionId` | 从 Session 移除 Skill |
| GET/PUT/POST | `/api/workspace/:id`、`/api/session/:sessionId` 的文件、diff、commit、revert、preview 子路径 | AgentEnd Workspace 代理 |
| GET | `/api/artifacts/:resourceId` | Artifact 元数据读取 |
| GET/HEAD | `/api/artifacts/:resourceId/content` | Artifact 内容读取 |
| POST | `/api/admin/auth` | 管理认证 |
| GET/DELETE/PUT | `/api/admin` 下的 health、avatar、resources、sessions、workspaces、agents、services、statistics | 管理面板 |

### 内部路由

`/api/internal` 不等于用户 JWT API：

- `POST /api/internal/artifacts` 只接受短期 Artifact capability。
- task run、stream、messages window、announcements read、builtin skill report 供服务间调用。
- 开启 `agentend.service_auth_enabled` 时，内部 run 路由组应用 `BACKEND_SERVICE_TOKEN` 校验。

Artifact upload 单独注册在服务认证中间件之前，因为它使用绑定资源的 capability，不使用通用 service token。这个隔离是有意设计，不能为“统一认证”随意合并。

### Workspace 代理

Go Backend 不直接编辑 Worktree。`WorkspaceController` 把浏览器调用转换为 AgentEnd `/v1/workspace`：文件读写、diff、commit、revert、preview、task git info、merge-to-main。

代理层执行：

- `sanitizePath` 拒绝任何 `..` 段。
- path segment 逐段 URL escape。
- 请求体限制 25 MiB。
- HTTP client 默认 30 秒超时。
- Session 路径先通过 AgentEnd `by-session` 解析 workspace ID。
- AgentEnd 非 2xx 被转换为稳定的 Backend 响应，而不是暴露内部地址。

### 健康检查

`/health` 只表示进程可响应。`/ready` 在 3 秒 context 内依次检查：MySQL、Redis；若启用头像 MinIO则检查 avatar storage；若启用 Artifact/Skill storage 则检查对应 store。任一必需依赖不可用返回 503。

### 中间件

- `Logger`：结构化请求日志。
- `CORS`：只允许配置的 origins。
- `Recovery`：捕获 panic。
- `JSONBodyLimit`：限制 JSON；Workspace 大 body 使用自己的路径策略。
- `AuthWithSkips`：可选用户 JWT，明确跳过 admin auth/health/avatar。
- `AdminAuth`：管理 JWT，用于管理 API和可选 SkillHub mutation。
- `ServiceAuth`：服务间 Bearer token，常量时间比较。
- `NewIPRateLimiter`：登录、run、Skill mutation、公开资源限流。

### 统一响应和错误

普通 JSON 使用 `vo` envelope，成功、created、accepted、bad request、internal error 等有固定方法。Service 返回的 BizError 在 `controller/impl/errors.go` 映射为 HTTP status。SSE 在真正开始写前使用 delayed writer，因此前置校验失败仍能返回正常 JSON 错误；一旦 header 已提交，后续错误只能结束流并记录日志。

### 后台 worker

SkillOperationWorker 从 DB 抢占带 lease 的 pending/failed job，执行对象删除、AgentEnd install/remove、迁移或 verify。`AgentSkillID` 形成 fence，防止旧重试误操作同名的新关系。失败写 attempts、next retry、last error。

TaskCleanupWorker 消费 TaskCleanupJob。删除 Task 的数据库事务记录 session ID 快照与清理意图；即使 AgentEnd 当时不可用，worker 后续仍可清理 workspace/branch。lease token 防止多 worker 重复持有。

定时维护还包括上传 receipt、临时目录、失败 Artifact 对象等清理。任何清理都不应绕过 retention/grace 配置。
