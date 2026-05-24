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
| tw-animate-css | ^1.4.0 | Tailwind 动画扩展 |
| @fontsource-variable/geist | ^5.2.9 | Geist Variable 字体 |

配色方案使用 oklch 色彩空间，通过 CSS 变量实现 light/dark 双主题。

## UI 组件库

| 库 | 版本 | 用途 |
|----|------|------|
| shadcn/ui (radix-nova 风格) | ^4.8.0 | 组件生成器（非运行时依赖） |
| radix-ui | ^1.4.3 | 无障碍原语组件 |
| @radix-ui/react-dialog | ^1.1.15 | Dialog 组件底层 |
| @radix-ui/react-slot | ^1.2.4 | Slot 模式支持 |
| class-variance-authority (cva) | ^0.7.1 | 组件变体管理 |
| clsx | ^2.1.1 | 条件 className 合并 |
| tailwind-merge | ^3.6.0 | Tailwind class 冲突合并 |
| lucide-react | ^1.16.0 | 图标库 |

已安装的 shadcn/ui 组件：Button、Card、Input、Dialog。

## 状态管理

| 库 | 版本 | 用途 |
|----|------|------|
| Zustand | ^5.0.13 | 全局状态管理 |
| @tanstack/react-query | ^5.100.11 | 服务端状态管理 + 数据缓存 |
| @tanstack/react-virtual | ^3.13.25 | 虚拟滚动（消息列表性能优化） |

Store 位于 `src/stores/chat.ts`，管理聊天导航、会话消息、流式状态。QueryClient 在 `main.tsx` 中注入。

## Markdown 与代码高亮

| 库 | 版本 | 用途 |
|----|------|------|
| react-markdown | ^10.1.0 | Markdown 渲染 |
| remark-gfm | ^4.0.1 | GFM 扩展（表格、删除线等） |
| shiki | ^4.1.0 | 代码语法高亮 |

## 代码规范

| 工具 | 配置 |
|------|------|
| ESLint | flat config，集成 typescript-eslint、react-hooks、react-refresh、prettier 插件 |
| Prettier | 无分号、单引号、尾逗号 all、行宽 100 |
| simple-import-sort | import 自动排序 |

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
    │   ├── markdown/       # Markdown 渲染
    │   └── ui/             # shadcn/ui 基础组件
    ├── pages/              # 页面
    ├── hooks/              # 自定义 Hooks
    ├── stores/             # Zustand Store
    ├── lib/                # 工具库（api, sse, constants, utils）
    └── generated/          # 契约生成的 TypeScript 类型
```

## 关键设计决策

- **路由模式**：BrowserRouter（客户端路由），单页应用仅 `"/"` → `ImPage`
- **CSS 变量主题**：通过 oklch 色彩空间定义 light/dark 双主题变量，Tailwind 直接引用
- **路径别名**：`@/` 映射到 `src/`，在 vite.config.ts 和 tsconfig.app.json 中同步配置
- **组件模式**：shadcn/ui 代码直接拷贝到项目中（非 npm 依赖），可自由修改
- **SSE 流式通信**：通过 EventSource 直连 Backend，不经过 Vite 代理，避免缓冲问题
- **虚拟滚动**：消息列表使用 @tanstack/react-virtual 优化长列表渲染性能
