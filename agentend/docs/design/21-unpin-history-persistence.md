# Pin 取消事件持久化 + save_mem_node 去重

## 实现了什么

解决两个问题：(1) Pin 取消后 Orchestrator 无法感知已取消的约束，可能继续按旧约束推理；(2) `save_mem_node` 每轮将全量 `memory_messages` 拼接到已有文件内容后，导致历史重复。

改动：`ConversationMemoryStore` 新增 `replace_messages()` 直接覆盖写入；`PinMemory.unpin()` 返回被移除的 pin 元数据；pin_remove API 取消后写入 SystemMessage 到对话记忆；新增 `POST /v1/pin/announcement-unpin` 端点供 Backend 通知。

## 怎么实现的

### 问题

#### 问题一：Pin 取消无历史痕迹

当前 `PinMemory.unpin()` 仅从 `_pins.yaml` 删除条目，Orchestrator 在后续轮次中无法感知「曾经有这个约束但已被取消」。如果用户取消了某个 Pin 约束，LLM 可能仍按照已取消的约束推理。

#### 问题二：save_mem_node 历史重复

当前 `save_mem_node` 调用 `save_messages()`，其内部逻辑为 `existing + new_entries`：

```python
def save_messages(self, messages: list) -> None:
    existing = self._load_raw()                    # ① 从文件读已有
    new_entries = messages_to_dict(messages)        # ② 序列化传入的消息
    combined = existing + new_entries               # ③ 拼接
```

而 `save_mem_node` 传入的是 `state["memory_messages"]`，它在 Turn 开始时通过 `load_messages()` 加载了全量历史：

```python
# orchestrator.py:135 — 加载全量历史
"memory_messages": ConversationMemoryStore(shared_dir).load_messages(),

# graph.py:91 — _add reducer 持续累加
memory_messages: Annotated[list, _add]
```

导致每轮结束时 `save_messages()` 执行：

```
existing  = _load_raw()          → [A, B]          (文件中的历史)
new       = messages_to_dict()   → [A, B, C, D]   (state = 加载的历史 + 本轮新增)
combined  = existing + new       → [A, B, A, B, C, D]   ← A, B 重复
```

## 改动清单

### 1. ConversationMemoryStore 新增 `replace_messages()`

**文件**：`src/orchestrator/memory/conversation_memory.py`

新增方法，直接用传入的消息覆盖文件，不读已有内容：

```python
def replace_messages(self, messages: list) -> None:
    """Replace the store with exactly *messages* (after trimming).

    Unlike ``save_messages`` which reads existing entries and appends,
    this method writes *messages* directly — no duplication.
    """
    entries = messages_to_dict(messages)
    trimmed = self._trim_to_turns(entries, _MAX_TURNS)
    self._write(trimmed)
```

`save_messages()` 保留不变，供外部增量写入场景使用（如 Pin 取消事件）。

### 2. save_mem_node 改用 `replace_messages()`

**文件**：`src/orchestrator/planning/graph.py`

`state["memory_messages"]` 已包含全量权威数据（加载的历史 + 本轮新增），直接覆盖即可：

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

### 3. PinMemory.unpin() 返回被移除的 pin 元数据

**文件**：`src/orchestrator/memory/pin_memory.py`

返回值从 `bool` 改为 `dict | None`，让调用方知道取消了什么：

```python
def unpin(self, filename: str) -> dict | None:
    """Remove pin and return the removed entry, or None if not found."""
    pins = self._load_pins()
    removed = next((p for p in pins if p["filename"] == filename), None)
    if not removed:
        return None
    self._save_pins([p for p in pins if p["filename"] != filename])
    return removed
```

> **兼容性**：`dict` 为 truthy、`None` 为 falsy，运行时与原 `bool` 行为一致。
> API 层 `pin_remove` 的 `if not removed` 判断无需修改。

### 4. pin_remove API 取消后写入 ConversationMemoryStore

**文件**：`src/api/v1/pin.py`

取消 Pin 后，追加一条 `SystemMessage` 到对话记忆，告知 LLM 该约束已失效：

```python
from langchain_core.messages import SystemMessage
from src.orchestrator.memory.conversation_memory import ConversationMemoryStore

@router.post("/remove")
async def pin_remove(req: PinRemoveRequest):
    pm = _pin_memory(req.shared_dir)
    removed = pm.unpin(req.filename)
    if not removed:
        raise HTTPException(status_code=404, detail=f"Pin not found: {req.filename}")

    # 将取消事件写入持久化历史（仅此一次）
    memory = ConversationMemoryStore(shared_dir=req.shared_dir)
    memory.save_messages([
        SystemMessage(content=(
            f"[Pin 约束已取消] **{removed['title']}** "
            f"(来源: {removed.get('source', 'unknown')}, "
            f"原摘要: {removed.get('summary', '')}) "
            f"— 该约束不再生效，后续规划无需遵守。"
        ))
    ])

    return {"success": True, "removed": removed}
```

此处使用 `save_messages()`（读取 + 追加），因为这是**增量写入**，只有 API 这一个调用方。

### 5. Backend 删除 pinned announcement 时通知 agentend

