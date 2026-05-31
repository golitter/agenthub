# Phase 7: 演示打磨 + 交付物

> 目标: 对照任务要求整理交付物，打磨 Demo 体验
> 预估: 2 天
> 前置: Phase 5 完成（Phase 6 可选）
> 状态: 📋 待开始
> 备注: 大部分交付物已具备基础

## 交付物清单

### 1. 产品设计文档

- [x] `docs/design/` 下的架构设计文档（三层架构设计、Planning Context Module、SOUL.md 身份文档）
- [x] 产品功能说明（IM 交互流程、群聊模式、产物预览）
- [x] `docs/AGENTS.md` 索引
- [ ] 补充完整的产品功能说明书

### 2. 技术文档

- [x] 三层架构说明（[docs/design/three-tier-design.md](../../design/three-tier-design.md)）
- [x] 契约层使用指南（[docs/guides/contract-layer.md](../../guides/contract-layer.md)）
- [x] SSE 流式通信方案（Backend RuntimeHub + StreamWriter）
- [x] Orchestrator 设计文档（[phase5-orchestrator.md](phase5-orchestrator.md)）
- [x] Git 规范（[docs/guides/git-conventions.md](../../guides/git-conventions.md)）
- [x] Makefile 指南（[docs/guides/makefile-guide.md](../../guides/makefile-guide.md)）
- [x] 环境搭建（[docs/guides/setup.md](../../guides/setup.md)）
- [ ] 补充 API 参考文档

### 3. 可运行 Demo

- [x] 三端一键启动（`make all`）
- [x] 4 种 Agent 类型（claude-code / opencode / orchestrator / codex）
- [x] 多 Agent 群聊演示（Orchestrator 自动编排）
- [x] Admin 管理面板（7 模块）
- [ ] 预置 Demo 数据脚本
- [ ] Demo 场景脚本（预设对话流）

### 4. AI 协作开发记录

- [x] CLAUDE.md / AGENTS.md（项目根目录 + 子项目目录）
- [x] Skills 配置说明（[docs/reference/skills.md](../../reference/skills.md)）
- [x] Git commit 历史（体现 AI 协作过程）
- [x] Bugfix 记录（[docs/testing/bugfix/](../../testing/bugfix/) 下 4 篇）
- [x] 代码审计报告（[docs/testing/code-audit-report.md](../../testing/code-audit-report.md)）

### 5. 3 分钟 Demo 视频

- [ ] 编写 Demo 演示脚本（场景 + 时间分配）
- [ ] 录制桌面操作 + 旁白
- [ ] 视频剪辑（字幕、关键操作标注）

## UI 打磨项

### 已完成

- [x] 统一颜色变量（主色、背景、边框、文字）
- [x] Agent 专属颜色（Claude: orange / OpenCode: green / Orchestrator: yellow / Codex: indigo）
- [x] 暗/亮模式支持（Tailwind dark mode + shadcn/ui）
- [x] Admin 密码认证 + JWT
- [x] 错误边界处理

### 待完善

- [ ] 主聊天布局适配 1280px / 1024px / 768px
- [ ] Agent 断连 → 显示重连提示 + 自动重试
- [ ] Agent 超时 → 显示超时状态 + 手动重试按钮
- [ ] 网络错误 → Toast 提示 + 不丢失已输入内容
- [ ] 空消息 / 空会话 → 合适的空状态引导

## Demo 场景脚本

### 场景 1: 单聊代码生成（30s）

1. 打开 AgentHub
2. 新建会话，选 Claude Code
3. 输入 "写一个 React Button 组件"
4. 展示流式回复 + 代码高亮 + DiffCard

### 场景 2: 多会话并行（30s）

1. 切换到另一个会话
2. 选 OpenCode
3. 输入 "审查刚才的 Button 组件"
4. 展示会话切换 + 历史保留

### 场景 3: Orchestrator 群聊（60s）

1. 新建群聊会话，选 Claude Code + OpenCode
2. Orchestrator 自动注入
3. 输入 "用 Claude Code 写一个登录页，OpenCode 审查"
4. 展示 PlanCard → Agent 依次执行（CoordChannel）→ FinalSummaryCard

### 场景 4: 产物预览 + 管理（30s）

1. 展示 DiffCard（代码差异 + 编辑）
2. 展示 Admin 面板（Dashboard / Health / Stats）
3. 展示 Agent Profile 编辑

## 稳定性保障

- [ ] 所有 API 端点正常响应（无 500）
- [ ] SSE 流稳定无断裂（连续运行 10 分钟）
- [ ] 会话切换无数据丢失
- [ ] 多 Agent 并发无竞态
- [ ] 错误恢复后可继续使用

## 已有基础设施

| 基础设施 | 状态 | 说明 |
|----------|------|------|
| 一键启动 | ✅ | `make all` 三端热重载 |
| 服务管理 | ✅ | `make status` / `make stop` / `make restart` |
| 日志系统 | ✅ | `logs/frontend.log` / `backend.log` / `agentend.log` |
| 契约生成 | ✅ | `make generate` 三端类型自动生成 |
| 测试清理 | ✅ | `scripts/test-clean.sh` MySQL + Redis 数据清理 |
| Git Hooks | ✅ | Pre-commit linting |
