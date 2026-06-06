# 跨 Agent 记忆 — "你离开期间其他人说了什么"

> 设计文档 v2 — 群聊中子 Agent 可见其他 Agent 的消息

## Context

当前群聊中，每个子 Agent（Claude Code / OpenCode / Codex）执行时只能看到：
1. 自己的 CLI session 历史（通过 `--session-id` 等）
2. Orchestrator 分发的任务内容（`dispatch.content`）

**问题**：Agent A 执行时完全不知道 Agent B/C 说了什么。

**目标**：当 Agent A 即将执行时，注入「自 A 上次发言以来，其他 Agent 的消息」作为上下文。

```
群聊序列:
  a: xxx      ← a 发言
  b: yyy      ← b 发言
  c: lll      ← c 发言
  a: 222      ← a 即将发言，需要看到 b:yyy 和 c:lll
```

**窗口语义**：从当前 Agent 上次发言到现在，这段时间内**其他 Agent** 的消息。不是全部历史。

### 两种场景的调用链

```
场景 A：单聊（用户 + 1 个 Agent）
═══════════════════════════════════
Frontend → Backend RunTask → AgentEnd /stream → adapter.stream_chat()
  • 不经过 ExecutionEngine
  • Backend 查窗口 → 只有 1 个 session → 结果为空 []
  • GroupChatRule.check() → false → 不触发
  • 行为和现在完全一样，零改动感知


场景 B：群聊（用户 + 多个 Agent）
═══════════════════════════════════
Frontend → Backend RunTask(orchestrator)
  → AgentEnd → OrchestratorAdapter
  → ExecutionEngine → BackendClient.run_task() → Backend RunTask
  → AgentEnd → adapter.stream_chat()
  • Backend 查窗口 → 有其他 session → 有内容 → 注入
  • GroupChatRule 触发 → system_prompt_append
```

**核心洞察**：不需要 `is_group_chat` flag。Backend 每次都查窗口，单聊自然为空，群聊自然有内容。系统天然自适应。

### 架构决策

| 决策点 | 结论 | 理由 |
|--------|------|------|
| 窗口查询时机 | Backend RunTask 每次都查 | 空结果不碍事，无需 flag |
| 窗口过滤 | `status IN ("completed","streaming")` | 兜底安全网 |
| 消息截断 | 每条 2000 字符 | 防 context 膨胀 |
| short-circuit | 删掉 | ExecutionEngine 只在群聊时被调用，条件永远为 True |
| `is_group_chat` flag | 不需要 | 系统天然自适应 |
| Wave 语义 | ExecutionEngine 串行处理 wave | 天然保证 wave 级别窗口隔离 |
| Orchestrator 自身 | 通过 API 查窗口 | Orchestrator 也需要看到其他 Agent 的消息 |

---

## 方案

### 数据流（子 Agent）

```
OrchestratorAdapter._handle_execute()
  │ 只管 dispatch 任务，不拼上下文
  │
  ▼
ExecutionEngine._execute_task()
  │ backend_client.run_task(task_id, session_id, message, agent_type, cwd)
  │ （short-circuit 已删除，统一走 HTTP）
  │
  ▼
Go Backend RunTask handler
  │ 1. 查窗口消息（自己有 MySQL，直接查）
  │    • 过滤 status IN ("completed", "streaming")
  │    • 每条消息截断 2000 字符
  │ 2. 把消息塞进请求体: { group_chat_messages: [...] }
  │ 3. 调 AgentEnd /v1/agent/stream
  │
  ▼
AgentEnd API /v1/agent/stream
  │ 1. 从请求体取出 group_chat_messages
  │ 2. 放进 rule context
  │ 3. Rules 引擎: GroupChatRule 格式化 → system_prompt_append
  │ 4. adapter.stream_chat(session_id, message, system_prompt_append=...)
```

### 数据流（Orchestrator 自身）

```
OrchestratorAdapter.stream_chat()
  │ backend_client.get_agent_window_messages(task_id, session_id)
  │ build_group_chat_context(messages)
  │
  ▼
REASON prompt: {orchestrator_context} 占位
```

