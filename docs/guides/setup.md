# Setup Guide — AI Runtime Platform

三层架构一键搭建指南：Frontend (React + Vite) + Backend (Go) + AgentEnd (Python)。

---

## 前置依赖

| 工具       | 版本要求     | 安装                                |
| ---------- | ---------- | ----------------------------------- | ------- |
| Go         | >= 1.26    | `brew install go`                   |
| Node.js    | >= 18      | `brew install node`                 |
| pnpm       | >= 8       | `npm i -g pnpm`                     |
| Python     | >= 3.10    | 系统自带 / `brew install python`     |
| uv         | latest     | `brew install uv`                   |
| MySQL      | >= 8.0     | `brew install mysql`                |
| Redis      | >= 7       | `brew install redis`                |
| Docker     | optional   | `brew install --cask docker`        |

验证：

```bash
go version && node --version && pnpm --version && uv --version
```

---

## 目录结构（目标）

```
agenthub/
├── frontend/          # React + Vite + Tailwind + shadcn/ui
├── backend/           # Go + Gin + GORM + MySQL
├── agentend/          # Python FastAPI（已有）
├── contracts/         # 三端共享契约（schemas + logs）
├── docs/
├── scripts/
├── Makefile
└── .env
```

---

## 1. AgentEnd（Python）— 已有，验证启动

```bash
cd agentend

# 安装依赖
uv sync

# 启动
uv run uvicorn src.app.main:app --host 0.0.0.0 --port 8001 --reload
```

验证：`curl http://localhost:8001/docs` → FastAPI Swagger 页面。

---

## 2. Backend（Go）— 从零初始化

### 2.1 初始化模块

```bash
cd backend
go mod init agenthub/backend
```

### 2.2 安装核心依赖

```bash
go get github.com/gin-gonic/gin \
       gorm.io/gorm \
       gorm.io/driver/mysql \
       gopkg.in/yaml.v3 \
       github.com/golang-jwt/jwt/v5 \
       github.com/gin-contrib/cors \
       github.com/google/uuid \
       github.com/joho/godotenv \
       github.com/qiniu/go-sdk/v7 \
       github.com/redis/go-redis/v9
```

### 2.3 创建目录结构

```bash
cd backend
mkdir -p cmd/server
mkdir -p internal/{conf,generated,controller/impl,service/impl,dao/gorm,dao/mock,middleware,model,stream,vo}
mkdir -p pkg/{agentend_client,db,qiniu,redis}
mkdir -p configs
```

目标结构：

```
backend/
├── cmd/server/main.go            # 入口：加载配置 → 连 DB → 注册路由 → 启动
├── internal/
│   ├── conf/conf.go              # 配置加载（YAML → struct + env override）
│   ├── generated/                # 契约生成的类型文件
│   ├── controller/               # Controller 层
│   │   ├── controller.go         # 接口定义（统一 RegisterRoutes）
│   │   └── impl/                 # Controller 实现
│   │       ├── task_controller.go
│   │       ├── session_controller.go
│   │       ├── message_controller.go
│   │       ├── stream_controller.go
│   │       ├── agent_profile_controller.go
│   │       ├── avatar_controller.go
│   │       ├── diff_snapshot_controller.go
│   │       ├── workspace_controller.go
│   │       ├── announcement_controller.go
│   │       ├── contact_group_controller.go
│   │       ├── skill_controller.go
│   │       ├── admin_controller.go
│   │       ├── agent_controller.go
│   │       └── errors.go
│   ├── service/                  # Service 层（纯业务逻辑，无 Gin 依赖）
│   │   ├── service.go            # 接口定义 + DTO
│   │   ├── bizerr.go             # 统一业务错误
│   │   └── impl/                 # Service 实现
│   ├── dao/                      # DAO 层（接口可 Mock 替换）
│   │   ├── dao.go                # 接口定义
│   │   ├── gorm/                 # GORM 实现
│   │   └── mock/                 # Mock 实现
│   ├── middleware/               # Gin 中间件（auth, admin_auth, cors, logger, rate_limit）
│   ├── model/                    # GORM 模型（11 个数据模型）
│   ├── stream/                   # Redis Stream 写入（RuntimeHub 低延迟推送 + Redis Stream → MySQL 批量刷写）
│   │   ├── hub.go
│   │   └── writer.go
│   └── vo/                       # View Object（API 响应结构）
├── pkg/
│   ├── agentend_client/          # AgentEnd HTTP Client
│   ├── db/                       # MySQL 连接（单例）
│   ├── qiniu/                    # 七牛云上传
│   └── redis/                    # Redis 连接
├── configs/config.yaml
├── go.mod
└── go.sum
```

