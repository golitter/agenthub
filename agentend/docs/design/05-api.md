# API Endpoints — HTTP 端点

## 实现了什么

FastAPI HTTP 端点，作为 Go Backend 调用 Runtime 的入口。

## 怎么实现的

### 依赖注入 (`src/api/dependencies.py`)

通过 FastAPI 的 `Request.app.state` 获取组件实例：

```python
def get_session_manager(request) -> SessionManager
def get_adapter_registry(request) -> AdapterRegistry
def get_rule_engine(request) -> RuleEngine
def get_session_store(request) -> SessionMappingStore
def get_workspace_manager(request) -> WorkspaceManager
def get_preview_manager(request) -> PreviewManager
def get_backend_client(request) -> BackendClient
def get_run_supervisor(request) -> RunSupervisor
def get_integration_service(request) -> IntegrationService
def get_conflict_recovery_coordinator(request) -> ConflictRecoveryCoordinator
def get_path_policy(request) -> PathPolicy
# 注：resources.py 不通过 DI，直接调用 shutil/platform 获取系统信息
```

### Health Check (`src/api/v1/health.py`)

```
GET /health        → {"status": "ok", "version": "<config.yaml app.version>"}
GET /health/live   → 存活探针（匿名，不经服务鉴权）
GET /health/ready  → 就绪探针；未就绪返回 503，含 service_auth_enabled / sandbox_mode /
                     sandbox_enforced / path_policy_configured / capabilities 等控制面状态
```

### Session CRUD (`src/api/v1/session.py`)

| 端点 | 方法 | 说明 |
|------|------|------|
| `GET /v1/session` | GET | 列出所有会话 |
| `GET /v1/session/{id}` | GET | 获取会话详情（不存在返回 404） |
| `POST /v1/session/{id}/interrupt` | POST | 中断运行中的会话（经 `RunSupervisor.cancel_session()` 取消该 session 的活跃 Run 并等待终态；无托管 Run 时返回提示） |
| `DELETE /v1/session/{id}` | DELETE | 取消关联 Run 后销毁会话并清理进程（不存在返回 404） |

### Agent 执行 (`src/api/v1/agent.py`)

#### `POST /v1/agent/stream` — SSE 流式

请求体为 `AgentRequest`（stream=True），返回 SSE 流。

执行流程：
1. `_resolve_workspace()` — `workspace_id` 优先（须已注册且匹配 task/session）；`workspace_path` 须匹配该 session 已注册的活跃 workspace；`repo_path` 自动创建 Git worktree；Orchestrator 类型改为创建 task-base worktree 并返回空 path
2. Rule Engine 评估请求 → 失败返回 HTTP 400
3. 从 AdapterRegistry 获取 Adapter 实例（Orchestrator 类型特殊实例化 `OrchestratorAdapter(registry=...)`）
4. `_resolve_session()` — 获取或创建 Session，查询 SessionMappingStore 获取 CLI session 映射
5. 构造 `RunSpec`（含预算 `_validated_budget()`，wall_time/max_turns 被 `execution.timeout` / `execution.max_turns` 封顶）并交 `RunSupervisor.start()` 托管；同名 run 冲突或父 run 已关闭返回 HTTP 409
6. `journal_stream()` 通过 `RunSupervisor.wait_for_events()` 从 SQLite 事件日志轮询产出 SSE 事件（响应头带 `X-Agent-Run-ID`）
7. INIT 事件触发 CLI session_id 回写到 SessionMappingStore；出站事件经 `sanitize_stream_event()` 净化
8. 执行完成后 Session 状态更新为 COMPLETED

#### `POST /v1/agent/review` — 规划审查

提交 Orchestrator 规划审查结果（approve / discuss / modify）：

```python
class ReviewRequest(BaseModel):
    session_id: str
    action: str       # "approve" | "discuss" | "modify"
    content: str = ""
```

调用 `submit_plan_review()` 将审查结果推送到 LangGraph 的 review 节点。无待审查规划时返回 HTTP 404。

#### `POST /v1/agent/execute` — 同步

请求体为 `AgentRequest`（stream=False），阻塞等待完成后返回 `AgentResponse`。

