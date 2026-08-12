# Docker 容器化部署指南

> **目标**：Frontend + Backend + MySQL + Redis + MinIO 跑在 Docker 容器中，Agentend 留在宿主机（需要本地文件系统 / git worktree）。

## 架构概览

```
┌─────────────────────── Docker ────────────────────────┐
│                                                        │
│   ┌──────────┐  /api/*  ┌──────────┐                  │
│   │ Frontend │─────────►│ Backend  │                  │
│   │ Nginx:80 │          │ Go:8080  │                  │
│   └──────────┘          └────┬─────┘                  │
│      :8787                  │                          │
│                   ┌─────────┼──────────┐               │
│                   ▼         ▼          ▼               │
│             ┌───────┐ ┌───────┐  ┌─────────┐          │
│             │ MySQL │ │ Redis │  │  MinIO  │          │
│             │ :3306 │ │ :6379 │  │ :9000   │          │
│             └───┬───┘ └───┬───┘  └────┬────┘          │
│                 │         │           │ :9001 console  │
└─────────────────┼─────────┼───────────┼────────────────┘
                  │         │           │
           端口映射到宿主机 localhost    │
                  │         │           │
           agentend 连 localhost:3306    │
           agentend 连 localhost:8080 ───┘
           (AgentEnd 运行在宿主机)
```

- **Frontend**：宿主机 `:8787` → 容器 `:80`（Nginx 反代 `/api/*` 与 `/uploads/*` → Backend）
- **Backend**：宿主机 `:8080` → 容器 `:8080`
- **MySQL / Redis**：端口映射到宿主机，agentend 无需改动配置即可连接
- **MinIO**：宿主机 `127.0.0.1:9000`（API）/ `127.0.0.1:9001`（Console）→ 容器同端口，仅绑定 localhost；一次性 `minio-init` 容器按功能开关创建私有 Bucket 和互相隔离的应用账号
- **Agentend**：宿主机本地运行，`make docker-up` 自动启动

## 文件结构

```
docker/
├── docker-compose.yml              # 编排文件
├── .env.example                    # Compose 插值与 MinIO Root 凭据模板 — 入库
├── configs/
│   └── backend/
│       ├── config.yaml             # Backend 配置（构建时 COPY 进容器）— 入库
│       ├── config.example.yaml     # 配置模板（敏感值留空）— 入库
│       └── .env.example            # Backend 密钥模板（Avatar MinIO/Skill MinIO）— 入库
│       # .env 由 cp .env.example .env 生成（Compose 运行时注入）— 不入库
├── backend/
│   └── Dockerfile                  # 多阶段构建（Go build → Alpine runtime）
├── frontend/
│   ├── Dockerfile                  # 多阶段构建（pnpm build → Nginx runtime）
│   └── nginx.conf                  # SPA 路由 + /api 代理 + SSE 支持
├── certs/                          # 可选：MinIO 客户端 CA（挂载到 Backend，只读）
├── minio/
│   ├── init.sh                     # minio-init 容器：建 bucket + 应用用户最小权限策略
│   ├── skill-package-policy.json   # skill-packages bucket 最小权限策略
│   ├── avatar-policy.json          # agenthub-assets/avatars 最小权限策略
│   ├── backup.sh                   # MinIO 对象清单 + MySQL 快照标识备份
│   └── restore.sh                  # 对象镜像恢复（只读，不替代数据库恢复/对账）
└── scripts/
    └── precheck.sh                 # 启动前配置校验
```

## 快速开始

```bash
# 1. 修改配置文件（⚠️ 必须修改密码/密钥）
vim docker/configs/backend/config.yaml    # MySQL 密码、JWT 密钥、Admin 密码

# 2. 准备 Compose 插值和应用密钥
cp docker/.env.example docker/.env                                      # Compose 插值；仅 minio/minio-init 读取 Root 凭据
cp docker/configs/backend/.env.example docker/configs/backend/.env   # Backend Avatar MinIO/Skill MinIO 应用密钥
cp agentend/.env.example agentend/.env                                # Agentend LLM 密钥

# 3. 一键启动（校验 → 构建容器 → 启动容器 → 本地启动 agentend）
make docker-up

# 4. 访问 http://localhost:8787
```