### 2.4 配置

创建 `backend/configs/config.yaml`：

```yaml
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  password: "123456"
  dbname: agenthub
  charset: utf8mb4

jwt:
  secret: "dev-secret-key"
  expire_hours: 24

agentend:
  host: http://localhost
  port: 8001

redis:
  host: 127.0.0.1
  port: 6379
  password: ""
  db: 0
```

### 2.5 配置加载

创建 `backend/internal/conf/conf.go`：

```go
package conf

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type MySQLConfig struct { ... }
type JWTConfig struct { ... }
type AgentEndConfig struct { ... }
type RedisConfig struct { ... }
type QiniuConfig struct { ... }
type Config struct {
	MySQL    MySQLConfig    `yaml:"mysql"`
	JWT      JWTConfig      `yaml:"jwt"`
	AgentEnd AgentEndConfig `yaml:"agentend"`
	Redis    RedisConfig    `yaml:"redis"`
	Qiniu    QiniuConfig    `yaml:"qiniu"`
}

func Load(path string) (*Config, error) {
	_ = godotenv.Load()
	// YAML parse + env override for secrets
}
```

> 完整代码参见 `backend/internal/conf/conf.go`。

### 2.6 MySQL 连接

创建 `backend/pkg/db/mysql.go` — 单例 MySQL 连接。详见实际文件。

### 2.7 统一响应

创建 `backend/internal/vo/response.go` — 统一 API 响应格式。详见实际文件。

### 2.8 中间件

创建 `backend/internal/middleware/` 下的 `cors.go`、`logger.go`、`auth.go`、`rate_limit.go`。详见实际文件。

### 2.9 main.go

创建 `backend/cmd/server/main.go`：

```go
package main

import (
	"log/slog"
	"net/http"
	"os"

	"agenthub/backend/internal/conf"
	"agenthub/backend/internal/controller"
	"agenthub/backend/internal/middleware"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/stream"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/db"
	"agenthub/backend/pkg/qiniu"
	"agenthub/backend/pkg/redis"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := conf.Load("configs/config.yaml")
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	if err := db.Init(&cfg.MySQL); err != nil {
		slog.Error("init db", "error", err)
		// ...
	}
	// ...
}
```

> 完整代码参见 `backend/cmd/server/main.go`。

### 2.10 启动 Backend

```bash
# 开发模式（Air 热重载）
cd backend && air

# 或通过根目录 Makefile
make run-backend
```

验证：`curl http://localhost:8080/ping` → `{"message":"pong"}`

---

## 3. Frontend（React + Vite）— 从零初始化

### 3.1 创建 Vite 项目

```bash
# 在项目根目录执行
pnpm create vite frontend --template react-ts
```

### 3.2 安装核心依赖

```bash
cd frontend

# Tailwind CSS
pnpm add -D tailwindcss @tailwindcss/vite

# React Router
pnpm add react-router

# 状态管理
pnpm add zustand

# 数据请求
pnpm add @tanstack/react-query

# UI 组件库
npx shadcn@latest init
npx shadcn@latest add button card input dialog

# 图标
pnpm add lucide-react

# Markdown 渲染
pnpm add react-markdown remark-gfm

# 代码高亮
pnpm add shiki
```

### 3.3 目录结构

```
frontend/src/
├── components/         # 通用组件
│   ├── chat/           # 聊天相关组件
│   ├── im/             # IM 会话管理组件
│   ├── cards/          # 技能输出卡片（DiffCard, HtmlCard, ImageCard 等）
│   ├── diff/           # Diff 查看器（多文件 tab + CodeMirror 编辑）
│   ├── layout/         # 布局组件（AdminMenu, IconSidebar）
│   ├── markdown/       # Markdown 渲染组件
│   └── ui/             # shadcn/ui 组件（自动生成）
├── pages/              # 页面组件
├── hooks/              # 自定义 hooks
├── stores/             # Zustand stores
├── lib/                # API 调用 + SSE + 工具函数
├── utils/              # 工具函数（时间格式化等）
├── generated/          # 契约生成的类型文件
└── main.tsx            # 入口
```

### 3.4 Vite 代理配置

`frontend/vite.config.ts` 已配置代理：

