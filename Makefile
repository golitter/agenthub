SHELL := /bin/bash
.DEFAULT_GOAL := all

.PHONY: all run-frontend run-backend run-agentend \
       stop stop-frontend stop-backend stop-agentend \
       restart restart-frontend restart-backend restart-agentend \
       status generate help backend config docker env skills \
       _backend-tidy _config-start _config-test \
       _docker-up _docker-down _docker-build _docker-logs _docker-status \
       _env-wsl _skills-build _skills-check _skills-migrate _skills-reconcile

SCRIPT := ./scripts/run.sh
CONFIG_CENTER_SCRIPT := ./config-center/run-config-center.sh
SERVER_ENV := if [[ -f ./scripts/server-env.sh ]]; then source ./scripts/server-env.sh; fi
COMMAND_GROUPS := backend config docker env skills

# GNU Make 会把 `make docker up` 解析成两个目标。分组目标负责执行命令，
# 第二个词只作为子命令占位；缺失、多余和未知子命令都由分组目标明确报错。
ifneq ($(filter $(firstword $(MAKECMDGOALS)),$(COMMAND_GROUPS)),)
ifneq ($(words $(MAKECMDGOALS)),2)
$(error 用法: make $(firstword $(MAKECMDGOALS)) <子命令>（运行 make help 查看）)
endif
.PHONY: $(word 2,$(MAKECMDGOALS))
$(word 2,$(MAKECMDGOALS)):
	@:
endif

define dispatch
	@case "$(word 2,$(MAKECMDGOALS))" in \
		$(1)) $(MAKE) --no-print-directory _$(2)-$(1) ;; \
		*) echo "未知子命令: make $(2) $(word 2,$(MAKECMDGOALS))" >&2; echo "运行 make help 查看可用命令" >&2; exit 2 ;; \
	esac
endef

# 默认：启动全部服务
all: _skills-check
	@$(SERVER_ENV) && $(SCRIPT) start

# 启动前端（热重载）— Vite dev server，localhost:5173
run-frontend:
	@$(SERVER_ENV) && $(SCRIPT) start frontend

# 启动后端（热重载）— Air，localhost:8080
run-backend:
	@$(SERVER_ENV) && $(SCRIPT) start backend

# 启动 Agent 端（热重载）— uvicorn --reload，localhost:8001
run-agentend: _skills-check
	@$(SERVER_ENV) && $(SCRIPT) start agentend

# 停止全部或单个服务
stop:
	@$(SERVER_ENV) && $(SCRIPT) stop

stop-frontend:
	@$(SERVER_ENV) && $(SCRIPT) stop frontend

stop-backend:
	@$(SERVER_ENV) && $(SCRIPT) stop backend

stop-agentend:
	@$(SERVER_ENV) && $(SCRIPT) stop agentend

# 重启全部或单个服务
restart:
	@$(SERVER_ENV) && $(SCRIPT) restart

restart-frontend:
	@$(SERVER_ENV) && $(SCRIPT) restart frontend

restart-backend:
	@$(SERVER_ENV) && $(SCRIPT) restart backend

restart-agentend:
	@$(SERVER_ENV) && $(SCRIPT) restart agentend

# 查看三端运行状态（端口 + PID）
status:
	@$(SERVER_ENV) && $(SCRIPT) status

# 从 contracts/schemas/ 生成三端类型文件（Python / TypeScript / Go）
generate:
	python3 scripts/generate_contracts.py

# ─── 低频命令分组 ─────────────────────────────────────────
backend:
	$(call dispatch,tidy,backend)

config:
	@case "$(word 2,$(MAKECMDGOALS))" in \
		start|test) $(MAKE) --no-print-directory _config-$(word 2,$(MAKECMDGOALS)) ;; \
		*) echo "未知子命令: make config $(word 2,$(MAKECMDGOALS))" >&2; echo "可用子命令: start, test" >&2; exit 2 ;; \
	esac

docker:
	@case "$(word 2,$(MAKECMDGOALS))" in \
		up|down|build|logs|status) $(MAKE) --no-print-directory _docker-$(word 2,$(MAKECMDGOALS)) ;; \
		*) echo "未知子命令: make docker $(word 2,$(MAKECMDGOALS))" >&2; echo "可用子命令: up, down, build, logs, status" >&2; exit 2 ;; \
	esac

