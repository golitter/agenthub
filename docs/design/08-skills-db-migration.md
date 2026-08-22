# 08 — Skills 本地文件存储 → 数据库存储迁移方案

> **状态**: ✅ 已实现
> **日期**: 2026-06-04
> **前置**: [07-skills-hub-external-skills.md](07-skills-hub-external-skills.md)
>
> 迁移已落地：`SkillHub.Content`（`longblob`）字段已存在，external skill 的 ZIP 内容直接存入数据库；`StoragePath` 模型字段与 `HubBasePath` 常量已移除，不再依赖本地 `data/skills/hub/` 目录。
> 该 DB blob 阶段后续已被 [10-skills-minio-storage-migration.md](10-skills-minio-storage-migration.md) 取代：MinIO 成为权威存储，`Content` 降级为迁移期兼容/影子字段；本文按该历史阶段保留。

## 实现了什么

External Skill 从“Backend 本地文件目录 + MySQL 元数据”迁移为“MySQL blob 存 ZIP 内容”。上传校验仍使用临时目录，确认导入时打包 ZIP 写入 `skill_hubs.content`；安装到 Session 时从 DB 读 ZIP 再转发给 AgentEnd。该描述仅反映本文的 DB blob 历史阶段；当前权威存储为 MinIO（见 [10-skills-minio-storage-migration.md](10-skills-minio-storage-migration.md)），`Content` 仅作迁移期兼容字段。

## 怎么实现的

### 数据模型 (`backend/internal/model/skill.go`)

```go
type SkillHub struct {
    Content []byte `gorm:"type:longblob" json:"-"`
}
```

### 确认与安装 (`backend/internal/service/impl/skill_service.go`)

```go
func (svc *SkillService) ConfirmSkill(ctx context.Context, name, _ string, _ int, _ int64, tmpDir string) (*service.SkillImportResult, error)
func (svc *SkillService) ImportSkill(ctx context.Context, skillName, sessionID string) (*service.SkillImportResult, error)
```

`ConfirmSkill` 将已校验临时目录打包后写入 `SkillHub.Content`；`ImportSkill` 通过 `SkillDao.GetSkillContent` 读取 blob 并调用 AgentEnd skills API。注：`ctx context.Context` 是后续 [10-skills-minio-storage-migration.md](10-skills-minio-storage-migration.md) 为支持请求取消在 Service 签名中引入的；本文 DB blob 路径同样沿用该签名。

---

## 1. 背景与动机

迁移前，external skills 的文件（SKILL.md、脚本等）存储在后端本地文件系统 `../data/skills/hub/{name}`，元数据则在 MySQL `skill_hubs` 表中。这个旧方案存在问题：

- **部署不便**: 文件系统与 DB 双写，扩容/迁移时需同步文件
- **一致性风险**: DB 记录与文件可能不一致（写入中途失败、手动删除文件等）
- **无法水平扩展**: 多 Backend 实例无法共享本地文件系统

**目标**: 将 skill 文件内容（zip blob）统一存储到 MySQL `skill_hubs` 表，消除对本地文件系统的依赖。

---

## 2. 现状架构

### 2.1 数据流

```
Upload (zip)                    Confirm                       Import
Frontend ──→ Backend /upload    Frontend ──→ Backend /confirm  Frontend ──→ Backend /import
             │                               │                              │
             ├─ ValidateZip                  ├─ ConfirmSkill()              ├─ SkillDao.GetSkillContent()
             │  解压到 /tmp                   │  PackValidatedSkillDir →      │  从 SkillHub.Content 读 zip
             │  校验 SKILL.md                 │  INSERT SkillHub.Content      │
             │  返回 tmpDir                   │  清理 /tmp                    │
             └─ 返回校验结果                   └─ 清理 /tmp                   └─ 发送 zip 给 Agentend
                                                                           └─ INSERT AgentSkill
```

### 2.2 涉及的关键文件

| 文件 | 职责 |
|---|---|
| `backend/internal/model/skill.go` | `SkillHub` 元数据模型 + `AgentSkill` 关联模型 |
| `backend/internal/service/skill_validator.go` | `ValidateZip` / `InspectValidatedSkillDir` / `PackValidatedSkillDir`，负责校验并重新打包已上传 skill |
| `backend/internal/service/impl/skill_service.go` | `ConfirmSkill` / `DeleteSkill` / `ImportSkill`，通过 DB blob 管理 external skill |
| `backend/internal/dao/gorm/skill_dao.go` | `CreateSkill` / `GetSkillContent` / `DeleteSkillCascade`，封装 SkillHub 与 AgentSkill 持久化 |
| `backend/internal/controller/impl/skill_controller.go` | HTTP controller，委托 service 层 |
| `agentend/src/api/v1/skills.py` | install/remove skill 到 worktree |
| `agentend/src/skills/provisioner.py` | builtin skills 文件复制到 worktree |

