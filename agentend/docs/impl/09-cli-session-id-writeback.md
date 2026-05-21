# CLI session_id 回写机制

## 问题

CLI（claudecode / opencode）首次调用时返回自己的 session ID（如 `ses_xxx`），但该 ID 没有被写回 `SessionMappingStore`。导致：

1. `_resolve_session` 预生成 UUID 写入 mapping，但 CLI 不认这个 UUID
2. kwargs key 不匹配：`agent.py` 传 `"cli_session_id"`，opencode adapter 读 `"opencode_session_id"`，`--session` 参数从未生效
3. 两个适配器（claudecode / opencode）返回的 cli_session_id 格式不同，但 mapping 逻辑应统一

## 修复

### 核心思路

- **首次调用**：mapping 为空，不预生成 UUID，不传 `--session`，CLI 自建 session
- **CLI 返回**：`step_start`(opencode) / `system`(claudecode) 事件带真实 `sessionID`
- **回写 mapping**：从 INIT 事件提取真实 cli_session_id，写入 `SessionMappingStore`
- **再次调用**：mapping 有值，传 `--session <real_id>` + `--fork` / `--resume`

### 改动文件

#### `src/api/v1/agent.py`

1. **`_resolve_session`**：移除 `uuid.uuid4()` 预生成逻辑。mapping 为空时返回 `("", False)`；有值时返回 `(stored_id, True)`
2. **`_execute_stream`**：新增 `session_store` 参数。收到 INIT 事件时提取 `cli_session_id` 写回 mapping
3. **`agent_execute`**：用内联 `_collect()` 替代 `adapter.chat()`，流式收集文本的同时在 INIT 事件时回写 mapping
4. kwargs key 统一为 `"cli_session_id"`，新增 `from src.schemas.events import EventType`

#### `src/adapters/opencode.py`

- `_build_command` 参数 `opencode_session_id` → `cli_session_id`
- `stream_chat` 中 `kwargs.get("opencode_session_id")` → `kwargs.get("cli_session_id")`

### 数据流

```
首次调用:
  request → _resolve_session → mapping 空 → ("", False)
  → adapter.stream_chat(cli_session_id="") → 不传 --session → CLI 自建 session
  → step_start 返回 sessionID="ses_xxx" → INIT 事件
  → session_store.set_cli_session_id() 写入 mapping
  → 返回响应

再次调用 (同一 session_id + task_id):
  request → _resolve_session → mapping 有值 → ("ses_xxx", True)
  → adapter.stream_chat(cli_session_id="ses_xxx", is_resume=True)
  → --session ses_xxx --fork → CLI resume 该 session
  → 返回响应
```

### 两个适配器的 INIT 事件差异

| 适配器 | CLI 事件类型 | session_id 字段 | resume 方式 |
|--------|-------------|----------------|------------|
| claudecode | `system` | `data["session_id"]` | `--resume <id>` |
| opencode | `step_start` | `data["sessionID"]` | `--session <id> --fork` |

两者在 `stream_chat` 中都通过 `kwargs.get("cli_session_id")` 读取，格式不同但 mapping 逻辑一致。
