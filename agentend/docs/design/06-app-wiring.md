# App Wiring — 应用组装与启动

## 实现了什么

FastAPI 应用入口，负责组件初始化、路由注册、CORS 配置和生命周期管理。

## 怎么实现的

### 配置管理 (`src/app/config.py`)

使用 `pydantic-settings` + `YamlConfigSettingsSource` 管理，从 `config.yaml` 读取所有配置，`.env` 读取 LLM 密钥（`.env` 模板见 [`agentend/.env.example`](../../../agentend/.env.example)，首次运行前 `cp .env.example .env` 并填入实际密钥）：

| 配置分区 | 说明 | 示例字段 |
|---------|------|---------|
| `server` | 监听地址、端口、CORS、热重载 | `host`, `port`, `cors` |
| `app` | 应用标题和版本 | `title`, `version` |
| `workspace` | Worktree 根目录、清理间隔、存储路径、默认分支 | `base_dir`, `cleanup_interval`, `store_path`, `git_default_branch` |
| `session` | 会话映射持久化路径 | `store_path` |
| `database` | MySQL 连接信息（用于 inactive 清理，`.env` 的 `MYSQL_*` 优先） | `host`, `port`, `user`, `password`, `dbname` |
| `execution` | 最大轮次、执行超时、进程终止超时、Run 存储、并发上限、沙箱 | `max_turns`, `timeout`, `process_terminate_timeout`, `run_store_path`, `max_concurrent_runs`, `sandbox.mode` |
| `security` | 控制面安全（服务鉴权、仓库根白名单、本地执行放行） | `service_auth_enabled`, `allowed_repo_roots`, `allow_unsafe_local_execution` |
| `skills` | 内置技能目录、卡片标记符号与分发清单 | `builtin_dir`, `block_marker`, `manifest` |
| `llm` | Orchestrator LLM 配置 | `model`, `base_url`, `api_key`（优先从 `.env` 的 `DS_MODEL`/`DS_BASE_URL`/`DS_API_KEY` 读取） |
| `orchestrator` | Orchestrator 运行参数 | `llm_request_timeout`, `ask_agent_timeout`, `ask_agent_stream_chunk_timeout`, `review_timeout`, `replan_max_iterations`, `reason_max_iterations`, `skill_execution_timeout` |
| `backend` | Go Backend 连接地址 | `url` |
| `agents` | 各 Agent CLI 配置路径映射 | `{agent_type: {config_path}}`；本机通过 `<AGENT_TYPE>_CONFIG_PATH` 环境变量覆盖 |

> 注：Langfuse 可观测性不在 `Settings` 中，由 `src/observability/config.py` 独立从 `LANGFUSE_*` 环境变量解析（详见 [18-langfuse-trace.md](18-langfuse-trace.md)）。

> **CLI 路径**：Agent CLI 路径不在 `config.yaml` 中，而是由 `agents.json` 统一管理（含 `cli_path`、`config_dir`、`event_type` 等字段）。详见 `src/app/agent_config.py`。

### DI 容器 (`src/app/dependencies.py`)

集中创建各组件实例：

```python
def create_adapter_registry() -> AdapterRegistry:
    registry = AdapterRegistry()
    registry.register(AgentType.CLAUDE_CODE, ClaudeCodeAdapter)
    registry.register(AgentType.OPENCODE, OpenCodeAdapter)
    registry.register(AgentType.ORCHESTRATOR, OrchestratorAdapter)
    registry.register(AgentType.CODEX, CodexAdapter)
    registry.register(AgentType.PI, PiAdapter)
    return registry

def create_session_manager() -> SessionManager:
    return SessionManager()

def create_session_store() -> SessionMappingStore:
    return SessionMappingStore()

def create_rule_engine() -> RuleEngine:
    rules = [SafetyRule(), PinRule(), SoulRule(), GroupChatRule(), ScopeRule(), TaskctlRule(), SkillRule()]
    return RuleEngine(rules)

def create_workspace_manager() -> WorkspaceManager:
    store = JsonFileWorkspaceStore()
    return WorkspaceManager(store)

def create_preview_manager() -> PreviewManager:
    return PreviewManager()

def create_backend_client() -> BackendClient:
    return BackendClient(base_url=settings.backend.url)

def create_db_reader() -> DBReader:
    return DBReader(host=..., port=..., user=..., password=..., db=...)
```