### Wave 级别的窗口隔离

```
ExecutionEngine.execute()
  │
  ├── Wave 1: [AgentA, AgentB]  ←── 并行执行
  │     ├── await asyncio.gather()  ←── 全部完成后消息 persisted
  │     └── wave 完成 ✓
  │
  ├── Wave 2: [AgentC]          ←── RunTask 查窗口
  │     │                           自然包含 Wave 1 全部输出
  │     └── ...
```

Wave 串行处理保证：Wave 2 的 RunTask 天然在 Wave 1 全部完成之后。
无需额外锁或协调机制，时间序保证语义正确。

---

## 实现步骤

### Step 1: Go Backend — 新增窗口查询 + RunTask 注入

#### 1a. 窗口查询方法（内部调用，不需要新路由）

**文件**: `backend/internal/service/impl/group_chat_window.go`（查询逻辑）+ `backend/internal/service/impl/task_service.go`（调用入口）

在 RunTask 中，调 AgentEnd 之前，查窗口消息：

```go
func (h *TaskHandler) RunTask(c *gin.Context) {
    ...existing logic...

    // 新增：查询群聊窗口消息
    groupChatMessages := h.fetchGroupChatWindow(taskID, sessionID)

    // 构建请求体时注入
    reqBody := map[string]interface{}{
        "message":              req.Message,
        "session_id":           sessionID,
        "agent_type":           req.AgentType,
        "cwd":                  req.CWD,
        "skip_user_message":    req.SkipUserMessage,
        "group_chat_messages":  groupChatMessages,  // 新增
    }
    // 发送给 AgentEnd
}

const maxGroupChatMsgLen = 2000 // 每条消息最大字符数

func (h *TaskHandler) fetchGroupChatWindow(taskID, sessionID string) []map[string]interface{} {
    // 1. 找到当前 session 最后一条 agent 消息的时间
    var lastMsg model.Message
    err := db.GetDB().
        Where("task_id = ? AND session_id = ? AND role = ?", taskID, sessionID, "agent").
        Order("created_at DESC").Limit(1).First(&lastMsg).Error

    // 2. 查询窗口：其他 session 在 T 之后的消息（含 streaming 作为兜底）
    query := db.GetDB().
        Where("task_id = ? AND session_id != ?", taskID, sessionID).
        Where("status IN ?", []string{"completed", "streaming"})
    if err == nil {
        query = query.Where("created_at > ?", lastMsg.CreatedAt)
    }

    var messages []model.Message
    query.Order("created_at ASC").Order("id ASC").Find(&messages)

    // 3. 转为 map 列表，截断长消息
    result := make([]map[string]interface{}, 0, len(messages))
    for _, m := range messages {
        content := m.Content
        runes := []rune(content)
        if len(runes) > maxGroupChatMsgLen {
            content = string(runes[:maxGroupChatMsgLen]) + "\n...[截断]"
        }
        result = append(result, map[string]interface{}{
            "role":       m.Role,
            "agent_name": m.AgentName,
            "content":    content,
        })
    }
    return result
}
```

#### 1b. 可选：也暴露为 API 路由（供 Orchestrator 自身查询用）

**文件**: `backend/internal/controller/impl/message_controller.go` + `backend/cmd/server/main.go`

```
GET /api/tasks/:taskId/messages/window?session_id=xxx
```

供 OrchestratorAdapter 查询 Orchestrator 自身的窗口消息。

### Step 2: AgentEnd — API 层解析 + GroupChatRule

#### 2a. API 层解析请求中的 group_chat_messages

**文件**: `agentend/src/api/v1/agent.py`

在 stream endpoint handler 中，从请求体取出 `group_chat_messages`，放进 rule context：

