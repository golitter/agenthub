# Conversation Memory — Orchestrator 跨轮推理记忆持久化

## 实现了什么

Orchestrator 的 `memory_messages` 跨轮对话持久化。LangGraph 的 `memory_messages` 使用 `_add` reducer（列表拼接），每轮 `stream_chat` 调用时从 `ConversationMemoryStore` 加载历史记忆到 `initial_state`，Graph 完成时由 `save_mem_node` 将本轮新增消息写回文件。

核心设计决策：
- **持久化范围**：对话链（HumanMessage + AIMessage + ToolMessage）— 写入 `conversation_memory.json`
- **不持久化**：4 个动态上下文消息（pin / evolution / replan / orchestrator_context）— 每轮从最新数据源重新构建注入
- **保留策略**：最近 10 轮对话，按轮次裁剪（以 HumanMessage 为起点），不截断半轮

## 怎么实现的

### 存储层 (`src/orchestrator/memory/conversation_memory.py`)

`ConversationMemoryStore` 使用 LangChain 内置序列化，保留完整的消息类型和结构：

```python
class ConversationMemoryStore:
    """Persist Orchestrator's memory_messages across conversation turns."""

    def __init__(self, shared_dir: str | Path) -> None:
        self.memory_dir = Path(shared_dir) / "memory"
        self.memory_dir.mkdir(parents=True, exist_ok=True)

    @property
    def memory_path(self) -> Path:
        return self.memory_dir / "conversation_memory.json"

    def save_messages(self, messages: list) -> None:
        """Serialize *messages*, append to file, trim to retention limit."""
        existing = self._load_raw()
        new_entries = messages_to_dict(messages)
        combined = existing + new_entries
        trimmed = self._trim_to_turns(combined, _MAX_TURNS)
        self._write(trimmed)

    def load_messages(self) -> list:
        """Deserialize stored messages back into LangChain message objects."""
        raw = self._load_raw()
        if not raw:
            return []
        try:
            return messages_from_dict(raw)
        except Exception:
            return []
```

存储路径：`{shared_dir}/memory/conversation_memory.json`

轮次裁剪策略 — 保留最近 10 轮，以 HumanMessage 为起点：

```python
_MAX_TURNS = 10

@staticmethod
def _trim_to_turns(entries: list[dict], max_turns: int) -> list[dict]:
    """Keep the last *max_turns* complete turns."""
    human_indices = [i for i, e in enumerate(entries) if e.get("type") == "human"]
    if len(human_indices) <= max_turns:
        return entries
    start = human_indices[-max_turns]
    return entries[start:]
```

### 加载时机 (`src/adapters/orchestrator.py`)

`stream_chat` 构建初始状态时从持久化存储加载历史：

```python
initial_state = {
    ...
    "memory_messages": ConversationMemoryStore(shared_dir).load_messages(),
    "pin_context": system_prompt_append or "",
    "orchestrator_context": orchestrator_context,
}
```

### 持久化节点 (`src/orchestrator/planning/graph.py`)

`save_mem_node` 在 Graph 完成前将本轮 `memory_messages` 写入文件。由于 `state["memory_messages"]` 在 Turn 开始时已通过 `load_messages()` 加载全量历史、并由 `_add` reducer 累加本轮新增，节点拿到的就是权威全量，故直接用 `replace_messages()` 覆盖写入，避免 `save_messages()`（读取 + 追加）导致的重复：

```python
def save_mem_node(state: GraphState) -> dict:
    """Persist memory_messages to file-based store before graph completes.

    Uses ``replace_messages`` to write the authoritative state directly,
    avoiding duplication that ``save_messages`` (load + append) would cause.
    """
    try:
        memory_messages = state.get("memory_messages", [])
        if memory_messages:
            store = ConversationMemoryStore(state["shared_dir"])
            store.replace_messages(memory_messages)
    except Exception:
        logger.exception("save_mem_node: failed to persist conversation memory")
    return {}
```

### 数据流

```
Turn N:
  stream_chat → load_messages() → initial_state["memory_messages"]
    → skill_prepare_node → 计算 pin/evolution context
    → reason_node → 注入 context_msgs + memory_messages + HumanMessage
    → LLM 调用 → 返回 [context_msgs, HumanMsg, AIMsg/ToolMsgs]
    → save_mem_node → replace_messages() → 写入 JSON

Turn N+1:
  stream_chat → load_messages() → 拿到 Turn N 的完整链
    → skill_prepare_node → 计算最新 pin/evolution → state
    → reason_node → 注入最新 context + 历史记忆 + 新 HumanMessage
```

### 动态上下文注入方式

动态上下文（pin / evolution / orchestrator_context）通过不同消息类型注入到 `reason_node` 的消息列表中：

| 上下文 | 消息类型 | 语义 |
|--------|---------|------|
| Pin 约束 | `SystemMessage` | 全局约束，必须遵守 |
| 编排经验 | `SystemMessage` | 历史经验参考 |
| 重规划反馈 | `HumanMessage` | 上一轮失败原因 |
| 群聊上下文 | `HumanMessage` | 其他 Agent 消息 |
