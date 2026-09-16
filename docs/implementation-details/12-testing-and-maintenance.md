# 测试、验证与维护地图

## 实现了什么

项目使用 Vitest、Go test、pytest 和构建检查覆盖三端；关键模块还包含集成测试、SQL mock、内存对象存储替身和恢复场景。维护时应按跨端影响选择测试，而不是只运行改动目录里的一个用例。

## 怎么实现的

### 测试栈

| 范围 | 工具 | 位置 |
|---|---|---|
| Frontend | Vitest + React/DOM 测试环境 | `frontend/src/**/__tests__`、hook tests |
| Backend | Go `testing`、sqlmock、内存 store | `backend/**/*_test.go` |
| AgentEnd | pytest + pytest-asyncio | `agentend/tests/` |
| Config Center API | pytest | `config-center/tests/` |
| Config Center Web | Vitest + TypeScript build | `config-center/web/src/*.test.ts` |
| Contracts | 生成后 diff/语言编译 | `make generate` + 三端测试/build |

### Frontend 关键用例

- `api.test.ts`：Task/Conversation 映射、分页 query、错误解包。
- `block-reducer.test.ts`：文本、未闭合 marker、image/attachment 等块。
- `query-keys.test.ts`：缓存 key 和 session patch/upsert。
- `sse.test.ts`：EventSource listener、close/error。
- `chat.test.ts`：多 Session store、流式更新、终态。
- `use-theme.test.ts`：system/light/dark 与事件。
- `use-resize.test.ts`：持久化、边界、折叠。

前端改动最低验证：`pnpm test`、`pnpm lint`、`pnpm build`。涉及响应式和可访问性的改动还需要浏览器手测小屏、键盘、Dialog 焦点和两种主题。

### Backend 关键用例

| 模块 | 重点 |
|---|---|
| conf | env override、非法 bool/size/duration、存储组合 |
| middleware | JWT/service auth、body limit、限流 |
| dao/gorm | migration、cascade、分页顺序、事务和错误映射 |
| task service | 路由、幂等 run、取消、cleanup outbox |
| stream | Hub 并发、writer 合并、SSE 前置错误 |
| skill | ZIP validator、scanner、MinIO、receipt、worker fence、全链路 |
| artifact | capability、大小/digest/idempotency、controller headers |
| storage | local/MinIO/memory、runtime selection、health |

普通验证在 `backend/` 运行 `go test ./...`。MinIO integration tests 需要显式环境和隔离 bucket，不应默认破坏共享数据。

### AgentEnd 关键用例域

AgentEnd tests 覆盖 Adapter 事件解析、Session ID 写回、Workspace Git 操作与恢复、路径策略、Run repository/supervisor、取消/超时、规则、Skill 原子安装、规划 graph、上下文压缩、Pin、BackendClient、Integration/Conflict recovery、Pi Adapter 和 taskctl merge。

异步测试必须等待真实终态或使用可控 event，不用固定长 sleep。涉及 subprocess 时验证 process group 被回收。涉及 JSON/SQLite 时使用临时目录，避免污染 `logs/`。

### Config Center 验证

`make config test` 是单一入口，依次：

1. `uv sync --locked`。
2. pnpm frozen lock install。
3. Python pytest。
4. Web vitest。
5. Web production build。

测试覆盖 dotenv/YAML/JSON round trip、profile 路径、备份恢复、API 校验、runner 限制和 draft 行为。

### 跨端变更矩阵

| 改动 | 必跑验证 |
|---|---|
| Contract 字段/枚举 | make generate；Frontend test/build；Backend go test；AgentEnd pytest |
| SSE 事件 | AgentEnd emitter tests；Backend stream tests；Frontend sse/reducer/store tests |
| Task/Session 模型 | migration tests；DAO/Service；API 映射；Frontend Conversation |
| Workspace/Git | PathPolicy；GitOps；recovery；Backend proxy；DiffCard |
| Run/取消 | repository/supervisor；Adapter process；Backend run API；Frontend stop UI |
| Skill | validator/scanner/store/worker；AgentEnd install recovery；SkillsHub UI |
| Artifact | render Go tests；Backend capability/store/controller；Frontend HtmlCard |
| 配置 | conf/settings validators；example/actual 同步；Config Center tests；precheck |
| Docker/Nginx | compose config；precheck；health/readiness；SSE 实测 |

