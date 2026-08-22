# Run 生命周期 + 控制面安全首竖切

## 实现了什么

为 AgentEnd 引入显式的 **Agent Run 生命周期**与**控制面安全**，使单次 Agent 执行可被外部观测、取消并受资源预算约束，同时收紧非 loopback 部署下的访问边界。本竖切包含四块能力：

1. **Run 生命周期** — 每次 `/v1/agent/stream` 与 `/v1/agent/execute` 调用被建模为一个 Run，由 `RunSupervisor` 托管，状态与事件流持久化到 SQLite（`SQLiteRunRepository`）。
2. **资源预算** — `AgentRunBudget` 约束 wall time、输出字节、事件数、并发与子 Run 数量；超限自动终止并标注原因。
3. **服务鉴权** — `ServiceAuthMiddleware` 校验 `AGENTEND_SERVICE_TOKEN`，非 loopback 监听强制开启。
4. **路径边界** — `PathPolicy` 按 `allowed_repo_roots` 白名单校验仓库与 worktree 路径，阻断路径注入。

新增模块：`src/execution/`、`src/security/`（顶层 `src/persistence.py` 提供 `atomic_write_text` 工具复用）。

## 怎么实现的

### 数据模型 (`src/execution/models.py`)

```python
@dataclass(frozen=True)
class RunSpec:
    run_id: str
    root_run_id: str
    task_id: str
    session_id: str
    workspace_id: str
    agent_type: str
    request_fingerprint: str = ""
    parent_run_id: str | None = None
    message_id: str | None = None
    requested_by: str = "backend"
    budget: AgentRunBudget = field(default_factory=AgentRunBudget)

    def fingerprint(self) -> str: ...   # sha256 over canonical JSON

@dataclass
class RunRecord:
    spec: RunSpec
    state: AgentRunState = AgentRunState.QUEUED
    termination_reason: str | None = None
    created_at: str = field(default_factory=utc_now)
    started_at: str | None = None
    finished_at: str | None = None
    last_event_seq: int = 0
    admission_closed: bool = False
    runtime: dict[str, Any] = field(default_factory=dict)

    @property
    def terminal(self) -> bool:
        return self.state in {AgentRunState.COMPLETED, AgentRunState.FAILED, AgentRunState.CANCELLED}
```

`AgentRunState` 与 `AgentRunTerminationReason` 来自契约生成（`src/generated/agent_run.py`）。状态机：

```
QUEUED → STARTING → RUNNING → COMPLETED
                            → FAILED
              CANCELLING → CANCELLED
```

`AgentRunBudget`（默认值）约束：`wall_time_seconds=1200`、`idle_time_seconds=180`、`max_turns=20`、`max_processes=64`、`max_memory_mb=4096`、`max_cpu_seconds=900`、`max_output_bytes=52428800`、`max_event_count=50000`、`max_workspace_growth_bytes=2147483648`、`max_children=12`、`max_parallelism=4`。

### SQLite 存储 (`src/execution/repository.py`)

`SQLiteRunRepository` 以 SQLite（WAL、`check_same_thread=False`、外键级联）持久化 Run 与事件：

```python
class SQLiteRunRepository:
    def __init__(self, path: str | Path): ...          # settings.execution.run_store_path
    async def create(self, spec: RunSpec) -> tuple[RunRecord, bool]
    async def get(self, run_id: str) -> RunRecord | None
    async def list_active(self) -> list[RunRecord]
    async def list_active_by_session(self, session_id: str) -> list[RunRecord]
    async def children(self, run_id: str) -> list[RunRecord]
    async def transition(self, run_id, expected: set[AgentRunState], target: AgentRunState, reason=None) -> bool
    async def close_admission(self, run_id: str) -> None
    async def append_event(self, run_id, event: dict, timestamp: float) -> AgentRunEventEnvelope
    async def read_events(self, run_id, after_seq, limit=1000) -> list[AgentRunEventEnvelope]
```

