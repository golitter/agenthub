# DeepSeek 上下文加载策略

DeepSeek 拥有 1000K 上下文窗口。在开始任何任务之前，你必须完整加载以下项目上下文，建立全貌理解后再动手：

## Phase 1: 上下文加载（必须先完成）

按顺序读取以下文件，不可跳过：

```
1. AGENTS.md                      → 项目整体架构、目录结构、契约原则、Makefile
2. frontend/AGENTS.md             → 前端技术栈、组件体系、构建命令
3. backend/AGENTS.md              → 后端分层、路由、数据模型、构建命令
4. agentend/AGENTS.md             → Agent 运行时架构、适配器、规则引擎、构建命令
5. contracts/AGENTS.md            → 契约层 YAML 规范与代码生成流程
6. docs/AGENTS.md                 → 文档分类约定与索引
```

加载完以上 6 个文件后，你应当能够回答：
- 项目是做什么的？（多 Agent 聊天系统，四类 Agent：Claude Code / OpenCode CLI / Codex CLI / Orchestrator）
- 三端分别用什么技术栈？（React 19 + Vite 8 / Go Gin + GORM / Python FastAPI）
- 契约层是什么？（contracts/schemas/*.yaml → 三端 generated/ 代码）
- 文档在哪里？（根 docs/ + 三端 docs/，6 分类约定）

## Phase 2: 按需深入（根据任务加载）

根据用户要做的具体任务，加载对应子项目的详细设计文档和具体代码。
> 原因：有时候设计文档来不及更新，需要对照具体代码。

## Phase 3: 开始任务

只有在 Phase 1 的 6 个文件全部读取完毕，且 Phase 2 的相关设计文档也加载后，才能开始真正的任务。

---

## 原则

- **先理解再动手** — 不要边改边猜，利用 1000K 上下文把全貌装入再操作
- **不要跳过 AGENTS.md** — 它们是地图，不看地图就行动容易迷路
- **改代码前先读 design/** — 了解模块为什么这样设计，避免引入回退
- **提交前复习 git-conventions.md** — 确保 commit message 格式正确
