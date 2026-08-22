# Session Manager — 会话管理

## 实现了什么

内存级会话管理，跟踪每个 Agent 会话的状态、进程句柄和消息历史。

## 怎么实现的

### Session 数据模型 (`src/session/models.py`)

`SessionState` 枚举定义在 `src/generated/session.py`，Session dataclass 在 models.py 中使用：

```python
class SessionState(str, Enum):     # 来自 generated/session.py
    IDLE = "idle"                  # 空闲
    RUNNING = "running"            # 执行中
    AWAITING_REVIEW = "awaiting_review"  # 等待审查（Orchestrator 规划审查）
    RESOLVING = "resolving"        # 冲突解决执行中（编排产物集成冲突）
    AWAITING_RESOLUTION = "awaiting_resolution"  # 等待冲突解决（待恢复协调器/用户处理）
    COMPLETED = "completed"        # 已完成
    INTERRUPTED = "interrupted"    # 已中断
    ERROR = "error"                # 错误
    INACTIVE = "inactive"          # 不活跃（DB 清理标记）

@dataclass
class Session:
    id: str                              # UUID
    agent_type: str                      # Agent 类型
    state: SessionState = IDLE
    process: asyncio.subprocess.Process | None = None  # 进程句柄
    workspace_path: str = ""             # 工作区路径
    created_at: datetime = field(default_factory=datetime.now)
    last_active: datetime = field(default_factory=datetime.now)
    history: list[dict] = field(default_factory=list)  # 消息历史
    metadata: dict = field(default_factory=dict)  # 扩展元数据
```

### 状态机

```
IDLE → RUNNING → COMPLETED → RUNNING
                 → INTERRUPTED → RUNNING
                 → ERROR → RUNNING
                 → AWAITING_REVIEW → RUNNING（审查通过后继续执行）
                 → RESOLVING → RUNNING / AWAITING_RESOLUTION / COMPLETED / ERROR
                 → AWAITING_RESOLUTION → RESOLVING / COMPLETED / INTERRUPTED
```

状态转移规则定义在 `_VALID_TRANSITIONS` 字典中（`src/session/models.py`）。非法转移抛出 `ValueError`。

仅 `INACTIVE` 为终态，不可再转移。`COMPLETED` / `INTERRUPTED` / `ERROR` 均可转回 `RUNNING`（同一会话再次收到请求时复用）。

### SessionManager (`src/session/manager.py`)

全部在内存中管理，通过 `dict[str, Session]` 存储。

#### CRUD 方法

| 方法 | 说明 |
|------|------|
| `create(agent_type, metadata, workspace_path, session_id=None)` | 创建新 Session；缺省生成 UUID，传入已存在的 `session_id` 抛 `ValueError` |
| `get(session_id)` | 获取 Session，不存在返回 `None` |
| `list()` | 返回所有 Session 列表 |
| `update_state(session_id, new_state)` | 状态转移，含合法性校验，并刷新 `last_active` |
| `destroy(session_id)` | 终止进程 + 移除 Session |
| `record_history(session_id, entry)` | 记录消息到 history，更新 last_active |

#### 销毁流程 (`destroy`)

1. 检查 Session 是否存在
2. 如果有运行中进程：`terminate_process_group()` 向进程所在进程组发 SIGTERM → 等待超时（`config.yaml` 的 `execution.process_terminate_timeout`）→ SIGKILL 整组
3. 从 `_sessions` 字典中移除
4. 返回 `True`（存在）/ `False`（不存在）
