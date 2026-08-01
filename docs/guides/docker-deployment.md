# Docker 容器化部署指南

> **目标**：Frontend + Backend + MySQL + Redis 跑在 Docker 容器中，Agentend 留在宿主机（需要本地文件系统 / git worktree）。

## 架构概览

```
┌──────────────────── Docker ────────────────────┐
│                                                 │
│  ┌──────────┐  /api/*  ┌──────────┐            │
│  │ Frontend │─────────►│ Backend  │            │
│  │ Nginx:80 │          │ Go:8080  │            │
│  └──────────┘          └────┬─────┘            │
│     :8787                    │                   │
│                    ┌────────┼────────┐          │
│                    ▼        ▼        ▼          │
│              ┌───────┐ ┌───────┐    ┌────────┐  │
│              │ MySQL │ │ Redis │    │AgentEnd│  │
│              │ :3306 │ │ :6379 │    │(宿主机) │  │
│              └───┬───┘ └───┬───┘    └────────┘  │
│                  │         │           ▲        │
└──────────────────┼─────────┼───────────┼────────┘
                   │         │           │
            端口映射到宿主机 localhost     │
                   │         │           │
            agentend 连 localhost:3306    │
            agentend 连 localhost:8080 ───┘
```

- **Frontend**：宿主机 `:8787` → 容器 `:80`（Nginx 反代 `/api/*` → Backend）
- **Backend**：宿主机 `:8080` → 容器 `:8080`
- **MySQL / Redis**：端口映射到宿主机，agentend 无需改动配置即可连接
- **Agentend**：宿主机本地运行，`make docker-up` 自动启动

## 文件结构

```
docker/
├── docker-compose.yml              # 编排文件
├── configs/
│   └── backend/
│       ├── config.yaml             # Backend 配置（构建时 COPY 进容器）— 入库
│       └── .env.example            # Backend 密钥模板（七牛云）— 入库
│       # .env 由 cp .env.example .env 生成（构建时 COPY 进容器）— 不入库
├── backend/
│   └── Dockerfile                  # 多阶段构建（Go build → Alpine runtime）
├── frontend/
│   ├── Dockerfile                  # 多阶段构建（pnpm build → Nginx runtime）
│   └── nginx.conf                  # SPA 路由 + /api 代理 + SSE 支持
└── scripts/
    └── precheck.sh                 # 启动前配置校验
```

## 快速开始

```bash
# 1. 修改配置文件（⚠️ 必须修改密码/密钥）
vim docker/configs/backend/config.yaml    # MySQL 密码、JWT 密钥、Admin 密码

# 2. 准备 .env
cp docker/configs/backend/.env.example docker/configs/backend/.env   # Backend 七牛云密钥
cp agentend/.env.example agentend/.env                                # Agentend LLM 密钥

# 3. 一键启动（校验 → 构建容器 → 启动容器 → 本地启动 agentend）
make docker-up

# 4. 访问 http://localhost:8787
```

`make docker-up` 会自动完成以下步骤：
1. 运行 `precheck.sh` 校验配置
2. `docker compose up --build -d` 构建并启动容器
3. `docker compose up --wait` 等待所有服务就绪（MySQL healthy）
4. `cd agentend && uv sync` 安装 agentend 依赖
5. `make run-agentend` 启动 agentend

## 配置文件说明

### docker/configs/backend/config.yaml

构建时 COPY 到容器的 `/app/configs/config.yaml`。与本地开发版本的区别：

| 字段 | 本地开发值 | Docker 值 | 说明 |
|------|-----------|-----------|------|
| `mysql.host` | `127.0.0.1` | `mysql` | Docker Compose 服务名 |
| `redis.host` | `127.0.0.1` | `redis` | Docker Compose 服务名 |
| `agentend.host` | `http://localhost` | `http://host.docker.internal` | 容器访问宿主机 |
| `cors.allow_origins` | `http://localhost:5173` | `http://localhost` + `http://localhost:8787` | Nginx 监听 80 端口，映射到 8787 |

**⚠️ 部署前必须修改**：
- `mysql.password` — 不能用 `123456`
- `jwt.secret` — 不能用 `agenthub-demo-secret`，用 `openssl rand -hex 32` 生成
- `admin.password` — 不能用 `123456`

`precheck.sh` 会按 YAML section 检查这些默认值并给出提醒；后端在 `APP_ENV=production` / `APP_ENV=prod` 或 `GIN_MODE=release` 下还会拒绝默认 JWT secret 和默认 Admin 密码，避免生产环境误启动。

生产部署建议启用普通 API Auth：将 `docker/configs/backend/config.yaml` 的 `auth.enabled` 改为 `true`，或为 backend 容器设置 `APP_ENV=production` / `API_AUTH_ENABLED=true`。

### docker/configs/backend/.env

构建时 COPY 到容器的 `/app/.env`，由 backend `godotenv` 加载。仅放七牛云密钥：

```bash
cp docker/configs/backend/.env.example docker/configs/backend/.env
# 然后编辑填入实际密钥；留空则容器内 backend 自动回退到本地磁盘存储
```

> 此文件是 Docker 构建的硬依赖（`docker/backend/Dockerfile` 第 26 行 `COPY docker/configs/backend/.env .env`），不存在则构建失败。

### docker-compose.yml 中的密码

`docker-compose.yml` 中 MySQL 的 `MYSQL_ROOT_PASSWORD` 需要与 `configs/backend/config.yaml` 中的 `mysql.password` 保持一致。

### agentend/.env

宿主机 agentend 运行时加载，需配置 LLM 密钥（`DS_API_KEY` 等）：

```bash
cp agentend/.env.example agentend/.env
# 编辑填入实际密钥
```

## 启动前校验（precheck.sh）

`make docker-up` 会自动运行 `precheck.sh`：

```
$ make docker-up
=== AgentHub Docker 部署校验 ===

[1/3] 检查配置文件
  ✓ backend: config.yaml
  ✓ backend: .env
  ✓ agentend/.env

[2/3] 检查配置安全性
  ⚠ backend MySQL 密码 仍为默认值 (123456)
  ✓ backend JWT 密钥
  ✓ backend Admin 密码
  ✓ agentend DS_API_KEY

[3/3] 检查 Docker 环境
  ✓ Docker 已运行
  ✓ docker compose 可用

================================
校验通过，1 个提醒

Docker 启动后，运行 agentend:
  cd agentend && uv sync && cd ..
  make run-agentend

是否继续启动 Docker？[y/N]
```

校验内容：
1. **配置文件是否存在**（缺失则阻断）
2. **密码/密钥是否仍为默认危险值**（仅提醒，不阻断）
3. **Docker 是否安装并运行**（缺失则阻断）

## Makefile 命令

| 命令 | 说明 |
|------|------|
| `make docker-up` | 校验配置 + 构建并启动容器 + 本地启动 agentend |
| `make docker-down` | 停止并移除容器 |
| `make docker-build` | 仅构建镜像（不启动） |
| `make docker-logs` | 查看容器实时日志 |
| `make docker-status` | 查看容器运行状态 |

## 注意事项

- **启动顺序**：`make docker-up` 已自动编排——先等 MySQL healthy，再启动 agentend
- **数据持久化**：MySQL 和 Redis 数据存储在 Docker named volume 中，`docker compose down` 不会丢失
- **完全清空数据**：`cd docker && docker compose down -v`（`-v` 删除 volume）
- **源码无需改动**：Backend、Frontend、Agentend 的源码完全不改，仅通过 `docker/configs/` 覆盖配置
