# 详细文档

## design/（开发实施文档）

- [01-models.md](../design/01-models.md) — 数据模型（Task / Session / Message / DiffSnapshot / SessionAgent GORM 模型）
- [02-handlers.md](../design/02-handlers.md) — HTTP 处理器（Task CRUD / Session / Message / Agent / Agent Profile / Avatar / Stream / DiffSnapshot / Workspace / Announcement / Admin）
- [03-stream.md](../design/03-stream.md) — SSE 流式中转（Redis Stream → MySQL 批量刷写）
- [04-config.md](../design/04-config.md) — 配置加载（config.yaml + .env overlay + Admin 密码）
- [05-wiring.md](../design/05-wiring.md) — 应用组装（main.go + DI + 路由注册 + 中间件 + Admin 路由）
- [06-message-pagination.md](../design/06-message-pagination.md) — 消息列表 Cursor 分页
- [07-admin-api.md](../design/07-admin-api.md) — 管理面板 API（密码认证 + 资源监控 + 会话清理 + 健康检查）

## reference/

- [tech-stack.md](tech-stack.md) — 技术栈详情