env:
	$(call dispatch,wsl,env)

skills:
	@case "$(word 2,$(MAKECMDGOALS))" in \
		build|check|migrate|reconcile) $(MAKE) --no-print-directory _skills-$(word 2,$(MAKECMDGOALS)) ;; \
		*) echo "未知子命令: make skills $(word 2,$(MAKECMDGOALS))" >&2; echo "可用子命令: build, check, migrate, reconcile" >&2; exit 2 ;; \
	esac

help:
	@echo "常用命令:"
	@echo "  make                              启动全部服务"
	@echo "  make run-<frontend|backend|agentend>"
	@echo "  make stop[-<frontend|backend|agentend>]"
	@echo "  make restart[-<frontend|backend|agentend>]"
	@echo "  make status                       查看服务状态"
	@echo "  make generate                     生成三端契约类型"
	@echo ""
	@echo "低频命令:"
	@echo "  make backend tidy"
	@echo "  make skills <build|check|migrate|reconcile> [ARGS=\"...\"]"
	@echo "  make docker <up|down|build|logs|status>"
	@echo "  make config <start|test>"
	@echo "  make env wsl"

# ─── 分组命令的内部实现 ───────────────────────────────────
_backend-tidy:
	cd backend && go mod tidy

_skills-migrate:
	cd backend && go run ./cmd/skill-migrate $(ARGS)

_skills-reconcile:
	cd backend && go run ./cmd/skill-reconcile $(ARGS)

_skills-build:
	@command -v go >/dev/null 2>&1 || { echo "缺少 Go 工具链，无法构建内置 skill CLI；请先安装 Go 后再运行 make skills build"; exit 1; }
	cd agentend/src/skills/builtin/taskctl && go build -o taskctl .
	cd agentend/src/skills/builtin/render && go build -o render .

_skills-check:
	@test -x agentend/src/skills/builtin/taskctl/taskctl || { echo "缺少 agentend/src/skills/builtin/taskctl/taskctl，请先运行 make skills build"; exit 1; }
	@test -x agentend/src/skills/builtin/render/render || { echo "缺少 agentend/src/skills/builtin/render/render，请先运行 make skills build"; exit 1; }

_env-wsl:
	@echo "WSL2 运行配置："
	@echo ""
	@echo "1. frontend 使用下面命令启动，让 Windows 可以通过 WSL2 IP 访问："
	@echo "   cd frontend && pnpm dev --host 0.0.0.0 --port 5173"
	@echo ""
	@echo "2. backend/.env 可能需要改成当前 WSL2 IP，可用 ifconfig 查看："
	@echo "   ifconfig"
	@echo "   CORS_ALLOW_ORIGINS=http://localhost:5173,http://<wsl2-ip>:5173"
	@echo ""
	@echo "3. 后端和 Agent 端按需启动："
	@echo "   make run-agentend && make run-backend"
	@echo ""
	@echo "注意：make env wsl 只打印说明，不会实际启动服务。"

# 前后端 + MySQL + Redis 跑在 Docker，Agentend 跑在本地
_docker-up:
	@$(SERVER_ENV) && $(MAKE) --no-print-directory _skills-check
	@$(SERVER_ENV) && docker/scripts/precheck.sh && cd docker && docker compose up --build -d && docker compose up --wait && cd .. && cd agentend && uv sync && cd .. && $(SCRIPT) start agentend

_docker-down:
	cd docker && docker compose down

_docker-build:
	cd docker && docker compose build

_docker-logs:
	cd docker && docker compose logs -f

_docker-status:
	cd docker && docker compose ps

_config-start:
	@$(SERVER_ENV) && $(CONFIG_CENTER_SCRIPT)

_config-test:
	@$(SERVER_ENV) && \
		uv sync --directory config-center --locked && \
		(cd config-center/web && "$${PNPM:-pnpm}" install --frozen-lockfile) && \
		uv run --directory config-center pytest && \
		(cd config-center/web && "$${PNPM:-pnpm}" test && "$${PNPM:-pnpm}" build)
