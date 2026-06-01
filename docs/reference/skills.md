# Skills 配置说明

本项目涉及两类 Skills：**Agent 运行时内置 Skills**（AgentEnd 提供）和 **Claude Code 开发 Skills**（开发时使用）。

## 一、Agent 运行时 Skills

### 架构

Skills 通过 `SkillProvisioner` 在 workspace 创建时注入到 Agent 的工作目录中。

```
WorkspaceManager.create()
  → SkillProvisioner.provision(worktree_path, agent_type)
    → 复制 builtin skills 到 .claude/skills/ 或 .opencode/skills/
  → SkillProvisioner.init_shared_dirs(worktrees_root, task_id, session_id)
```

配置位于 `agentend/config.yaml`：

```yaml
skills:
  builtin_dir: "src/skills/builtin"
  manifest:
    taskctl:
      file:
        - SKILL.md
        - taskctl
```

### 内置 Skill：taskctl

多 Agent 协作的上下文与状态管理工具（Go 编译二进制）。

**核心能力：**
- 任务级配置和内存管理
- 共享内存（common）和私有内存（sub）隔离
- 基于 Git 分支的多 Agent 协作
- Agent 分支 → 任务分支的安全合并

**主要命令：**

| 命令 | 说明 |
|------|------|
| `help` | 显示可用命令 |
| `ls` | 列出共享目录结构 |
| `summary` | 显示任务概览 |
| `common-memory [file]` | 读写共享内存 |
| `sub-memory [file]` | 读写私有内存 |
| `write-sub-memory <file> [content...]` | 写入私有内存 |
| `merge` | 将 Agent 分支合并到任务分支 |

**规则集成：** `TaskctlRule`（`src/rules/builtin.py`）在检测到 Agent 消息中的合并关键词时，自动注入使用 `taskctl merge` 的指令。

## 二、Claude Code 开发 Skills

以下 Skills 安装在项目 `.claude/skills/` 目录，供开发时使用。

### OpenSpec 系列（SDD 编程工作流）

| Skill | 说明 |
|-------|------|
| openspec-propose | 快速创建变更提案（设计 + 规格 + 任务一步生成） |
| openspec-explore | 探索模式 — 思考伙伴，用于理清需求和问题 |
| openspec-apply-change | 按任务列表实施变更 |
| openspec-verify-change | 验证实现是否匹配变更规格 |
| openspec-archive-change | 归档已完成的变更 |

来源：OpenSpec 社区包

### 项目维护 Skills

| Skill | 说明 |
|-------|------|
| agentsmd-linter | 全项目 AGENTS.md 同步精炼 — 扫描全部 5 个 AGENTS.md + 三端 details.md，交叉验证后直接修改 |
| doc-linter | 全项目文档同步精炼 — 读取全部 AGENTS.md 建立上下文，全量扫描根 docs/ + 三端 docs/，交叉验证代码引用后直接修改文档 |

### UI/UX 设计 Skills

来源：[taste-skill](https://github.com/Leonxlnx/taste-skill)

| Skill | 说明 |
|-------|------|
| design-taste-frontend | 全能高级前端 — 产出高质量前端代码，不限单一视觉风格 |
| redesign-existing-projects | 项目重构优化 — 审查并修复现有 UI 的布局、间距、层级 |
| high-end-visual-design | 高端柔和视觉 — 柔和对比度、留白、高级字体、弹性动效 |
| minimalist-ui | 极简编辑风 — Notion/Linear 风格，克制用色，结构清晰 |
| full-output-enforcement | 防偷懒强制输出 — 禁止占位符注释，强制完整代码输出 |

来源：[ui-ux-pro-max-skill](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill)

| Skill | 说明 |
|-------|------|
| ui-ux-pro-max | 67 种风格、96 个调色板、57 个字体配对，覆盖 React/Vue/Svelte/SwiftUI/Flutter 等 |
