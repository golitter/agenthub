# SkillsHub — 内置与外置 Skills 架构设计

> SkillsHub 是外置 Skills 的统一仓库，支持上传压缩包、格式校验、确认入库、导入到 Agent、独立移除。
> 内置 Skills 由 Agentend 启动时上报 Backend，写入数据库，不可删除或修改。

---

## 一、核心决策

| # | 决策 | 结论 |
|---|------|------|
| 1 | SkillsHub 存储位置 | **Backend 端**，文件系统 `data/skills/hub/` |
| 2 | 导入方式 | **物理复制**到 Agent 运行时 skills 目录 |
| 3 | 运行中导入 | 不管 Agent 状态，**直接导入** |
| 4 | 删除 hub 源文件 | **不影响已导入副本**，各 Agent session 独立管理 |
| 5 | 已导入 Skill 删除 | 到**对应 Agent 详情页**移除 |
| 6 | 外置 Skill 可导入范围 | **仅 adapter 层**：claude-code / opencode / codex（Orchestrator 是调度层，不直接执行 skill，因此禁止导入） |
| 7 | SkillsHub 管理入口 | **左侧边栏**，通讯录下面，新增 "技能" Tab |
| 8 | 管理粒度 | **Session 级别**（每个 session 独立 worktree，无共享冲突） |
| 9 | Builtin Skills 注册 | Agentend **启动时**将 builtin skills 列表上报 Backend，写入数据库（upsert 幂等，含重试） |
| 10 | External 命名约束 | 不允许与 builtin skill 同名，前端 + 后端双重校验 |
| 11 | Zip 安全限制 | 解压后总大小 ≤ 10MB，文件数 ≤ 100，防 zip bomb |

---

## 二、整体架构

```
                    ┌─────────────────────────────────────────────────┐
                    │                  Frontend                       │
                    │                                                 │
  ┌──────────────┐  │  ┌────────────┐  ┌──────────────┐  ┌────────┐ │
  │ 侧边栏-技能Tab│  │  │ Agent详情页 │  │  聊天页      │  │ 管理页 │ │
  │              │  │  │            │  │              │  │        │ │
  │ 上传zip ─────┼──┼─▶│            │  │              │  │        │ │
  │ 查看列表     │  │  │ 导入skill  │  │              │  │        │ │
  │ 删除skill ◀──┼──┼──│ 移除skill  │  │              │  │        │ │
  │              │  │  │            │  │              │  │        │ │
  │ SkillsHub管理│  │  │ Skills展示 │◀─│ 点击头像进入 │  │        │ │
  └──────────────┘  │  └─────┬──────┘  └──────────────┘  └────────┘ │
                    │        │                          │            │
                    └────────┼──────────────────────────┼────────────┘
                             │                          │
           ┌─────────────────┼──────────────────────────┼────────┐
           │                 ▼                          ▼        │
           │  ┌──────────────────────┐  ┌──────────────────────┐ │
           │  │ SkillsHub CRUD       │  │ Agent Skills 读取    │ │
           │  │                      │  │                      │ │
           │  │ POST   /skills/upload│  │ GET /agents/:sid     │ │
           │  │ POST   /skills/confirm│ │      → skills[]     │ │
           │  │ GET    /skills        │  │                      │ │
           │  │ DELETE /skills/:name  │  │                      │ │
           │  │                      │  │                      │ │
           │  │ POST   /skills/import│  │                      │ │
           │  │ DELETE /skills/remove│  │                      │ │
           │  └──────┬───────────────┘  └──────────┬───────────┘ │
           │         │                             │             │
  B        │    ┌────▼─────┐                 ┌─────▼─────┐       │
  a        │    │data/skills│                 │调用agentend│       │
  c        │    │   /hub/   │                 │ 读skills  │       │
  k        │    │           │                 └─────┬─────┘       │
  e        │    │ ┌my-skill/│                       │             │
  n        │    │ │ SKILL.md│                       │ HTTP        │
  d        │    │ └another/ │                       │             │
           │    └───────────┘                       ▼             │
  (Go)     │                              ┌──────────────────┐   │
           │                              │  Agentend         │   │
           │                              │                   │   │
           │                              │ 启动时:           │   │
           │                              │  上报 builtin      │   │
           │                              │  skills → Backend  │   │
           │                              │                   │   │
           │                              │ 按需:             │   │
           │                              │  GET /api/v1/     │   │
           │                              │   skills/:type    │   │
           │                              │  扫描 skills 目录  │   │
           │                              └──────────────────┘   │
           └─────────────────────────────────────────────────────┘
```

