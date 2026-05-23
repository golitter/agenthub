# Setup Guide — AI Runtime Platform

三层架构一键搭建指南：Frontend (Next.js) + Backend (Go) + AgentEnd (Python)。

---

## 前置依赖

| 工具       | 版本要求     | 安装                                |
| ---------- | ---------- | ----------------------------------- | ------- |
| Go         | >= 1.22    | `brew install go`                   |
| Node.js    | >= 18      | `brew install node`                 |
| pnpm       | >= 8       | `npm i -g pnpm`                     |
| Python     | >= 3.10    | 系统自带 / `brew install python`     |
| uv         | latest     | `brew install uv`                   |
| PostgreSQL | >= 15      | `brew install postgresql@16`        |
| Redis      | >= 7       | `brew install redis`                |
| Docker     | optional   | `brew install --cask docker`        |

验证：

```bash
go version && node --version && pnpm --version && uv --version
```

---

## 目录结构（目标）

```
bytedanceai/
├── frontend/          # Next.js + Tailwind + shadcn/ui
├── backend/           # Go + Gin + GORM
├── agentend/          # Python FastAPI（已有）
├── docs/
├── scripts/
├── docker-compose.yml
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
uv run uvicorn src.api.app:app --host 0.0.0.0 --port 8001 --reload
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
# Web 框架
go get github.com/gin-gonic/gin

# ORM + 数据库驱动
go get gorm.io/gorm
go get gorm.io/driver/postgres

# 日志
go get go.uber.org/zap

# 配置
go get github.com/spf13/viper

# UUID
go get github.com/google/uuid

# 参数校验
go get github.com/go-playground/validator/v10

# JWT
go get github.com/golang-jwt/jwt/v5

# 环境变量
go get github.com/joho/godotenv

# CORS
go get github.com/gin-contrib/cors
```

一条命令版本：

```bash
go get github.com/gin-gonic/gin \
       gorm.io/gorm \
       gorm.io/driver/postgres \
       go.uber.org/zap \
       github.com/spf13/viper \
       github.com/google/uuid \
       github.com/go-playground/validator/v10 \
       github.com/golang-jwt/jwt/v5 \
       github.com/joho/godotenv \
       github.com/gin-contrib/cors
```

### 2.3 创建目录结构

```bash
cd backend
mkdir -p cmd/server
mkdir -p internal/{api,middleware,service,config,models}
mkdir -p configs
```

目标结构：

```
backend/
├── cmd/server/main.go
├── internal/
│   ├── api/           # HTTP handlers
│   ├── middleware/     # Gin 中间件（CORS, Auth, Logger）
│   ├── service/       # 业务逻辑
│   ├── config/        # Viper 配置加载
│   └── models/        # GORM models
├── configs/
│   └── config.yaml
├── go.mod
└── go.sum
```

### 2.4 最小 main.go

创建 `backend/cmd/server/main.go`：

```go
package main

import (
	"log"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// --- Database ---
	dsn := "host=localhost user=postgres password=postgres dbname=agenthub port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}
	log.Println("database connected")

	// --- Router ---
	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	// --- Health ---
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	// --- Proxy to AgentEnd ---
	r.Any("/api/agent/*path", proxyAgentEnd("http://localhost:8001"))

	// --- Start ---
	log.Println("backend running on :8080")
	r.Run(":8080")
}

func proxyAgentEnd(target string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 简单反向代理占位，后续用 httputil.ReverseProxy 替换
		c.JSON(http.StatusOK, gin.H{
			"proxy":  "agentend",
			"target": target,
			"path":   c.Param("path"),
		})
	}
}
```

### 2.5 启动 Backend

```bash
cd backend
go run cmd/server/main.go
```

验证：`curl http://localhost:8080/ping` → `{"message":"pong"}`

---

## 3. Frontend（Next.js）— 从零初始化

### 3.1 创建 Next.js 项目

```bash
# 在项目根目录执行
npx create-next-app@latest frontend \
  --typescript \
  --tailwind \
  --eslint \
  --app \
  --src-dir \
  --use-pnpm \
  --turbopack
```

交互项全部选 YES（import alias 选默认 `@/*`）。

### 3.2 安装核心依赖

```bash
cd frontend

# 状态管理
pnpm add zustand

# 数据请求
pnpm add @tanstack/react-query

# UI 组件库
npx shadcn@latest init
# Style: Default, Base color: Slate, CSS variables: Yes

# 常用组件
npx shadcn@latest add button card input dialog

# 图标
pnpm add lucide-react

# Markdown 渲染
pnpm add react-markdown remark-gfm
```

