# 系统总览与端到端链路

## 实现了什么

AgentHub 是一个把即时通讯界面、持久化控制面和本地 Coding Agent 执行面组合起来的 monorepo。系统不是让浏览器直接启动 CLI，而是将职责拆为四层：

1. React Frontend 负责交互、服务端数据缓存、流式 UI 和卡片渲染。
2. Go Backend 负责公开 API、业务状态、认证、Agent 路由、SSE 中转和持久化。
3. Python AgentEnd 负责规则、会话、Worktree、CLI 适配、Run 生命周期和多 Agent 编排。
4. Contracts 负责 TypeScript、Go、Python 共享的协议定义与生成。

这种划分让聊天状态与实际进程生命周期解耦：浏览器刷新不会自动杀死 Agent；AgentEnd 的某个 CLI 失败也不应破坏 Task、Session 和既有消息；Backend 重启后能以 MySQL 为最终状态来源，并用 Redis Stream 补偿尚未落库的流事件。

## 怎么实现的

### 进程与端口

| 进程 | 默认端口 | 入口 | 职责 |
|---|---:|---|---|
| Frontend | 5173 | `frontend/src/main.tsx` + Vite | SPA、路由、REST/SSE 客户端 |
| Backend | 8080 | `backend/cmd/server/main.go` | `/api`、业务持久化、流式代理 |
| AgentEnd | 8001 | `agentend/src/app/main.py` | `/v1`、CLI 与 Orchestrator 执行 |
| Config Center Web | 5174 | `config-center/web/src/main.tsx` | example/actual 双栏配置编辑 |
| Config Center API | 9100 | `config-center/server/main.py` | 配置解析、保存、备份与服务控制 |
| MySQL | 3306 | 外部或 Docker | Task、Session、Message、Skill、Artifact 等 |
| Redis | 6379 | 外部或 Docker | 流事件、上传会话和低延迟恢复辅助 |
| MinIO API/Console | 9000/9001 | Docker 可选 | 头像、Skill 包、Artifact 的独立 bucket |

生产式 Docker 路径把 Frontend、Backend、MySQL、Redis、MinIO 放在容器中，AgentEnd 仍在宿主机运行，以便访问本机 Agent CLI、用户配置目录和代码仓库。容器 Frontend 由 Nginx 暴露在 8787。

### 核心实体与标识

| 实体 | 标识 | 所属边界 | 含义 |
|---|---|---|---|
| Task | UUID `task_id` | Backend/MySQL | 一次单聊或群聊任务，持有标题、仓库路径、置顶和活动状态 |
| Session | `session_id` | Backend + AgentEnd | Task 内某个 Agent 的逻辑会话，保存 Agent 类型、名称、头像和状态 |
| Message | UUID `message_id` + 数字主键 | Backend/MySQL | 用户或 Agent 消息；数字主键用于稳定游标分页 |
| Run | UUID `run_id` | AgentEnd/SQLite，Backend 保存关联字段 | 一次执行尝试，包含父子关系、预算、状态和事件 journal |
| Workspace | `workspace_id` | AgentEnd/JSON + Git | Session 的隔离 Worktree 与分支 |
| Skill | `name` | Backend/MySQL + MinIO；AgentEnd 工作区 | 技能仓库条目及安装到某 Session 的副本 |
| Artifact | UUID `resource_id` | Backend/MySQL + 私有 MinIO | 与 task/session/message 绑定的不可变大对象 |
| Conflict | UUID `conflict_id` | AgentEnd integration repository | Orchestrator 集成冲突及其恢复动作 |

标识不可以互换。尤其是 Task 聚合多个 Session；前端当前选中的通常是主 Session，但群聊消息请求必须同时携带 Task 级视角。Run 又比 Message 更细：同一消息的幂等运行通过 `run_key`、`run_request_hash` 和请求指纹约束。

### 创建会话链路

1. 用户在 `NewChatDialog` 输入仓库路径和一个或多个 Agent。
2. 前端先调用 `POST /api/validate-repo-path`；非 Git 目录可调用 `POST /api/init-git-repo`。
3. `createConversation` 调用 `POST /api/tasks`，请求含标题、`repo_path` 和 Agent 列表。
4. Backend 的 TaskController 只负责绑定参数；TaskService 校验并创建 Task、Session 等业务记录。
5. 多 Agent 但未显式选择 Orchestrator 时，前端会注入 Orchestrator；仅选择 Orchestrator 的无成员群聊被前端拒绝。
6. 返回 Task 后，前端将多个 Session 聚合成一个 Conversation。群聊优先选择 Orchestrator Session 作为主 Session。
7. `useConversations` 把结果写入 React Query 缓存；当前 Session 同步进 URL `?session=` 和 `localStorage`。

### 发送消息与流式返回

