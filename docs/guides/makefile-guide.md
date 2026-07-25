# Makefile 使用指南

根目录 `Makefile` 通过 `scripts/run.sh` 脚本统一管理前端、后端、Agent 三端服务的启停与状态查看。

## 运行机制

- 脚本通过 `lsof` 检测端口占用判断服务是否运行，无需 PID 文件
- 启动前检测端口 + 进程是否存活，已运行则跳过
- `make all` 或 `make` 可同时启动全部服务
- 也可通过 `make run-<service>` 单独启动某个服务

## 命令一览

### 启动

| 命令 | 服务 | 热重载工具 | 端口 |
|------|------|-----------|------|
| `make` 或 `make all` | 启动全部服务 | — | — |
| `make run-frontend` | Vite dev server | Vite 内置 HMR | localhost:5173 |
| `make run-backend` | Go server | Air | localhost:8080 |
| `make run-agentend` | FastAPI server | uvicorn --reload | localhost:8001 |

### 停止

| 命令 | 说明 |
|------|------|
| `make stop` | 停止全部服务 |
| `make stop-frontend` | 停止前端 |
| `make stop-backend` | 停止后端 |
| `make stop-agentend` | 停止 Agent 端 |

### 重启

| 命令 | 说明 |
|------|------|
| `make restart` | 重启全部服务 |
| `make restart-frontend` | 重启前端 |
| `make restart-backend` | 重启后端 |
| `make restart-agentend` | 重启 Agent 端 |

### 其他

| 命令 | 说明 |
|------|------|
| `make status` | 查看三端运行状态与 PID |
| `make tidy` | 执行 `go mod tidy` |
| `make generate` | 从 `contracts/schemas/*.yaml` 生成三端类型文件（Python / TypeScript / Go） |
| `make wsl` | 打印 WSL2 从 Windows 浏览器访问的配置说明（只展示，不执行） |

### Docker 部署

| 命令 | 说明 |
|------|------|
| `make docker-up` | 启动前校验 + 构建并启动容器（前后端 + MySQL + Redis）+ 等待就绪后启动 agentend |
| `make docker-down` | 停止并移除容器 |
| `make docker-build` | 仅构建镜像（不启动） |
| `make docker-logs` | 查看容器实时日志 |
| `make docker-status` | 查看容器运行状态 |

> Docker 配置文件位于 `docker/configs/`，启动前请参考 [docker-deployment.md](docker-deployment.md)。

## 直接使用脚本

```bash
./scripts/run.sh start <frontend|backend|agentend>
./scripts/run.sh stop [<frontend|backend|agentend>]
./scripts/run.sh restart <frontend|backend|agentend>
./scripts/run.sh status
```
