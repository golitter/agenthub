# 全端代码审计报告

**审计日期**: 2026-05-25
**审计范围**: frontend / backend / agentend / 跨端一致性
**代码规模**: ~2056 行 TypeScript + ~1627 行 Go + ~2917 行 Python

> **注意 (2026-06-06)**：审计后 Backend 已从 `handler/` 单层重构为 `controller/impl/ + service/impl/ + dao/` 三层架构。以下文件路径映射：
> - `handler/task.go` → `controller/impl/task_controller.go` + `service/impl/task_service.go`
> - `handler/stream.go` → `controller/impl/stream_controller.go` + `service/impl/stream_service.go`
> - `handler/message.go` → `controller/impl/message_controller.go` + `service/impl/message_service.go`
> - 行号为审计时的原始行号，可能已变更。

---

## 总览

| 严重程度 | 前端 | 后端 | Agent 端 | 跨端 | 合计 |
|----------|------|------|----------|------|------|
| **Critical** | 1 | 2 | 1 | 3 | **7** |
| **High** | 4 | 8 | 5 | 3 | **20** |
| **Medium** | 8 | 14 | 14 | 10 | **46** |
| **Low** | 8 | 3 | 5 | 8 | **24** |
| **合计** | 21 | 27 | 25 | 24 | **97** |

---

## P0 — Critical（必须立即修复）

