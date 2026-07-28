# SkillsHub 外置 Skills — 初版设计记录

## 实现了什么

本文保留 SkillsHub external skill 的初版设计背景。当前实现已经迁移到 MySQL `skill_hubs.Content` DB blob 存储，不再以本地 `data/skills/hub/` 文件系统目录作为权威来源。

当前权威文档：

- `docs/design/08-skills-db-migration.md`
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
| ZIP 文件保存在 `data/skills/hub/` | ZIP 内容存储在 MySQL `skill_hubs.Content` |
| `storage_path` 记录本地路径 | DB blob 是权威内容来源 |
| 物理复制到工作区 | Backend 读取 DB 内容并交给 AgentEnd 安装 |
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
3. 存储策略变化更新 `docs/design/08-skills-db-migration.md`。
4. AgentEnd skill 安装、taskctl/render 行为更新到 `agentend/docs/design/19-skills-taskctl.md`。