### 应用入口 (`src/app/main.py`)

#### Lifespan 生命周期

```python
@asynccontextmanager
async def lifespan(app: FastAPI):
    # 启动校验：非 loopback 监听必须开启服务鉴权；unsafe_process 执行需 loopback + 显式放行
    ...
    # 启动时初始化
    app.state.adapter_registry = create_adapter_registry()
    app.state.session_manager = create_session_manager()
    app.state.session_store = create_session_store()
    app.state.rule_engine = create_rule_engine()
    app.state.workspace_manager = create_workspace_manager()
    app.state.preview_manager = create_preview_manager()
    app.state.backend_client = create_backend_client()
    app.state.path_policy = PathPolicy(settings.security.allowed_repo_roots)
    # Run 存储与监督者直接在 lifespan 内构造（无工厂函数）
    app.state.run_repository = SQLiteRunRepository(settings.execution.run_store_path)
    app.state.run_supervisor = RunSupervisor(
        app.state.run_repository, max_concurrent_runs=settings.execution.max_concurrent_runs
    )
    await app.state.run_supervisor.recover()

    # 从持久化加载 workspace + 恢复
    ws_mgr = app.state.workspace_manager
    await ws_mgr._load_from_store()
    repo_paths = {ws.repo_path for ws in ws_mgr.list()}
    for rp in repo_paths:
        await recover_workspaces(ws_mgr._git, ws_mgr._store, rp)

    # 恢复 Skill 原子安装残留 + 启动周期清理 loop
    ...

    # 连接 DB + 启动 inactive 清理
    db_reader = create_db_reader()
    await db_reader.connect()
    await ws_mgr.start_inactive_cleanup(db_reader, interval=settings.workspace.cleanup_interval)

    # 上报内置技能到 Backend
    asyncio.create_task(_report_builtin_skills())

    yield

    # 关闭：停止清理 + 关闭预览 + 关闭 Run 监督 + 关闭 Backend Client + 关闭 DB + 关闭 Langfuse
    await ws_mgr.stop_inactive_cleanup()
    await app.state.preview_manager.stop_all()
    await app.state.run_supervisor.shutdown()
    await app.state.run_repository.close()
    await app.state.backend_client.close()
    await db_reader.close()
    await shutdown_langfuse()
```

#### 路由注册

```python
app.include_router(health_router)     # GET /health, /health/live, /health/ready
app.include_router(session_router)    # /v1/session/*
app.include_router(agent_router)      # /v1/agent/*
app.include_router(agents_router)     # GET /v1/agents/configs
app.include_router(pin_router)        # /v1/pin/*
app.include_router(workspace_router)  # /v1/workspace/*
app.include_router(validate_router)   # /v1/validate-repo-path, /v1/init-git-repo
app.include_router(resources_router)  # GET /v1/resources
app.include_router(runs_router)       # /v1/runs/*
app.include_router(skills_router)     # /v1/skills/*
```

#### 中间件

- `CORSMiddleware` — 参数全部来自 `config.yaml` 的 `server.cors` 分区，不再硬编码。
- `ServiceAuthMiddleware` — 服务间鉴权（`enabled=settings.security.service_auth_enabled`），校验 `Authorization: Bearer <AGENTEND_SERVICE_TOKEN>`，详见 [22-run-lifecycle-and-sandbox.md](22-run-lifecycle-and-sandbox.md)。

#### 启动

```bash
uv run python -m src.app.main
# host/port/reload 均来自 config.yaml 的 server 分区
```
