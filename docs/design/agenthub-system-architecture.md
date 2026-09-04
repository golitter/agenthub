# AgentHub 核心架构

**[在线打开交互式架构图](https://golitter.github.io/agenthub/)**

## 架构结论

AgentHub 采用“聊天控制面 + Agent 执行面”的分层方式。开发者在 React IM 中创建任务，Go Backend 负责统一 API、业务状态和流式中转；AgentEnd 负责安全规则、Run 生命周期、Agent 适配与多 Agent 编排，最终将工作限制在会话级 Git Worktree 中。

图中仅保留 10 个核心组件，并突出一条主路径：

> 开发者 → React Frontend → Go Backend → AgentEnd Runtime → 适配与编排层 → Git Worktree

结果事件沿相同 SSE 链路反向返回，避免在图中重复添加回程连线。Redis Stream 负责事件回放与断线恢复，MySQL 持久化任务、会话和消息，MinIO 可选承载 Skill、头像与 Artifact 大对象。

## 信任边界与外部依赖

- AgentHub 受信服务边界包含 Frontend、Backend、AgentEnd、编排层、Worktree 与平台数据面。
- Claude Code、OpenCode、Codex、Pi 等 Coding CLI 及其模型服务属于外部依赖，通过受控子进程协议或模型调用接入。
- AgentEnd 使用服务令牌、`PathPolicy`、安全规则和 Git Worktree 隔离控制面权限与代码执行目录。
- `contracts/schemas/` 是 TypeScript、Go、Python 跨端类型的单一来源，不额外画成连线密集的组件，而是作为架构约束记录。

## 交付文件

- [在线交互式 Archify 架构图](https://golitter.github.io/agenthub/)
- [仓库内 HTML](agenthub-system-architecture.html)
- [Archify 源规格](agenthub-system-architecture.archify.json)

架构图基于仓库修订 `7319c2692e2f2cfb925d80dab2d9023def70c817` 生成，组件内包含 19 个可追溯源码引用。

## 在线发布

`.github/workflows/publish-architecture-pages.yml` 使用 GitHub Pages 官方 Actions 发布架构站点：

- 推送到 `main` 且 `docs/design/**/*.html` 发生变化时自动部署。
- 支持在 Actions 页面通过 `workflow_dispatch` 手动部署。
- `agenthub-system-architecture.html` 同时发布为站点首页 `/`。
- `docs/design/` 下的其他 HTML 按 `design/<文件名>.html` 暴露。

仓库首次启用时，需要在 **Settings → Pages → Build and deployment → Source** 中选择 **GitHub Actions**。部署成功后的站点地址为 `https://golitter.github.io/agenthub/`。
