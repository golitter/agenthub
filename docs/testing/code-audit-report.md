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
- **文件**: [CodeBlock.tsx:56](frontend/src/components/markdown/CodeBlock.tsx#L56)
- **问题**: `dangerouslySetInnerHTML` 渲染 Shiki 高亮输出，未经 DOMPurify 消毒。Agent 返回内容如果包含恶意 HTML，可能导致 XSS。
- **修复**: 安装 `dompurify`，在 `setHtml` 前调用 `DOMPurify.sanitize(result)`。

### C-2. 后端: 所有 API 路由无认证
- **文件**: [main.go:61-77](backend/cmd/server/main.go#L61-L77)
- **问题**: `middleware.Auth()` 已实现但从未应用到路由组。所有 `/api` 端点对未认证用户完全开放。
- **修复**: `api := r.Group("/api").Use(middleware.Auth(cfg.JWT.Secret))`

### C-3. 后端: config.yaml 硬编码密钥
- **文件**: [config.yaml:2,10](backend/configs/config.yaml#L2)
- **问题**: MySQL 密码 `"123456"` 和 JWT secret `"agenthub-demo-secret"` 明文硬编码在 Git 追踪文件中。攻击者可伪造 JWT 或直连数据库。
- **修复**: 迁移至环境变量，创建 `.env.example` 模板，在 `.gitignore` 中排除实际配置。

### C-4. Agent 端: 所有路由无认证
- **文件**: [api/v1/*.py](agentend/src/api/v1)
- **问题**: 全部 API 路由（含工作区创建/删除、Agent 执行）无认证中间件。任何人可执行任意 Git 操作。
- **修复**: 添加 API Key 或 JWT 认证依赖，应用于所有路由。

### C-5. Agent 端 config.yaml 硬编码数据库密码
- **文件**: [agentend/config.yaml:44](agentend/config.yaml#L44)
- **问题**: 数据库密码 `"123456"` 明文硬编码在 Git 追踪文件中。
- **修复**: 同 C-3，迁移至环境变量。

### C-6. 跨端: 日志泄漏 MySQL DSN（含密码）
- **文件**: [mysql.go:23](backend/pkg/db/mysql.go#L23)
- **问题**: `slog.Info("connecting to mysql", "dsn", dsn)` 将完整 DSN（含密码）写入日志。
- **修复**: 仅记录 host 和 db_name，脱敏 DSN。

### C-7. 跨端: .env 中存在 API 密钥
- **文件**: [agentend/.env:3](agentend/.env#L3)
- **问题**: DeepSeek API Key 明文存储在 `.env` 中。虽在 `.gitignore` 内，但存在意外提交风险。
- **修复**: 添加 pre-commit hook 扫描密钥模式；确保 `.env` 永不被提交。

---

## P1 — High（应尽快修复）

### H-1. 前端: SSE 重连无消息去重/间隙检测
- **文件**: [sse.ts](frontend/src/lib/sse.ts)
- **问题**: `EventSource` 自动重连时，无法检测断线期间丢失的消息。无 `lastEventId` 或序列号机制。
- **修复**: 利用 SSE 内置 `Last-Event-ID` 或自定义序列号检测消息间隙。

### H-2. 前端: SSE 无连接超时
- **文件**: [sse.ts:18-59](frontend/src/lib/sse.ts#L18-L59)
- **问题**: 无连接超时。如果服务器不响应，EventSource 将永远挂起。AbortController 未在错误路径上触发。
- **修复**: 添加 10s 连接超时，在 `onopen` 后清除超时定时器。

### H-3. 前端: 未使用的 `radix-ui` 包
- **文件**: [package.json:21](frontend/package.json#L21)
- **问题**: `radix-ui` 包已安装但从未 import，增加 bundle 大小。
- **修复**: 从 `package.json` 移除并执行 `pnpm install`。

### H-4. 前端: 整个 generated/ 下 response.ts 和 session.ts 未被使用
- **文件**: [generated/response.ts](frontend/src/generated/response.ts), [generated/session.ts](frontend/src/generated/session.ts)
- **问题**: 契约生成的类型文件从未被任何代码导入。
- **修复**: 让消费者使用这些类型（如在 store 中使用 `SessionState`），或调整生成器不生成未使用文件。

### H-5. 后端: 内部错误信息泄漏给客户端
- **文件**: [task.go:55,78,155,159](backend/internal/handler/task.go#L55)
- **问题**: `err.Error()` 直接返回在 API 响应中，暴露 GORM 内部错误（表名、DSN 片段）。
- **修复**: 返回通用错误消息，完整错误仅记录到服务端日志。

### H-6. 后端: CreateTask 无事务保护
- **文件**: [task.go:42-57](backend/internal/handler/task.go#L42-L57)
- **问题**: Task 和 Session 创建不在事务中，Session 插入失败会留下孤立 Task 记录。
- **修复**: 使用 `db.Transaction()` 包裹 Task + Session 创建。

### H-7. 后端: DeleteTask 无级联删除
- **文件**: [task.go:99-103](backend/internal/handler/task.go#L99-L103)
- **问题**: 仅删除 Task 记录，不删除关联的 Session 和 Message，导致引用完整性破坏。
- **修复**: 添加级联删除或使用 GORM 软删除 + 关联。

### H-8. 后端: Redis XRead 未使用消费者组
- **文件**: [stream.go:80,135](backend/internal/handler/stream.go#L80)
- **问题**: 使用裸 `XREAD` 而非 `XREADGROUP` + `XACK`，handler 重启时会重复发送已处理消息。
- **修复**: 创建消费者组，使用 `XREADGROUP` + `XACK` 实现至少一次处理。

### H-9. 后端: Redis XADD 失败被静默吞没
- **文件**: [writer.go:118-130](backend/internal/stream/writer.go#L118-L130)
- **问题**: `XADD` 失败仅 `slog.Warn` 但内容丢失，`lastSeq` 仍会更新跳过中间内容。
- **修复**: XADD 失败时内存缓冲并标记重试。

### H-10. 后端: goroutine 使用 context.Background() 与请求生命周期脱耦
- **文件**: [task.go:182-225](backend/internal/handler/task.go#L182-L225)
- **问题**: `RunTask` 的后台 goroutine 使用 `context.Background()`，客户端断开后仍运行至 30 分钟超时。
- **修复**: 传递 `c.Request.Context()` 或派生 context。

### H-11. 后端: CORS 仅允许 localhost:5173
- **文件**: [cors.go:12](backend/internal/middleware/cors.go#L12)
- **问题**: CORS origin 硬编码为 `http://localhost:5173`，无环境感知。
- **修复**: 从 config 加载 origins，生产环境使用严格域名列表。

### H-12. 后端: RunTask 未验证 agentType
- **文件**: [task.go:108-120](backend/internal/handler/task.go#L108-L120)
- **问题**: 用户可传入任意 `agentType` 字符串，无白名单验证。
- **修复**: 根据 `generated.AgentType` 常量校验。

### H-13. Agent 端: plan_node 无 LLM 错误处理
- **文件**: [graph.py:45-61](agentend/src/orchestrator/graph.py#L45-L61)
- **问题**: `llm.invoke()` + JSON 解析无 try/except，LLM 调用失败或返回无效 JSON 会导致图崩溃。
- **修复**: 包装 try/except，返回 fallback 状态。

### H-14. Agent 端: 所有 LLM 调用无超时
- **文件**: [graph.py:46](agentend/src/orchestrator/graph.py#L46), [aggregator.py:36](agentend/src/orchestrator/aggregator.py#L36), [pin_memory.py:43](agentend/src/orchestrator/pin_memory.py#L43)
- **问题**: 所有 `ChatOpenAI` 实例未设置 `request_timeout`，LLM 调用可无限阻塞。
- **修复**: 添加 `request_timeout=30` 参数。

### H-15. Agent 端: 无 429 限流/重试处理
- **文件**: 同 H-14
- **问题**: 无 retry/backoff 逻辑，API 限流时直接失败。
- **修复**: 配置 `max_retries=3` 或实现自定义退避。

### H-16. Agent 端: worktree create() 失败时分支泄漏
- **文件**: [manager.py:47-88](agentend/src/workspace/manager.py#L47-L88)
- **问题**: `worktree_add` 失败时已创建的任务分支未删除。
- **修复**: 在 except 块中调用 `branch_delete` 清理。

### H-17. Agent 端: lifespan shutdown 未清理活跃 worktree
- **文件**: [main.py:25-51](agentend/src/app/main.py#L25-L51)
- **问题**: 关闭时未清理活跃 worktree，崩溃后可能残留锁定的 worktree。
- **修复**: 在 shutdown handler 中遍历并清理所有活跃 worktree。

### H-18. 跨端: CORS + 开发环境 URL 不匹配
- **文件**: [sse.ts:23](frontend/src/lib/sse.ts#L23) vs [cors.go:12](backend/internal/middleware/cors.go#L12)
- **问题**: 前端 SSE 直连 8080 端口，但后端 CORS 仅允许 5173 来源。
- **修复**: 使 CORS origins 可配置，开发环境包含两个端口。

### H-19. 跨端: 后端日志泄漏 MySQL DSN
- **文件**: [mysql.go:23](backend/pkg/db/mysql.go#L23)
- **问题**: 同 C-6，DSN 含密码被写入日志。
- **修复**: 仅记录 host/dbname。

### H-20. 跨端: 后端所有秘密字段未统一外部化
- **文件**: [conf.go:79-80](backend/internal/conf/conf.go#L79-L80)
- **问题**: 仅 Qiniu 密钥使用环境变量覆盖，MySQL/JWT/Redis 密码未外部化。
- **修复**: 对所有秘密字段统一使用 `os.Getenv` 覆盖模式。

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
| M-8 | FE | ui/card,button,input.tsx | 3 个 shadcn/ui 组件已安装但从未使用 |
| M-9 | BE | task.go:42-57 | CreateTask 中 Session 创建失败被 Warn 吞没 |
| M-10 | BE | task.go:64-67 | ListTasks 无分页，全表扫描 |
| M-11 | BE | task.go:85-94 | GetTask 两次查询，第二次错误被忽略 |
| M-12 | BE | message.go:26 | ListMessages 的 Find 错误被忽略 |
| M-13 | BE | model/*.go | 无 GORM 关联定义，无软删除 |
| M-14 | BE | writer.go:22-23 | MAXLEN=10000，旧消息被修剪，新连接可能丢失历史 |
| M-15 | BE | redis.go:14-18 | Init() 不 Ping 验证连接，Close() 未在 shutdown 调用 |
| M-16 | BE | writer.go:107-110 | doFlush 中 lastSeq 与实际写入不一致 |
| M-17 | BE | task.go:131-163 | goroutine 快速完成时竞态：messageID 返回前 registry.Delete |
| M-18 | BE | task.go:108-162 | RunTask 无速率限制，可被 DoS |
| M-19 | BE | conf.go:68 | godotenv.Load() 错误被 `_ =` 忽略 |
| M-20 | BE | task.go:129-135 | 用户消息保存失败后仍返回成功 |
| M-21 | BE | stream.go:62+ | 所有 fmt.Fprintf SSE 写入错误被忽略 |
| M-22 | BE | writer.go:187-192 | updateStatus 失败后消息可能永远卡在 streaming |
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
| M-36 | X | contracts 生成器 | message.yaml 和 validate-repo-path.yaml 未被代码生成器处理 |
| M-37 | X | session/models.py | `_VALID_TRANSITIONS` 缺少 `inactive` 状态 |
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
| L-6 | FE | assets/ | 3 个未使用的 Vite 脚手架资源文件 |
| L-7 | FE | use-hover-style.ts | JS hover 应替换为 CSS :hover |
| L-8 | FE | api.ts | deleteTask, patchSession 和多余 StreamEvent 重导出为死代码 |
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
1. **H-1 + H-2**: 前端 SSE 添加重连去重和连接超时
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