---

## 三、三阶段数据流

### Phase 1: 上传 → 校验 → 确认入库

```
┌────────┐      .zip       ┌──────────┐              ┌──────────┐
│        │ ──────────────▶ │          │              │          │
│ Front- │                 │ Backend  │              │          │
│ end    │                 │          │              │          │
│        │                 │ 1.解压到  │              │          │
│        │                 │   临时目录│              │          │
│        │                 │          │              │          │
│        │ ◀──────────── │ 2.校验    │              │          │
│        │  {valid, files, │   结果   │              │          │
│        │   description}  │          │              │          │
│        │                 │          │              │          │
│        │ ──────────────▶ │          │              │          │
│        │  confirm + name │          │              │          │
│        │                 │ 3.重命名到│              │          │
│        │                 │   hub目录 │              │          │
│        │                 │   写DB   │              │          │
│        │ ◀──────────── │          │              │          │
│        │  success       │          │              │          │
└────────┘                 └──────────┘              └──────────┘
```

**Backend 校验逻辑 (Go)：**

1. 解压到临时目录
2. 遍历 entries:
   - ✅ 拒绝路径含 `..` (path traversal)
   - ✅ 拒绝符号链接指向包外
   - ✅ 拒绝绝对路径
   - ✅ 解压后总大小超过 **10MB** → 拒绝 (zip bomb 防护)
   - ✅ 解压文件数超过 **100** → 拒绝 (资源耗尽防护)
3. 检查根目录是否有 `SKILL.md`
   - ❌ 没有 → 返回 "missing SKILL.md"
4. 解析 `SKILL.md` 的 YAML frontmatter
   - ❌ 没有 frontmatter → 返回 "missing frontmatter"
   - ❌ frontmatter 没有 `name` → 返回 "missing name field"
5. 提取 `description` (可选)
6. 统计文件数、总大小
7. 校验 `name` 不与已有 builtin skill 冲突
   - ❌ 冲突 → 返回 "name conflicts with builtin skill"

**用户确认流程：**

校验通过后，前端弹出确认框，用户填写最终的 Skill 名称，确认后入库。Backend **先写 DB（skill_hub 表），成功后再将文件从临时目录移到** `data/skills/hub/<name>/`。若 DB 写入失败，临时目录文件由定时清理 job 回收，避免产生孤儿文件。

### Phase 2: 导入到 Agent

```
┌────────┐                 ┌──────────┐                 ┌───────────┐
│Frontend│                 │ Backend  │                 │ 文件系统   │
└───┬────┘                 └────┬─────┘                 └─────┬─────┘
    │                           │                             │
    │ POST /api/skills/import   │                             │
    │ { skill_name, session_id} │                             │
    │ ─────────────────────────▶│                             │
    │                           │                             │
    │                           │ 1. 校验 agent_type           │
    │                           │    仅 adapter 层可导入       │
    │                           │                             │
    │                           │ 2. 查找 session → worktree  │
    │                           │                             │
    │                           │ 3. 查找 hub 中的 skill      │
    │                           │    data/skills/hub/<name>/   │
    │                           │                             │
    │                           │ 4. 物理复制                  │
    │                           │ ───────────────────────────▶│
    │                           │    hub/<name>/ →             │
    │                           │    worktree/.claude/skills/  │
    │                           │              <name>/         │
    │                           │                             │
    │                           │ 5. 写入 agent_skill 表       │
    │                           │                             │
    │ ◀─────────────────────────│                             │
    │   success                 │                             │
```

**约束：**
- 只有 `claude-code` / `opencode` / `codex` 可导入（前端 + 后端双重校验）
- `orchestrator` 不允许导入外部 Skill（Orchestrator 是调度层，不直接执行 skill）
- 不管 Agent 当前运行状态，直接复制文件
- **重复导入拦截**：若 `agent_skill` 表已存在 `(session_id, skill_name)` 记录且物理文件存在，前端提前提示「该 Skill 已导入」并阻止重复操作

### Phase 3: 运行时读取（展示 Skills）

