# Session ID 与 CLI Session 关联实现

## 实现了什么

在 API session_id 与 CLI session UUID 之间建立映射关系，解决各 CLI Agent（Claude CLI / OpenCode CLI / Codex CLI / Pi CLI）不认识外部传入的 session ID、一次性执行模式默认不持久化会话的问题。通过文件持久化存储映射，支持跨请求的会话恢复。

## 怎么实现的

### 映射策略

在内部维护 `request.session_id` <-> `CLI session UUID` 的映射关系：

1. **首次调用**（传入 `session_id`，无映射记录）→ 不传 session 参数 → CLI 自建 session → INIT 事件回写 mapping
2. **后续调用**（传入同一 `session_id`，有映射记录）→ 从映射中取出 CLI UUID → `--resume` / `--session --fork` 传给 CLI → CLI 恢复上下文

`session_id` 由 `AgentRequest` 强制校验（必填，1–128 位 `[A-Za-z0-9_-]`，首尾为字母数字），不存在"未传 session_id"的调用路径。

### SessionMappingStore（`src/session/store.py`）

文件持久化存储，管理 `session_id -> CLI session UUID` 映射：

- 存储路径：来自 `config.yaml` 的 `session.store_path`（默认 `logs/session_mappings.json`）
- Key 格式：`{session_id}::{task_id}`（复合键，同一 session 不同 task 有独立映射）
- 格式：`{"session_id::task_id": "cli_session_uuid", ...}`
- 每次写入后立即持久化到文件

### _resolve_session 流程（`src/api/v1/agent.py`）

```
查询 store.get_cli_session_id(session_id, task_id)
  ├─ 有映射 → 返回 (internal_id, cli_uuid, is_resume=True)  → 用 --resume / --session --fork
  └─ 无映射 → 返回 (internal_id, "", is_resume=False) → 不传 session 参数
              CLI 自建 session → INIT 事件回写 → 后续调用走映射
```

### Claude CLI 参数映射

`claude.py` 在 `cli_session_id` 非空时才追加 session 参数，否则完全不传，由 CLI 自建会话并经 INIT 事件回写：

| 场景 | CLI 参数 |
|---|---|
| 首次调用（无映射） | 不传 session 相关参数 → CLI 自建 → INIT 回写映射 |
| 恢复会话（resume） | `--resume <uuid>` |
| Fork 会话 | `--session-id <uuid>` |

### CLI 输出解析

CLI `stream-json --verbose` 输出的 `assistant` 消息结构：
```json
{"type": "assistant", "message": {"content": [{"type": "text", "text": "..."}]}}
```
文本在 `data.message.content[]`，不在 `data.content[]`。

`result` 消息结构：
```json
{"type": "result", "result": "最终文本", "usage": {...}}
```
文本在 `result` 字段，`usage` 包含 token 统计。

### 改动文件

| 文件 | 说明 |
|---|---|
| `src/session/store.py` | **新建** — 文件持久化存储，管理 `session_id -> CLI session UUID` 映射 |
| `src/schemas/events.py` | 新增 `INIT` 事件类型，用于标识 CLI 的 `system/init` 事件 |
| `src/schemas/response.py` | 响应模型（不暴露 `cli_session_id` 给调用方） |
| `src/adapters/claude.py` | 修复 CLI 参数：`--session-id`（新建）/ `--resume`（恢复）；修复输出解析：`assistant` 事件从 `data.message.content` 取文本；新增 `--verbose` 标志 |
| `src/api/v1/agent.py` | 核心串联：`_resolve_session()` 返回 `(internal_session_id, cli_session_id, is_resume)`；首次分配 UUID 并持久化；后续通过 `--resume` 恢复 |
| `src/api/dependencies.py` | 注册 `SessionMappingStore` 依赖 |
| `src/app/dependencies.py` | 添加 `create_session_store()` 工厂函数 |
| `src/app/main.py` | lifespan 中初始化 `session_store` |

### 已知限制

1. **存储方式**：当前使用 JSON 文件（`atomic_write_text` 原子写），仅适合单实例开发环境，生产环境需替换为 Redis/MySQL。
2. **INIT 回写依赖事件流**：如果 CLI 未输出 INIT 事件（异常退出），mapping 不会被建立，后续无法 resume。