```typescript
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
```

### 3.5 启动 Frontend

```bash
cd frontend
pnpm dev
```

验证：浏览器打开 `http://localhost:5173` → React 页面。

---

## 4. 基础设施 — MySQL + Redis

### 4.1 使用 Docker Compose（推荐）

创建项目根目录 `docker-compose.yml`：

```yaml
services:
  mysql:
    image: mysql:8.0
    ports:
      - "3306:3306"
    environment:
      MYSQL_ROOT_PASSWORD: "123456"
      MYSQL_DATABASE: agenthub
    volumes:
      - mysqldata:/var/lib/mysql

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

volumes:
  mysqldata:
```

启动：

```bash
docker compose up -d
```

### 4.2 手动安装（不用 Docker）

```bash
# MySQL
brew install mysql
brew services start mysql
mysql -u root -e "CREATE DATABASE agenthub;"

# Redis
brew install redis
brew services start redis
```

---

## 5. 环境变量（密钥注入）

项目不使用项目根 `.env`，密钥由各子项目自己的 `.env` 注入。模板已入库，复制即可：

```bash
# Backend — 七牛云密钥（可选，留空回退本地磁盘）
cp backend/.env.example backend/.env

# AgentEnd — DeepSeek API Key（Orchestrator 必填）+ LangSmith（可选）
cp agentend/.env.example agentend/.env
```

| 文件 | 字段 | 用途 |
|------|------|------|
| `backend/.env` | `QINIU_ACCESS_KEY` / `QINIU_SECRET_KEY` | 七牛云对象存储 |
| `agentend/.env` | `DS_MODEL` / `DS_BASE_URL` / `DS_API_KEY` | Orchestrator LLM |
| `agentend/.env` | `LANGSMITH_TRACING` / `LANGSMITH_API_KEY` / `LANGSMITH_PROJECT` | LangSmith 追踪（可选） |

> 主配置（MySQL/Redis/JWT/Admin/CORS 等）走各端 `config.yaml`，不在 `.env` 中。

---

## 6. Makefile

根目录 `Makefile` 已配置，通过 `scripts/run.sh` 统一管理三端服务。常用命令：

```bash
make run-frontend     # 启动前端（Vite HMR, :5173）
make run-backend      # 启动后端（Air 热重载, :8080）
make run-agentend     # 启动 Agent 端（uvicorn --reload, :8001）
make stop             # 停止全部服务
make status           # 查看三端运行状态
make generate         # 从 contracts/schemas/ 生成三端类型文件
make tidy             # 执行 go mod tidy
```

详见 [makefile-guide.md](makefile-guide.md)。

---

## 7. 热更新（Backend）

```bash
go install github.com/air-verse/air@latest
```

创建 `backend/.air.toml`（最小配置）：

```toml
[build]
  cmd = "go build -o ./tmp/main ./cmd/server"
  bin = "./tmp/main"
  include_ext = ["go"]
  exclude_dir = ["tmp", "vendor"]
```

之后用 `air` 代替 `go run`：

```bash
cd backend && air
```

---

## 8. 完整启动流程

```bash
# 1. 启动基础设施（MySQL + Redis）
# Docker Compose 或手动 brew services start mysql/redis

# 2. 安装依赖（首次）
cd frontend && pnpm install
cd backend && go mod download
cd agentend && uv sync

# 3. 分别启动（推荐开发时用，每个终端一个）
make run-backend     # → :8080
make run-agentend    # → :8001
make run-frontend    # → :5173
```

---

## 端口分配

| 服务         | 端口   |
| ------------ | ------ |
| Frontend     | 5173   |
| Backend      | 8080   |
| AgentEnd     | 8001   |
| MySQL        | 3306   |
| Redis        | 6379   |

---

## 验证清单

- [ ] `curl localhost:8080/ping` → `{"message":"pong"}`
- [ ] `curl localhost:8001/docs` → FastAPI Swagger
- [ ] 浏览器 `localhost:5173` → React 页面
- [ ] `docker compose ps` → mysql + redis running

---

## 常见问题

### MySQL 连接失败

```bash
# 检查是否运行
docker compose ps
# 或
brew services list | grep mysql

# 手动测试连接
mysql -u root -p agenthub
```

### Frontend 无法连接 Backend

检查 `frontend/vite.config.ts` 中的 proxy target 是否正确（默认 `http://localhost:8080`），重启 dev server。

### Go 依赖下载慢

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```
