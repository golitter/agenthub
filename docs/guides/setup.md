# Setup Guide

本文是当前项目的最短可用启动指南。历史上的“从零创建目录 / 初始化框架”步骤已移出本文，避免和实际代码、AGENTS.md、Makefile 重复维护。

## 前置依赖

| 依赖 | 用途 |
|------|------|
| Node.js + pnpm | Frontend：React + Vite |
| Go 1.26+ | Backend 与内置 skill CLI |
| Python 3.10+ + uv | AgentEnd |
| MySQL 8+ | Backend 业务数据 |
| Redis 7+ | SSE 流式补偿 |
| Docker / Docker Compose | 可选：容器化运行 MySQL、Redis、MinIO、Backend、Frontend |

当前技术栈版本以这些文件为准：

- `frontend/package.json`
- `backend/go.mod`
- `agentend/pyproject.toml`

## 首次准备

```bash
# 1. 安装前端依赖
cd frontend
pnpm install
cd ..

# 2. 安装 AgentEnd 依赖
cd agentend
uv sync
cd ..

# 3. 构建内置 skill CLI
make build-skills

# 4. 准备环境变量
cp backend/.env.example backend/.env
cp agentend/.env.example agentend/.env
```

需要填写的密钥：

| 文件 | 变量 | 说明 |
|------|------|------|
| `backend/.env` | `QINIU_ACCESS_KEY` / `QINIU_SECRET_KEY` | 可选；留空回退本地磁盘存储 |
| `agentend/.env` | `DS_API_KEY` | Orchestrator LLM 必填 |
| `agentend/.env` | `LANGFUSE_PUBLIC_KEY` / `LANGFUSE_SECRET_KEY` | 可选；未配置不影响主流程 |

Backend 主配置在 `backend/configs/config.yaml`，AgentEnd 主配置在 `agentend/config.yaml`，Agent CLI 路径在 `agentend/agents.json`。

## 启动基础设施

开发环境可以自行启动 MySQL / Redis，也可以使用 Docker。

```bash
# 使用 Docker 部署相关容器
make docker-up

# 停止 Docker 容器
make docker-down
```

如果只想手动启动基础设施，请确保：

| 服务 | 默认端口 |
|------|----------|
| MySQL | `3306` |
| Redis | `6379` |

## 启动三端

推荐通过根目录 Makefile 启动：

```bash
make run-agentend
make run-backend
make run-frontend
```

常用命令：

| 命令 | 说明 |
|------|------|
| `make run-frontend` | 启动 Vite dev server，默认 `localhost:5173` |
| `make run-backend` | 启动 Go Backend，默认 `localhost:8080` |
| `make run-agentend` | 启动 AgentEnd，默认 `localhost:8001` |
| `make stop` | 停止三端服务 |
| `make restart` | 重启三端服务 |
| `make status` | 查看端口和 PID |
| `make generate` | 从 `contracts/schemas/` 生成三端类型 |
| `make build-skills` | 构建内置 taskctl / render CLI |

完整命令说明见 `docs/guides/makefile-guide.md`。

## 端口分配

| 服务 | 端口 | 健康检查 / 入口 |
|------|------|----------------|
| Frontend | `5173` | `http://localhost:5173` |
| Backend | `8080` | `GET /health`、`GET /ping` |
| AgentEnd | `8001` | `GET /health` |
| MySQL | `3306` | Backend 配置 |
| Redis | `6379` | Backend 配置 |

## 日志与排障

`scripts/run.sh` 启动的服务日志在根目录 `logs/`：

| 文件 | 内容 |
|------|------|
| `logs/frontend.log` | Vite dev server |
| `logs/backend.log` | Air / Go Backend |
| `logs/agentend.log` | uvicorn / AgentEnd |

常见检查：

```bash
make status
curl http://localhost:8080/health
curl http://localhost:8001/health
```

## 契约变更流程

跨端协议必须走契约层：

```bash
# 1. 修改 contracts/schemas/*.yaml
# 2. 生成三端类型
make generate
# 3. 在 contracts/logs/ 记录变更
```

详见 `docs/guides/contract-layer.md` 与 `contracts/AGENTS.md`。
