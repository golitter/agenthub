# Docker 容器化部署指南

> **目标**：Frontend + Backend + MySQL + Redis 跑在 Docker 容器中，Agentend 留在宿主机（需要本地文件系统/git worktree）。

## 架构概览

```
┌──────────────────── Docker ────────────────────┐
│                                                 │
│  ┌──────────┐  /api/*  ┌──────────┐            │
│  │ Frontend │─────────►│ Backend  │            │
│  │ Nginx:80 │          │ Go:8080  │            │
│  └──────────┘          └────┬─────┘            │
│                             │                   │
│                    ┌────────┼────────┐          │
│                    ▼        ▼        ▼          │
│              ┌───────┐ ┌───────┐ ┌──────────┐  │
│              │ MySQL │ │ Redis │ │ Agentend │  │
│              │ :3306 │ │ :6379 │ │ (宿主机)  │  │
│              └───┬───┘ └───┬───┘ └──────────┘  │
│                  │         │           ▲        │
└──────────────────┼─────────┼───────────┼────────┘
                   │         │           │
            端口映射到宿主机 localhost       │
                   │         │           │
                   ▼         ▼           │
              agentend 连 localhost:3306  │
              agentend 连 localhost:8080 ─┘
```

**核心决策**：MySQL/Redis 端口映射到宿主机 → agentend 的 `config.yaml` **无需任何改动**（与本地开发一致）。

## 文件结构

```
docker/
├── docker-compose.yml              # 编排文件
├── configs/
│   ├── backend/
│   │   └── config.yaml             # Backend 配置（构建时 COPY 进容器）
│   └── agentend/
│       └── config.yaml             # Agentend 配置（本地启动前 cp 到 agentend/）
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
vim docker/configs/agentend/config.yaml   # MySQL 密码（与 backend 一致）

# 2. 启动 Docker 容器（自动校验配置 → 构建 → 启动）
make docker-up

# 3. 等待所有服务就绪后，启动本地 agentend
cp docker/configs/agentend/config.yaml agentend/config.yaml
make run-agentend

# 4. 访问 http://localhost
```

## 配置文件说明

用户只需要关心 `docker/configs/` 下的两个文件：

### docker/configs/backend/config.yaml

构建时 COPY 到容器的 `/app/configs/config.yaml`。与本地开发版本的区别：

| 字段 | 本地开发值 | Docker 值 | 说明 |
|------|-----------|-----------|------|
| `mysql.host` | `127.0.0.1` | `mysql` | Docker Compose 服务名 |
| `redis.host` | `127.0.0.1` | `redis` | Docker Compose 服务名 |
| `agentend.host` | `http://localhost` | `http://host.docker.internal` | 容器访问宿主机 |
| `cors.allow_origins` | `http://localhost:5173` | `http://localhost` | Nginx 监听 80 端口 |

**⚠️ 部署前必须修改**：
- `mysql.password` — 不能用 `123456`
- `jwt.secret` — 不能用 `agenthub-demo-secret`，用 `openssl rand -hex 32` 生成
- `admin.password` — 不能用 `123456`

### docker/configs/agentend/config.yaml

本地 agentend 运行时的配置。与本地开发版本**几乎一样**，因为 MySQL/Redis 端口映射到宿主机。

**⚠️ 部署前必须修改**：
- `database.password` — 与 backend 的 MySQL 密码保持一致
- `agents.*.config_path` — 填写实际的 CLI 配置路径

启动前需要复制到 agentend 目录：
```bash
cp docker/configs/agentend/config.yaml agentend/config.yaml
```

### docker-compose.yml 中的密码

`docker-compose.yml` 中 MySQL 的 `MYSQL_ROOT_PASSWORD` 也需要与 `configs/backend/config.yaml` 中的 `mysql.password` 保持一致。

## 启动前校验（precheck.sh）

`make docker-up` 会自动运行 `precheck.sh`：

```
$ make docker-up
=== AgentHub Docker 部署校验 ===

[1/4] 检查配置文件
  ✓ backend: config.yaml
  ✓ agentend: config.yaml

[2/4] 检查配置安全性
  ✓ backend MySQL 密码
  ✓ backend JWT 密钥
  ✓ backend Admin 密码
  ✓ agentend MySQL 密码

[3/4] 检查 Docker 环境
  ✓ Docker 已运行
  ✓ docker compose 可用

[4/4] 检查 AgentEnd 连通性
  ⚠ agentend 不可达 (localhost:8001)
    这是正常的，请确保 Docker 启动后再运行 make run-agentend

================================
校验通过 (1 个警告)
```

校验内容：
1. 配置文件是否存在
2. 密码/密钥是否仍为默认危险值（`123456`、`agenthub-demo-secret`）
3. Docker 是否安装并运行
4. agentend 是否可达（可选，warning 级别）

## Makefile 命令

| 命令 | 说明 |
|------|------|
| `make docker-up` | 校验配置 + 构建并启动容器 |
| `make docker-down` | 停止并移除容器 |
| `make docker-build` | 仅构建镜像 |
| `make docker-logs` | 查看实时日志 |
| `make docker-status` | 查看容器状态 |

## 注意事项

- **启动顺序**：先 `make docker-up`（等待 MySQL healthy），再 `make run-agentend`
- **数据持久化**：MySQL 和 Redis 数据存储在 Docker named volume 中，`docker compose down` 不会丢失
- **完全清空数据**：`cd docker && docker compose down -v`（`-v` 删除 volume）
- **源码无需改动**：Backend、Frontend、Agentend 的源码完全不改