### 关键手工场景

1. 创建单 Agent 会话，连续发送两条消息，刷新后内容、头像、Session ID 不变。
2. 流式过程中刷新/断网重连，不重复 token，最终 Message completed。
3. 群聊由 Orchestrator 规划，approve/discuss/modify 三条审查路径均可继续。
4. 两个并行 Agent 修改相同文本文件，生成冲突投影并分别测试 retry、accept、cancel。
5. 停止正在运行的 CLI，确认子进程组退出、Run/Message 有终态、按钮恢复。
6. 上传恶意 ZIP：`../`、绝对路径、symlink、超高压缩比、过多文件均被拒绝。
7. Artifact token 过期、跨 message 复用、错误 digest、超大 multipart 均失败；正常 HTML 可在 sandbox 中展示。
8. Backend 在 AgentEnd 离线时删除 Task，恢复 AgentEnd 后 cleanup job 最终完成。
9. 杀死 AgentEnd 后重启，Workspace registry、Git worktree、Run/integration/conflict 状态能恢复。
10. MinIO 不可用时 `/ready` 对已启用 store 返回 503；关闭 gate 后不阻塞无关功能。

### 数据库迁移维护

模型字段变化不能只依赖 AutoMigrate。`internal/dao/gorm/migrations.go` 使用 migration 表和 advisory lock，包含历史数据修复与索引/约束变更。新增迁移应：

- 有唯一版本和幂等检查。
- 在旧数据、空库、重复运行、并发启动下测试。
- 先扩展再迁移写端，最后收紧/删除。
- 大表变更评估锁时间。
- 同步模型文档与 contract（若跨端）。

### 排障地图

| 症状 | 日志/状态 | 下一步 |
|---|---|---|
| 前端空白 | `logs/frontend.log`、browser console | Vite chunk、route ErrorBoundary、API proxy |
| Backend 起不来 | `logs/backend.log` | config validate、MySQL migration、Redis、store health |
| AgentEnd 起不来 | `logs/agentend.log` | YAML fail-fast、安全配置、CLI/Skill binary、SQLite |
| Run 卡住 | `/v1/runs/:id` + events | supervisor task、Adapter stdout/stderr、timeout |
| 消息丢失 | MySQL Message + Redis Stream | last_seq、writer、SSE identity |
| Workspace 找不到 | workspace JSON + `git worktree list` | recovery、base_dir、session mapping |
| Skill sync_error | SkillOperationJob + audit | AgentEnd install、object hash、fence/lease |
| Artifact 404 | Artifact metadata/status + MinIO | capability upload、object key、retention |

### 文档维护规则

- 源码行为变化时同步本目录对应专题，不把未来计划写成当前行为。
- 路由以 RegisterRoutes/APIRouter decorator 为准。
- 版本以 package.json/go.mod/pyproject.toml 为准。
- 配置以 model、example 和 env override 三者交叉验证。
- 新增专题文件后更新本目录 README。
- 跨端协议修改必须同时写 contracts/logs 变更记录。

### 发布前完整检查

- `git diff --check` 无空白错误。
- 文档内反引号路径实际存在，或明确标为运行时生成路径。
- 所有 Markdown 相对链接可解析。
- Contract 生成无未提交的意外差异。
- Frontend test/lint/build、Backend go test、AgentEnd pytest、Config Center test 按影响范围通过。
- example 配置不含真实 secret，实际 `.env` 未入库。
- Docker compose health、Backend `/ready`、AgentEnd `/health/ready` 通过。
- 手工执行至少一条完整 REST + SSE + persistence 主链路。