```
┌────────┐                 ┌──────────┐                 ┌───────────┐
│Frontend│                 │ Backend  │                 │ Agentend  │
└───┬────┘                 └────┬─────┘                 └─────┬─────┘
    │                           │                             │
    │ GET /api/agents/:sid      │                             │
    │ ─────────────────────────▶│                             │
    │                           │                             │
    │                           │ GET /api/v1/skills/:type    │
    │                           │ ?workspace_path=xxx         │
    │                           │ ───────────────────────────▶│
    │                           │                             │
    │                           │                             │ 扫描目录
    │                           │                             │ worktree/
    │                           │                             │ └.claude/
    │                           │                             │   └skills/
    │                           │                             │     ├render/
    │                           │                             │     │ └SKILL.md
    │                           │                             │     ├taskctl/
    │                           │                             │     │ └SKILL.md
    │                           │                             │     └my-skill/
    │                           │                             │       └SKILL.md
    │                           │                             │
    │                           │ ◀───────────────────────────│
    │                           │ [                           │
    │                           │   {name:"render",builtin:T},│
    │                           │   {name:"taskctl",builtin:T},│
    │                           │   {name:"my-skill",builtin:F│
    │                           │    source:"hub"}            │
    │                           │ ]                           │
    │                           │                             │
    │ ◀─────────────────────────│                             │
    │ AgentDetailResponse       │                             │
    │   .skills = [...]         │                             │
    │                           │                             │
    │ 渲染 SkillCard 列表       │                             │
    │ ✅ render    [builtin]    │                             │
    │ ✅ taskctl   [builtin]    │                             │
    │ 📦 my-skill  [external]   │                             │
```

---

## 四、Builtin Skills 上报

Agentend 启动时，主动将 builtin skills 列表上报给 Backend：

```
Agentend 启动
    │
    ▼
读取 config.yaml → settings.skills.manifest
    │
    ▼
遍历 builtin_dir，解析每个 SKILL.md frontmatter
    │
    ▼
POST /api/internal/builtin-skills
body: [
    { name: "render", description: "...", builtin: true, source: "builtin" },
    { name: "taskctl", description: "...", builtin: true, source: "builtin" },
]
    │
    ▼
Backend → UPSERT skill_hub 表 (builtin=true)
  - 按 name 匹配，存在则更新 description
  - 不存在则插入
    │
    ▼ [失败？]
  指数退避重试 (3次，间隔 2s/4s/8s)
  仍失败则记录错误日志，不阻塞启动
```

**设计选择：** 使用 `skill_hub` 表统一管理，增加 `builtin` 布尔字段区分。

- Builtin 记录：不可删除、不可修改（后端强制）
- External 记录：可删除、可修改
- 上报使用 **UPSERT** 保证幂等，Agentend 重启不会产生重复记录

---

## 五、数据模型

```sql
-- SkillsHub 统一仓库 (builtin + external)
CREATE TABLE skill_hub (
    id           BIGINT PRIMARY KEY AUTO_INCREMENT,
    name         VARCHAR(128) NOT NULL UNIQUE,
    builtin      BOOLEAN NOT NULL DEFAULT FALSE,       -- true=builtin, false=external
    storage_path VARCHAR(512),                         -- 仅 external 有值
    description  TEXT,
    file_count   INT DEFAULT 0,
    total_size   BIGINT DEFAULT 0,
    uploaded_by  VARCHAR(64),                          -- 仅 external 有值
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

-- Session ↔ Skill 关联 (仅 external skills 需要关联)
CREATE TABLE agent_skill (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    session_id  VARCHAR(128) NOT NULL,
    skill_name  VARCHAR(128) NOT NULL,
    agent_type  VARCHAR(32)  NOT NULL,                -- claude-code / opencode / codex
    imported_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_session_skill (session_id, skill_name)
);
```

---

## 六、API 端点

### Backend (Go)

