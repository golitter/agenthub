# Phase 7: 演示打磨 + 交付物

> 目标: 对照任务要求整理交付物，打磨 Demo 体验
> 预估: 2 天
> 前置: Phase 5 完成（Phase 6 可选）
> 对应任务要求: 交付物清单

## 交付物清单

> 对照 `docs/internal/任务要求.md` 的 5 类交付物

### 1. 产品设计文档

- [ ] 整理 `docs/design/` 下的架构设计文档
- [ ] 补充产品功能说明（IM 交互流程、群聊模式、产物预览）
- [ ] 更新 `docs/AGENTS.md` 索引

### 2. 技术文档

- [ ] 三层架构说明（React + Go + Python）
- [ ] 契约层使用指南（已存在 `docs/guides/contract-layer.md`）
- [ ] SSE 流式通信方案
- [ ] Orchestrator 设计文档（已有 `docs/internal/orchestrator-plan-phase.md`）

### 3. 可运行 Demo

- [ ] 三端一键启动（`make run-all`）
- [ ] 预置 Demo 数据（至少 2 个会话 + 2 种 Agent）
- [ ] 编写 Demo 场景脚本（预设对话流）

### 4. AI 协作开发记录

- [ ] CLAUDE.md / AGENTS.md（已存在）
- [ ] Skills 配置说明（已存在 `docs/reference/skills.md`）
- [ ] Rules / Spec 协作规范
- [ ] Git commit 历史整理（体现 AI 协作过程）

### 5. 3 分钟 Demo 视频

- [ ] 编写 Demo 演示脚本（场景 + 时间分配）
- [ ] 录制桌面操作 + 旁白
- [ ] 视频剪辑（字幕、关键操作标注）

## UI 打磨项

### 响应式适配

- [ ] 主聊天布局适配 1280px / 1024px / 768px
- [ ] 移动端浏览器基本可用（不做独立应用）

### 主题一致性

- [ ] 统一颜色变量（主色、背景、边框、文字）
- [ ] 组件间距对齐（shadcn/ui 默认值检查）
- [ ] 暗/亮模式一致性

### 错误处理

- [ ] Agent 断连 → 显示重连提示 + 自动重试
- [ ] Agent 超时 → 显示超时状态 + 手动重试按钮
- [ ] 网络错误 → Toast 提示 + 不丢失已输入内容
- [ ] 空消息 / 空会话 → 合适的空状态引导

## Demo 场景脚本

### 场景 1: 单聊代码生成（30s）

1. 打开 AgentHub
2. 新建会话，选 Claude Code
3. 输入 "写一个 React Button 组件"
4. 展示流式回复 + 代码高亮

### 场景 2: 多会话并行（30s）

1. 切换到另一个会话
2. 选 OpenCode
3. 输入 "审查刚才的 Button 组件"
4. 展示会话切换 + 历史保留

### 场景 3: Orchestrator 群聊（60s）

1. 新建群聊会话
2. 选 Orchestrator
3. 输入 "用 Claude Code 写一个登录页，OpenCode 审查"
4. 展示任务拆解 → Agent 依次执行 → 结果汇总

### 场景 4: 产物预览（30s）

1. 在聊天中展示 Artifact 卡片
2. 点击预览 → 全屏展示
3. 展示部署状态卡片

## 稳定性保障

- [ ] 所有 API 端点正常响应（无 500）
- [ ] SSE 流稳定无断裂（连续运行 10 分钟）
- [ ] 会话切换无数据丢失
- [ ] 多 Agent 并发无竞态
- [ ] 错误恢复后可继续使用