执行流程：
1. `_resolve_workspace()` — 同 stream 路径
2. Rule Engine 评估请求 → 失败返回 HTTP 400
3. `_resolve_session()` — 同 stream 路径
4. 构造 `RunSpec` 交 `RunSupervisor.start()` 托管（同 stream 路径，runner 内 `_execute_stream` 负责 INIT 事件回写 mapping）
5. `RunSupervisor.wait_for_events()` 轮询 SQLite 事件日志，收集 TEXT 事件文本；预算 wall_time 由 `_validated_budget()` 封顶到 `config.yaml` 的 `execution.timeout`
6. Run 进入终态：`completed` → 返回 `AgentResponse`；`wall_time_exceeded` → HTTP 408；其余非完成终态 → HTTP 409（响应含 run_id/state/termination_reason）

### Workspace 管理 (`src/api/v1/workspace.py`)

提供工作区 CRUD、文件操作、diff、commit、merge、preview 等端点：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/workspace/create` | POST | 创建 workspace（含 worktree + 技能分发） |
| `/v1/workspace/{id}/files/{path}` | GET | 读取文件（防路径穿越） |
| `/v1/workspace/{id}/files/{path}` | PUT | 写入文件（防路径穿越） |
| `/v1/workspace/{id}/diff` | GET | 获取 `git diff HEAD` |
| `/v1/workspace/{id}/commit` | POST | 提交变更（`git add -A && git commit`） |
| `/v1/workspace/{id}/revert` | POST | 撤销变更（`git checkout HEAD -- .`） |
| `/v1/workspace/{id}/merge` | POST | 合并分支 |
| `/v1/workspace/{id}/preview/start` | POST | 启动预览服务器（aiohttp 静态文件） |
| `/v1/workspace/{id}/preview/stop` | POST | 停止预览服务器 |
| `/v1/workspace/task/{task_id}/merge-to-main` | POST | 合并 task branch 到仓库默认分支（路径名保留 main 兼容旧接口） |
| `/v1/workspace/{id}` | DELETE | 清理 workspace（worktree + branch） |
| `/v1/workspace` | GET | 列出所有 workspace |
| `/v1/workspace/by-session/{session_id}` | GET | 按 session_id 查找活跃 workspace |
| `/v1/workspace/task/{task_id}` | DELETE | 清理 task 下所有 workspace |
| `/v1/workspace/task/{task_id}/cleanup-branches` | POST | 强制清理 task 分支（无活跃 workspace 时） |
| `/v1/workspace/task/{task_id}/git-info` | GET | 获取 task 分支的 Git 信息（分支、提交、日志） |

### Pin 管理 (`src/api/v1/pin.py`)

Pin Memory 上下文管理端点，允许用户将关键约束"钉住"供 Orchestrator 使用。

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/pin/add` | POST | 添加 Pin |
| `/v1/pin/remove` | POST | 移除 Pin（同时写入 unpin SystemMessage 到对话记忆） |
| `/v1/pin/announcement-unpin` | POST | Backend 通知 pinned announcement 已删除（写入 unpin SystemMessage） |
| `/v1/pin/list` | GET | 列出所有 Pin |

### Resources (`src/api/v1/resources.py`)

系统资源监控端点，返回磁盘和内存使用情况：

```
GET /v1/resources → {"disk": {"used": ..., "total": ..., "unit": "GB"}, "memory": {...}}
```

macOS 通过 `sysctl` + `vm_stat` 获取内存信息，Linux 优先解析 `free -b` 输出，不可用时回退 `/proc/meminfo`。

### Validate Repo Path (`src/api/v1/validate.py`)

校验仓库路径是否有效，供前端新建对话时使用。

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/validate-repo-path` | POST | 校验仓库路径是否存在且为 Git 仓库 |
| `/v1/init-git-repo` | POST | 在指定路径初始化 Git 仓库（`git init`） |

### Agent Configs (`src/api/v1/agents.py`)

读取各 Agent CLI 的系统级配置文件内容，由后端 admin 接口调用：

```
GET /v1/agents/configs → [{"type": "claude-code", "name": "Claude Code", "description": "...", "configPath": "...", "configContent": "..."}, {"type": "pi", "name": "Pi", ...}, ...]
```

### Skills 管理 (`src/api/v1/skills.py`)

技能发现与安装管理端点：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/skills/{agent_type}` | GET | 扫描当前 workspace 中已安装的技能列表 |
| `/v1/skills/{agent_type}/{skill_name}/install` | POST | 安装指定技能到 workspace |
| `/v1/skills/{agent_type}/{skill_name}` | DELETE | 移除指定技能 |

`agent_type` 用于定位配置目录（如 `.claude` / `.opencode` / `.pi`）。安装端点接收原始 ZIP 字节流（`request.stream()`，受 `MAX_SKILL_PACKAGE_BYTES=12MiB` 等限额约束），`session_id` 为必填 query 参数；`orchestrator` 不支持安装/移除。

