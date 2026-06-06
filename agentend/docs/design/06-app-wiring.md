# App Wiring — 应用组装与启动

## 实现了什么

FastAPI 应用入口，负责组件初始化、路由注册、CORS 配置和生命周期管理。

## 怎么实现的

### 配置管理 (`src/app/config.py`)

使用 `pydantic-settings` + `YamlConfigSettingsSource` 管理，从 `config.yaml` 读取所有配置，`.env` 读取 LLM 密钥：

| 配置分区 | 说明 | 示例字段 |
|---------|------|---------|
| `server` | 监听地址、端口、CORS、热重载 | `host`, `port`, `cors` |
| `app` | 应用标题和版本 | `title`, `version` |
| `workspace` | Worktree 根目录、清理间隔、存储路径 | `base_dir`, `cleanup_interval` |
| `session` | 会话映射持久化路径 | `store_path` |
| `database` | MySQL 连接信息（用于 inactive 清理） | `host`, `port`, `user`, `password`, `dbname` |
| `execution` | 最大轮次、执行超时、进程终止超时 | `max_turns`, `timeout`, `process_terminate_timeout` |
| `skills` | 内置技能目录与分发清单 | `builtin_dir`, `manifest` |
| `llm` | Orchestrator LLM 配置 | `model`, `base_url`, `api_key`（优先从 `.env` 的 `DS_MODEL`/`DS_BASE_URL`/`DS_API_KEY` 读取） |
| `backend` | Go Backend 连接地址 | `url` |

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
    # 启动时初始化
    app.state.adapter_registry = create_adapter_registry()
    app.state.session_manager = create_session_manager()
    app.state.session_store = create_session_store()
    app.state.rule_engine = create_rule_engine()
    app.state.workspace_manager = create_workspace_manager()
    app.state.preview_manager = create_preview_manager()
    app.state.backend_client = create_backend_client()

    # 从持久化加载 workspace + 恢复
    ws_mgr = app.state.workspace_manager
    await ws_mgr._load_from_store()
    repo_paths = {ws.repo_path for ws in ws_mgr.list()}
    for rp in repo_paths:
        await recover_workspaces(ws_mgr._git, ws_mgr._store, rp)

    # 连接 DB + 启动 inactive 清理
    db_reader = create_db_reader()
    await db_reader.connect()
    await ws_mgr.start_inactive_cleanup(db_reader, interval=settings.workspace.cleanup_interval)

    yield

    # 关闭时停止清理 + 关闭预览 + 关闭 Backend Client + 关闭 DB
    await ws_mgr.stop_inactive_cleanup()
    await app.state.preview_manager.stop_all()
    await app.state.backend_client.close()
    await db_reader.close()
```

#### 路由注册

```python
app.include_router(health_router)     # GET /health
app.include_router(session_router)    # /v1/session/*
app.include_router(agent_router)      # /v1/agent/*
app.include_router(pin_router)        # /v1/pin/*
app.include_router(workspace_router)  # /v1/workspace/*
app.include_router(validate_router)   # /v1/validate-repo-path
app.include_router(resources_router)  # GET /v1/resources
app.include_router(skills_router)     # /v1/skills/*
```

#### CORS

参数全部来自 `config.yaml` 的 `server.cors` 分区，不再硬编码。

#### 启动

```bash
uv run python -m src.app.main
# host/port/reload 均来自 config.yaml 的 server 分区
```