```
┌────────┬──────────────────────────────┬──────────────────────────────────┐
│ Method │ Path                         │ 说明                             │
├────────┼──────────────────────────────┼──────────────────────────────────┤
│        │ === Builtin 上报 (内部) ===  │                                  │
│ POST   │ /api/internal/builtin-skills │ Agentend 启动时上报 builtin 列表  │
│        │                              │                                  │
│        │ === SkillsHub 管理 ===       │                                  │
│ POST   │ /api/skills/upload           │ 上传 zip，返回校验结果            │
│ POST   │ /api/skills/confirm          │ 确认入库 + 用户命名              │
│ GET    │ /api/skills                  │ 列出 hub 中所有 skills           │
│ DELETE │ /api/skills/:name            │ 从 hub 删除 (仅 external)        │
│        │                              │                                  │
│        │ === Agent 级别操作 ===       │                                  │
│ POST   │ /api/skills/:name/import     │ 导入到指定 session               │
│        │  body: { session_id }        │                                  │
│ DELETE │ /api/skills/:name/sessions/  │ 从 session 移除                  │
│        │        :sessionId            │  (路径参数，避免 DELETE + body)  │
│        │                              │                                  │
│        │ === 读取 (改造现有) ===       │                                  │
│ GET    │ /api/agents/:sessionId       │ 返回含 skills 的详情             │
│        │  → 代理到 agentend 读取      │                                  │
└────────┴──────────────────────────────┴──────────────────────────────────┘
```

### Agentend (Python) — 新增

```
┌────────┬───────────────────────────────┬──────────────────────────────────┐
│ Method │ Path                          │ 说明                             │
├────────┼───────────────────────────────┼──────────────────────────────────┤
│ GET    │ /api/v1/skills/:agent_type    │ 扫描 agent skills 目录           │
│        │ ?workspace_path=xxx           │ 返回 [{name, desc, builtin,      │
│        │                               │   source}]                       │
└────────┴───────────────────────────────┴──────────────────────────────────┘
```

Builtin 判定：Agentend 加载自身 `config.yaml` 的 `manifest.keys()`，在 manifest 中的即为 builtin，不需要 Backend 传参。

---

## 七、前端改动

### 7.1 侧边栏

```
┌──────────┐
│  👤 头像  │
│          │
│  💬 聊天  │
│  👥 通讯录│
│  🧩 技能  │  ← 新增 NavTab
│  📊 管理  │
│          │
│  ⚙ 设置  │
│  🔗 Logo │
└──────────┘
```

`NavTab` 类型扩展：`'chat' | 'contacts' | 'skills' | 'admin' | 'settings'`

### 7.2 SkillsHub 管理页

```
┌──────────────────────────────────────────────────────────┐
│  🧩 Skills 技能库                                        │
│                                                          │
│  ┌──────────────────────────────────────────────────┐    │
│  │  [+ 上传 Skill]               🔍 搜索...         │    │
│  └──────────────────────────────────────────────────┘    │
│                                                          │
│  ┌──────────────────────────────────────────────────┐    │
│  │  render                        [builtin]         │    │
│  │  渲染工具，生成 HTML/图片/附件卡片                   │    │
│  └──────────────────────────────────────────────────┘    │
│                                                          │
│  ┌──────────────────────────────────────────────────┐    │
│  │  taskctl                        [builtin]         │    │
│  │  任务控制，自动提交与合并管理                        │    │
│  └──────────────────────────────────────────────────┘    │
│                                                          │
│  ┌──────────────────────────────────────────────────┐    │
│  │  my-custom-skill               [external]        │    │
│  │  自定义技能描述                                     │    │
│  │  ────────────────────────────────────────         │    │
│  │  已被 1 个 Agent 导入                [删除]        │    │
│  └──────────────────────────────────────────────────┘    │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

- Builtin 条目：只读，无删除按钮，不显示导入数量（builtin 对所有 Agent 默认可用）
- External 条目：有删除按钮（仅删除 hub 中的源文件，不影响已导入副本）
- 「已被 N 个 Agent 导入」：仅 External 条目显示，通过 `agent_skill` 表 `COUNT` 查询获取，数据量大时考虑在 `skill_hub` 表增加 `import_count` 冗余字段，导入/移除时同步更新

### 7.3 Agent 详情页 Skills 区域改造

```
┌──────────────────────────────────────────────────────────┐
│  Skills                                                  │
│                                                          │
│  ✅ render                          [builtin]           │
│  渲染工具，生成 HTML/图片/附件卡片                          │
│                                                          │
│  ✅ taskctl                         [builtin]           │
│  任务控制，自动提交与合并管理                                │
│                                                          │
│  📦 my-custom-skill                 [external]  [移除]  │
│  自定义技能描述                                           │
│                                                          │
│  ┌──────────────────────────────────────────────────┐    │
│  │  [+ 导入外部 Skill]  ← 仅 adapter agents 可见     │    │
│  └──────────────────────────────────────────────────┘    │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