### 3.3 目录结构

```
frontend/src/
├── app/                # Next.js App Router pages
│   ├── layout.tsx
│   ├── page.tsx
│   └── globals.css
├── components/         # 通用组件
│   └── ui/             # shadcn/ui 组件（自动生成）
├── features/           # 业务模块
│   ├── chat/
│   ├── runtime/
│   └── agent/
├── hooks/              # 自定义 hooks
├── stores/             # Zustand stores
├── services/           # API 调用封装
├── types/              # TypeScript 类型
└── lib/                # 工具函数
```

### 3.4 最小 API 服务层

创建 `frontend/src/services/api.ts`：

```typescript
const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

export async function fetchAPI<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export const api = {
  ping: () => fetchAPI<{ message: string }>("/ping"),
};
```

### 3.5 环境变量

创建 `frontend/.env.local`：

```env
NEXT_PUBLIC_API_URL=http://localhost:8080
```

### 3.6 启动 Frontend

```bash
cd frontend
pnpm dev
```

验证：浏览器打开 `http://localhost:3000` → Next.js 默认页面。

---

## 4. 基础设施 — PostgreSQL + Redis

### 4.1 使用 Docker Compose（推荐）

创建项目根目录 `docker-compose.yml`：

```yaml
services:
  postgres:
    image: postgres:16
    ports:
      - "5432:5432"
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: agenthub
    volumes:
      - pgdata:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

volumes:
  pgdata:
```

启动：

```bash
docker compose up -d
```

### 4.2 手动安装（不用 Docker）

```bash
# PostgreSQL
brew install postgresql@16
brew services start postgresql@16
createdb agenthub

# Redis
brew install redis
brew services start redis
```

---

## 5. 统一环境变量

创建项目根目录 `.env`：

```env
# Backend
DATABASE_URL=postgres://postgres:postgres@localhost:5432/agenthub?sslmode=disable
REDIS_URL=redis://localhost:6379
AGENTEND_URL=http://localhost:8001

# Frontend
NEXT_PUBLIC_API_URL=http://localhost:8080
```

---

## 6. Makefile

创建项目根目录 `Makefile`：

```makefile
.PHONY: dev frontend backend agentend db migrate

# 一键启动所有服务
dev:
	@make -j3 _frontend _backend _agentend

_frontend:
	cd frontend && pnpm dev

_backend:
	cd backend && go run cmd/server/main.go

_agentend:
	cd agentend && uv run uvicorn src.api.app:app --host 0.0.0.0 --port 8001 --reload

# 单独启动
frontend:
	cd frontend && pnpm dev

backend:
	cd backend && go run cmd/server/main.go

agentend:
	cd agentend && uv run uvicorn src.api.app:app --host 0.0.0.0 --port 8001 --reload

# 数据库
db:
	docker compose up -d

db-down:
	docker compose down

# 安装所有依赖
install:
	cd frontend && pnpm install
	cd backend && go mod download
	cd agentend && uv sync
```

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
# 1. 启动基础设施
make db

# 2. 安装依赖（首次）
make install

# 3. 启动所有服务
make dev

# 或者分别启动（推荐开发时用，每个终端一个）
make backend     # → :8080
make agentend    # → :8001
make frontend    # → :3000
```

---

## 端口分配

| 服务         | 端口   |
| ------------ | ---- |
| Frontend     | 3000 |
| Backend      | 8080 |
| AgentEnd     | 8001 |
| PostgreSQL   | 5432 |
| Redis        | 6379 |

---

## 验证清单

- [ ] `curl localhost:8080/ping` → `{"message":"pong"}`
- [ ] `curl localhost:8001/docs` → FastAPI Swagger
- [ ] 浏览器 `localhost:3000` → Next.js 页面
- [ ] `docker compose ps` → postgres + redis running

---

## 常见问题

### PostgreSQL 连接失败

```bash
# 检查是否运行
docker compose ps
# 或
brew services list | grep postgres

# 手动测试连接
psql postgres://postgres:postgres@localhost:5432/agenthub
```

### Frontend 无法连接 Backend

检查 `frontend/.env.local` 中 `NEXT_PUBLIC_API_URL` 是否正确，重启 dev server。

### Go 依赖下载慢

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```
