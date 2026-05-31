# Phase 4: 产物与打磨 — 卡片组件 + Artifact 预览

> 目标: Agent 回复中的富媒体卡片（代码块、工具进度、产物预览）。
> 预估: 2-3 天
> 前置: Phase 3 完成 (IM 基础体验可用)
> 状态: ✅ 已完成

## 交付标准

1. ✅ Agent 回复中代码块有语法高亮 + 复制按钮
2. ✅ 工具调用有进度卡片（命令 + 实时输出）
3. ✅ 产物文件有预览卡片（图片可直接看，代码文件有详情）
4. ✅ 群聊模式：选择 Orchestrator 后看到多 Agent 协作

## 实际实现

### Block 解析系统

采用 `aka_yhy` 协议解析消息 Block，支持 **13 种 Block 类型**：

| Block 类型 | 组件 | 功能 |
|------------|------|------|
| `text` | MarkdownRenderer | 文本 + Markdown 渲染 |
| `html-render` | HtmlCard | HTML 内容渲染 |
| `image` | ImageCard | 图片展示 |
| `attachment` | AttachmentCard | 文件附件下载 |
| `diff` | DiffCard | Git Diff 查看（Split/Unified + 编辑） |
| `preview` | PreviewCard | 文件预览 |
| `plan` | PlanCard | 任务规划展示（任务列表 + Agent 分配 + 状态） |
| `runtime_status` | RuntimeStatus | 任务执行状态 |
| `coordination` | CoordChannel | 多 Agent 协调通道（轮次 + 摘要） |
| `ask_agent` | AskAgentCard | Agent 间问答 |
| `task_failure` | TaskFailureCard | 任务失败通知 |
| `final_summary` | FinalSummaryCard | 任务完成摘要 |
| `tool_call` / `tool_result` | ToolCard | 工具调用/结果展示 |

### 核心卡片组件

#### DiffCard — 代码差异视图

```
┌─ src/components/Button.tsx ──── [Split ▼] [Commit] [Revert] ─┐
│ ┌─────────────────────┬─────────────────────┐                │
│ │ 旧代码               │ 新代码               │                │
│ │   function Button() │   export function B()│                │
│ │ -  return <button>  │ +  return <Button>   │                │
│ └─────────────────────┴─────────────────────┘                │
│ [📄 File1.tsx] [📄 File2.tsx] [📄 File3.tsx]                 │
└──────────────────────────────────────────────────────────────┘
```

- ✅ Split/Unified 视图切换
- ✅ 多文件 Tab 导航
- ✅ 行内编辑（CodeMirror）
- ✅ Commit / Revert 操作

#### CoordChannel — 协调通道

```
┌─ 🎯 协调通道 ─────────────────────────────────────┐
│ Round 1                                            │
│   [Claude Code] 实现了登录页面组件                   │
│   [OpenCode] 审查了代码质量，发现 2 个问题           │
│                                                    │
│ 📋 结论: 登录页基本完成，需修复输入验证              │
└────────────────────────────────────────────────────┘
```

- ✅ 轮次展示
- ✅ 可折叠
- ✅ 协调结论摘要

#### PlanCard — 任务规划

```
┌─ 📋 执行计划 ─────────────────────────────────────┐
│ 概述: 实现登录页并审查代码质量                       │
│                                                    │
│ ● Task 1 — 实现登录页     [Claude Code]  ✓ 完成    │
│ ● Task 2 — 审查代码质量   [OpenCode]     ○ 待执行   │
└────────────────────────────────────────────────────┘
```

- ✅ 任务列表 + Agent 分配
- ✅ 状态图标（● 运行中 / ✓ 完成 / ✗ 失败 / ○ 待执行）

### Block 解析流程

```
SSE Event
    ↓
block-reducer.ts
    ├── Legacy 格式（fenced blocks）→ 解析旧版 fence 标记
    ├── Modern 格式（type: field）→ 直接解析 JSON
    ↓
Block Coalescing
    ├── 相邻 text block 合并
    ├── Plan 任务聚合
    ├── Coordination 消息分组
    ├── Task Failure 标记解析
    ↓
MessageRenderer
    ↓
对应 Card 组件渲染
```

## 与原计划的差异

| 原计划 | 实际实现 | 理由 |
|--------|----------|------|
| 3 种卡片（CodeBlock / ToolProgress / Artifact） | 11 种卡片组件 | 富媒体需求远超预期 |
| Artifact Manager（AgentEnd） | 集成到 Workspace Manager | 简化架构 |
| Artifact 代理（Go） | Workspace 代理统一处理 | 减少重复代码 |
| 简单 Agent 标签 | CoordChannel + PlanCard 完整协调 UI | 多 Agent 协作需要更丰富的 UI |
| Event 分发到 Store | Block-based 消息渲染 | Block 解析更灵活 |

## 超出计划的实现

| 功能 | 说明 |
|------|------|
| DiffCard + 编辑 | 多文件 diff + CodeMirror 行内编辑 + Commit/Revert |
| CoordChannel | 多 Agent 协调通道，轮次展示 + 结论摘要 |
| PlanCard | 任务规划进度，Agent 分配 + 状态追踪 |
| RuntimeStatus | 任务执行实时状态 |
| FinalSummaryCard | 任务完成总结 |
| TaskFailureCard | 任务失败通知 + 错误信息 |
| AskAgentCard | Agent 间交互问答 |
| Block 解析引擎 | 支持 Legacy + Modern 两种格式，自动合并、分组 |
| Diff Snapshot | 代码差异快照保存/恢复 |

## 注意事项

- Block 解析使用 `aka_yhy` 协议，需同时支持 Legacy 和 Modern 格式
- DiffCard 的编辑功能通过 CodeMirror 实现，支持 Split/Unified 两种视图
- CoordChannel 自动按轮次分组展示 Agent 间的协调消息
- PlanCard 状态实时更新，支持运行中/完成/失败/待执行四种状态
- 工具调用（tool_call/tool_result）由 ToolCard 统一渲染
