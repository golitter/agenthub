# Phase 3: IM 体验补全 — 会话管理 + Agent 切换 + Markdown

> 目标: 完整的 IM 基础体验 — 会话管理、Agent 选择、Markdown 渲染、消息历史。
> 预估: 2 天
> 前置: Phase 2 完成 (基础聊天可用)

## 交付标准

1. 左侧会话列表可新建、切换、删除
2. 顶部有 Agent 类型选择器 (claude-code / opencode / orchestrator)
3. Agent 回复支持 Markdown 渲染 (代码块高亮)
4. 切换会话后能看到历史消息
5. 整体布局和交互接近 IM 应用

## 页面升级

```
┌──────────────────────────────────────────────────────────┐
│  AgentHub                                                │
├───────────┬──────────────────────────────────────────────┤
│           │  聊天标题          [Agent: Claude Code ▼]    │
│ Sessions  │──────────────────────────────────────────────│
│           │                                              │
│ [+ New]   │  👤 你                                        │
│           │  帮我写一个 React 组件                         │
│ > Chat 1  │                                              │
│   Chat 2  │  🤖 Claude Code                              │
│   Chat 3  │  好的，这是一个 React Button 组件：            │
│           │                                              │
│           │  ┌─ src/components/Button.tsx ────────────┐  │
│           │  │ import React from 'react';             │  │
│           │  │                                        │  │
│           │  │ export function Button({ children }) { │  │
│           │  │   return <button>{children}</button>;  │  │
│           │  │ }                                      │  │
│           │  └────────────────────────────────────────┘  │
│           │                                              │
│           │──────────────────────────────────────────────│
│           │ ┌─────────────────────┐  [Send]              │
│           │ │ 输入消息...          │                      │
│           │ └─────────────────────┘                      │
└───────────┴──────────────────────────────────────────────┘
```

## 要写的文件 / 修改

### 1. Go Backend 补全

**修改**: `backend/internal/handler/task.go`

```
新增路由:
  GET /api/sessions/:sid/tasks          查询 session 下的 task 列表
```

> 前端加载历史消息时，从 task 列表中恢复 user message + result。

### 2. Agent 选择器

**文件**: `frontend/src/components/chat/AgentSelector.tsx`

```
下拉选择器:
  - 选项: claude-code, opencode, orchestrator
  - 当前选中的 agent 显示在聊天区顶部
  - 切换 agent 不影响当前会话，只是下次发消息用新 agent
  - 状态存在 chat store 中
```

### 3. Markdown 渲染

**依赖安装**: `pnpm add react-markdown remark-gfm rehype-highlight`

**文件**: `frontend/src/components/chat/MarkdownRenderer.tsx`

```
职责: 将 markdown 文本渲染为富文本

功能:
  - 标题、粗体、斜体、链接、列表
  - 代码块 (语法高亮)
  - 表格 (remark-gfm)
  - 行内代码
```

**修改**: `frontend/src/components/chat/MessageBubble.tsx`

```
变更:
  - assistant 消息的 content 改用 MarkdownRenderer 渲染
  - user 消息保持纯文本
```

### 4. 消息历史

**文件**: `frontend/src/api/client.ts` (修改)

```
新增方法:
  - listTasks(sessionId: string): Promise<Task[]>
```

**文件**: `frontend/src/stores/chat.ts` (修改)

```
新增 action:
  - loadHistory(sessionId: string)
      调用 listTasks → 把 task 列表转换为 messages 格式
      user message = task.message
      assistant message = task.result (或从 SSE 流中已缓存的内容)
```

**修改**: 切换 session 时调用 `loadHistory(sessionId)`

### 5. 会话列表完善

**修改**: `frontend/src/components/chat/Sidebar.tsx`

```
改进:
  - 空列表时显示 "新建一个对话开始吧" 提示
  - 删除 session 后自动切换到下一个
  - 最后一条消息预览 (截断显示)
```

### 6. 路由 (可选)

**文件**: `frontend/src/App.tsx` (修改)

```
如需 URL 可分享:
  / → ChatPage (默认新建或跳转到最近 session)
  /chat/:sessionId → 特定 session

Phase 3 可以不做路由，用 store 管理当前 session 即可。
```

## 文件清单

```
Go Backend:
├── internal/handler/task.go            # 修改: 加 listTasks 路由

Frontend:
├── src/
│   ├── components/chat/
│   │   ├── AgentSelector.tsx           # 新增 ~50 行
│   │   ├── MarkdownRenderer.tsx        # 新增 ~40 行
│   │   ├── MessageBubble.tsx           # 修改: 接 MarkdownRenderer
│   │   └── Sidebar.tsx                 # 修改: 完善交互
│   ├── stores/
│   │   └── chat.ts                     # 修改: 加 loadHistory + agentType
│   └── api/
│       └── client.ts                   # 修改: 加 listTasks
└── package.json                        # 修改: 加 react-markdown 等依赖
```

**新增代码量: ~90 行，修改 ~100 行**

## 注意事项

- react-markdown 是最轻量的 Markdown 渲染方案，不需要 MDX
- 代码高亮用 rehype-highlight (基于 highlight.js)，不需要额外主题文件
- Agent 选择器用 shadcn/ui 的 Select 组件（需 `npx shadcn add select`）
- 历史消息加载时，如果 task.result 为空（还在运行），只显示 user message
- 删除 session 时加确认弹窗（用已有的 shadcn Dialog）
