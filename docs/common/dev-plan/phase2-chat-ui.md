# Phase 2: 最小聊天界面 — 发消息 + 流式回复

> 目标: 浏览器打开页面，输入消息，看到 Agent 流式回复文字。
> 预估: 3 天
> 前置: Phase 1 完成 (Go SSE proxy 可用)
> 状态: ✅ 已完成

## 交付标准

1. ✅ 浏览器打开 `http://localhost:5173`
2. ✅ 看到聊天页面 — 左侧空会话列表 + 右侧聊天区
3. ✅ 输入框输入消息，点击发送
4. ✅ 右侧实时出现 Agent 流式回复文字
5. ✅ 消息结束后停止 streaming 状态

## 实际实现

### 已完成文件

```
frontend/
├── src/
│   ├── App.tsx                          # ✅ 应用入口（路由 + 布局）
│   ├── pages/
│   │   └── ImPage.tsx                   # ✅ 主页面（QQ 风格三栏布局）
│   │   └── AgentProfilePage.tsx         # ✅ Agent Profile 页面
│   ├── api/
│   │   ├── api.ts                       # ✅ 统一 API 层（tasks, conversations, messages, agents, admin 等）
│   │   └── sse.ts                       # ✅ SSE 连接层（EventSource + 自动重连 + 超时检测）
│   ├── stores/
│   │   ├── chat.ts                      # ✅ 聊天状态（多会话、流式、Block 解析、分页）
│   │   └── admin.ts                     # ✅ Admin 状态（认证、导航）
│   ├── hooks/
│   │   └── use-chat-stream.ts           # ✅ 聊天流 Hook（RAF 文本批处理、Agent 名跟踪）
│   ├── components/
│   │   ├── im/
│   │   │   ├── ConversationList.tsx     # ✅ 会话列表（按 Task 分组）
│   │   │   ├── ConversationItem.tsx     # ✅ 单个会话项
│   │   │   ├── AgentSelectList.tsx      # ✅ Agent 选择列表（多选 + Orchestrator 自动注入）
│   │   │   ├── NewChatDialog.tsx        # ✅ 新建会话弹窗
│   │   │   └── RepoPathInput.tsx        # ✅ 仓库路径输入
│   │   ├── chat/
│   │   │   ├── ChatArea.tsx             # ✅ 聊天主区域
│   │   │   ├── MessageList.tsx          # ✅ 消息列表（自动滚动）
│   │   │   ├── MessageBubble.tsx        # ✅ 消息气泡（用户/Agent/系统）
│   │   │   ├── MessageRenderer.tsx      # ✅ 消息渲染器
│   │   │   ├── MessageInput.tsx         # ✅ 输入区域（自适应高度 + 快捷键）
│   │   │   ├── AgentAvatar.tsx          # ✅ Agent 头像（状态指示）
│   │   │   ├── AgentHoverCard.tsx       # ✅ Agent 悬浮卡片
│   │   │   ├── AgentMeta.tsx            # ✅ Agent 元信息
│   │   │   ├── TimeDivider.tsx          # ✅ 时间分割线
│   │   │   └── SkillCard.tsx            # ✅ 技能卡片
│   │   ├── layout/
│   │   │   └── IconSidebar.tsx          # ✅ 应用侧边栏导航
│   │   └── admin/
│   │       ├── AdminMenu.tsx            # ✅ Admin 导航
│   │       ├── AdminPasswordDialog.tsx  # ✅ Admin 登录弹窗
│   │       └── (7 个管理页面)            # ✅ Dashboard / Session / Workspace / Agent / Health / Stats / User
│   └── main.tsx                         # ✅ 入口
└── vite.config.ts                       # ✅ 代理配置 → Go 后端
```

### 与原计划的差异

| 原计划 | 实际实现 | 理由 |
|--------|----------|------|
| 简单两栏布局 | QQ 风格三栏布局（导航 + 会话列表 + 聊天） | 更符合 IM 体验 |
| Zustand store | Zustand store ✅ | 一致 |
| fetch + ReadableStream | EventSource + 自定义 SSE | 更稳定的事件流解析 |
| 纯文本渲染 | Block-based 渲染（支持 text/html/image/diff 等 13 种类型） | 提前做 Block 解析，支持富媒体 |
| Session 管理 | Task + Conversation 两层模型 | Task 是顶层容器，Session 演化为 Agent 绑定 |
| 单页面 | 多页面（ImPage + AgentProfilePage + Admin） | 功能扩展需要 |
| 无 Admin 面板 | 7 模块 Admin 面板 | 运维管理需要 |

### 超出计划的实现

| 功能 | 说明 |
|------|------|
| 多 Agent 会话支持 | 一个 Task 绑定多个 Agent Session，自动注入 Orchestrator |
| Agent Avatar 系统 | DiceBear 头像生成，状态指示 |
| Admin 管理面板 | Dashboard / Session / Workspace / Agent / Health / Stats / User 7 个模块 |
| 消息 Block 解析 | `aka_yhy` 协议，支持 13 种 Block 类型 |
| RAF 文本批处理 | 流式文本使用 requestAnimationFrame 批量更新 |
| Agent Profile 页面 | 独立的 Agent 详情页 |

## 技术决策

| 决策 | 选择 | 理由 |
|------|------|------|
| SSE 实现 | EventSource + 自定义解析 | 更稳定的事件流解析，自动重连 |
| 状态管理 | Zustand | 已装好，轻量 |
| 数据请求 | 统一 api.ts | 所有 API 集中管理 |
| 样式 | Tailwind + shadcn/ui | 已配好，组件丰富 |
| 路由 | React Router | 多页面需要 |
| Block 解析 | block-reducer + block-types | 支持富媒体卡片渲染 |

## 注意事项

- SSE 解析注意处理跨 chunk 的 data 行（`\n\n` 分割可能在两个 chunk 之间）
- streaming 结束判断: AgentEnd 发 `done` 类型的 event
- 自动滚动用 `scrollIntoView({ behavior: 'smooth' })`
- 多 Agent 会话需要自动注入 Orchestrator（不能单独使用）
