# 08 — Skills 本地文件存储 → 数据库存储迁移方案

> **状态**: 草案
> **日期**: 2026-06-04
> **前置**: [07-skills-hub-external-skills.md](07-skills-hub-external-skills.md)

---

## 1. 背景与动机

当前 external skills 的文件（SKILL.md、脚本等）存储在后端本地文件系统 `../data/skills/hub/{name}`，元数据则在 MySQL `skill_hubs` 表中。存在问题：

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
             ├─ ValidateZip                  ├─ ConfirmSkill()              ├─ PackSkillDir()
             │  解压到 /tmp                   │  copyDir →                   │  从本地目录读文件
             │  校验 SKILL.md                 │    ../data/skills/hub/{name} │  打包为 zip
             │  返回 tmpDir                   │  INSERT SkillHub             │
             └─ 返回校验结果                   └─ 清理 /tmp                   └─ 发送 zip 给 Agentend
                                                                           └─ INSERT AgentSkill
```

### 2.2 涉及的关键文件

| 文件 | 职责 |
|---|---|
| `backend/internal/model/skill.go` | `SkillHub` 元数据模型 + `AgentSkill` 关联模型 |
| `backend/internal/service/skill_validator.go` | `ValidateZip` / `ConfirmSkill` / `DeleteSkillFromHub` / `PackSkillDir`，操作文件系统 |
| `backend/internal/controller/impl/skill_controller.go` | HTTP controller，委托 service 层 |
| `agentend/src/api/v1/skills.py` | install/remove skill 到 worktree |
| `agentend/src/skills/provisioner.py` | builtin skills 文件复制到 worktree |

### 2.3 当前存储位置

| 数据 | 存储位置 |
|---|---|
| Skill 元数据 | MySQL `skill_hubs` 表 |
| Skill 文件内容 | 本地文件系统 `../data/skills/hub/{name}/` |
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
    StoragePath string    `gorm:"size:512" json:"-"`              // Deprecated: 迁移后清空
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
- **`StoragePath` 改为 `json:"-"`**: 不再暴露给前端，迁移后清空该列
- **`json:"-"`** on `Content`: 避免 API 意外泄露大字段
- **无需新表**: zip 整包直接挂在 `SkillHub` 行上，一个 skill 一行，简单直接

**为什么选 blob 而非拆文件**:

| 维度 | zip blob（本方案） | 拆文件 SkillFile 表 |
|---|---|---|
| 模型复杂度 | 零新增表 | 新增 `SkillFile` 表 + 复合唯一索引 |
| ConfirmSkill | zip tmpDir → 一列写入 | 逐文件读取 → 批量 INSERT N 行 |
| PackSkillDir | 直接返回 blob | 从 DB 查 N 行 → 内存拼 zip |
| 删除 | 删一行，blob 随行消亡 | 先删 N 个 SkillFile 再删 SkillHub |
| 查询单文件 | 需解压（但当前场景不需要） | 可直接 SQL 查 |
| 适用场景 | 当前只用 Import（需完整 zip） | 需要文件级 CRUD |

当前所有消费方（Agentend install、迁移脚本）都只需要完整 zip，不存在查询单文件的需求。

### 3.2 Service 层改造

**文件**: `backend/internal/service/skill_validator.go`

#### 3.2.1 `ConfirmSkill` — zip tmpDir → 写 DB blob

```
当前流程: tmpDir 文件 → copyDir → ../data/skills/hub/{name}
迁移流程: tmpDir 文件 → zipDir → SkillHub.Content (longblob) → 清理 tmpDir
```

```go
// zipDir 将目录打包为 zip 字节流（从原 PackSkillDir 文件系统逻辑提取）
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

