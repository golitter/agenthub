# AGENTS.md — frontend

基于 React 19 + Vite 8 + TypeScript 的前端项目，使用 Tailwind CSS + shadcn/ui（radix-nova 风格）组件库，Zustand 状态管理，TanStack React Query 数据请求。包管理 pnpm，代码检查 ESLint + Prettier。

## 目录结构

```
src/
├── main.tsx                # 应用入口
├── index.css               # 全局样式（Tailwind + shadcn 语义 token）
├── components/
│   ├── chat/               # 聊天模块
│   ├── im/                 # 会话列表模块
│   ├── markdown/           # Markdown 渲染
│   └── ui/                 # shadcn/ui 基础组件
├── pages/
│   └── ImPage.tsx          # 主页面
├── hooks/                  # 自定义 Hooks（use-chat-stream, use-conversations 等）
├── stores/
│   └── chat.ts             # Zustand Store
├── lib/                    # 工具库（api, sse, constants, utils）
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

- Makefile 完整说明：[docs/common/makefile-guide.md](../docs/common/makefile-guide.md)

## 详细文档

- 技术栈详情：[docs/common/tech-stack.md](docs/common/tech-stack.md)
