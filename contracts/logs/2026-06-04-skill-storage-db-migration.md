# 2026-06-04-skill-storage-db-migration

## 变更原因

Skills 文件存储从本地文件系统迁移至 MySQL longblob，消除对本地文件系统的依赖，支持水平扩展。

## 变更文件

| 文件 | 变更 |
|---|---|
| `backend/internal/model/skill.go` | `SkillHub` 新增 `Content` 字段 (`json:"-"` 不暴露)；`StoragePath` json tag 改为 `"-"` |
| `backend/internal/service/skill_validator.go` | `ConfirmSkill` / `DeleteSkillFromHub` / `PackSkillDir` 改写为 DB 操作 |
| `backend/internal/handler/skill.go` | `Import` 调用签名变更；`AgentSkill.ImportedAt` 修复零值 bug |

## API 变更

- **`SkillHub.StoragePath` 字段从 JSON 响应中移除**（`json:"storage_path,omitempty"` → `json:"-"`）
  - 该字段原为内部使用，前端未消费，无跨端影响
- **新增 `SkillHub.Content` 字段**（`json:"-"` 不暴露到 API）
- 所有 REST 端点（`/skills`、`/skills/upload`、`/skills/confirm`、`/skills/{name}`、`/skills/{name}/import`）路径、请求、响应格式均不变

## 跨端影响

- **Frontend**: 无影响 — API 契约不变，`storage_path` 字段前端未使用
- **Agentend**: 无影响 — 仍从 Backend 接收 zip bytes，接口不变
- **Contracts schemas**: 无 skill 相关 schema，无需修改

## 不涉及 contracts/schemas

本次变更为 Backend 内部存储层改造，不涉及 `contracts/schemas/*.yaml` 中的跨端契约定义。