### 2.3 当前存储位置

| 数据 | 存储位置 |
|---|---|
| Skill 元数据 | MySQL `skill_hubs` 表 |
| Skill 文件内容 | MySQL `skill_hubs.Content`（external skill zip blob） |
| Session-Skill 关联 | MySQL `agent_skill` 表 |
| Builtin skill 文件 | Agentend 本地 `settings.skills.builtin_dir_resolved` |

---

## 3. 迁移方案

### 3.1 `SkillHub` 模型新增 `Content` 字段

**文件**: `backend/internal/model/skill.go`

```go
type SkillHub struct {
    ID          uint      `gorm:"primarykey" json:"id"`
    Name        string    `gorm:"uniqueIndex;size:128;not null" json:"name"`
    Builtin     bool      `gorm:"not null;default:false" json:"builtin"`
    Description string    `gorm:"type:text" json:"description"`
    FileCount   int       `gorm:"default:0" json:"file_count"`
    TotalSize   int64     `gorm:"default:0" json:"total_size"`
    Content     []byte    `gorm:"type:longblob" json:"-"`         // zip blob，external skill 专用
    UploadedBy  string    `gorm:"size:64" json:"uploaded_by,omitempty"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

变更说明：

- **新增 `Content []byte`**: `type:longblob`，存储整个 zip 包的二进制内容
- **移除 `StoragePath` 模型字段**: 不再暴露或读写本地路径；旧数据库列如仍存在，由后续手工迁移清理
- **`json:"-"`** on `Content`: 避免 API 意外泄露大字段
- **无需新表**: zip 整包直接挂在 `SkillHub` 行上，一个 skill 一行，简单直接

**为什么选 blob 而非拆文件**:

| 维度 | zip blob（本方案） | 拆文件 SkillFile 表 |
|---|---|---|
| 模型复杂度 | 零新增表 | 新增 `SkillFile` 表 + 复合唯一索引 |
| ConfirmSkill | zip tmpDir → 一列写入 | 逐文件读取 → 批量 INSERT N 行 |
| GetSkillContent | 直接返回 blob | 从 DB 查 N 行 → 内存拼 zip |
| 删除 | 删一行，blob 随行消亡 | 先删 N 个 SkillFile 再删 SkillHub |
| 查询单文件 | 需解压（但当前场景不需要） | 可直接 SQL 查 |
| 适用场景 | 当前只用 Import（需完整 zip） | 需要文件级 CRUD |

当前所有消费方（Agentend install、迁移脚本）都只需要完整 zip，不存在查询单文件的需求。

### 3.2 Service 层改造

**文件**: `backend/internal/service/skill_validator.go`

#### 3.2.1 `ConfirmSkill` — zip tmpDir → 写 DB blob

```
迁移前: tmpDir 文件 → copyDir → ../data/skills/hub/{name}
当前流程: tmpDir 文件 → InspectValidatedSkillDir → PackValidatedSkillDir → SkillHub.Content (longblob) → 清理 tmpDir
```

```go
// zipDir 将目录打包为 zip 字节流（从原文件系统打包逻辑提取）
func zipDir(src string) ([]byte, error) {
    var buf bytes.Buffer
    w := zip.NewWriter(&buf)

    err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return fmt.Errorf("walk error at %s: %w", path, err)
        }
        if info.IsDir() {
            return nil
        }
        rel, err := filepath.Rel(src, path)
        if err != nil {
            return fmt.Errorf("relative path error: %w", err)
        }
        rel = filepath.ToSlash(rel) // 统一正斜杠，跨平台一致
        f, err := w.Create(rel)
        if err != nil {
            return err
        }
        in, err := os.Open(path)
        if err != nil {
            return err
        }
        defer in.Close()
        _, err = io.Copy(f, in)
        return err
    })
    if err != nil {
        return nil, err
    }
    if err := w.Close(); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func (svc *SkillService) ConfirmSkill(ctx context.Context, name, _ string, _ int, _ int64, tmpDir string) (*service.SkillImportResult, error) {
    name, err := normalizeSkillName(name)
    if err != nil {
        return nil, err
    }
    metadata, err := service.InspectValidatedSkillDir(name, tmpDir)
    if err != nil {
        return nil, err
    }
    defer os.RemoveAll(tmpDir)

    zipData, err := service.PackValidatedSkillDir(name, tmpDir)
    if err != nil {
        return nil, err
    }

    if err := svc.skillDao.CreateSkill(model.SkillHub{
        Name:        name,
        Builtin:     false,
        Description: metadata.Description,
        FileCount:   metadata.FileCount,
        TotalSize:   metadata.TotalSize,
        Content:     zipData,
    }); err != nil {
        return nil, err
    }
    return &service.SkillImportResult{Success: true, Name: name}, nil
}
```

