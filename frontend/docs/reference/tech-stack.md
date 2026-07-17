# Frontend 技术栈

## 构建工具

| 工具 | 版本 | 用途 |
|------|------|------|
| Vite | ^8.0.12 | 开发服务器 + 生产构建 |
| TypeScript | ~6.0.2 | 类型检查 |
| pnpm | — | 包管理器 |

## 核心框架

| 库 | 版本 | 用途 |
|----|------|------|
| React | ^19.2.6 | UI 框架 |
| React DOM | ^19.2.6 | DOM 渲染 |
| React Router | ^7.15.1 | 客户端路由（BrowserRouter） |

## 样式方案

| 库 | 版本 | 用途 |
|----|------|------|
| Tailwind CSS | ^4.3.0 | 原子化 CSS 框架 |
| @tailwindcss/vite | ^4.3.0 | Tailwind Vite 插件 |
| @tailwindcss/typography | ^0.5.19 | Markdown prose 排版插件（devDependency） |
| tw-animate-css | ^1.4.0 | Tailwind 动画扩展 |
| @fontsource-variable/geist | ^5.2.9 | Geist Variable 字体 |

配色方案通过 CSS 变量实现 light/dark 双主题，使用 hex / rgba 色值定义，品牌色 Indigo `#6366F1`。

## UI 组件库

| 库 | 版本 | 用途 |
|----|------|------|
| shadcn/ui (radix-nova 风格) | ^4.8.0 | 组件生成器（非运行时依赖） |
| @radix-ui/react-dialog | ^1.1.15 | Dialog 组件底层（无障碍原语） |
| radix-ui | ^1.4.3 | Radix UI 统一包（Popover 等组件原语） |
| clsx | ^2.1.1 | 条件 className 合并 |
| tailwind-merge | ^3.6.0 | Tailwind class 冲突合并 |
| lucide-react | ^1.16.0 | 图标库 |

已安装的 shadcn/ui 组件：Dialog、Popover；另有项目自维护的 `error-boundary.tsx` 兜底组件。

## 状态管理

| 库 | 版本 | 用途 |
|----|------|------|
| Zustand | ^5.0.13 | 全局状态管理 |
| @tanstack/react-query | ^5.100.11 | 服务端状态管理 + 数据缓存 |
| @tanstack/react-virtual | ^3.13.25 | 虚拟滚动（消息列表性能优化） |

Store 位于 `src/stores/`，包含 `navigation-store.ts`（导航状态）、`session-store.ts`（各会话独立数据 Map）、`message-store.ts`（消息流式更新 + runtime blocks + 公告管理）、`chat.ts`（barrel re-export 组合三 Store）、`admin.ts`（管理面板认证 + 菜单状态）。管理聊天导航（currentSessionId）及各会话独立的消息/流式/分页状态。QueryClient 在 `main.tsx` 中注入。

## Markdown 与代码高亮

| 库 | 版本 | 用途 |
|----|------|------|
| react-markdown | ^10.1.0 | Markdown 渲染 |
| remark-gfm | ^4.0.1 | GFM 扩展（表格、删除线等） |
| shiki | ^4.1.0 | 代码语法高亮 |

## Diff 查看器与代码编辑

| 库 | 版本 | 用途 |
|----|------|------|
| react-diff-view | ^3.3.3 | Unified Diff 视图渲染 |
| @uiw/react-codemirror | ^4.25.10 | CodeMirror React 封装（Diff 文件编辑） |
| @codemirror/merge | ^6.12.1 | CodeEditor 合并视图 |
| @codemirror/lang-javascript | ^6.2.5 | JavaScript/TypeScript 语法 |
| @codemirror/lang-python | ^6.2.1 | Python 语法 |
| @codemirror/lang-css | ^6.3.1 | CSS 语法 |
| @codemirror/lang-html | ^6.4.11 | HTML 语法 |
| @codemirror/lang-json | ^6.0.2 | JSON 语法 |
| @codemirror/theme-one-dark | ^6.1.3 | One Dark 主题 |

## 代码规范

| 工具 | 配置 |
|------|------|
| ESLint | flat config，集成 typescript-eslint、react-hooks、react-refresh、eslint-config-prettier、simple-import-sort |
| Prettier | 无分号、单引号、尾逗号 all、行宽 100 |
| simple-import-sort | import 自动排序 |
| Vitest | ^4.1.7，单元测试框架（lib/__tests__/） |

## 项目结构

```
frontend/
├── index.html              # 入口 HTML
├── vite.config.ts          # Vite 配置（React + Tailwind 插件，@ 别名，API 代理）
├── tsconfig.json           # TypeScript 项目引用
├── components.json         # shadcn/ui 配置
└── src/
    ├── main.tsx            # 应用入口（StrictMode + QueryClient + BrowserRouter）
    ├── index.css           # 全局样式（Tailwind + CSS 变量主题）
    ├── components/
    │   ├── chat/           # 聊天模块
    │   ├── im/             # 会话列表模块
    │   ├── cards/          # 技能输出卡片（DiffCard, HtmlCard, ImageCard 等）
    │   ├── diff/           # Diff 查看器（多文件 tab + 可编辑 CodeMirror）
    │   ├── layout/         # 布局组件（IconSidebar + AdminMenu + AdminPasswordDialog）
    │   ├── markdown/       # Markdown 渲染
    │   └── ui/             # shadcn/ui 基础组件（dialog、popover）+ ErrorBoundary
    ├── pages/              # 页面
    ├── hooks/              # 自定义 Hooks
    ├── stores/             # Zustand Store（chat.ts barrel + navigation-store + session-store + message-store + admin）
    ├── lib/                # 工具库（api, sse, constants, utils, ui-text, block-reducer, block-types, diff-parser）
    ├── utils/              # 工具函数（time.ts）
    └── generated/          # 契约生成的 TypeScript 类型
```

## 关键设计决策

- **路由模式**：BrowserRouter（客户端路由），两条路由：`/agent/:sessionId` → AgentProfilePage、`/*` → ImPage
- **CSS 变量主题**：通过 hex / rgba 色值定义 light/dark 双主题变量，Tailwind 直接引用
- **路径别名**：`@/` 映射到 `src/`，在 vite.config.ts 和 tsconfig.app.json 中同步配置
- **组件模式**：shadcn/ui 代码直接拷贝到项目中（非 npm 依赖），可自由修改
- **SSE 流式通信**：通过 EventSource 直连 Backend，开发环境绕过 Vite 代理避免缓冲问题
- **虚拟滚动**：消息列表使用 @tanstack/react-virtual 优化长列表渲染性能
