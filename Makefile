.PHONY: all run-frontend run-backend run-agentend \
       stop stop-frontend stop-backend stop-agentend \
       restart restart-frontend restart-backend restart-agentend \
       status tidy generate \
       docker-up docker-down docker-build docker-logs docker-status

SCRIPT := ./scripts/run.sh

# 默认：启动全部服务
all:
	$(SCRIPT) start

# 启动前端（热重载）— Vite dev server，localhost:5173
run-frontend:
	$(SCRIPT) start frontend

# 启动后端（热重载）— Air，localhost:8080
run-backend:
	$(SCRIPT) start backend

# 启动 Agent 端（热重载）— uvicorn --reload，localhost:8001
run-agentend:
	$(SCRIPT) start agentend

# 停止全部服务
stop:
	$(SCRIPT) stop

# 停止前端
stop-frontend:
	$(SCRIPT) stop frontend

# 停止后端
stop-backend:
	$(SCRIPT) stop backend

# 停止 Agent 端
stop-agentend:
	$(SCRIPT) stop agentend

# 重启全部服务
restart:
	$(SCRIPT) restart

# 重启前端（热重载）
restart-frontend:
	$(SCRIPT) restart frontend

# 重启后端（热重载）
restart-backend:
	$(SCRIPT) restart backend

# 重启 Agent 端（热重载）
restart-agentend:
	$(SCRIPT) restart agentend

# 查看三端运行状态（端口 + PID）
status:
	$(SCRIPT) status

# 整理 Go 依赖（go mod tidy）
tidy:
	cd backend && go mod tidy

# 从 contracts/schemas/ 生成三端类型文件（Python / TypeScript / Go）
generate:
	python3 scripts/generate_contracts.py

# ─── Docker 部署命令 ───────────────────────────────────────
# 前后端 + MySQL + Redis 跑在 Docker，Agentend 跑在本地
# 配置文件在 docker/configs/ 下，启动前请先检查

# Docker 启动前校验 + 构建并启动容器 + 等待就绪后启动 agentend
docker-up:
	docker/scripts/precheck.sh && cd docker && docker compose up --build -d && docker compose up --wait && cd .. && cd agentend && uv sync && cd .. && $(SCRIPT) start agentend

# 停止并移除容器
docker-down:
	cd docker && docker compose down

# 仅构建镜像（不启动）
docker-build:
	cd docker && docker compose build

# 查看容器实时日志
docker-logs:
	cd docker && docker compose logs -f

# 查看容器运行状态
docker-status:
	cd docker && docker compose ps
