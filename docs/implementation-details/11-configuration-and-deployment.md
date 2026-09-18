# 配置、启动与部署超详细实现

## 实现了什么

项目通过根 Makefile 统一三端生命周期；Backend 使用 YAML 加 `.env` 覆盖，AgentEnd 使用 Pydantic YAML 并对数据库/LLM字段显式读取 `.env`，Config Center 提供 example 驱动编辑器，Docker 使用“前后端与数据面容器化、AgentEnd 宿主运行”的混合拓扑。

## 怎么实现的

### 根命令

| 命令 | 行为 |
|---|---|
| `make` / `make all` | 检查 builtin skill binary，然后启动三端 |
| `make run-frontend/backend/agentend` | 启动单端热重载 |
| `make stop`、`make stop-frontend/backend/agentend` | 全部或单端停止 |
| `make restart`、`make restart-frontend/backend/agentend` | 全部或单端重启 |
| `make status` | 检查端口和 PID |
| `make generate` | 从 schema 生成三端契约 |
| `make backend tidy` | Go mod tidy |
| `make skills build/check` | 构建/检查 taskctl、render Go CLI |
| `make skills migrate/reconcile` | Skill MinIO 迁移和对账 |
| `make evals validate/baseline/release/batch/speedup/build-dataset` | Coding Agent 评测（默认数据集 `evals/datasets/agenthub-agent-v2`） |
| `make docker up/down/build/logs/status` | Docker 混合部署 |
| `make config start/test` | 配置中心与完整验收 |
| `make env wsl` | 打印 WSL2 说明，不启动服务 |

分组命令严格要求两个 make goal，未知或多余子命令返回错误。可选 `scripts/server-env.sh` 在每个运行命令前 source，模板是 `server-env.example.sh`。

### scripts/run.sh

运行脚本为 frontend/backend/agentend 管理启停与 `logs/*.log`，不维护 PID 文件，运行状态通过端口监听检测（`ss` 优先，`lsof` 回退）：

- Frontend 用 pnpm Vite。
- Backend 用 Air 热重载。
- AgentEnd 用 uv/uvicorn reload。
- start 先检查端口是否已监听，已在运行则跳过，避免重复进程。
- stop 按端口找到进程组并等待退出，同时清理 air/uvicorn 热重载残留的孤儿进程。
- status 输出各端口监听状态与对应 PID 的表格。

脚本不能代替依赖服务：MySQL、Redis 和启用的 MinIO 必须先就绪。

### Backend 配置优先级

`conf.Load` 先 `godotenv.Load()`，读取指定 `configs/config.yaml`，设置安全默认值，YAML unmarshal，再逐字段应用环境变量覆盖，最后 validate。

重要默认：Skill require admin=true、shadow blob=true、legacy tmp confirm=true，即使 YAML 漏字段也采取迁移期安全/可回滚行为。

环境变量分组：

- MySQL：`MYSQL_HOST/PORT/USER/PASSWORD/DBNAME/CHARSET`。
- JWT/Admin/Auth/CORS/Server。
- AgentEnd 地址、`AGENTEND_SERVICE_AUTH_ENABLED`、服务 token。
- Avatar：`AVATAR_STORAGE_WRITE_PROVIDER`、`ASSET_MINIO_*`、`LOCAL_STORAGE_*`。
- Artifact：`ARTIFACT_STORAGE_ENABLED`、`ARTIFACT_MINIO_*`、大小/数量/TTL/capability/retention。
- Skill：`SKILL_STORAGE_ENABLED`、`MINIO_*`、上传/验证/迁移开关与限制。

validate 检查端口、secret、存储组合、byte size/duration、TLS CA、最大值关系等。启用某功能却缺少凭据应启动失败，不应推迟到首个用户请求。

### AgentEnd 配置

`agentend/src/app/config.py` 用基于 `__file__` 的绝对路径找 `config.yaml` 和 `.env`，与当前工作目录无关。Settings 数据源只接受初始化值与 YAML source；普通 pydantic env source 被禁用。例外由模型 validator 显式处理：

- DatabaseConfig 从 `MYSQL_*` 非空值覆盖 YAML。
- LlmConfig 从 `DS_MODEL/DS_BASE_URL/DS_API_KEY` 补空字段。