> **注意**: 单行 INSERT 无需事务 — GORM `Create` 本身是原子的，不存在中间状态。

#### 3.2.2 `DeleteSkill` — 删行即删文件 + 级联清理关联

本文阶段简化逻辑（仅 DB blob，无对象存储状态机）：

```go
func (svc *SkillService) DeleteSkill(ctx context.Context, name string) error {
    skill, err := svc.skillDao.GetSkillByName(name)
    if err != nil {
        return err
    }
    if skill == nil {
        return service.ErrNotFound("skill not found")
    }
    if skill.Builtin {
        return service.ErrForbidden("cannot delete builtin skill")
    }
    importCount, err := svc.skillDao.CountImportsBySkillName(name)
    if err != nil {
        return err
    }
    if importCount > 0 {
        return service.ErrConflict("skill is imported by active sessions; remove it from sessions first")
    }
    return svc.skillDao.DeleteSkillCascade(name)
}
```

> 注：当前 `DeleteSkill` 实现已由 [10-skills-minio-storage-migration.md](10-skills-minio-storage-migration.md) 扩展为 `ready → deleting → 已删除` 状态机，并增加对象删除持久化补偿任务；上面代码片段只反映本文 DB blob 阶段的语义。

#### 3.2.3 `GetSkillContent` — 从 DB 读 blob

```go
func (dao *SkillDao) GetSkillContent(name string) ([]byte, error) {
    var skill model.SkillHub
    if err := db.GetDB().Select("content").Where("name = ?", name).First(&skill).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return nil, nil
        }
        return nil, err
    }
    return skill.Content, nil
}
```

#### 3.2.4 `HubBasePath` 常量

已移除，运行逻辑不再通过本地 hub 目录读取 external skill：

```go
// removed: external skill 内容从 SkillHub.Content 读取
```

### 3.3 Controller 层改造

**文件**: `backend/internal/controller/impl/skill_controller.go`

Controller 层改动极小，大部分接口不变：

| Controller 方法 | 变化 |
|---|---|
| `Upload` | **无变更** — 仍然上传 zip → 校验 → tmpDir |
| `Confirm` | **无变更** — 调用 `service.ConfirmSkill`，内部逻辑变了 |
| `List` | **无变更** — 仍然查 MySQL |
| `Delete` | **无变更** — 调用 `service.DeleteSkill` |
| `Import` | **极小变更** — 从 `SkillHub.Content` 读取 zip blob |
| `ReportBuiltinSkills` | **无变更** — Builtin 不涉及文件存储 |

`Import` 方法改为通过 DAO 读取 zip 内容：

```go
zipData, err := skillDao.GetSkillContent(skillName)
err = agentClient.InstallSkill(agentType, sessionID, skillName, zipData)
```

### 3.4 Builtin Skills 处理策略

**策略: Builtin 保持文件系统，仅 External 存 blob**

理由：
- Builtin skills 在 Agentend 本地目录，由 `SkillProvisioner` 直接 copy 到 worktree，不经过 Backend 文件系统
- Builtin 不需要上传/删除/更新流程
- `SkillHub.Content` 仅在 `Builtin = false` 时有值
- 未来如需 Builtin 也入库，可增量扩展

### 3.5 Agentend 层：无变更

Agentend 从 Backend 接收 zip 包安装到 worktree，这个接口不变。Backend 只是 zip 的来源从「读本地目录并打包」变成「从 DB 直接返回 blob」。

### 3.6 Frontend 层：无变更