### C-1. 前端: CodeBlock.tsx XSS 风险
- **文件**: [CodeBlock.tsx:56](../../frontend/src/components/markdown/CodeBlock.tsx#L56)
- **问题**: `dangerouslySetInnerHTML` 渲染 Shiki 高亮输出，未经 DOMPurify 消毒。Agent 返回内容如果包含恶意 HTML，可能导致 XSS。
- **修复**: 安装 `dompurify`，在 `setHtml` 前调用 `DOMPurify.sanitize(result)`。

### C-2. 后端: 所有 API 路由无认证
- **文件**: [main.go:61-77](../../backend/cmd/server/main.go#L61-L77)
- **问题**: `middleware.Auth()` 已实现但从未应用到路由组。所有 `/api` 端点对未认证用户完全开放。
- **修复**: `api := r.Group("/api").Use(middleware.Auth(cfg.JWT.Secret))`
- **当前状态**: 已改为可配置认证闸。`auth.enabled=true` 或生产模式默认开启时，`/api` 挂载 `AuthWithSkips`；公开跳过 `/api/admin/auth`、`/api/admin/health`、`/api/admin/avatar`，其余普通 API 需要 Bearer JWT，只有 `GET .../stream` SSE 支持 `access_token` query。

### C-3. 后端: config.yaml 硬编码密钥
- **文件**: [config.yaml:2,10](../../backend/configs/config.yaml#L2)
- **问题**: MySQL 密码 `"123456"` 和 JWT secret `"agenthub-demo-secret"` 明文硬编码在 Git 追踪文件中。攻击者可伪造 JWT 或直连数据库。
- **修复**: 迁移至环境变量，创建 `.env.example` 模板，在 `.gitignore` 中排除实际配置。
- **当前状态**: `MYSQL_*`、`JWT_SECRET`、`ADMIN_PASSWORD`、`REDIS_*`、`SERVER_PORT`、`API_AUTH_ENABLED` 等已支持环境变量覆盖；生产模式会拒绝默认 JWT secret 与默认 Admin 密码。开发配置中仍保留本地默认值，部署时需通过环境变量覆盖。

### C-4. Agent 端: 所有路由无认证
- **文件**: [api/v1/*.py](../../agentend/src/api/v1)
- **问题**: 全部 API 路由（含工作区创建/删除、Agent 执行）无认证中间件。任何人可执行任意 Git 操作。
- **修复**: 添加 API Key 或 JWT 认证依赖，应用于所有路由。

### C-5. Agent 端 config.yaml 硬编码数据库密码
- **文件**: [agentend/config.yaml:44](../../agentend/config.yaml#L44)
- **问题**: 数据库密码 `"123456"` 明文硬编码在 Git 追踪文件中。
- **修复**: 同 C-3，迁移至环境变量。
- **当前状态**: `agentend/src/app/config.py` 会用 `MYSQL_HOST` / `MYSQL_PORT` / `MYSQL_USER` / `MYSQL_PASSWORD` / `MYSQL_DBNAME` 覆盖 YAML 默认值；开发配置中仍保留本地默认值，部署时需通过环境变量覆盖。

### C-6. 跨端: 日志泄漏 MySQL DSN（含密码）
- **文件**: [mysql.go:23](../../backend/pkg/db/mysql.go#L23)
- **问题**: `slog.Info("connecting to mysql", "dsn", dsn)` 将完整 DSN（含密码）写入日志。
- **修复**: 仅记录 host 和 db_name，脱敏 DSN。
- **当前状态**: `backend/pkg/db/mysql.go` 已不再打印 DSN；连接日志只输出连接状态和连接池参数。

### C-7. 跨端: .env 中存在 API 密钥
- **文件**: [agentend/.env:3](../../agentend/.env#L3)
- **问题**: DeepSeek API Key 明文存储在 `.env` 中。虽在 `.gitignore` 内，但存在意外提交风险。
- **修复**: 添加 pre-commit hook 扫描密钥模式；确保 `.env` 永不被提交。

---

## P1 — High（应尽快修复）

### H-1. 前端: SSE 重连去重/间隙补偿状态
- **文件**: [sse.ts](../../frontend/src/lib/sse.ts)
- **当前状态**: 已通过 Backend 三段补偿和前端 replay offset 收敛。Backend 对同一 `session_id + message_id` 订阅先输出 MySQL content，再从 Redis Stream 的 `last_seq` 后追补事件，最后接入 RuntimeHub 实时流；前端 `message-store` 使用 `streamingMessageId + streamingReplay.offset` 去重 replay 文本。

### H-2. 前端: SSE 连接超时状态
- **文件**: [sse.ts](../../frontend/src/lib/sse.ts)
- **当前状态**: 已添加 `openTimeoutMs`（默认 30s）和 `staleTimeoutMs`（默认 5min），`onopen/onmessage/onerror/abort` 统一刷新活跃时间或清理 timer 并关闭 EventSource。

### H-3. 前端: `radix-ui` 包使用状态
- **文件**: [package.json:21](../../frontend/package.json#L21)
- **当前状态**: `radix-ui` 统一包已被 [popover.tsx](../../frontend/src/components/ui/popover.tsx) 使用（`import { Popover as PopoverPrimitive } from 'radix-ui'`），不应移除。

### H-4. 前端: 整个 generated/ 下 response.ts 和 session.ts 未被使用
- **文件**: [generated/response.ts](../../frontend/src/generated/response.ts), [generated/session.ts](../../frontend/src/generated/session.ts)
- **当前状态**: 这两个文件由契约层固定生成，是否被业务代码直接 import 不作为删除依据；保留生成输出可避免三端 schema 映射不完整。

### H-5. 后端: 内部错误信息泄漏给客户端
- **文件**: [task_controller.go](../../backend/internal/controller/impl/task_controller.go), [task_service.go](../../backend/internal/service/impl/task_service.go)
- **问题**: `err.Error()` 直接返回在 API 响应中，暴露 GORM 内部错误（表名、DSN 片段）。
- **修复**: 返回通用错误消息，完整错误仅记录到服务端日志。
- **当前状态**: Controller 统一错误处理器已只透出 `BizError` 的业务消息；未知错误写入服务端日志，并向客户端返回固定 `internal server error`。Task review、Admin workspace/agent 聚合、Skill install/remove 等跨 AgentEnd 调用也已改为“日志保留详情、HTTP 返回稳定文案”。

### H-6. 后端: CreateTask 无事务保护
- **文件**: [task_service.go](../../backend/internal/service/impl/task_service.go)
- **问题**: Task 和 Session 创建不在事务中，Session 插入失败会留下孤立 Task 记录。
- **修复**: 使用 `db.Transaction()` 包裹 Task + Session 创建。

### H-7. 后端: DeleteTask 无级联删除
- **文件**: [task_service.go](../../backend/internal/service/impl/task_service.go), [cascade.go](../../backend/internal/dao/gorm/cascade.go)
- **问题**: 仅删除 Task 记录，不删除关联的 Session 和 Message，导致引用完整性破坏。
- **修复**: 添加级联删除或使用 GORM 软删除 + 关联。
- **当前状态**: Task / Admin session 删除已通过共享 cascade helper 清理 Message、SessionAgent、DiffSnapshot、AgentSkill，并由 Task 级删除继续清理 Session、Announcement、ContactGroupItem。

### H-8. 后端: Redis XRead 未使用消费者组
- **文件**: [stream_service.go](../../backend/internal/service/impl/stream_service.go)
- **问题**: 使用裸 `XREAD` 而非 `XREADGROUP` + `XACK`，handler 重启时会重复发送已处理消息。
- **修复**: 创建消费者组，使用 `XREADGROUP` + `XACK` 实现至少一次处理。

### H-9. 后端: Redis XADD 失败被静默吞没
- **文件**: [writer.go:118-130](../../backend/internal/stream/writer.go#L118-L130)
- **问题**: `XADD` 失败仅 `slog.Warn` 但内容丢失，`lastSeq` 仍会更新跳过中间内容。
- **修复**: XADD 失败时内存缓冲并标记重试。

### H-10. 后端: goroutine 使用 context.Background() 与请求生命周期脱耦
- **文件**: [task_service.go](../../backend/internal/service/impl/task_service.go)
- **问题**: `RunTask` 的后台 goroutine 使用 `context.Background()`，客户端断开后仍运行至 30 分钟超时。
- **修复**: 传递 `c.Request.Context()` 或派生 context。
- **当前状态**: `runStream` 保持 202 后后台执行语义，但会创建 30 分钟 timeout context，并传给 `StreamAgentWithContext` 和 `StreamWriter`，超时可取消底层 AgentEnd HTTP stream，避免卡在 body read。

### H-11. 后端: CORS 仅允许 localhost:5173
- **文件**: [cors.go](../../backend/internal/middleware/cors.go), [conf.go](../../backend/internal/conf/conf.go)
- **当前状态**: CORS origins 从 `config.yaml` 的 `cors.allow_origins` 加载，默认保留开发服务器 `http://localhost:5173`；Docker 配置可加入 `http://localhost` / `http://localhost:8787`，生产环境通过配置或环境变量注入严格列表。

### H-12. 后端: RunTask 未验证 agentType
- **文件**: [task_service.go](../../backend/internal/service/impl/task_service.go)
- **问题**: 用户可传入任意 `agentType` 字符串，无白名单验证。
- **修复**: 根据 `generated.AgentType` 常量校验。
- **当前状态**: `CreateTask` 与 `RunTask` 都在 Service 层校验 AgentType 枚举；`RunTask` 还会校验 message/session_id/cwd 长度和 session 归属，不再通过 `EnsureSession` 隐式创建未知 Session。

### H-13. Agent 端: plan_node 无 LLM 错误处理
- **文件**: [graph.py](../../agentend/src/orchestrator/planning/graph.py)
- **问题**: `llm.invoke()` + JSON 解析无 try/except，LLM 调用失败或返回无效 JSON 会导致图崩溃。
- **修复**: 包装 try/except，返回 fallback 状态。

### H-14. Agent 端: 所有 LLM 调用无超时
- **文件**: [graph.py](../../agentend/src/orchestrator/planning/graph.py), [aggregator.py](../../agentend/src/orchestrator/reporting/aggregator.py), [pin_memory.py](../../agentend/src/orchestrator/memory/pin_memory.py)
- **问题**: 所有 `ChatOpenAI` 实例未设置 `request_timeout`，LLM 调用可无限阻塞。
- **修复**: 添加 `request_timeout=30` 参数。

### H-15. Agent 端: 无 429 限流/重试处理
- **文件**: 同 H-14
- **问题**: 无 retry/backoff 逻辑，API 限流时直接失败。
- **修复**: 配置 `max_retries=3` 或实现自定义退避。

### H-16. Agent 端: worktree create() 失败时分支泄漏
- **文件**: [manager.py:47-88](../../agentend/src/workspace/manager.py#L47-L88)
- **问题**: `worktree_add` 失败时已创建的任务分支未删除。
- **修复**: 在 except 块中调用 `branch_delete` 清理。

### H-17. Agent 端: lifespan shutdown 未清理活跃 worktree
- **文件**: [main.py:25-51](../../agentend/src/app/main.py#L25-L51)
- **问题**: 关闭时未清理活跃 worktree，崩溃后可能残留锁定的 worktree。
- **修复**: 在 shutdown handler 中遍历并清理所有活跃 worktree。

### H-18. 跨端: CORS + 开发环境 URL 不匹配
- **文件**: [sse.ts](../../frontend/src/lib/sse.ts), [05-wiring.md](../../backend/docs/design/05-wiring.md)
- **当前状态**: 前端 SSE 默认同源 `/api/...`，开发环境走 Vite `/api` 代理；需要直连时才通过 `VITE_SSE_BASE_URL` 覆盖。后端 CORS origins 已由 `config.yaml` 的 `cors.allow_origins` 配置。

### H-19. 跨端: 后端日志泄漏 MySQL DSN
- **文件**: [mysql.go:23](../../backend/pkg/db/mysql.go#L23)
- **问题**: 同 C-6，DSN 含密码被写入日志。
- **修复**: 仅记录 host/dbname。
- **当前状态**: 已脱敏，当前日志不包含 DSN。

### H-20. 跨端: 后端所有秘密字段未统一外部化
- **文件**: [conf.go:79-80](../../backend/internal/conf/conf.go#L79-L80)
- **问题**: 仅 Qiniu 密钥使用环境变量覆盖，MySQL/JWT/Redis 密码未外部化。
- **修复**: 对所有秘密字段统一使用 `os.Getenv` 覆盖模式。
- **当前状态**: Backend 配置已集中在 `applyEnvOverrides` 支持 MySQL、JWT、AgentEnd、Redis、CORS、Admin、Auth 和 Server 端口环境变量覆盖。

---

## P2 — Medium（计划内修复）

| ID | 端 | 文件 | 问题 |
|----|-----|------|------|
| M-1 | FE | use-chat-stream.ts:120 | useEffect 清理未中止 SSE 连接 |
| M-2 | FE | use-chat-stream.ts:14,59 | store 闭包陈旧风险，eslint-disable 掩盖 |
| M-3 | FE | sse.ts:23 | 开发 URL 硬编码无 fallback |
| M-4 | FE | chat.ts:131 | streamingContent 无限增长无上限 |
| M-5 | FE | api.ts:42,48,76,197 | 4 个 API 函数未检查 HTTP 状态码 |
| M-6 | FE | chat.ts:32-45 | nav 层级设计冗余，no-op 函数与实际操作并存 |
| M-7 | FE | chat.ts:164 | Date.now() 作为消息 ID 有冲突风险 |
| M-8 | FE | ui/card,button,input.tsx | 历史项：这些 shadcn/ui 文件已不在当前代码树中；现保留 `dialog.tsx`、`popover.tsx` 与自维护 `error-boundary.tsx` |
| M-9 | BE | task.go:42-57 | CreateTask 中 Session 创建失败被 Warn 吞没 |
| M-10 | BE | task.go:64-67 | ListTasks 无分页，全表扫描。当前状态：Backend 已增加 `limit` / `before` cursor 分页，默认 50、最大 100，并通过响应 header 返回下一页游标。 |
| M-11 | BE | task.go:85-94 | GetTask 两次查询，第二次错误被忽略 |
| M-12 | BE | message.go:26 | ListMessages 的 Find 错误被忽略 |
| M-13 | BE | model/*.go | 无 GORM 关联定义，无软删除 |
| M-14 | BE | writer.go:22-23 | MAXLEN=10000，旧消息被修剪，新连接可能丢失历史 |
| M-15 | BE | redis.go:14-18 | Init() 不 Ping 验证连接，Close() 未在 shutdown 调用 |
| M-16 | BE | writer.go:107-110 | doFlush 中 lastSeq 与实际写入不一致 |
| M-17 | BE | task.go:131-163 | goroutine 快速完成时竞态：messageID 返回前 registry.Delete |
| M-18 | BE | task.go:108-162 | RunTask 无速率限制，可被 DoS。当前状态：`POST /api/tasks/:taskId/run` 已挂载 per-IP 限流，默认 30 次/分钟。 |
| M-19 | BE | conf.go:68 | godotenv.Load() 错误被 `_ =` 忽略 |
| M-20 | BE | task.go:129-135 | 用户消息保存失败后仍返回成功 |
| M-21 | BE | stream.go:62+ | 所有 fmt.Fprintf SSE 写入错误被忽略 |
| M-22 | BE | writer.go:187-192 | updateStatus 失败后消息可能永远卡在 streaming |
| M-22a | BE | task_service.go / writer.go | AgentEnd stream 建连失败会把底层错误拼进用户可见消息。当前状态：前端只接收脱敏错误文案，详细错误写日志；错误事件 Redis 发布使用 5 秒 context 并记录失败。 |
| M-22b | BE | diff_snapshot_service.go | DiffSnapshot 保存入口未限制状态枚举和 diff 大小。当前状态：Service 已校验 snapshot_id/session_id、status 白名单和 2MB diff 上限，并保留终态不可覆盖语义。 |
| M-22c | BE | session_service.go / avatar_service.go / agent_profile_service.go | Session 展示信息与 Profile/Soul 入口缺少统一输入边界。当前状态：Service 会 trim 并校验 session_id、agent_name、avatar_url；头像 URL 限制为本地绝对路径或 http/https，长度上限与模型字段一致。 |
| M-22d | BE | task_controller.go / message_service.go / stream_service.go / skill_service.go / admin_service.go / contact_group_service.go | 代理型入口、查询入口和跨进程同步入口缺少统一白名单。当前状态：repo_path、message mode、stream ids、skill name、group/task/admin session_id 均已 trim/限长/拒绝非法值；AgentEnd 错误详情只进日志。 |
| M-22e | BE | skill_service.go | 无效 skill ZIP 被当作 internal error。当前状态：坏 ZIP 返回 `valid:false` 校验结果，真正的服务端失败才返回 500。 |
| M-22f | BE | agentend_client/client.go | AgentEnd client 部分方法把 3xx 当成功。当前状态：skill install/remove、workspace/skills/config/health 等非流式调用统一按 2xx 成功语义处理，非 2xx 返回受限长度错误。 |
| M-22g | BE | task_service.go / admin_service.go / session_dao.go | Session 状态写入与契约不一致，出现 `active` / `failed` / `cleaned`，且状态更新 0 行会静默成功。当前状态：普通 Session 生命周期改为契约值 `idle` / `running` / `awaiting_review` / `completed` / `error` / `inactive`；DAO 增加状态白名单和 0 行回查，Admin workspace 仅在响应层映射 `inactive` 为 `cleaned`。 |
| M-22h | BE | cascade.go / contact_group_dao.go | DAO 级联删除 helper 和 ContactGroup 删除 items 时未检查中间操作错误，可能主记录删除成功但关联数据残留。当前状态：级联 helper 返回 error 并检查每个 `Pluck` / `Delete`；Task/Admin 删除事务会在任一步失败时回滚；ContactGroup 删除 items 失败也会回滚。 |
| M-22i | BE | model/session.go | Session 创建逻辑已改为初始 `idle`，但模型数据库默认值仍是 `running`，绕过 Service 或迁移默认值时会把未运行会话标成运行中。当前状态：GORM tag 改为 `default:idle`，并增加模型 tag 测试防止回退。 |
| M-22j | BE | message_dao.go / writer.go | 启动清理遗留 `streaming` Message 时只把 Message 标记为 `failed`，对应 Session 可能继续停在 `running` / `awaiting_review`。当前状态：`FailStaleStreamingMessages` 在事务中先定位遗留消息的 `(session_id, task_id)`，再把 Message 标记为 `failed`，并把仍在运行态的关联 Session 标记为契约状态 `error`。 |
| M-22k | BE | message_dao.go | Message 状态更新入口接受任意字符串，流处理路径可能写入未定义状态。当前状态：`UpdateMessageStatus` 增加生成契约白名单，只允许 `streaming` / `completed` / `failed`，非法值在访问 DB 前返回错误。 |
| M-22l | BE | message_dao.go | Message 内容、last_seq、状态更新命中 0 行会静默成功，流处理可能误以为消息已落库。当前状态：这些更新统一增加 0 行回查；同值更新且消息存在算成功，消息不存在返回 not found。 |
| M-22m | BE | message_dao.go | Message 创建入口未兜底校验 role/status，绕过 Service 的写入可能产生非契约消息。当前状态：`CreateMessage` 只允许 `user` / `agent` role；status 空值补 `completed`，非空只允许 `streaming` / `completed` / `failed`。 |
| M-22n | BE | message_dao.go | Message 创建入口未兜底校验关键 ID，绕过 Service 时可能写入不可追踪或超长的 message/task/session 关联。当前状态：`CreateMessage` 会 trim 并校验 `message_id` / `task_id` / `session_id` 必填与长度，同时限制 agent 元信息长度且不改动正文 content。 |
| M-22o | BE | message_dao.go / stream_service.go | Message 创建和 SSE 查询的 `message_id` / `task_id` 长度上限大于模型 `size:36`，校验放行后仍可能在 DB 层失败或截断。当前状态：Message DAO 的 `message_id` / `task_id` 上限改为 36，SSE `message_id` 查询上限也改为 36。 |
| M-22p | BE | task_service.go / contact_group_service.go / diff_snapshot_service.go | TaskID、ContactGroupID、DiffSnapshotID 的 Service 校验上限大于模型 `size:36`。当前状态：这些 UUID 形态字段的校验上限统一改为 36；SessionID 保持与模型 `size:128` 对齐。 |
| M-22q | BE | announcement_service.go / announcement_dao.go | Announcement 删除路径把路由字符串 ID 直接交给 `uint` 主键查询，依赖数据库隐式转换且非数字 ID 的语义不清。当前状态：Service 先把公告 ID 解析为正整数，DAO 接口改为接收 `uint`，非法 ID 在访问 DB 前返回 400。 |
| M-22r | BE | writer.go / admin_service.go | Redis `EXPIRE` 与 Admin 资源页 `INFO memory` 使用裸 `context.Background()`，Redis 卡顿时可能拖住清理或资源查询路径。当前状态：stream TTL 设置复用 5 秒超时上下文，Admin Redis memory 查询使用 3 秒超时并保留原有降级返回。 |
| M-22s | BE | message_dao.go / writer.go / task_service.go | Message 读写路径仍散落裸 `agent` / `user` / `completed` / `streaming` / `failed` 字符串，和契约白名单校验来源不一致。当前状态：DAO 查询、StreamWriter 写状态、RunTask 创建消息统一改用 `generated.MessageRole` / `generated.MessageStatus` 常量。 |
| M-23 | AG | pin.py:47 | pin_list 使用裸字符串查询参数，未用 Pydantic |
| M-24 | AG | workspace.py:50-76 | 多个端点缺少异常处理 |
| M-25 | AG | main.py | 无全局异常处理器 |
| M-26 | AG | graph.py:71 | 生产代码使用 assert 做控制流 |
| M-27 | AG | state.py | RuntimeState 未持久化，崩溃丢失 |
| M-28 | AG | manager.py:77 | 共享目录创建失败时孤立目录 |
| M-29 | AG | recovery.py:30-70 | 恢复仅启动时运行，运行时可能累积孤立项 |
| M-30 | AG | builtin.py:7-29 | SafetyRule 阻止列表过于宽松 |
| M-31 | AG | prompts.py:57-67 | 宽泛 `except Exception: pass` 隐藏故障 |
| M-32 | AG | builtin.py:32-51 | ScopeRule 可被 `..` 路径遍历绕过 |
| M-33 | AG | graph/aggregator/pin_memory | 每次 LLM 调用创建新 ChatOpenAI 实例 |
| M-34 | AG | pin_memory.py:42-70 | async 方法中同步 LLM 调用阻塞事件循环 |
| M-35 | AG | graph.py:37-42 | _extract_json 未处理格式错误的 LLM 输出 |
| M-36 | X | contracts 生成器 | message.yaml 和 validate-repo-path.yaml 未被代码生成器处理。当前状态：`scripts/generate_contracts.py` 已为 `message`、`validate-repo-path` 配置 Python / TypeScript / Go 三端生成目标，schema 已纳入生成链路。 |
| M-37 | X | session/models.py | `_VALID_TRANSITIONS` 缺少 `inactive` 状态。当前状态：`agentend/src/session/models.py` 的 `_VALID_TRANSITIONS` 已新增 `SessionState.INACTIVE: set()` 终态，配合 Backend `PATCH /api/sessions/:sessionId` 停用语义。 |
| M-38 | X | api.ts vs main.go | PUT vs PATCH 会话更新约定混乱 |
| M-39 | X | agentend vs backend | 响应包装格式无明确契约（FastAPI 扁平 vs backend `{code,data,msg}`） |
| M-40 | X | response.go vs agentend | 三端无统一错误码体系 |
| M-41 | X | session.go / session.py | "session not found" 消息大小写不一致 |
| M-42 | X | stream.go:28 | SSE 错误响应绕过标准 vo.Response 格式 |
| M-43 | X | task.go:68, writer.go | 基础设施故障（MySQL/Redis）使用 Warn 而非 Error |
| M-44 | X | config.yaml:26 | Qiniu 域名使用 HTTP 而非 HTTPS |
| M-45 | X | main.go | Auth 中间件已定义但从未应用（同 C-2 但作为配置问题） |
| M-46 | X | main.go:85-86 | 服务器端口 8080 硬编码，未从配置读取 |

---

## P3 — Low（改善代码质量）

| ID | 端 | 文件 | 问题 |
|----|-----|------|------|
| L-1 | FE | MessageList.tsx | 181 行，MessageRenderer 应提取为独立文件 |
| L-2 | FE | ImPage.tsx / ConversationList.tsx | 重复空状态模式 |
| L-3 | FE | sse.ts:34 | 解析错误仅 console.warn |
| L-4 | FE | AgentAvatar.tsx:55 | 外部图片 URL 未做域验证 |
| L-5 | FE | chat.ts:194 | 流式错误时 streamingContent 被丢弃 |
| L-6 | FE | assets/ | 当前已不存在 `frontend/src/assets/` 脚手架资源目录 |
| L-7 | FE | use-hover-style.ts | 当前已不存在该 Hook，hover 交互由 CSS/Tailwind 状态类承载 |
| L-8 | FE | api.ts | 当前已无 `deleteTask` / `patchSession` 死代码或 `StreamEvent` 重导出；`StreamEvent` 从生成契约直接导入 |
| L-9 | BE | main.go:38 | AutoMigrate 在生产启动时运行 |
| L-10 | BE | mysql.go:16 | DB 单例读取无内存排序保证（实际风险极低） |
| L-11 | BE | agentend_client/client.go:26 | HTTP Client 无超时 |
| L-12 | AG | validate.py:18-27 | 文件系统路径侦察端点（无认证时） |
| L-13 | AG | agent.py:131-173 | SSE 流无超时/断开检测 |
| L-14 | AG | engine.py:21-24 | allowed_tools 可能有重复项 |
| L-15 | AG | engine.py:8-30 | 规则异常未捕获，可跳过后续安全检查 |
| L-16 | AG | manager.py:29-32 | _locks 字典无限增长 |
| L-17 | AG | aggregator.py:28-43 | 同步 aggregate() 阻塞事件循环 |
| L-18 | X | generated/events.py:21 | timestamp 字段默认 None 但 YAML 声明为 required number |
| L-19 | X | generated/request.py:20 | rules 字段类型为 list[Any]，应为 list[str] |
| L-20 | X | task.go:229-234 | RunTask 返回值绕过 vo.Response 包装 |
| L-21 | X | agent.py vs stream.go | SSE event: 字段未被后端处理 |
| L-22 | X | agentend 全局 | 无日志格式/级别配置 |
| L-23 | X | sse.ts:35 | 生产构建保留 console.warn |
| L-24 | X | conf.go:68 | .env 加载失败静默忽略 |

---

## 修复优先级建议

### 第一阶段 — 安全加固（1-2 天）
1. **C-2 + H-12**: 后端启用 JWT 认证中间件 + 验证 agentType
2. **C-4**: Agent 端添加 API Key 认证
3. **C-3 + C-5 + H-20**: 所有密钥迁移至环境变量，config.yaml 仅含占位符
4. **C-6 + H-19**: 脱敏 DSN 日志
5. **C-7**: 添加 pre-commit hook 扫描密钥
6. **C-1**: CodeBlock.tsx 添加 DOMPurify 消毒

### 第二阶段 — 可靠性修复（2-3 天）
1. **H-1 + H-2**: 前端 SSE 重连去重和连接超时已落地；后续只需补 UI 连接状态提示
2. **H-6 + H-7**: 后端事务保护和级联删除
3. **H-8 + H-9**: Redis Streams 使用消费者组 + XACK
4. **H-10 + H-17**: 后端/Agent 端资源生命周期与 context 绑定
5. **H-13 + H-14 + H-15**: Agent 端 LLM 调用添加错误处理、超时、重试
6. **H-16**: Worktree 创建失败时清理分支

### 第三阶段 — 代码质量（3-5 天）
1. 清理前端死代码和未使用依赖（H-3, H-4, M-8）
2. 统一三端错误码和响应格式（M-40, M-42）
3. 完善契约层代码生成（M-36, M-37）
4. 修复后端错误处理模式（M-9 ~ M-22）
5. Agent 端事件循环阻塞修复（M-34, M-35）

### 第四阶段 — 防御性改进（持续）
1. 添加三端测试覆盖
2. 后端添加 golangci-lint
3. 前端添加 Vitest
4. Agent 端补充 pytest 覆盖
5. CI/CD 集成静态分析

---

*本报告由自动化代码审计工具生成，审计范围覆盖前端 29 个源文件、后端 24 个 Go 文件、Agent 端 50+ 个 Python 文件。所有发现均附有文件路径和修复建议。*