1. `MessageInput` 提交文本给 `POST /api/tasks/:taskId/run`。
2. Backend 创建用户消息，选择实际路由 Session/Agent，并为 Agent 消息准备稳定的 `message_id`。
3. Backend 调用 AgentEnd `/v1/agent/stream`，携带 task/session/message、repo、Agent 类型、会话上下文、预算和可选 Artifact 上传上下文。
4. AgentEnd 使用 `PathPolicy` 验证仓库路径，解析或创建 Workspace 与 Session，执行规则引擎，然后注册或复用 Run。
5. 普通 Agent 由对应 Adapter 启动 CLI 子进程；Orchestrator 进入 LangGraph 规划与执行图。
6. Adapter 把不同 CLI 的原始协议归一为 `StreamEvent`。出站前由 transport sanitizer 限制超大 text、tool args 和 tool result。
7. Backend 一边把事件发布到内存 `RuntimeHub` 供当前连接低延迟读取，一边写 Redis Stream 作为可重放顺序日志。
8. Backend stream writer 聚合文本并批量刷入 MySQL Message，终态更新 `completed`、`failed` 或对应 termination reason。
9. 浏览器的 `EventSource` 订阅 `/api/tasks/:taskId/stream`，并以查询参数携带实际 `session_id` 与 `message_id`；`useChatStream` 把事件分发给 Zustand message store。
10. message store 使用 block reducer 把纯文本和结构化事件归并为 `MessageBlock[]`，再由 `BlockRenderer` 选择 Markdown、Tool、Plan、Diff、HTML、Image 等组件。

### 群聊和 Orchestrator 链路

群聊不是让所有 Agent 共享一个 Session。Task 下仍有多个独立 Session，Orchestrator Session 负责主对话和协调：

1. Backend 路由层把群聊请求发给 Orchestrator。
2. Orchestrator 的 reason 节点结合用户消息、历史摘要、active pins、系统约束和渐进加载的 Skill。
3. 模型可先发现可用 Agent，再生成 `PlanOutput`。需要审查时发出 `plan_review` 并等待外部 `/v1/agent/review`。
4. Dispatcher 将任务按依赖关系拓扑分层，ExecutionEngine 逐波执行；同波无依赖任务可以并行。
5. 每个子任务通过目标 Agent Adapter，在自己的 Session Worktree 中运行，并通过 coordination 事件报告进度。
6. taskctl 产出结构化 Git 集成事实，IntegrationService 按绑定关系执行或记录操作。
7. 合并冲突进入 ConflictRecoveryCoordinator；重试、接受某侧或取消都以 expected attempt 和唯一终态保护。
8. Aggregator 汇总全部 TaskResult，发出 final summary；记忆模块保存对话摘要、Pin 和演化信息。

### 数据分层与恢复来源

| 存储 | 数据 | 为什么使用它 | 恢复方式 |
|---|---|---|---|
| MySQL | Task、Session、Message、Skill、Artifact、公告、联系人、清理 Job | 业务最终状态与关系查询 | Backend 启动迁移后直接读取 |
| Redis Stream | 流事件和 sequence | 低延迟、断线续接、落库前缓冲 | SSE 按 sequence 重放，writer 追平 MySQL |
| Redis KV/会话 | Skill 上传确认相关临时状态 | 有 TTL 的短生命周期协调 | DB receipt 提供确认幂等兜底 |
| SQLite | Run journal、集成操作、冲突恢复记录 | AgentEnd 本地事务与重启恢复 | lifespan 中 supervisor/integration recover |
| JSON 文件 | Session mapping、Workspace registry | 小型本地状态、可人工检查 | 原子写入，启动时加载并与 Git 事实对账 |
| Git | Worktree、分支、提交、冲突 | 代码事实和隔离执行 | `git worktree list` 与 registry 双向恢复 |
| MinIO | 三类大对象 | 避免数据库 BLOB/SSE 承载大资源 | 元数据在 MySQL；对象健康检查与对账 worker |

### 启动与关闭顺序

Backend 启动时先加载 YAML 和 `.env` 覆盖，再连接 MySQL、执行显式迁移、清理过期收据、连接 Redis、清理遗留 streaming 消息、创建 AgentEnd client 和各存储 Runtime，最后组装 Router 与后台 worker。HTTP server 监听 SIGINT/SIGTERM，取消共享 context 后优雅关闭。

AgentEnd lifespan 先进行危险配置校验，再创建 registry、session、rules、workspace、preview、BackendClient、PathPolicy、Run repository、Integration repository 和 supervisor。随后恢复 Run、Worktree、未完成集成、冲突恢复与 Skill staging，连接 MySQL 只读器并启动 inactive cleanup。关闭时依次取消清理任务、停止预览、关闭 supervisor/repository/client/database 和可观测性客户端。

### 关键不变量

- 跨端枚举和字段先改 schema，再生成；不能三端手工分别修改。
- Controller 不承载数据库查询；业务错误从 Service 返回 `BizError`，在 HTTP 边界映射。
- 一个 Agent 的执行目录必须是经过 PathPolicy 允许的仓库或由 WorkspaceManager 管理的 Worktree。
- SSE 的 UI 实时性不等于持久化完成；终态前必须保证事件 journal 和 Message 状态可恢复。
- Artifact 上传令牌必须绑定 task/session/message/kind/大小/摘要，不能退化成通用 MinIO 凭据。
- 删除 Task 与外部清理意图必须通过持久化 Job 解耦，不能因为 AgentEnd 暂时不可用而丢失清理动作。
- 群聊消息展示以 Task 聚合，但 Agent 身份、Workspace、CLI Session 仍以 Session 隔离。
