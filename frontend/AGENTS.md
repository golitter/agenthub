# AGENTS.md — frontend

基于 React 19 + Vite 8 + TypeScript 的前端项目，使用 Tailwind CSS + shadcn/ui（radix-nova 风格）组件库，Zustand 状态管理，TanStack React Query 数据请求。包管理 pnpm，代码检查 ESLint + Prettier。

## 目录结构

```
src/
├── main.tsx                # 应用入口
├── index.css               # 全局样式（Tailwind + shadcn 语义 token）
├── assets/                  # 静态资源
├── components/
│   ├── chat/               # 聊天模块
│   ├── im/                 # 会话列表模块
│   ├── cards/              # 技能输出卡片（DiffCard, HtmlCard, ImageCard 等）
│   ├── diff/               # Diff 查看器（多文件 tab + CodeMirror 编辑）
│   ├── layout/             # 布局组件（IconSidebar + AdminMenu + AdminPasswordDialog）
│   ├── markdown/           # Markdown 渲染
│   └── ui/                 # shadcn/ui 基础组件
├── pages/
│   ├── ImPage.tsx          # 主页面（三栏布局 + NavTab 路由）
│   ├── AgentProfilePage.tsx # Agent 详情页
│   ├── SkillsHubPage.tsx   # SkillsHub 技能库页面
│   └── admin/              # 管理面板（7 模块页面）
├── hooks/                  # 自定义 Hooks（use-chat-stream, use-conversations, use-contact-groups, use-hover-style, use-message-scroll, use-resize, use-theme）
├── stores/
│   ├── chat.ts             # Barrel re-export（组合 navigation + session + message 三 Store）
│   ├── navigation-store.ts # 导航状态（currentSessionId + activeTab）
│   ├── session-store.ts    # 各会话独立数据 Map
│   ├── message-store.ts    # 消息流式更新 + runtime blocks
│   └── admin.ts            # 管理面板认证 Store
├── lib/                    # 工具库（api, sse, constants, utils, block-reducer, block-types, diff-parser, ui-text）
├── utils/                  # 工具函数（time.ts）
└── generated/              # 契约生成的 TypeScript 类型（勿手改）
```

## 常用命令

> 通过根目录 Makefile 统一管理，需在项目根目录执行。

```bash
make run-frontend          # 启动（热重载）
make stop-frontend         # 停止
make restart-frontend      # 重启
make status                # 查看状态
```

- Makefile 完整说明：[docs/guides/makefile-guide.md](../docs/guides/makefile-guide.md)
- 排查问题查看 `../logs/frontend.log`

## 详细文档

详见 [docs/reference/details.md](docs/reference/details.md)。
