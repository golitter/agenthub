# 详细文档

## design/（开发实施文档）

- [00-backend-deep-dive.md](../design/00-backend-deep-dive.md) — 后端阅读入口（边界、启动链路、专题文档导航）
- [01-models.md](../design/01-models.md) — 数据模型（Task / Session / Message / DiffSnapshot / SessionAgent / AdminSetting / Announcement / ContactGroup / ContactGroupItem / SkillHub / AgentSkill）
- [02-handlers.md](../design/02-handlers.md) — 三层架构：Controller → Service → DAO（13 组业务模块 + BizError 统一错误处理）
- [03-stream.md](../design/03-stream.md) — SSE 流式中转（RuntimeHub + Redis Stream → MySQL 批量刷写）
- [04-config.md](../design/04-config.md) — 配置加载（config.yaml + .env overlay + Admin 密码）
- [05-wiring.md](../design/05-wiring.md) — 应用组装（main.go + internal/app.NewRouter + 自注册路由 + 优雅关闭）
- [06-message-pagination.md](../design/06-message-pagination.md) — 消息列表 Cursor 分页 + mode 可见性控制
- [07-admin-api.md](../design/07-admin-api.md) — 管理面板 API（密码认证 + 资源监控 + 会话清理 + IP 限流）
- [layered-refactoring.md](../design/layered-refactoring.md) — 三层架构重构说明（Controller/Service/DAO 拆分要点）

## reference/

- [tech-stack.md](tech-stack.md) — 技术栈详情

## API 端点摘要

- 健康检查：`GET /ping`、`GET /health`
- Task：`/api/tasks`（创建/列表/详情/删除/置顶）、`/api/tasks/:taskId/run`、`/review`、`/leave`、`/stream`，以及 `POST /api/validate-repo-path`、`POST /api/init-git-repo`
- Message：`GET /api/tasks/:taskId/messages`、`/messages/window`
- Announcement：`/api/tasks/:taskId/announcements`
- Session：`PATCH /api/sessions/:sessionId`，Profile/SOUL：`/api/sessions/:sessionId/profile|detail|soul`
- Workspace：`/api/workspace/...` 与 `/api/session/:sessionId/...` 代理 AgentEnd 文件、diff、commit、revert、preview、merge
- DiffSnapshot：`GET/PUT /api/diff-snapshots/:snapshotId`
- ContactGroup：`/api/contact-groups` 与 `/api/contact-groups/:groupId/items`
- SkillsHub：`/api/skills` 上传、确认、导入、删除、移除会话关联，`POST /api/internal/builtin-skills`
- Admin：`/api/admin/auth|health|avatar|resources|sessions|workspaces|agents|services|statistics`
- Agent：`GET /api/agent-types`

## 核心架构

- `internal/app` 组装 DAO → Service → Controller 并统一注册 Gin 路由。
- Controller 负责绑定/响应，Service 承载业务和 `BizError`，DAO 封装 GORM/MySQL 访问。
- `internal/stream` 将 AgentEnd SSE 事件中转到前端，并通过 Redis Stream 批量落库到 MySQL。
- `pkg/agentend_client` 连接 AgentEnd；`pkg/storage` 在七牛云与本地磁盘间选择上传后端。
