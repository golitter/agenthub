# Phase 3: IM 体验补全 — 会话管理 + Agent 切换 + Markdown

> 目标: 完整的 IM 基础体验 — 会话管理、Agent 选择、Markdown 渲染、消息历史。
> 预估: 2 天
> 前置: Phase 2 完成 (基础聊天可用)
> 状态: ✅ 已完成

## 交付标准

1. ✅ 左侧会话列表可新建、切换、删除
2. ✅ 顶部有 Agent 类型选择器（claude-code / opencode / orchestrator / codex）
3. ✅ Agent 回复支持 Markdown 渲染（代码块语法高亮）
4. ✅ 切换会话后能看到历史消息
5. ✅ 整体布局和交互接近 IM 应用

## 实际实现

### 已完成功能

```
┌──────────────────────────────────────────────────────────┐
│  🟣  │  会话列表           │  聊天区域                    │
│  导  │                    │                              │
│  航  │  🔍 搜索...        │  Task 标题                   │
│      │                    │                              │
│  💬  │  📋 Task 1         │  👤 你                       │
│  ⚙️  │    ├ Agent: Claude │    帮我写一个 React 组件      │
│      │    └ Agent: Open   │                              │
│      │  📋 Task 2         │  🤖 Claude Code              │
│      │    └ Agent: Codex  │    好的，这是一个组件：        │
│      │                    │    ┌─ Button.tsx ─────────┐  │
│      │  [+ 新建会话]      │    │ export function ...  │  │
│      │                    │    └─────────────────────┘  │
│      │                    │                              │
│      │                    │  ─────────────────────────── │
│      │                    │  ┌─────────────────┐ [发送]  │
│      │                    │  │ 输入消息...      │         │
│      │                    │  └─────────────────┘         │
└──────────────────────────────────────────────────────────┘
```

### 会话管理

- ✅ **Task 为顶层容器**：每个 Task 是一次协作任务
- ✅ **多 Agent 绑定**：一个 Task 可绑定多个 Agent（多 Session）
- ✅ **Orchestrator 自动注入**：多 Agent 时自动添加 Orchestrator
- ✅ **会话创建弹窗**：选择 Agent + 输入 Repo Path + 标题
- ✅ **会话列表分组**：按 Task 分组展示

### Agent 选择器

- ✅ **多选 Agent**：支持同时选择多个 Agent
- ✅ **Orchestrator 保护**：不能单独选 Orchestrator
- ✅ **Agent 类型**：claude-code / opencode / orchestrator / codex
- ✅ **Agent Profile**：名称、头像、Soul MD 可编辑
- ✅ **Avatar 系统**：七牛云上传 + DiceBear 默认生成

### Markdown 渲染

- ✅ **MarkdownRenderer**：标题、粗体、斜体、链接、列表、表格
- ✅ **CodeBlock**：语法高亮 + 复制按钮 + 文件名标签
- ✅ **行内代码**：高亮显示

### 消息历史

- ✅ **分页加载**：`limit` + `before` 游标分页
- ✅ **历史回放**：MySQL 历史 + Redis 填补 + RuntimeHub 实时
- ✅ **Session 过滤**：可按 Session ID 过滤消息
- ✅ **消息持久化**：MySQL 存储，Redis Stream 缓冲

## 与原计划的差异

| 原计划 | 实际实现 | 理由 |
|--------|----------|------|
| Session 为顶层 | Task + Session 两层 | Task 是协作容器，Session 是 Agent 绑定 |
| 单选 Agent | 多选 Agent + 自动 Orchestrator | 多 Agent 协作是核心功能 |
| AgentSelector 下拉 | AgentSelectList 多选列表 | 多选需要列表而非下拉 |
| `react-markdown` | 自定义 MarkdownRenderer + CodeBlock | 更灵活的代码块渲染 |
| 无 Admin 面板 | 7 模块 Admin 面板 | 运维管理需要 |

## 超出计划的实现

| 功能 | 说明 |
|------|------|
| Agent Profile 编辑 | 名称、头像、Soul MD 可自定义 |
| 消息分页 | 游标分页，支持大数据量 |
| Admin 面板 | Dashboard / Session / Workspace / Agent / Health / Stats / User |
| 头像上传 | 七牛云存储，支持 jpg/png/gif/webp |
| Agent Hover Card | 悬浮展示 Agent 详情 |
| Soul MD 注入 | 通过 Go Backend 注入 Agent 个性配置 |

## 注意事项

- Agent 选择时 Orchestrator 会被自动注入，不能单独使用
- 消息历史加载采用 MySQL 历史 → Redis 填补 → 实时推送三段式
- Markdown 渲染优先用 CodeBlock 组件处理代码块，其余用 MarkdownRenderer
- 会话创建需要指定 Repo Path，后端会调用 AgentEnd 验证
