# 2026-05-27 — Admin Dashboard API（管理面板后端接口）

## 变更原因

新增管理面板功能，后端需提供 `/api/admin/*` 系列接口，前端需对应的类型定义和 API 调用函数。

## 变更文件

**无 schema 文件变更。**

Admin API 类型（ResourcesResponse、WorkspaceItem、AgentInfo、ServiceInfo、StatisticsResponse 等）直接在后端 handler 和前端 `lib/api.ts` 中定义，不走 `contracts/schemas/` 生成流程。

理由：Admin API 为单端（Backend ↔ Frontend）通信，不涉及 AgentEnd；数据模型后续可能随管理功能迭代调整，提前写入 schema 增加重构成本。

## 新增接口

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| POST | `/api/admin/auth` | 无 | 密码验证，签发 admin JWT |
| GET | `/api/admin/resources` | admin JWT | 系统资源（磁盘/内存/Redis） |
| DELETE | `/api/admin/sessions` | admin JWT | 批量清理会话 |
| GET | `/api/admin/workspaces` | admin JWT | 工作区列表 |
| DELETE | `/api/admin/workspaces/:id` | admin JWT | 清理单个工作区 |
| GET | `/api/admin/agents` | admin JWT | Agent 列表 + 脱敏配置 |
| GET | `/api/admin/services` | admin JWT | 三端服务健康状态 |
| GET | `/api/admin/statistics` | admin JWT | 会话/消息/存储统计 |
| GET | `/api/admin/avatar` | admin JWT | 获取头像 URL |
| PUT | `/api/admin/avatar` | admin JWT | 更新头像 URL |
| GET | `/api/admin/health` | 无 | Admin 路由健康检查 |
| GET | `/health` | 无 | Backend 健康检查 |

## 认证方案

- 密码明文存储在 `configs/config.yaml` 的 `admin.password` 字段
- 验证通过后签发 JWT（1 小时有效期），使用现有 `jwt.secret`
- admin JWT claims 包含 `"admin": true` 字段，区别于用户 JWT

## 跨端影响

| 端 | 影响 |
|------|------|
| Backend | 新增 8 个 admin handler 文件 + 1 个 admin auth 中间件 + 路由注册 |
| Frontend | 新增 6 个管理页面组件 + AdminMenu + AdminPasswordDialog + admin API 函数 + Zustand store |
| AgentEnd | 无影响 |
