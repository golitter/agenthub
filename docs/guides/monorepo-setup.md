# Monorepo 工程化配置说明

本文记录当前仓库的工程化配置与依赖入口，不再包含从零创建 Vite / Go module / Python 项目的历史初始化步骤。首次启动请优先参考 [setup.md](setup.md)，命令入口以根目录 `Makefile` 为准。

## 包管理

| 子项目 | 包管理器 | 说明 |
|--------|---------|------|
| 根目录 | pnpm | Husky / commitlint / lint-staged |
| frontend | pnpm | React + Vite 生态 |
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
| aiohttp | 工作区预览静态文件服务 |
| aiomysql | MySQL 异步访问 |
| fastapi | HTTP 框架 |
| uvicorn | ASGI 服务器 |
| pydantic / pydantic-settings | 数据校验 + 配置加载 |
| langchain / langchain-core / langchain-anthropic / langchain-openai | LLM 调用 |
| langgraph | Agent DAG 编排 |
| langfuse | Langfuse Cloud trace |
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
| Node.js | >= 20 | `brew install node` |
| pnpm | >= 8 | `npm i -g pnpm` |

### 技术栈

| 技术 | 用途 |
|------|------|
| React 19 | UI |
| Vite 8 | 构建 |
| TypeScript | 类型 |
| Tailwind CSS 4 | 样式 |
| shadcn/ui | 组件库 |
| TanStack Query | API 状态 |
| Zustand | 本地状态 |
| React Router | 路由 |

### 安装依赖

```bash
cd frontend
pnpm install
```

核心依赖以 `frontend/package.json` 为准；shadcn/ui 配置在 `frontend/components.json`，当前基础组件位于 `frontend/src/components/ui/`。

### 启动

```bash
cd frontend
pnpm dev
```


## Backend（Go Gin + GORM）

### 前置要求

| 工具 | 版本 | 安装 |
|------|------|------|
| Go | >= 1.26 | `brew install go` |
| MySQL | >= 8.0 | `brew install mysql` 或 Docker |

### 核心依赖

定义在 `go.mod`：

| 依赖 | 用途 |
|------|------|
| gin-gonic/gin | HTTP 框架 |
| gorm.io/gorm + gorm.io/driver/mysql | ORM + MySQL 驱动 |
| gopkg.in/yaml.v3 | 配置解析（YAML → struct） |
| golang-jwt/jwt/v5 | JWT 认证 |
| google/uuid | UUID 生成 |
| joho/godotenv | 环境变量加载 |
| gin-contrib/cors | CORS 中间件 |
| qiniu/go-sdk/v7 | 七牛云文件上传 |
| redis/go-redis/v9 | Redis 客户端 |

### 依赖维护

```bash
cd backend
go mod tidy
```

### 启动

```bash
cd backend
air  # 热重载模式（需安装 air）
# 或 go run cmd/server/main.go
```
