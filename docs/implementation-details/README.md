# AgentHub 超详细实现说明

## 实现了什么

本目录是 AgentHub 当前实现的集中式代码导读。它把原本分散在 `frontend/docs/`、`backend/docs/`、`agentend/docs/`、`docs/design/` 和源码中的事实重新按运行链路组织到一个目录，目标是让维护者不用在三端之间反复跳转，也能回答以下问题：

- 一个聊天请求从浏览器进入后，经过哪些进程、对象和状态，最后如何回到页面。
- React 前端如何组织路由、服务端缓存、本地流状态、消息块和管理页面。
- Go Backend 如何分层、建模、路由、持久化、代理 AgentEnd，以及怎样保证流式消息可恢复。
- AgentEnd 如何选择适配器、创建会话和 Worktree、执行规则、监督 Run、调用 CLI、编排多个 Agent。
- YAML 契约如何生成 TypeScript、Go、Python 类型，哪些字段是跨端兼容边界。
- MySQL、Redis、SQLite、JSON 文件、Git Worktree、MinIO 分别保存什么，发生故障后由谁恢复。
- 认证、路径策略、上传限制、能力令牌、进程终止和沙盒开关如何共同形成安全边界。
- 本地开发、配置中心和 Docker 混合部署怎样组装。

文档只描述仓库中已经存在的实现。规划、遗留 TODO 和历史迁移背景仍保留在原有 `design/`、`backlog/`、`common/dev-plan/` 文档中，不混入本目录的运行事实。

## 怎么实现的

### 阅读顺序

| 编号 | 文档 | 重点 | 主要源码入口 |
|---|---|---|---|
| 01 | [系统总览与端到端链路](01-system-overview.md) | 进程边界、同步与异步调用、核心对象、故障边界 | `frontend/src/main.tsx`、`backend/cmd/server/main.go`、`agentend/src/app/main.py` |
| 02 | [前端实现](02-frontend.md) | 路由、三栏 UI、React Query、Zustand、SSE、消息块、Diff | `frontend/src/` |
| 03 | [Go Backend 实现](03-backend.md) | Controller/Service/DAO、路由、模型、任务与消息业务 | `backend/internal/` |
| 04 | [AgentEnd Runtime 实现](04-agentend-runtime.md) | FastAPI 生命周期、规则、会话、适配器、Run 监督 | `agentend/src/` |
| 05 | [Orchestrator 实现](05-orchestrator.md) | 规划图、工具发现、审查、并行波次、记忆、冲突恢复 | `agentend/src/orchestrator/`、`agentend/src/integration/` |
| 06 | [Workspace 与 Git 实现](06-workspace-and-git.md) | Worktree 命名、文件操作、Diff、提交、合并、恢复、预览 | `agentend/src/workspace/`、`agentend/src/preview/` |
| 07 | [SSE、消息与前端渲染](07-streaming-and-messages.md) | 事件协议、RuntimeHub、Redis Stream、MySQL 刷写、断线续接 | 三端流式代码 |
| 08 | [契约、API 与类型生成](08-contracts-and-api.md) | Schema 单一来源、生成矩阵、Backend/AgentEnd 端点表 | `contracts/`、`scripts/generate_contracts.py` |
| 09 | [存储、技能与 Artifact](09-storage-skills-artifacts.md) | 三类 MinIO bucket、技能上传/安装、Artifact 能力令牌、头像 | Backend 存储包、AgentEnd Skills |
| 10 | [安全与可靠性](10-security-and-reliability.md) | JWT、服务认证、路径策略、限流、输入限制、Outbox、恢复 | `middleware/`、`security/`、workers |
| 11 | [配置、启动与部署](11-configuration-and-deployment.md) | 配置优先级、Makefile、运行脚本、配置中心、Docker | 配置文件、`scripts/`、`docker/` |
| 12 | [测试与维护地图](12-testing-and-maintenance.md) | 测试分层、关键不变量、改动联动表、排障路径 | 三端测试目录与文档 |

### 文档约定

1. 所有路径都相对于仓库根目录 `agenthub/`。
2. “Backend”特指 Go 控制面；“AgentEnd”特指 Python 执行面。
3. `task_id` 是一次聊天任务或群聊的聚合标识；`session_id` 是其中某个 Agent 的会话标识；`message_id` 标识持久化消息；`run_id` 标识一次受监督执行。
4. `/api/*` 是浏览器主要访问的 Go Backend API；`/v1/*` 是 Backend 调用 AgentEnd 的控制面 API。
5. `contracts/schemas/` 是跨端协议的单一来源；三个 `generated/` 目录是生成结果，不能手改。
6. 文档中的默认值以 example 配置和代码默认值为准。实际部署可由 `config.yaml`、`.env` 或 Docker 环境变量覆盖。
7. 带 feature gate 的能力会明确写出开关；关闭开关时不能把相关路径理解为无条件可用。

### 事实来源优先级

遇到冲突时按以下优先级判断：

1. 当前可执行源码、路由注册和数据模型。
2. `contracts/schemas/*.yaml` 中的跨端协议。
3. 依赖清单与 example 配置。
4. 各端 `AGENTS.md` 的项目地图。
5. 原有设计和参考文档。

### 快速定位

| 想解决的问题 | 首先查看 |
|---|---|
| 新增一种 SSE 事件 | 07、08，然后修改 schema 并生成三端类型 |
| 新增 Backend API | 03、08，按 Controller → Service → DAO 边界实现 |
| 新增 Agent 类型 | 04、08、11，同时更新 registry、配置、契约和前端图标/名称 |
| 修改消息分页 | 02、03、07，核对 React Query 缓存和 MySQL 查询顺序 |
| 修改 Worktree 行为 | 05、06、10，重点验证路径策略、分支恢复和冲突终态 |
| 修改 Skill 上传 | 03、04、09、10，重点验证 ZIP、对象存储、Outbox 和原子安装 |
| 修改 Artifact 渲染 | 02、07、09、10，重点验证能力令牌、CSP、消息归属和大小限制 |
| 排查刷新后消息缺失 | 03、07、12，依次检查 Redis Stream、writer、MySQL 和前端对账 |