主要段：

| 段 | 内容 |
|---|---|
| server/app | host/port/reload/CORS、标题版本 |
| workspace/session | base dir、cleanup interval、JSON store、默认分支 |
| database/backend | MySQL 与 Backend URL |
| execution | turns、timeout、terminate timeout、SQLite、并发、sandbox |
| security | service auth、allowed roots、unsafe local execution |
| skills | builtin dir、block marker、manifest |
| orchestrator | LLM/ask/review/retry/conflict/context budgets |
| llm | model/base URL/API key |
| agents | 每类 Agent 的 config path |

模块 import 时立刻实例化 Settings，所以缺 `config.yaml`、字段不全或预算关系非法会直接退出。

### agents.json

`agentend/agents.example.json` 是 Agent 类型统一元数据：config_dir、event_type、cli_path。Claude/OpenCode/Codex/Pi 有 CLI；Orchestrator 没有单一 cli_path。新增 Agent 必须同步 agents.json、AgentEnd registry/adapter、contract AgentType、Backend list 和前端展示。

### Config Center

Config Center 是独立 Python API + React Web：

- profiles 定义可编辑的 Backend、AgentEnd、Docker 等配置对。
- example 文件定义结构、注释和默认展示；actual 文件保存本机值。
- formats 支持 dotenv、YAML、JSON，YAML 用 ruamel 保留格式/注释能力。
- ConfigService 负责读取、校验、备份、保存。
- runner 提供受限的服务启动/停止操作。
- Web 的 TemplateEditor 维护 draft，BackupManager 展示恢复点。

`make config test` 使用 locked uv、frozen pnpm lock，运行 Python pytest、Web vitest 和生产 build。

### Docker 拓扑

Compose 服务：mysql 8、redis 7 alpine、固定版本 MinIO、一次性 minio-init、Backend、Frontend。依赖条件：Backend 等 MySQL/Redis healthy 和 minio-init 成功；Frontend 依赖 Backend。

端口：Backend 8080、Frontend 8787、MySQL 3306、Redis 6379；MinIO 9000/9001 绑定 127.0.0.1。卷：mysql_data、redis_data、minio_data、skill_tmp、avatar_uploads。证书目录只读挂载到 Backend。

`make docker up` 顺序：检查 builtin binaries → precheck 配置/端口 → compose build/up → wait → 在宿主 `agentend` 执行 uv sync → 用 run.sh 启动 AgentEnd。

### Nginx Frontend

Docker Frontend 使用多阶段构建产出 Vite 静态文件，Nginx 服务 SPA，并将 `/api` 代理到 Backend。SPA fallback 返回 index.html；SSE 代理必须关闭 buffering 并使用足够长 read timeout。静态 hash asset 可长缓存，index.html 不应永久缓存。

### 首次本地启动

1. 复制 Backend `.env.example` 与 `configs/config.example.yaml`。
2. 复制 AgentEnd `.env.example`、`config.example.yaml`、`agents.example.json`。
3. 填 MySQL/Redis、存储、JWT/service token、DeepSeek/LLM 和 CLI 路径。
4. 启动 MySQL/Redis/按 gate 启动 MinIO。
5. `make skills build`。
6. `make generate` 并确认无意外 diff。
7. `make all`，用 `make status` 与三个 log 排查。

### WSL2

Frontend 需监听 `0.0.0.0` 才能从 Windows 浏览器访问。Backend CORS 加 localhost 与当前 WSL IP。WSL IP 可能重启变化，`make env wsl` 只输出提示。仓库与 Worktree 最好位于 Linux 文件系统，以避免跨文件系统 Git/rename 性能和原子性问题。

### 生产配置检查

- 替换 example 中所有默认密码和 MinIO root secret。
- AgentEnd 非 loopback 时开启 service auth，并限制 allowed roots。
- 启用 TLS 时配置 endpoint/use_ssl/CA file 一致。
- CORS 不使用宽泛 wildcard + credentials。
- 独立备份 MySQL、Redis 持久卷、MinIO 和 AgentEnd logs/SQLite/JSON。
- 配置日志轮转、磁盘告警、temp volume 空间和 object retention。
- 校验 Nginx SSE buffering、body size 与 Backend 配置一致。