### Run 生命周期 (`src/api/v1/runs.py`)

Agent Run 的状态查询与取消端点（数据由 `RunSupervisor` 写入 SQLite，详见 [22-run-lifecycle-and-sandbox.md](22-run-lifecycle-and-sandbox.md)）：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/runs` | GET | 列出所有活跃 Run |
| `/v1/runs/{run_id}` | GET | 获取单个 Run 状态（不存在返回 404） |
| `/v1/runs/{run_id}/events` | GET | 读取 Run 事件流；query：`after_seq`（≥0）、`wait_seconds`（0–30，长轮询等待新事件） |
| `/v1/runs/{run_id}/cancel` | POST | 取消 Run（含递归取消子 Run 及其关联集成操作）；可选 body `CancelAgentRunRequest{reason}` |
| `/v1/runs/{run_id}/resume` | POST | 冲突恢复动作入口：body `ResumeRunRequest{action, task_id, session_id, root_run_id, conflict_id, expected_attempt, ...}` 转发 `ConflictRecoveryCoordinator.handle_action()`；`root_run_id` 与路径不匹配返回 409 |

### Integration 内部端点 (`src/api/v1/integration.py`)

编排产物集成与冲突恢复的内部端点（两个 router，均不面向公网）：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/internal/integration-operations/metrics` | GET | 集成操作指标快照 |
| `/v1/internal/integration-operations/{operation_id}` | GET | 操作投影（普通读路径不返回 workspace handle 等持久绑定） |
| `/v1/internal/integration-operations/{operation_id}/git-record` | GET | Git 集成记录（诊断端点，需内部鉴权） |
| `/v1/internal/integration-operations/{operation_id}/resolution-attempts` | GET | 冲突解决尝试历史（诊断端点） |
| `/v1/internal/integration-operations/{operation_id}/execute` | POST | 执行集成操作（需 Bearer capability；`orchestrator.integration_service_execute_enabled=false` 时返回 503） |
| `/v1/internal/conflicts/{conflict_id}` | GET | 冲突记录 + 解决尝试（诊断端点） |
| `/v1/internal/conflicts/{conflict_id}/projection` | GET | 冲突投影（诊断端点） |
| `/v1/internal/conflicts/{conflict_id}/actions` | POST | 应用冲突恢复动作（诊断端点，经 `ConflictRecoveryCoordinator.handle_action()`） |

诊断端点在 loopback 开发模式下放行，其余情况要求 `AGENTEND_SERVICE_TOKEN` Bearer 鉴权；`IntegrationError` 按错误码映射 400/401/404/409。

`/v1/agent/stream` 与 `/v1/agent/execute` 在内部都会构造 `RunSpec` 并交由 `RunSupervisor.start()` 托管，SSE 响应实际由 `RunSupervisor.wait_for_events()` 从 SQLite 事件日志轮询产出。

### 完整请求生命周期

```
Go Backend 发送 POST /v1/agent/stream
  ↓
FastAPI 路由匹配
  ↓
依赖注入获取 SessionManager / AdapterRegistry / RuleEngine / SessionMappingStore / WorkspaceManager
  ↓
_resolve_workspace() → workspace_path（workspace_id / 已注册 workspace_path / repo_path 自动创建 worktree）
  ↓
RuleEngine.evaluate(request_context)
  ├─ 失败 → HTTP 400 {"error": "...", "rule": "..."}
  └─ 通过 ↓
AdapterRegistry.get(AgentType) → Adapter()
  ↓
_resolve_session() → (internal_session_id, cli_session_id, is_resume)
  ↓
SessionManager.update_state(RUNNING)
  ↓
RunSupervisor.start(RunSpec, runner, cancel_adapter)   # 同 run_id 冲突 → HTTP 409
  ↓
Adapter.stream_chat(**stream_kwargs)
  → asyncio.create_subprocess_exec("claude", "-p", ..., "--output-format", "stream-json", "--verbose", "--include-partial-messages")
  → 逐行读取 stdout → _parse_stream_line() → StreamEvent
  → INIT 事件: session_store.set_cli_session_id() 回写 mapping
  → sanitize_stream_event() 净化后写入 SQLite 事件日志
  ↓
journal_stream() → RunSupervisor.wait_for_events() 轮询事件日志
  → SSE: event: <type>\ndata: <json>\n\n（id 为事件 seq）
  ↓
SessionManager.update_state(COMPLETED)
```
