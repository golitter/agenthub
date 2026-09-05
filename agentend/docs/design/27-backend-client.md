# BackendClient — 与 Go Backend 的出站通信客户端

## 实现了什么

`src/clients/backend_client.py` 提供 AgentEnd → Go Backend 的唯一出站 HTTP 通道，覆盖五类调用：

1. **子 Run 创建** — `run_task()` 经 Backend 内部接口创建带完整血缘身份的子 Agent Run（Orchestrator 分发与 `ask_agent` 咨询共用）。
2. **子 Run SSE 订阅** — `stream_result()` 订阅子 Run 的事件流并解析为 dict。
3. **Active Pin 查询** — `get_pinned_announcements()` 获取 pinned announcements，是 Active Pin Snapshot 的唯一权威数据源（fail-closed，见 [26-orchestrator-context-compaction.md](26-orchestrator-context-compaction.md)）。
4. **群聊窗口查询** — `get_agent_window_messages()` 读取会话的群聊窗口消息。
5. **内置技能上报** — `report_builtin_skills()` 启动时向 Backend 上报内置 skill 清单（taskctl / render）。

## 怎么实现的

### 客户端构造

```python
class BackendClient:
    def __init__(self, base_url: str, timeout: float = 30.0) -> None:
        self._base_url = _normalize_loopback_url(base_url)
        token = os.environ.get("BACKEND_SERVICE_TOKEN", "").strip()
        self._service_headers = {"Authorization": f"Bearer {token}"} if token else {}
        # run_task 用短超时；stream_result 使用独立的 stream client
        self._client = httpx.AsyncClient(timeout=timeout, trust_env=False)
```

- `base_url` 来自 `config.yaml` 的 `backend.url`（默认 `http://localhost:8080`）。
- `_normalize_loopback_url()` 把 `localhost` 改写为 `127.0.0.1`，避免 IPv6 解析偏差。
- 出站鉴权使用 `.env` 的 `BACKEND_SERVICE_TOKEN` Bearer 头，与入站的 `AGENTEND_SERVICE_TOKEN`（`ServiceAuthMiddleware`）相互独立。
- `trust_env=False` 阻止 httpx 读取系统代理环境变量，保证本机回环调用不受代理干扰。

### run_task() — 创建子 Run

`POST /api/internal/tasks/{task_id}/run`，请求体携带完整内部子 Run 身份字段（与 `AgentRequest` 的内部血缘字段一一对应）：

```python
async def run_task(
    self, task_id: str, session_id: str, message: str, agent_type: str,
    cwd: str = "", skip_user_message: bool = True,
    root_run_id: str = "", parent_run_id: str = "", budget: dict | None = None,
    run_id: str = "", integration_attempt: int = 0, current_run_id: str = "",
    plan_task_id: str = "", workspace_id: str = "", integration_operation_id: str = "",
    workspace_handle: str = "", integration_capability: str = "",
) -> RunTaskResult:   # RunTaskResult(run_id, message_id)
```

- `run_id` 缺省时客户端生成 UUID，由 Backend 落库后原样返回。
- 响应必须同时包含 `run_id` 与 `message_id`，缺失即抛 `ValueError`（不静默降级）。
- 调用方：`ExecutionEngine`（子任务分发）与 `reason_node` 的 `_handle_ask_agent_call`（咨询，带 3 次重试）。

### stream_result() — 订阅子 Run SSE

`GET /api/internal/tasks/{task_id}/stream?message_id=...&session_id=...`：

```python
async def stream_result(self, task_id: str, message_id: str, session_id: str) -> AsyncIterator[dict]:
    # 独立 httpx.AsyncClient：connect/write/pool 10s，read 600s（_SSE_READ_TIMEOUT）
    # aiter_text() 增量接收 + 手动按 "\n" 分行，只解析 "data: " 前缀的 JSON 行
    # 流结束后排空残留缓冲（最后一个事件可能不以换行结尾）
```

SSE 长连接使用独立客户端与长读取超时，不复用 `run_task` 的短超时客户端；单行 JSON 解析失败仅记 warning 并跳过，不中断整条流。

### get_pinned_announcements() — Active Pin 权威查询

`GET /api/internal/tasks/{task_id}/announcements?pinned=true`：

```python
async def get_pinned_announcements(self, task_id: str) -> list[dict]:
    """空列表只表示 Backend 成功确认当前没有 Pin；传输、HTTP、JSON
    或响应结构错误必须抛出，调用方据此执行 fail-closed。"""
```

- 响应结构非法（非 dict、缺 `data`、`data` 非 list）直接抛 `ValueError`，不返回 `[]`。
- 与 `get_agent_window_messages()` 的"出错返回空列表（优雅降级）"策略刻意相反：Pin 是硬约束，状态未知时必须停止规划而非无约束执行（详见 [26-orchestrator-context-compaction.md](26-orchestrator-context-compaction.md) 第 4 节）。

### get_agent_window_messages() — 群聊窗口查询

`GET /api/internal/tasks/{task_id}/messages/window?session_id=...`。出错时记录 warning 并返回 `[]`；窗口消息属于参考资料，缺失不应阻断主流程。

### report_builtin_skills() — 内置技能上报

`POST /api/internal/builtin-skills`，指数退避重试（2s/4s/8s 共 3 次尝试）：

```python
delays = [2.0, 4.0, 8.0]
for attempt, delay in enumerate(delays, 1):
    try:
        resp = await self._client.post(..., json=skills, headers=self._service_headers)
        resp.raise_for_status(); return
    except Exception:
        if attempt < len(delays):
            await asyncio.sleep(delay)
# 全部失败只记 error，不抛出 —— 不阻塞服务启动
```

上报内容由 `src/app/main.py` lifespan 内的 `_report_builtin_skills()` 生成：扫描 `config.yaml` 的 `skills.builtin_dir`，解析每个 `SKILL.md` 的 frontmatter（name/description），以 `{"name", "description", "builtin": True, "source": "builtin"}` 列表异步上报（`asyncio.create_task`）。

### 生命周期

- 组装：`create_backend_client()`（`src/app/dependencies.py`），lifespan 启动时挂到 `app.state.backend_client`。
- 关闭：lifespan shutdown 中 `await app.state.backend_client.close()`。

## 相关文档

- [11-orchestrator-planning.md](11-orchestrator-planning.md) — ExecutionEngine / ask_agent 如何消费 run_task + stream_result
- [22-run-lifecycle-and-sandbox.md](22-run-lifecycle-and-sandbox.md) — 出站 `BACKEND_SERVICE_TOKEN` 与入站服务鉴权的边界
- [26-orchestrator-context-compaction.md](26-orchestrator-context-compaction.md) — Pin 查询 fail-closed 语义
