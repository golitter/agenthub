# 契约、API 与类型生成超详细实现

## 实现了什么

`contracts/schemas/` 是跨端 JSON Schema 的唯一手写来源。`scripts/generate_contracts.py` 把 YAML 转换为 Python、TypeScript、Go 类型，防止事件枚举、请求字段和状态机在三端漂移。

## 怎么实现的

### Schema 矩阵

| Schema | 内容 | Python | TypeScript | Go |
|---|---|---|---|---|
| event-types | EventType、StreamEvent | `generated/events.py` | `generated/events.ts` | `generated/events.go` |
| agent-request | AgentType、AgentRequest | `request.py` | `request.ts` | `request.go` |
| agent-response | AgentResponse | `response.py` | `response.ts` | `response.go` |
| agent-routing | route 与 RunTask 响应、群聊查询 | `agent_routing.py` | `agent-routing.ts` | `agent_routing.go` |
| session-state | 状态与合法转换 | `session.py` | `session.ts` | `session.go` |
| message | 消息角色、状态、stream 追踪 | `message.py` | `message.ts` | `message.go` |
| validate-repo-path | 校验与初始化仓库 | `validate_repo_path.py` | `validate-repo-path.ts` | `validate_repo_path.go` |
| skill-storage | 上传、确认、存储元数据 | `skill_storage.py` | `skill-storage.ts` | `skill_storage.go` |
| agent-run | Run 状态、预算、事件、取消 | `agent_run.py` | `agent-run.ts` | `agent_run.go` |
| integration-result | taskctl Git 集成事实 | `integration_result.py` | `integration-result.ts` | `integration_result.go` |
| conflict-recovery | 冲突记录、动作、响应 | `conflict_recovery.py` | `conflict-recovery.ts` | `conflict_recovery.go` |

### 生成流程

`make generate` 调用 Python 生成器。生成器读取全部 schema definitions，按语言映射 enum、object、array、required/optional、nullable 和 description，使用稳定顺序输出。生成文件头标明不可手改。

修改流程固定为：

1. 修改 `contracts/schemas/*.yaml`。
2. 在 `contracts/logs/YYYY-MM-DD-description.md` 记录原因、字段差异和三端影响。
3. 运行 `make generate`。
4. 检查三端生成 diff。
5. 修改业务代码和测试。

只修改 generated 文件会在下次生成丢失，而且其他两端仍旧不兼容。

### 主要枚举

AgentType 当前包含 claude-code、opencode、orchestrator、codex、pi。SessionState 包含 idle、running、awaiting_review、resolving、awaiting_resolution、completed、interrupted、error、inactive。Run 状态和终止原因由 agent-run schema定义。EventType 还包含 runtime/coordination/ask-card 扩展，业务代码不得用未登记的任意字符串冒充正式事件。

`StreamEvent` 的 required 字段只有 `type`；可选 `content` 是对象，可选 `timestamp` 是 Unix 秒时间戳。Run journal 的 `seq` 不在 StreamEvent 本体中，而在 `AgentRunEventEnvelope` 中。

### AgentRequest 的语义分组

- 身份：agent_type、task/session/message/run 相关字段。
- 用户输入：prompt/message 与 system prompt append。
- 工作区：repo_path、workspace/task/session binding。
- 会话延续：CLI session id 与 resume 控制。
- 群聊：agents、group id、primary session、上下文。
- 工具和规则：allowed tools、skills、pins、Soul。
- 执行：预算、artifact upload context、integration capability。

required 仅表示协议层必须存在；业务层还会校验非空、绑定一致、路径合法和 feature gate。

### Backend 公开 API 分组

| 分组 | 基础路径 | 消费方 |
|---|---|---|
| 健康 | `/ping`,`/health`,`/ready` | 运维 |
| Task/Run | `/api/tasks` | Frontend |
| Stream/Message | `/api/tasks/:taskId/stream|messages` | Frontend/AgentEnd internal |
| Session/Profile | `/api/sessions` | Frontend |
| Workspace proxy | `/api/workspace`,`/api/session` | Frontend |
| Skill | `/api/skills` | Frontend + AgentEnd report |
| Artifact | `/api/artifacts`,`/api/internal/artifacts` | Frontend/render |
| Admin | `/api/admin` | Frontend admin |
| Assets | `/api/assets/avatars` | 浏览器公开读 |

普通 JSON 返回 Backend `vo` envelope；Frontend api client 解包。二进制、HEAD、SSE 不使用 envelope。

### AgentEnd API 精确端点

| 方法 | 路径 |
|---|---|
| POST | `/v1/agent/stream` |
| POST | `/v1/agent/review` |
| POST | `/v1/agent/execute` |
| GET | `/v1/agents/configs` |
| GET | `/v1/session`、`/v1/session/:id` |
| POST | `/v1/session/:id/interrupt` |
| DELETE | `/v1/session/:id` |
| GET | `/v1/runs`、`/v1/runs/:id`、`/v1/runs/:id/events` |
| POST | `/v1/runs/:id/cancel`、`/v1/runs/:id/resume` |
| GET/POST/PUT/DELETE | `/v1/workspace`、`/v1/workspace/:workspaceId` 的 files/diff/commit/revert/preview/merge/task 子路径 |
| POST | `/v1/validate-repo-path`、`/v1/init-git-repo` |
| GET | `/v1/resources` |
| GET/POST/DELETE | `/v1/skills/:agentType`、`/v1/skills/:agentType/:skillName/install`、`/v1/skills/:agentType/:skillName` |
| POST/GET | `/v1/pin/add|remove|announcement-unpin|list` |
| GET/POST | `/v1/internal/integration-operations` 下的 metrics、operation、git-record、resolution-attempts、execute |
| GET/POST | `/v1/internal/conflicts/:conflictId`、`/projection`、`/actions` |
| GET/POST | `/v1/evals` 下的 datasets、experiments、trials、compare 与 trial reviews |

### 兼容策略

- 新字段优先做 optional/nullable，再分阶段启用写端，最后提升为 required。
- enum 新值必须保证旧前端有 unknown fallback；删除值需要先停止写入并迁移存量。
- IntegrationResult V1/V2 使用 version 与写开关过渡。
- legacy Skill DB blob、tmp confirm 等由配置 gate 维持迁移窗口。
- `merge-to-main` 路径名保留兼容，但实现使用实际默认分支。

### 校验重点

生成后应确认：

- 三端 AgentType 和 EventType 项完全相同。
- JSON 名称、Go tag、Pydantic alias、TS property 一致。
- required/optional 没被语言默认值掩盖。
- int64/sequence 在浏览器不会超出安全数值；不适合 number 的 ID 使用 string。
- 状态转换不只存在于 schema，也在 Backend Service 和 AgentEnd repository 执行。
- API 文档中的 endpoint 与实际 RegisterRoutes/APIRouter decorator 一致。
