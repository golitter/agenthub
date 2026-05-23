# Monorepo 工程化配置说明

## 包管理

| 子项目 | 包管理器 | 说明 |
|--------|---------|------|
| 根目录 | pnpm | Husky / commitlint / lint-staged |
| frontend | pnpm | Next.js 生态 |
| backend | go mod | Go 模块管理 |
| agentend | uv | Python 依赖管理 |

---

## Git Hooks（Husky）

| 钩子 | 触发时机 | 作用 |
|------|---------|------|
| pre-commit | `git commit` 前 | 运行 lint-staged，检查暂存文件的代码风格 |
| commit-msg | `git commit` 前 | 运行 commitlint，校验 commit message 格式 |

---

## AgentEnd（Python FastAPI）

### 前置要求

| 工具 | 版本 | 安装 |
|------|------|------|
| Python | >= 3.10 | 系统自带 / `brew install python` |
| uv | latest | `brew install uv` |

### 核心依赖

定义在 `pyproject.toml`：

| 依赖 | 用途 |
|------|------|
| fastapi | HTTP 框架 |
| uvicorn | ASGI 服务器 |
| pydantic / pydantic-settings | 数据校验 + 配置加载 |
| langchain-core / langchain-anthropic / langchain-openai | LLM 调用 |
| langgraph | Agent DAG 编排 |
| sse-starlette | SSE 流式推送 |
| httpx | 异步 HTTP 客户端 |
| pyyaml | YAML 配置解析 |
| python-dotenv | .env 加载 |

开发依赖：`pytest` + `pytest-asyncio`。

---

## Frontend（React + Vite）

### 前置要求

| 工具 | 版本 | 安装 |
|------|------|------|
| Node.js | >= 18 | `brew install node` |
| pnpm | >= 8 | `npm i -g pnpm` |

### 技术栈

| 技术 | 用途 |
|------|------|
| React | UI |
| Vite | 构建 |
| TypeScript | 类型 |
| Tailwind | 样式 |
| shadcn/ui | 组件库 |
| TanStack Query | API 状态 |
| Zustand | 本地状态 |
| React Router | 路由 |

### 初始化

```bash
cd frontend
pnpm create vite . --template react-ts
pnpm install
```

### 核心依赖

```bash
cd frontend

# Tailwind
pnpm add -D tailwindcss @tailwindcss/vite

# React Router
pnpm add react-router

# Zustand
pnpm add zustand

# TanStack Query
pnpm add @tanstack/react-query

# shadcn/ui（按提示选择 TypeScript + Tailwind）
npx shadcn@latest init
npx shadcn@latest add button card input dialog
```

### 启动

```bash
cd frontend
pnpm dev
```


## Backend（Go Gin + GORM）

### 前置要求

| 工具 | 版本 | 安装 |
|------|------|------|
| Go | >= 1.22 | `brew install go` |
| MySQL | >= 8.0 | `brew install mysql` 或 Docker |

### 核心依赖

定义在 `go.mod`：

| 依赖 | 用途 |
|------|------|
| gin-gonic/gin | HTTP 框架 |
| gorm.io/gorm + gorm.io/driver/mysql | ORM + MySQL 驱动 |
| spf13/viper | 配置管理 |
| go.uber.org/zap | 结构化日志 |
| golang-jwt/jwt/v5 | JWT 认证 |
| go-playground/validator/v10 | 参数校验 |
| google/uuid | UUID 生成 |
| joho/godotenv | 环境变量加载 |
| gin-contrib/cors | CORS 中间件 |

### 初始化

```bash
cd backend
go mod init agenthub/backend
go get github.com/gin-gonic/gin \
       gorm.io/gorm \
       gorm.io/driver/mysql \
       go.uber.org/zap \
       github.com/spf13/viper \
       github.com/google/uuid \
       github.com/go-playground/validator/v10 \
       github.com/golang-jwt/jwt/v5 \
       github.com/joho/godotenv \
       github.com/gin-contrib/cors
```

### 启动

```bash
cd backend
go run cmd/server/main.go
```

