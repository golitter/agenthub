# SkillsHub 外置 Skills — 初版设计记录

## 实现了什么

本文保留 SkillsHub external skill 的初版设计背景。存储已从本地 `data/skills/hub/` 目录先迁移到 MySQL `skill_hubs.Content` DB blob（见 `docs/design/08-skills-db-migration.md`），再进一步迁移到 MinIO 对象存储（见 `docs/design/10-skills-minio-storage-migration.md`，核心实现完成）。当前 `SkillHub.Content` 仅作迁移期兼容字段，新上传 External Skill 的权威内容存放在 MinIO 私有 Bucket，MySQL 只保存元数据、对象键和完整性信息。

当前权威文档：

- `docs/design/10-skills-minio-storage-migration.md`（MinIO 迁移，当前权威存储策略）
- `docs/design/08-skills-db-migration.md`（DB blob 迁移历史）
- `backend/docs/design/01-models.md`
- `backend/docs/design/02-handlers.md`
- `agentend/docs/design/19-skills-taskctl.md`

## 怎么实现的

### 初版目标

初版设计想解决的问题：

| 问题 | 目标 |
|------|------|
| 外置 Skill 需要统一上传入口 | 由 Backend 管理 SkillsHub 元数据 |
| Session 需要按需导入 Skill | 将选中的 Skill 注入目标 Agent 工作区 |
| 前端需要展示技能市场 | SkillsHub 页面展示可导入 Skill |
| AgentEnd 需要获得文件内容 | Backend 调 AgentEnd 安装/移除到 worktree |

### 当前实现差异

| 初版设计 | 当前实现 |
|----------|----------|
| ZIP 文件保存在 `data/skills/hub/` | ZIP 内容存入 MinIO 私有 Bucket（`skill-packages/skills/{name}/{sha256}.zip`） |
| `storage_path` 记录本地路径 | `SkillHub.ObjectKey` 记录 MinIO 对象键，`Content` 降级为迁移期兼容字段 |
| 物理复制到工作区 | Backend 从 MinIO 读取并交给 AgentEnd 安装 |
| 旧式 `/skills/import`、`/skills/remove` 草案 | 以 `backend/docs/design/02-handlers.md` 中 SkillController API 为准 |

### 当前边界

| 端 | 职责 |
|----|------|
| Frontend | SkillsHub 页面展示、上传、导入、移除 |
| Backend | ZIP 校验、元数据与 blob 存储、Session-Skill 关系、AgentEnd 调用 |
| AgentEnd | 将 Skill 内容安装到指定 Agent 工作区，提供内置 skill CLI |
| Contracts | 仅跨端协议字段需要进入 `contracts/schemas/` |

### 维护规则

1. 新实现细节不要继续写入本文。
2. SkillsHub 的当前模型和 API 更新到 `backend/docs/design/01-models.md` / `02-handlers.md`。
3. 存储策略变化更新 `docs/design/10-skills-minio-storage-migration.md`（MinIO 权威）；`docs/design/08-skills-db-migration.md` 仅作 DB blob 阶段的历史记录。
4. AgentEnd skill 安装、taskctl/render 行为更新到 `agentend/docs/design/19-skills-taskctl.md`。