```python
@router.post("/stream")
async def stream_agent(request: AgentStreamRequest):
    ...
    # 新增：取出群聊消息，放进 rule context
    rule_context = {
        ...existing context fields...,
        "group_chat_messages": request.group_chat_messages or [],
    }

    # 应用 rules
    rule_results = apply_rules(rule_context)
    system_prompt_append = rule_results.get("system_prompt_append", "")
    ...
    async for event in adapter.stream_chat(session_id, message, system_prompt_append=system_prompt_append):
        ...
```

#### 2b. GroupChatRule

**文件**: `agentend/src/rules/builtin.py`（新增类）

```python
class GroupChatRule(BaseRule):
    name = "group_chat"
    description = "Injects group chat context from other agents"
    phase = "pre"
    priority = 6

    def check(self, context: dict) -> bool:
        return bool(context.get("group_chat_messages"))

    def enforce(self, context: dict) -> dict:
        from src.orchestrator.prompts.group_chat import build_group_chat_context

        messages = context.get("group_chat_messages", [])
        text = build_group_chat_context(cross_round_messages=messages)
        if not text:
            return {}

        return {"system_prompt_append": text}
```

### Step 3: AgentEnd — 群聊 Prompt 模板

**新文件**: `agentend/src/orchestrator/prompts/group_chat.py`

```python
GROUP_CHAT_CONTEXT = """\
## 群聊上下文

你正在参与一个多 Agent 协作群聊。以下是你上次发言后，其他成员发出的消息。
请参考这些内容来执行你的任务——了解当前进展、避免重复工作、与其他成员协作。

{messages}
"""


def build_group_chat_context(cross_round_messages: list[dict] | None = None) -> str:
    if not cross_round_messages:
        return ""

    lines = []
    for msg in cross_round_messages:
        role = msg.get("role", "")
        name = msg.get("agent_name", "")
        content = msg.get("content", "")
        if role == "user":
            lines.append(f"👤 用户:\n{content}")
        elif role == "agent" and name:
            lines.append(f"🤖 {name}:\n{content}")

    if not lines:
        return ""

    return GROUP_CHAT_CONTEXT.format(messages="\n\n".join(lines))
```

### Step 4: ExecutionEngine — 去掉 short-circuit

**文件**: `agentend/src/orchestrator/execution/engine.py`

删除 `_get_adapter`、`_iter_adapter_with_timeout`，统一走 HTTP：

```python
async def _execute_task(self, dispatch, timeout):
    ...
    agent_cwd = await self._ensure_worktree(dispatch)

    # 统一走 HTTP → Go Backend 查窗口 → GroupChatRule 注入
    message_id = await asyncio.wait_for(
        self._backend_client.run_task(
            task_id=self._task_id,
            session_id=session_id,
            message=dispatch.content,
            agent_type=agent_type,
            cwd=agent_cwd,
        ),
        timeout=30.0,
    )

    async for event in self._backend_client.stream_result(...):
        ...  # 现有逻辑不变
```

去掉的代码：`_get_adapter()`、`_iter_adapter_with_timeout()`、short-circuit 分支、`adapter_registry` 参数。

**为什么可以直接删**：ExecutionEngine 只在 Orchestrator 群聊场景下被调用，不存在单聊走 Engine 的路径。单聊直接 Backend → AgentEnd → adapter，不经过 ExecutionEngine。

### Step 5: Orchestrator 自身上下文

**文件**: `agentend/src/adapters/orchestrator.py`

Orchestrator 查自身窗口（知道用户 @其他 Agent 时发生了什么）：

```python
async def stream_chat(self, session_id, message, **kwargs):
    ...
    # 新增：Orchestrator 自身的跨 Agent 窗口
    orchestrator_context = ""
    if backend_client:
        window = await backend_client.get_agent_window_messages(task_id, session_id)
        if window:
            orchestrator_context = build_group_chat_context(cross_round_messages=window)

    initial_state = {
        ...
        "orchestrator_context": orchestrator_context,  # 新增
    }
```

**文件**: `agentend/src/orchestrator/planning/prompts.py`

REASON_PROMPT 新增 `{orchestrator_context}` 占位。