func ConfirmSkill(name string, description string, fileCount int, totalSize int64, tmpDir string) error {
    // 1. 确定源目录（zip 可能有或没有外层目录）
    srcDir := filepath.Join(tmpDir, name)
    if info, err := os.Stat(srcDir); err != nil || !info.IsDir() {
        srcDir = tmpDir
    }

    // 2. 将已校验的文件重新打包为 zip
    zipData, err := zipDir(srcDir)
    if err != nil {
        return fmt.Errorf("pack skill files: %w", err)
    }

    // 3. 写入 DB（单行含元数据 + zip blob）
    skill := model.SkillHub{
        Name:        name,
        Builtin:     false,
        Description: description,
        FileCount:   fileCount,
        TotalSize:   totalSize,
        Content:     zipData,
    }
    if err := db.GetDB().Create(&skill).Error; err != nil {
        return fmt.Errorf("db write failed: %w", err)
    }

    // 4. 清理临时目录
    os.RemoveAll(tmpDir)
    return nil
}
```

> **注意**: 单行 INSERT 无需事务 — GORM `Create` 本身是原子的，不存在中间状态。

#### 3.2.2 `DeleteSkillFromHub` — 删行即删文件 + 级联清理关联

```go
func DeleteSkillFromHub(name string) error {
    var skill model.SkillHub
    if err := db.GetDB().Where("name = ?", name).First(&skill).Error; err != nil {
        return fmt.Errorf("skill not found")
    }
    if skill.Builtin {
        return fmt.Errorf("cannot delete builtin skill")
    }

    // 事务：级联删除 agent_skill 关联 + 删除 SkillHub（含 Content blob）
    return db.GetDB().Transaction(func(tx *gorm.DB) error {
        // 修复现有 bug：原代码未清理 agent_skill 孤儿记录
        if err := tx.Where("skill_name = ?", name).Delete(&model.AgentSkill{}).Error; err != nil {
            return fmt.Errorf("delete agent skill associations: %w", err)
        }
        if err := tx.Delete(&skill).Error; err != nil {
            return fmt.Errorf("delete skill hub: %w", err)
        }
        return nil
    })
}
```

#### 3.2.3 `PackSkillDir` — 从 DB 读 blob，签名变更

```go
// PackSkillDir 从 DB 读取 skill 的 zip blob
// 签名变更: (src string) → (skillName string)
func PackSkillDir(skillName string) ([]byte, error) {
    var skill model.SkillHub
    if err := db.GetDB().Select("content").Where("name = ?", skillName).First(&skill).Error; err != nil {
        return nil, fmt.Errorf("skill not found: %w", err)
    }
    if len(skill.Content) == 0 {
        return nil, fmt.Errorf("no zip data for skill %s", skillName)
    }
    return skill.Content, nil
}
```

> **注意**: `PackSkillDir` 签名从 `(src string)` 改为 `(skillName string)`，调用方 [skill_controller.go](../backend/internal/controller/impl/skill_controller.go) 需同步更新。

#### 3.2.4 `HubBasePath` 常量

保留但标记 deprecated，仅供迁移脚本使用：

```go
// Deprecated: 仅用于迁移脚本，迁移完成后移除
const HubBasePath = "../data/skills/hub"
```

### 3.3 Handler 层改造

**文件**: `backend/internal/controller/impl/skill_controller.go`

Handler 层改动极小，大部分接口不变：

| Handler | 变化 |
|---|---|
| `Upload` | **无变更** — 仍然上传 zip → 校验 → tmpDir |
| `Confirm` | **无变更** — 调用 `service.ConfirmSkill`，内部逻辑变了 |
| `List` | **无变更** — 仍然查 MySQL |
| `Delete` | **无变更** — 调用 `service.DeleteSkillFromHub` |
| `Import` | **极小变更** — `service.PackSkillDir(skillName)` 签名变了 |
| `ReportBuiltinSkills` | **无变更** — Builtin 不涉及文件存储 |

`Import` 方法需要调整一行（[skill_controller.go](../backend/internal/controller/impl/skill_controller.go)）：

```go
// Before
srcPath := filepath.Join(service.HubBasePath, skillName)
zipData, err := service.PackSkillDir(srcPath)

