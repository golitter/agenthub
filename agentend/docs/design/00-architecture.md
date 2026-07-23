# AgentEnd Runtime — 架构总览

## 实现了什么

AgentEnd Runtime 是一个 Python FastAPI 服务，作为 AgentHub 多 Agent 协作平台的 AI 执行引擎。架构定位：

```
React 前端 → Go Backend → AgentEnd Runtime (本服务) → Claude Code CLI / OpenCode CLI / Orchestrator
```

Go Backend 通过 HTTP 调用 Runtime，Runtime 启动 CLI 子进程执行编码任务，结果通过 SSE 流式返回。

核心模块：
- **adapters/** — Agent 适配器（Claude CLI / OpenCode CLI / Codex CLI / Orchestrator）
- **api/v1/** — HTTP 端点（agent, session, workspace, validate, health, pin, resources, skills）
- **app/** — 应用入口、配置（Pydantic Settings）、依赖注入
- **clients/** — 外部服务客户端（BackendClient 与 Go Backend 通信）
- **orchestrator/** — Orchestrator 规划模块（planning/execution/memory/reporting 子模块）
- **preview/** — 工作区预览服务（aiohttp 静态文件服务器）
- **rules/** — 规则引擎（Safety / Pin / Soul / GroupChat / Scope / Taskctl / Skill）
- **session/** — 会话管理（状态机 + 持久化）
- **skills/** — 技能供给系统（内置 taskctl + render）
- **workspace/** — 工作区管理（Git Worktree 隔离）

## 怎么实现的

### 应用入口 (`src/app/main.py`)

FastAPI 应用通过 lifespan 管理生命周期，启动时完成依赖注入、工作区恢复和清理任务：

```python
@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.adapter_registry = create_adapter_registry()
    app.state.session_manager = create_session_manager()
    app.state.session_store = create_session_store()
    app.state.rule_engine = create_rule_engine()
    app.state.workspace_manager = create_workspace_manager()
    app.state.preview_manager = create_preview_manager()
    app.state.backend_client = create_backend_client()

    # Startup: load persisted workspaces and recover
    ws_mgr = app.state.workspace_manager
    await ws_mgr._load_from_store()
    repo_paths = {ws.repo_path for ws in ws_mgr.list()}
    for rp in repo_paths:
        await recover_workspaces(ws_mgr._git, ws_mgr._store, rp)

    # Startup: connect DB reader and begin inactive cleanup
    db_reader = create_db_reader()
    await db_reader.connect()
    await ws_mgr.start_inactive_cleanup(db_reader, interval=settings.workspace.cleanup_interval)

    # Startup: report builtin skills to Backend
    asyncio.create_task(_report_builtin_skills())

    yield

    # Shutdown
    await ws_mgr.stop_inactive_cleanup()
    await app.state.preview_manager.stop_all()
    await app.state.backend_client.close()
    await db_reader.close()
```

### 项目结构

```
agentend/
├── src/
│   ├── adapters/       # Adapter 适配器层
│   ├── api/            # FastAPI HTTP 端点
│   │   └── v1/         # v1 版本 API
│   ├── app/            # 应用入口、配置、DI
│   ├── clients/        # 外部服务客户端（BackendClient）
│   ├── generated/      # 契约生成的 Python 类型（勿手改）
│   ├── orchestrator/   # Orchestrator 规划模块
│   │   ├── planning/   #   LangGraph 规划（graph + prompts + tools）
│   │   ├── execution/  #   任务执行（engine + dispatcher + coordination + wave）
│   │   ├── memory/     #   持久记忆（pin_memory + conversation_memory + evolution）
│   │   ├── prompts/    #   提示模板（group_chat 跨 Agent 上下文构建）
│   │   └── reporting/  #   报告汇总（aggregator）
│   ├── preview/        # 工作区预览服务（aiohttp 静态文件服务器）
│   ├── rules/          # Rule Engine 规则引擎
│   ├── schemas/        # 数据模型
│   ├── session/        # Session 会话管理
│   ├── skills/         # 技能供给系统（内置 taskctl + render）
│   └── workspace/      # 工作区管理（Git Worktree 隔离）
├── pyproject.toml      # 项目配置与依赖
└── ruff.toml           # 代码风格
```