`create()` 以 `BEGIN IMMEDIATE` 实现幂等：相同 `request_fingerprint` 返回已有记录；否则强制单 session 单活跃 Run、父子 admission 规则、子预算与并行度不超父。冲突抛 `RunConflictError` / `ParentRunClosedError`。事件 `seq` 单调递增。

### RunSupervisor (`src/execution/supervisor.py`)

监督者持有并发信号量与条件变量，把 Runner 抽象为 `Callable[[Emit], Awaitable[None]]`：

```python
class RunSupervisor:
    def __init__(self, repository: SQLiteRunRepository, max_concurrent_runs: int = 4): ...
    async def start(self, spec: RunSpec, runner: Runner, cancel_hook: CancelHook | None = None) -> tuple[RunRecord, bool]
    async def resume(self, run_id: str, runner: Runner, cancel_hook: CancelHook | None = None) -> RunRecord | None
        # 冲突恢复后按已存在 RunSpec 重新托管 runner（/v1/runs/{run_id}/resume 消费）
    async def cancel(self, run_id: str, reason=AgentRunTerminationReason.USER_CANCELLED) -> RunRecord | None
    async def cancel_session(self, session_id: str, reason=...) -> list[RunRecord]
    async def wait_for_events(self, run_id, after_seq, timeout=15.0) -> tuple[list, RunRecord | None]
    async def wait_until_terminal(self, run_id, timeout) -> RunRecord | None
    async def recover(self, preserve_run_ids: set[str] | None = None) -> None
        # 启动时把残留活跃 Run 标记为 AGENTEND_RECOVERY 取消；
        # preserve_run_ids 中的根 Run 保留（集成恢复还需读取其事件/状态）
    async def shutdown(self) -> None   # 关闭时取消所有活跃 Run
```

`_run_inner` 的 `emit` 闭包在写入每条事件前检查 `max_event_count` 与 `max_output_bytes`，超限抛出带终止原因的 `RuntimeError`；`asyncio.wait_for(runner, timeout=spec.budget.wall_time_seconds)` 兜底 wall time。`cancel()` 关闭 root+自身 admission，递归取消非终态子 Run，处理 QUEUED 直销与活跃态 CANCELLING→CANCELLED 的竞态收敛。

### 与 agent 路由的集成 (`src/api/v1/agent.py`)

`agent_stream` / `agent_execute` 构造 `RunSpec` 并托管给监督者，SSE 由事件日志轮询产出：

```python
spec = RunSpec(run_id=run_id, root_run_id=root_run_id, parent_run_id=request.parent_run_id,
               task_id=request.task_id, session_id=request.session_id, message_id=request.message_id,
               workspace_id=workspace_id, agent_type=request.agent_type.value,
               budget=budget, request_fingerprint=_request_fingerprint(...))

async def runner(emit):
    async for item in _execute_stream(...):
        await emit(json.loads(item["data"]))

async def cancel_adapter() -> None:
    await adapter.interrupt(session_id)

await run_supervisor.start(spec, runner, cancel_adapter)

async def journal_stream():
    after_seq = 0
    while True:
        events, record = await run_supervisor.wait_for_events(run_id, after_seq, timeout=15)
        for envelope in events:
            after_seq = envelope.seq
            yield {...}            # SSE 帧
```

`/v1/session/{id}/interrupt` 通过 `cancel_session()` 取消该 session 下所有活跃 Run。

### Run 查询端点 (`src/api/v1/runs.py`)

```python
router = APIRouter(prefix="/v1/runs", tags=["runs"])

@router.get("")
async def list_active_runs(...): ...
@router.get("/{run_id}")
async def get_run(run_id, ...): ...
@router.get("/{run_id}/events")
async def get_run_events(run_id, after_seq: int = Query(0, ge=0), wait_seconds: float = Query(0, ge=0, le=30), ...): ...
@router.post("/{run_id}/cancel")
async def cancel_run(run_id, request: CancelAgentRunRequest | None = None, ...): ...
@router.post("/{run_id}/resume")
async def resume_run(run_id, request: ResumeRunRequest, ...): ...   # 冲突恢复动作，转发 ConflictRecoveryCoordinator
```