API 接口（`/skills`、`/skills/upload`、`/skills/confirm`、`/skills/{name}`、`/skills/{name}/import`）均不变。

---

## 4. 数据迁移

### 4.1 数据库迁移

`SkillHub` 已纳入迁移基线（当前由 [backend/internal/dao/gorm/migrations.go](../../backend/internal/dao/gorm/migrations.go) 的 `RunMigrations` 版本化迁移统一管理，入口在 [backend/cmd/server/main.go](../../backend/cmd/server/main.go)），路由和服务接线由 [backend/internal/app/app.go](../../backend/internal/app/app.go) 组装。新增 `Content` 列由迁移自动添加，无需额外操作。

```go
// migrations.go baseline_backend_schema — GORM 会自动添加新字段
gdb.AutoMigrate(
    &model.Session{}, ..., &model.SkillHub{}, &model.AgentSkill{},
    // 以下三项由 10-skills-minio-storage-migration.md 引入：
    // &model.SkillUploadReceipt{}, &model.SkillOperationJob{}, &model.SkillAuditEvent{},
)
```

### 4.2 手动迁移

测试环境中 external skill 数量极少，无需编写迁移脚本。部署新代码后：

1. **清理旧数据**: 删除 `skill_hubs` 表中 `builtin = false` 的记录，清空 `../data/skills/hub/` 目录
2. **重新上传**: 通过前端 Upload → Confirm 流程重新入库，新流程自动将 zip blob 写入 `Content` 字段

---

## 5. 清理与废弃

| 项目 | 操作 | 时机 |
|---|---|---|
| `SkillHub.StoragePath` 模型字段 | 已移除；旧数据库列如仍存在，由手工迁移清理 | 本次迁移 |
| `HubBasePath` 常量 | 已移除 | 本次迁移 |
| `copyDir` / `copyFile` 辅助函数 | 已无运行调用 | 本次迁移 |
| 原文件系统打包逻辑 | 提取为 `zipDir` 私有函数供 `PackValidatedSkillDir` 使用 | 本次迁移 |
| `../data/skills/hub/` 目录 | 手动迁移完成后归档/删除 | 迁移完成后 |

---

## 6. 改动范围汇总

| 层次 | 文件 | 改动量 | 说明 |
|---|---|---|---|
| Model | `backend/internal/model/skill.go` | 极小 | `SkillHub` 使用 `Content` 字段存储 external skill zip |
| Service | `backend/internal/service/skill_validator.go` + `backend/internal/service/impl/skill_service.go` | 中等 | `ConfirmSkill` / `DeleteSkill` / `ImportSkill` 改为围绕 DB blob 工作，保留 `zipDir` 私有打包逻辑 |
| Controller | `backend/internal/controller/impl/skill_controller.go` | 极小 | Controller 继续委托 service，API 形状不变 |
| Agentend | 无变更 | — | 仍从 Backend 接收 zip |
| Frontend | 无变更 | — | API 接口不变 |

**总改动文件数: 以当前代码为准，核心涉及 model / dao / service / handler 文档与实现。**

---

## 7. 风险与注意事项

| 风险 | 影响 | 缓解措施 |
|---|---|---|
| 大文件占用 DB 空间 | 当前限制 10MB/zip，影响可控 | 监控 DB 磁盘，必要时引入对象存储 |
| DB 备份体积增长 | zip blob 计入 mysqldump | 监控备份大小，调整 `max_allowed_packet`，必要时增量备份 |
| `GetSkillContent` 性能 | 从磁盘读改为 DB 读 | 单行 SELECT 一个 longblob，比原方案遍历目录+打包更快 |
| `agent_skill` 孤儿数据 | 删除 SkillHub 时关联记录残留（现有 bug，本次一并修复） | `DeleteSkillCascade` 事务中级联删除 `agent_skill` |
| 回滚方案 | 迁移后发现问题需要回退 | 清空 `skill_hubs` 中 external 记录 + 重新上传即可 |

---

## 8. 未来扩展

- **对象存储**: MinIO 迁移方案见 [10-skills-minio-storage-migration.md](10-skills-minio-storage-migration.md)，`SkillHub` 将只保留对象键与完整性元数据
- **Builtin 入库**: 如需统一管理 builtin 文件，可扩展 `ReportBuiltinSkills` 上传文件内容
- **版本管理**: `SkillHub` 增加 `version` 字段，或引入 `skill_versions` 表，支持多版本共存