// After
zipData, err := service.PackSkillDir(skillName)
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

### 4.1 GORM AutoMigrate

`SkillHub` 已在 AutoMigrate 列表中（[cmd/server/main.go:38](../backend/cmd/server/main.go)），新增 `Content` 列由 GORM 自动添加，无需额外操作。

```go
// 现有代码，无需修改 — GORM 会自动添加新字段
db.GetDB().AutoMigrate(&model.Session{}, ..., &model.SkillHub{}, &model.AgentSkill{})
```

### 4.2 手动迁移

测试环境中 external skill 数量极少，无需编写迁移脚本。部署新代码后：

1. **清理旧数据**: 删除 `skill_hubs` 表中 `builtin = false` 的记录，清空 `../data/skills/hub/` 目录
2. **重新上传**: 通过前端 Upload → Confirm 流程重新入库，新流程自动将 zip blob 写入 `Content` 字段

---

## 5. 清理与废弃

| 项目 | 操作 | 时机 |
|---|---|---|
| `SkillHub.StoragePath` 字段 | 标记 `deprecated`，后续版本移除 | 本次迁移 |
| `HubBasePath` 常量 | 标记 `deprecated`，确认无调用方后移除 | 本次迁移 |
| `copyDir` / `copyFile` 辅助函数 | 确认无其他调用方后移除 | 本次迁移 |
| 原 `PackSkillDir` 文件系统逻辑 | 提取为 `zipDir` 私有函数供 ConfirmSkill 使用 | 本次迁移 |
| `../data/skills/hub/` 目录 | 手动迁移完成后归档/删除 | 迁移完成后 |

---

## 6. 改动范围汇总

| 层次 | 文件 | 改动量 | 说明 |
|---|---|---|---|
| Model | `backend/internal/model/skill.go` | 极小 | `SkillHub` 新增 `Content` 字段，`StoragePath` 标 deprecated |
| Service | `backend/internal/service/skill_validator.go` | 中等 | `ConfirmSkill` / `DeleteSkillFromHub` / `PackSkillDir` 改写 + 新增 `zipDir` |
| Handler | `backend/internal/controller/impl/skill_controller.go` | 极小 | `Import` 中 `PackSkillDir` 调用签名变更 |
| Agentend | 无变更 | — | 仍从 Backend 接收 zip |
| Frontend | 无变更 | — | API 接口不变 |

**总改动文件数: 3 个**

---

## 7. 风险与注意事项

| 风险 | 影响 | 缓解措施 |
|---|---|---|
| 大文件占用 DB 空间 | 当前限制 10MB/zip，影响可控 | 监控 DB 磁盘，必要时引入对象存储 |
| DB 备份体积增长 | zip blob 计入 mysqldump | 监控备份大小，调整 `max_allowed_packet`，必要时增量备份 |
| `PackSkillDir` 性能 | 从磁盘读改为 DB 读 | 单行 SELECT 一个 longblob，比原方案遍历目录+打包更快 |
| `agent_skill` 孤儿数据 | 删除 SkillHub 时关联记录残留（现有 bug，本次一并修复） | `DeleteSkillFromHub` 事务中级联删除 `agent_skill` |
| 回滚方案 | 迁移后发现问题需要回退 | 清空 `skill_hubs` 中 external 记录 + 重新上传即可 |

---

## 8. 未来扩展

- **对象存储**: 如 skill 文件增大或数量增多，可将 `Content` 迁移到 S3 / 七牛云，`SkillHub` 只存 URL
- **Builtin 入库**: 如需统一管理 builtin 文件，可扩展 `ReportBuiltinSkills` 上传文件内容
- **版本管理**: `SkillHub` 增加 `version` 字段，或引入 `skill_versions` 表，支持多版本共存
