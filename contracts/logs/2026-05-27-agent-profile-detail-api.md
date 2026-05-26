# 2026-05-27 — Agent Profile & Detail API（Mock 阶段）

## 变更原因

新增 Agent Hover Card 和详情页功能，后端需要提供 `GET /api/sessions/:sid/profile` 和 `GET /api/sessions/:sid/detail` 接口，前端需要对应的类型定义。

## 变更文件

**无 schema 文件变更。**

新增的 API 类型（AgentProfile、AgentDetail、AgentSkill）直接在后端 handler 和前端 `lib/api.ts` 中定义，不走 `contracts/schemas/` 生成流程。

理由：Skills 数据当前为 mock 硬编码，后续大修时会重新定义完整的数据模型和契约。当前阶段提前写入 schema 会增加后续重构成本。

## 跨端影响

| 端 | 影响 |
|------|------|
| Backend | 新增 `internal/handler/agent_profile.go`，定义 `AgentSkill`、`AgentProfileResponse`、`AgentDetailResponse` 结构体 |
| Frontend | 新增 `lib/api.ts` 中的 `AgentSkill`、`AgentProfile`、`AgentDetail` TypeScript 接口 |
| AgentEnd | 无影响（Skills 上报后续大修） |

## 接口定义

### GET /api/sessions/:sid/profile

```json
{
  "agent_name": "string",
  "agent_type": "string",
  "avatar_url": "string?",
  "status": "string",
  "session_id": "string",
  "skills": [
    { "name": "string", "description": "string", "builtin": true, "source": "string" }
  ]
}
```

### GET /api/sessions/:sid/detail

包含 profile 全部字段 + `task_id`、`workspace_path`、`created_at`、`message_count`。

## 后续计划

Skills 数据模型和上报接口定型后，将新增 `agent-skill.yaml` 到 `contracts/schemas/`，运行 `make generate` 统一生成三端类型。
