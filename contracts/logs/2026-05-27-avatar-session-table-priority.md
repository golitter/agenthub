# 2026-05-27 — Avatar 写入表优先级修复

## 变更原因

`PUT /api/sessions/:sid` 更新头像时写入 `session_agents` 表，但 `GET /api/sessions/:sid/detail` 从 `sessions` 表读取 `avatar_url`，导致头像上传后不生效。同时 `GET /api/tasks/:id` 的会话列表也从 `session_agents` 表读取，存在同样的数据不一致。

## 变更文件

**无 schema 文件变更。**

仅修改后端 handler 内部逻辑：

- `backend/internal/handler/avatar.go` — `UpdateSession` 改为写 `sessions` 表
- `backend/internal/handler/task.go` — `GetTask` 的 avatar 读取优先 `sessions` 表，fallback `session_agents` 表

## 对比结果

API 接口入参和返回结构无变化，仅内部数据写入/读取的表优先级调整。

## 跨端影响

| 端 | 影响 |
|------|------|
| Backend | `UpdateSession` 写 `sessions` 表而非 `session_agents` 表；`GetTask` avatar 读取优先 `sessions` 表 |
| Frontend | 无影响（API 接口不变） |
| AgentEnd | 无影响 |