- `[移除]` 按钮：仅 external skills 显示，删除 worktree 中的物理文件 + `agent_skill` 表记录
- `[+ 导入外部 Skill]`：弹出选择框，列出 SkillsHub 中可导入的 skills
- Orchestrator 类型的 Agent：整个导入区域隐藏

### 7.4 SkillCard 组件改造

现有 `SkillCard` 增加 external 标签样式：

```tsx
// builtin 标签 (现有)
<span className="bg-success/8 text-success">builtin</span>

// external 标签 (新增)
<span className="bg-brand/8 text-brand">external</span>
```

---

## 八、文件系统结构

### Hub 存储 (Backend)

```
data/skills/hub/
├── my-custom-skill/
│   ├── SKILL.md
│   └── scripts/
│       └── do_something.sh
└── another-skill/
    └── SKILL.md
```

### Agent 运行时 (每个 session 独立 worktree)

```
repo_parent/worktrees/
├── task_abc/
│   ├── session_xxx/                          ← Claude Code
│   │   └── .claude/skills/
│   │       ├── render/          (builtin)
│   │       ├── taskctl/         (builtin)
│   │       └── my-custom-skill/ (external, 从 hub 导入)
│   │
│   └── session_yyy/                          ← OpenCode
│       └── .opencode/skills/
│           ├── render/
│           ├── taskctl/
│           └── my-custom-skill/ (独立副本)
│
└── task_def/
    └── session_zzz/
        └── .claude/skills/
            ├── render/
            └── taskctl/         (未导入外部 skill)
```

---

## 九、改动文件清单

### Backend (Go)

**新增：**
- `internal/handler/skill.go` — SkillsHub CRUD + Import/Remove
- `internal/model/skill.go` — `SkillHub` + `AgentSkillRelation` DB model
- `internal/service/skill_validator.go` — 解压、格式校验、路径安全检查
- `pkg/agentend_client/skill_client.go` — 调用 Agentend 读取 skills 的 HTTP client
- `data/skills/hub/` — 文件存储目录

**改造：**
- `internal/handler/agent_profile.go` — `GetDetail` 改为调 Agentend 读取实际 skills
- 路由注册 — 新增 skills 相关路由

### Agentend (Python)

**新增：**
- `api/v1/skills.py` — `GET /api/v1/skills/:agent_type` 路由
- 启动时上报 builtin skills 到 Backend 的逻辑

**改造：**
- `app/main.py` — 注册新路由 + 启动钩子

### Frontend (React)

**新增：**
- `pages/SkillsHubPage.tsx` — SkillsHub 管理页面
- `components/SkillUploadDialog.tsx` — 上传确认对话框
- `components/SkillImportDialog.tsx` — 导入选择对话框

**改造：**
- `components/layout/IconSidebar.tsx` — 新增 "技能" NavItem
- `stores/navigation-store.ts` — `NavTab` 增加 `'skills'`
- `pages/AgentProfilePage.tsx` — Skills 区域增加导入/移除功能
- `components/chat/SkillCard.tsx` — 增加 external 标签样式
- `lib/api.ts` — 新增 skills 相关 API 函数
- `pages/ImPage.tsx` — 路由新增 skills tab

---

## 十、外置 Skill 压缩包格式规范

合法的外置 Skill 包结构：

```
my-skill.zip
├── SKILL.md              ← 必须，入口文件
├── scripts/              ← 可选，可执行脚本
│   └── do_something.sh
├── references/           ← 可选，L3 资源文件
│   └── api.md
└── ...                   ← 其他资源
```

**SKILL.md 格式要求：**

```markdown
---
name: my-skill-name
description: 一句话描述这个 Skill 的能力
---

# My Skill

详细的 Skill 指令内容...
```

**校验规则：**

| 规则 | 说明 |
|------|------|
| 根目录必须有 `SKILL.md` | 缺少则拒绝 |
| `SKILL.md` 必须有 YAML frontmatter | `---` 包裹的元数据块 |
| frontmatter 必须包含 `name` 字段 | 作为 Skill 标识 |
| `name` 不可与 builtin skill 冲突 | 前端 + 后端双重校验 |
| 不允许路径穿越 (`..`) | 安全检查 |
| 不允许符号链接指向包外 | 安全检查 |
| 不允许绝对路径 | 安全检查 |
| 解压后总大小 ≤ **10MB** | zip bomb 防护 |
| 解压文件数 ≤ **100** | 资源耗尽防护 |
| `description` 字段可选 | 缺失时用空字符串 |
