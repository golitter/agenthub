# AGENTS.md — frontend

基于 React 19 + Vite + TypeScript 的前端项目，使用 Tailwind CSS + shadcn/ui 组件库，Zustand 状态管理，TanStack React Query 数据请求。包管理 pnpm。

## 目录结构

```
src/
├── main.tsx            # 应用入口（StrictMode + QueryClient + BrowserRouter）
├── App.tsx             # 主页面组件
├── index.css           # 全局样式（Tailwind + CSS 变量主题）
├── assets/             # 静态资源
├── components/ui/      # shadcn/ui 组件（button, card, input, dialog）
├── lib/utils.ts        # cn() 工具函数
└── stores/app.ts       # Zustand store
```

## 常用命令

> 通过根目录 Makefile 统一管理，需在项目根目录执行。

```bash
make run-frontend          # 启动（热重载）
make stop-frontend         # 停止
make restart-frontend      # 重启
make status                # 查看状态
```

如需手动启动：`cd frontend && pnpm dev`

- Makefile 完整说明：[docs/common/makefile-guide.md](../docs/common/makefile-guide.md)

## 详细文档

- 技术栈详情：[docs/tech-stack.md](docs/tech-stack.md)