**问题**：前端删除 pinned announcement 走的是 `DELETE /api/tasks/:taskId/announcements/:id`（Backend MySQL），不经过 agentend 的 `/v1/pin/remove`。Orchestrator 无感知。

#### 5a. agentend 新增 `POST /v1/pin/announcement-unpin`

**文件**：`src/api/v1/pin.py`

新端点接收 `{shared_dir, content, sender_name}`，直接写入 unpin SystemMessage：

```python
@router.post("/announcement-unpin")
async def announcement_unpin(req: AnnouncementUnpinRequest):
    memory = ConversationMemoryStore(shared_dir=req.shared_dir)
    memory.save_messages([
        SystemMessage(content=(
            f"[公告约束已取消] 来自 **{req.sender_name}** 的置顶公告已删除: "
            f"\"{req.content[:200]}\" "
            f"— 该约束不再生效，后续规划无需遵守。"
        ))
    ])
    return {"success": True}
```

#### 5b. Backend agentend_client 新增 `NotifyAnnouncementUnpin`

**文件**：`backend/pkg/agentend_client/client.go`

新增请求结构体和 POST 方法，复用 `ReviewAgent` 模式。

#### 5c. Backend `DeleteAnnouncement` 增加 unpin 通知

**文件**：`backend/internal/handler/announcement.go`

- struct 新增 `agentClient` 字段
- 删除前先查公告获取 pinned 状态和内容
- 若 `pinned=true`，异步 goroutine 通知 agentend
- shared_dir 派生：`filepath.Join(filepath.Dir(repoPath), "worktrees", taskID, "shared", ".agent")`

## 数据流

### 路径 A：通过 agentend pin API（文件系统 Pin）

```
用户取消 Pin（agentend pin_remove）
  │
  ▼
pin_remove API
  ├── pm.unpin(filename)              → 返回 {title, summary, source, ...}
  └── memory.save_messages([SystemMessage("[Pin 已取消] ...")])
       │
       ▼
  文件追加 unpin SystemMessage（第一次也是唯一一次写入）
```

### 路径 B：通过 Backend 删除 pinned announcement（前端实际路径）

```
前端 DELETE /api/tasks/:taskId/announcements/:id
  │
  ▼
Backend DeleteAnnouncement
  ├── MySQL DELETE announcement
  └── (if pinned) goroutine → agentend POST /v1/pin/announcement-unpin
       │                         {shared_dir, content, sender_name}
       └── ConversationMemoryStore.save_messages([SystemMessage("[公告约束已取消] ...")])
```

### 后续 Orchestrator 对话（两条路径共用）

```
stream_chat → load_messages()
  └── 读到 [历史..., unpin SystemMessage]    → 放入 state["memory_messages"]
        │
        ▼
reason_node
  └── memory_messages 注入 LLM context（含 unpin 事件）
        │
        ▼
save_mem_node
  └── replace_messages(state["memory_messages"])   ← 直接覆盖，不重复
```

## 消息结构变化

修改后 reason_node 的完整消息注入顺序：

```
┌─────────────────────────────────────┐
│  系统提示词 (SystemMessage)          │
│  基础身份 + 规则 + 工具 + 技能       │
│  {agents_desc}    可用 Agent 列表    │
│  {soul_section}   SOUL.md 身份定义   │
│  {skills_section} L1 技能元数据      │
│  {tools_section}  工具说明           │
│  {workspace_section} 工作区目录说明  │
│  规则（判断逻辑、Agent/Skill 区别等）│
├─────────────────────────────────────┤
│  动态上下文消息（每轮重建，不持久化）│
│  SystemMessage: Pin 约束（活跃的）   │
│  SystemMessage: 编排经验             │
│  HumanMessage: 重规划反馈（仅重规划）│
│  HumanMessage: 群聊上下文            │
├─────────────────────────────────────┤
│  memory_messages（持久化历史）       │
│  对话链：Human / AI / Tool 消息     │
│  SystemMessage: Pin 取消事件（如有）│  ← 新增
├─────────────────────────────────────┤
│  HumanMessage: 当前用户消息          │
│  HumanMessage: 审查反馈（仅审查时）  │
└─────────────────────────────────────┘
```

变化集中在 `memory_messages` 层：取消事件以 `SystemMessage` 形式持久化，LLM 可感知已取消的约束。

上方「动态上下文」层的 Pin 约束不受影响 — 它始终反映**当前活跃**的 Pin 列表（通过 `get_context()`）。两层职责不重叠。

## 涉及文件汇总

| 文件 | 改动类型 |
|------|---------|
| `src/orchestrator/memory/conversation_memory.py` | 新增 `replace_messages()` |
| `src/orchestrator/planning/graph.py` | `save_mem_node` 改用 `replace_messages()` |
| `src/orchestrator/memory/pin_memory.py` | `unpin()` 返回 `dict \| None` |
| `src/api/v1/pin.py` | 取消后写入 `SystemMessage`；新增 `POST /announcement-unpin` 端点 |
| `backend/pkg/agentend_client/client.go` | 新增 `NotifyAnnouncementUnpin` 方法 |
| `backend/internal/handler/announcement.go` | struct 加 agentClient，DeleteAnnouncement 增加 unpin 通知 |
| `backend/cmd/server/main.go` | 传 agentClient 给 NewAnnouncementHandler |
