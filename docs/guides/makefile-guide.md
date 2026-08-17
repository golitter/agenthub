# Makefile 使用指南

根目录 `Makefile` 统一管理前端、后端、Agent 三端服务以及独立配置中心的启动与验收。

## 运行机制

- 脚本通过 `ss`（优先）与 `lsof`（回退）检测端口占用判断服务是否运行，无需 PID 文件
- 启动前检测端口是否已被监听，已在运行则跳过
- `make all` 或 `make` 可同时启动全部服务
- 也可通过 `make run-<service>` 单独启动某个服务
- Agent 端依赖本地构建的内置 skill CLI；首次启动 Agent 端前需执行 `make build-skills`

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
| `make build-skills` | 构建内置 skill CLI（`taskctl` / `render`，本地产物不入库） |
| `make skill-migrate ARGS="--dry-run --batch-size 10"` | 分批迁移/校验历史 Skill BLOB；观察期后可用 `--clear-content --confirm-clear-content=CLEAR-SKILL-BLOBS` 显式清理 |
| `make skill-reconcile ARGS="--verify"` | 对账 MinIO、MySQL、过期 incoming（默认不删除对象；异常会标记并排入补偿）；显式 `--repair` 才清理 |
| `make check-skills` | 检查内置 skill CLI 是否已构建 |
| `make wsl` | 打印 WSL2 从 Windows 浏览器访问的配置说明（只展示，不执行） |
| `make config-center` | 启动独立的 example/actual 配置编辑器（Web 5174 / API 9100） |
| `make test-config-center` | 运行配置中心 Python、Web 测试及 Vite production build |

服务器上的 `pnpm` 不在 PATH 时，先创建一次本机环境文件：

```bash
cp scripts/server-env.example.sh scripts/server-env.sh
# 编辑 scripts/server-env.sh 中的 PNPM
make config-center
```

`scripts/server-env.sh` 被 Git 忽略，各开发环境互不影响。相关 Make recipe 会在文件存在时于同一个 Bash 中先 source，随后执行 Config Center 或 `scripts/run.sh`；文件不存在时直接使用 PATH 中的 `pnpm`。直接绕过 Make 使用脚本时，才需要手动执行 `source scripts/server-env.sh`。

### Docker 部署

| 命令 | 说明 |
|------|------|
| `make docker-up` | 启动前校验 + 构建并启动容器（前后端 + MySQL + Redis + MinIO）+ 等待就绪后启动 agentend |
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
