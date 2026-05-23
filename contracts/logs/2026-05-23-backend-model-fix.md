## 变更原因

Backend 数据模型与 AgentEnd 概念不匹配：Session 被当作顶层容器、Task 被当作一次性消息，实际上 Task 才是群组任务（绑定 repo_path），Session 是 Agent 会话（session_id 由调用方传入）。

## 变更文件

无。`contracts/schemas/` 下的契约定义**未修改**。

## 对比结果

| Schema | 是否需要修改 | 原因 |
|--------|-------------|------|
| `agent-request.yaml` | 不需要 | AgentRequest 字段（task_id, session_id, message, agent_type）不变 |
| `session-state.yaml` | 不需要 | 会话状态机（idle/running/completed/interrupted/error）不变 |
| `agent-response.yaml` | 不需要 | 响应格式不变 |
| `event-types.yaml` | 不需要 | SSE 事件类型不变 |

## 跨端影响

| 端 | 影响 | 说明 |
|----|------|------|
| Backend | **内部变更** | Task 变顶层实体，Session 从属 Task；API 从 `/api/sessions` 改为 `/api/tasks` |
| AgentEnd | 无影响 | Backend 仍以相同 AgentRequest 格式调用 AgentEnd |
| Frontend | Phase 2 适配时更新 | API 路径从 `/api/sessions` 改为 `/api/tasks`，`session_id` 改为前端传入 |

## 契约变更

无。此次为 Backend 内部重构，跨端协议不变。