### 服务鉴权 (`src/security/authentication.py`)

ASGI 中间件，仅校验入站请求，与出站 `BACKEND_SERVICE_TOKEN` 区分：

```python
_ANONYMOUS_PATHS = frozenset({"/health/live"})

class ServiceAuthMiddleware:
    def __init__(self, app: ASGIApp, enabled: bool): ...
    # enabled=False 或 path ∈ _ANONYMOUS_PATHS 时放行
    # 否则要求 Authorization: Bearer <AGENTEND_SERVICE_TOKEN>
    # hmac.compare_digest 比对；失败返回 401
```

### 路径边界 (`src/security/path_policy.py`)

```python
class PathPolicy:
    def __init__(self, allowed_repo_roots: list[str]): ...  # 拒绝 / 、$HOME 及其祖先、非目录
    @property
    def configured(self) -> bool: ...
    def resolve_repo(self, raw: str, *, must_exist: bool = True) -> Path: ...   # 必须落在某个 root 下
    def validate_managed_path(self, raw: str, expected: str) -> Path: ...        # expected="git_repo" 校验 .git
    @staticmethod
    def safe_open_parent(path: Path, boundary: Path) -> None: ...                # 写入前最终父目录 + 符号链接校验
```

`agent.py` 的 `_resolve_workspace` 在 `path_policy.configured` 时校验 `repo_path`（须为 git 仓库）与 worktree 路径，`_write_soul_document` 用 `safe_open_parent` 防越界。

### 启动校验 (`src/security/startup_validation.py`)

```python
def is_loopback_host(host: str) -> bool: ...           # localhost 或 loopback IP
def sandbox_capabilities() -> dict[str, bool]: ...      # 探测 bwrap / userns / cgroup2 等
def strict_sandbox_enforced(capabilities: dict[str, bool]) -> bool: ...
```

`lifespan` 据此强制：非 loopback 且未开启鉴权时直接 `RuntimeError`；`unsafe_process` 沙箱必须 loopback + `allow_unsafe_local_execution`。

### 配置项

| 配置 | 位置 | 默认值 | 说明 |
|---|---|---|---|
| `execution.run_store_path` | `config.yaml` | `logs/runs.sqlite3` | Run SQLite 路径 |
| `execution.max_concurrent_runs` | `config.yaml` | `4` | 并发 Run 上限 |
| `execution.sandbox.mode` | `config.yaml` | `unsafe_process` | `strict` / `unsafe_process` |
| `security.service_auth_enabled` | `config.yaml` | `false` | 是否启用服务鉴权 |
| `security.allowed_repo_roots` | `config.yaml` | `[]` | 仓库根白名单 |
| `security.allow_unsafe_local_execution` | `config.yaml` | `true` | 放行 unsafe_process 本地执行 |
| `AGENTEND_SERVICE_TOKEN` | `.env` | — | 入站服务鉴权 token |
| `BACKEND_SERVICE_TOKEN` | `.env` | — | 出站调用 Backend 的 token |

## 已知限制

1. **沙箱仅探测未强制**：`sandbox_capabilities` 探测 bubblewrap/userns/cgroup2，但当前默认 `unsafe_process`，strict 沙箱执行路径尚未接入；`health/ready` 的 `sandbox_enforced` 默认为 false。
2. **Run 状态仅 SQLite**：适合单实例；多实例需外部协调。
3. **预算中部分维度尚未运行时强制**：当前在 emit 路径强制 `max_event_count` / `max_output_bytes`，wall time 由 `wait_for` 兜底；`max_memory_mb` / `max_cpu_seconds` / `max_workspace_growth_bytes` 等需配合 strict 沙箱才完整生效。