### Step 6: AgentEnd — BackendClient 新增方法（Orchestrator 自身用）

**文件**: `agentend/src/clients/backend_client.py`

```python
async def get_agent_window_messages(self, task_id: str, session_id: str) -> list[dict]:
    """GET /api/tasks/:taskId/messages/window?session_id=xxx"""
    resp = await self._client.get(
        f"{self._base_url}/api/tasks/{task_id}/messages/window",
        params={"session_id": session_id},
    )
    resp.raise_for_status()
    return resp.json().get("data", [])
```

### Step 7: OrchestratorAdapter._handle_execute() 简化

**文件**: `agentend/src/adapters/orchestrator.py`

不再需要 ContextBuilder，直接 dispatch：

```python
async def _handle_execute(self, ...):
    task_results = []
    engine = ExecutionEngine(
        backend_client=backend_client,
        workspace_mgr=workspace_mgr,
        repo_path=repo_path,
        task_id=task_id,
        shared_dir=shared_dir,
        cwd=cwd,
        # adapter_registry 去掉
    )

    for wave in execution_waves:
        async for event, result in self._stream_wave(engine, wave):
            yield event
            if result is not None:
                task_results.append(result)
                ...
```

`_stream_wave` 恢复原始简单版本（不再需要 context_builder 参数）。

---

## 文件变更总结

| 文件 | 变更 | 说明 |
|------|------|------|
| `backend/internal/service/impl/task_service.go` | 修改 | RunTask 中查窗口消息 + 注入请求体 + 截断 |
| `backend/internal/service/impl/group_chat_window.go` | 新增/修改 | 窗口查询逻辑 |
| `backend/internal/controller/impl/message_controller.go` | 新增方法 | `WindowMessages`（供 Orchestrator 自身查） |
| `backend/cmd/server/main.go` | 新增路由 | `GET /tasks/:taskId/messages/window` |
| `agentend/src/api/v1/agent.py` | 修改 | 解析 `group_chat_messages` → rule context |
| `agentend/src/rules/builtin.py` | 新增类 | `GroupChatRule` |
| `agentend/src/orchestrator/prompts/group_chat.py` | **新建** | 群聊 Prompt 模板 |
| `agentend/src/clients/backend_client.py` | 新增方法 | `get_agent_window_messages()`（Orchestrator 自身用） |
| `agentend/src/orchestrator/execution/engine.py` | **重写** | 去掉 short-circuit，统一 HTTP |
| `agentend/src/orchestrator/planning/prompts.py` | 修改 | REASON_PROMPT 新增 `{orchestrator_context}` |
| `agentend/src/adapters/orchestrator.py` | 修改 | `stream_chat` 加窗口查询；`_handle_execute` 去掉 ContextBuilder |

**不改**：claude.py、opencode.py、codex.py、Frontend

**删除**：`agentend/src/orchestrator/execution/context_builder.py`（不再需要）

---

## 验证

1. **单聊无影响**：单聊走 Backend → AgentEnd → adapter，窗口查询为空，GroupChatRule 不触发
2. **窗口过滤**：Go Backend 查询逻辑（无历史 → 全部；有历史 → 窗口过滤；status 过滤）
3. **消息截断**：超长消息被截断为 2000 字符 + `[截断]` 后缀
4. **GroupChatRule**：有消息时返回 system_prompt_append，无消息时返回空
5. **端到端**：群聊 2 Agent → 第 2 轮 agent 的 system_prompt_append 包含第 1 轮另一个 Agent 的输出
6. **Rules 验证**：子 Agent 的 system_prompt_append 同时包含 Safety/Soul/Skill/GroupChat
7. **Wave 隔离**：并行 wave 内 Agent 互相看不到，串行 wave 间能看到
8. **Orchestrator 自身**：直接 @Agent 后再 @Orchestrator → REASON prompt 包含之前其他 Agent 的消息
9. **short-circuit 删除**：ExecutionEngine 统一走 HTTP，所有 Agent 均经过 Rules 引擎
