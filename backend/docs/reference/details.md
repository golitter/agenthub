# 详细文档

## design/（开发实施文档）

- [01-models.md](../design/01-models.md) — 数据模型（Task / Session / Message / DiffSnapshot / SessionAgent / AdminSetting / Announcement / ContactGroup / ContactGroupItem / SkillHub / AgentSkill）
- [02-handlers.md](../design/02-handlers.md) — 三层架构：Controller → Service → DAO（13 组业务模块 + BizError 统一错误处理）
- [03-stream.md](../design/03-stream.md) — SSE 流式中转（RuntimeHub + Redis Stream → MySQL 批量刷写）
- [04-config.md](../design/04-config.md) — 配置加载（config.yaml + .env overlay + Admin 密码）
- [05-wiring.md](../design/05-wiring.md) — 应用组装（main.go + Controller DI + 自注册路由 + 优雅关闭）
- [06-message-pagination.md](../design/06-message-pagination.md) — 消息列表 Cursor 分页 + mode 可见性控制
- [07-admin-api.md](../design/07-admin-api.md) — 管理面板 API（密码认证 + 资源监控 + 会话清理 + IP 限流）

## reference/

- [tech-stack.md](tech-stack.md) — 技术栈详情
