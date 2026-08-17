# 出站 SSE 负载净化（transport sanitizer）

## 实现了什么

在 AgentEnd 的出站边界裁剪事件负载，避免单个工具的大体量输出（如内置 render/taskctl 的 HTML、文件正文）逐帧放大 SQLite 事件日志、Redis 与 SSE 带宽。净化发生在事件进入 Run 事件日志之前，`/v1/agent/stream`、`/v1/agent/execute` 与 `/v1/runs/{run_id}/events` 的消费者看到的都是净化后副本；Agent 内部上下文与 Langfuse trace（在净化前包装原始流）不受影响。

新增模块：`src/transport/sanitizer.py`。

## 怎么实现的

### 净化函数 (`src/transport/sanitizer.py`)

`sanitize_stream_event` 返回一个有界副本（`event.model_copy(update={"content": content})`），永不修改原始事件对象：

```python
_MAX_ERROR_BYTES = 8 * 1024

def sanitize_stream_event(event: StreamEvent) -> StreamEvent:
    content = dict(event.content or {})
    content.pop("raw", None)                 # CLI 原始 JSON 始终剥离
    # 按 event_type 分支裁剪（见下表），记录字节规模但移除大体量负载
    return event.model_copy(update={"content": content})
```

各事件类型的处理：

| EventType | 处理 |
|---|---|
| `TOOL_CALL` | 移除 `args`/`result`，改记 `input_size`/`output_size`（字节） |
| `TOOL_RESULT` | 移除 `result`，改记 `output_size` |
| `DONE` | 移除 `text`，置 `text_omitted=True` + `text_bytes` |
| `ERROR` | `error`/`message` 超过 8KiB 时按 UTF-8 安全截断（后缀 `…[truncated]`），记 `{key}_truncated` + `{key}_bytes` |
| 其他 | 仅剥离 `raw` |

### 字节计量

`_byte_size` 对 `str`/`bytes` 直接计数，对 dict/list 走 `json.JSONEncoder.iterencode` 逐块求和，避免为了"报体积"再物化一份完整 JSON 字符串造成额外 O(n) 分配。`_truncate_utf8` 在截断时关闭替换解码，保证可见前缀不会落在半个 UTF-8 码字上。

### 设计要点

- **不改 Agent 上下文**：CLI 适配器仍聚合完整的工具输出供 `/execute` 与 Langfuse trace；净化仅发生在出站 SSE 这一跳。
- **副本而非深拷贝**：仅浅拷贝顶层 mapping，移除键但不改其嵌套值，避免把要剔除的大负载再复制一份。
- **`raw` 一律剥离**：适配器 fallback 字段含完整 CLI JSON，无稳定 UI 语义，绝不越过传输边界。

## 相关模块

- 调用方：`src/api/v1/agent.py` 的 `_execute_stream()` 在 yield 每条事件前调用本函数，随后事件经 `emit` 写入 Run 事件日志，再由 `journal_stream()` 轮询产出 SSE 帧。
- 原始事件：`src/schemas/events.py` 的 `StreamEvent` / `EventType`。