`make docker-up` 会自动完成以下步骤：
1. `make check-skills` 检查内置 skill CLI（taskctl / render）是否已构建
2. 运行 `precheck.sh` 校验配置
3. `docker compose up --build -d` 构建并启动容器
4. `docker compose up --wait` 等待所有服务就绪（含 MinIO 初始化任务）
5. `cd agentend && uv sync` 安装 agentend 依赖
6. `./scripts/run.sh start agentend` 启动 agentend

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

### docker/.env

Compose 会从 `docker/` 目录读取这个文件，用于 MinIO Root 凭据和服务级插值。
`MINIO_ROOT_USER`/`MINIO_ROOT_PASSWORD` 只注入 `minio` 与一次性 `minio-init`，不会进入
Backend 容器；生产环境必须替换示例值，并优先改为 Secret 管理。

### docker/configs/backend/.env

由 Compose `env_file` 在运行时注入 Backend，不会 COPY 到镜像层。这里填写 Avatar MinIO
和 Skill MinIO 的应用级凭据；不要放 MinIO Root 凭据：

```bash
cp docker/configs/backend/.env.example docker/configs/backend/.env
# 编辑填入 ASSET_MINIO_* 与（启用 Skill 时）MINIO_* 应用凭据；头像
# write_provider / enabled / endpoint 等 Compose 覆盖项在 docker/.env 配置
# MinIO 写入模式默认只读挂载历史 uploads；切换本地写入时在 docker/.env 设置
# AVATAR_STORAGE_WRITE_PROVIDER=local、LOCAL_STORAGE_VOLUME_MODE=rw。
```

> `make docker-up` 的 `precheck.sh` 会要求此文件存在；它只作为运行时 `env_file` 使用，
> 不会写入镜像层。

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
  ✓ docker/.env
  ✓ agentend/.env

[2/3] 检查配置安全性
  ⚠ backend MySQL 密码 仍为默认值 (123456)
  ✓ backend JWT 密钥
  ✓ backend Admin 密钥
  ✓ agentend DS_API_KEY

[3/3] 检查 Docker 环境
  ✓ Docker 已运行
  ✓ docker compose 可用

================================
校验通过，1 个提醒

  需要关注的配置文件:
    docker/configs/backend/config.yaml    → MySQL 密码、JWT 密钥、Admin 密码
    agentend/.env                         → DS_API_KEY（LLM 密钥）

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

- **启动顺序**：`make docker-up` 已自动编排——先等 MySQL、MinIO 和初始化任务完成，再启动 Backend/agentend
- **数据持久化**：MySQL、Redis、MinIO 对象、Backend 头像 `uploads/` 和 Skill 临时目录都存储在 Docker named volume 中，`docker compose down` 不会丢失；临时目录卷也避免容器重建时误用宿主机系统临时目录
- **头像 uploads 挂载**：默认 MinIO 写入只以 `ro` 挂载历史 `uploads/`；只有显式切换
  `AVATAR_STORAGE_WRITE_PROVIDER=local` 时才设置 `LOCAL_STORAGE_VOLUME_MODE=rw`，并由预检阻断
  忘记切换挂载权限的配置。
- **私有对象存储**：Compose 会先启动 MinIO，再由一次性 `minio-init` 使用 Root 凭据按开关创建
  `agenthub-assets` 与 `skill-packages` 私有 Bucket 和最小权限应用用户；仅在对应
  `ASSET_MINIO_ENABLED` / `SKILL_STORAGE_ENABLED` 开启时创建对应资源。Backend 与
  `minio-init` 分别读取 `docker/configs/backend/.env` 中的 `ASSET_MINIO_ACCESS_KEY` /
  `ASSET_MINIO_SECRET_KEY` 与（启用 Skill 时）`MINIO_ACCESS_KEY`/`MINIO_SECRET_KEY`，不要把
  Root 凭据注入 Backend。Asset 账号只允许 `avatars/*` 的 Get/Put/Stat，Skill 账号不能访问
  `agenthub-assets`。
  生产 TLS 通过 `ASSET_MINIO_USE_SSL=true` / `MINIO_USE_SSL=true` 和挂载到 Backend 的
  `ASSET_MINIO_CA_CERT` / `MINIO_CA_CERT`（或对应 `ca_file`）校验证书。把 CA 文件放到
  `docker/certs/`，并在 `docker/.env` 使用容器路径 `/etc/agenthub/certs/<filename>`；该目录以
  只读方式挂载。MinIO API/Console 不应直接暴露到公网。
- **完全清空数据**：`cd docker && docker compose down -v`（`-v` 删除 volume）
