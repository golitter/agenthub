# Orchestrator 群聊消息机制缺陷（重复存储 + 身份伪造 + 上下文贫瘠）

**状态**: 已修复（2026-05-31）
**发现日期**: 2026-05-31
**核实日期**: 2026-05-31
**修复日期**: 2026-05-31
**影响范围**: 群聊场景，ask_agent / ExecutionEngine 两条路径均存在
**数据库表**: agenthub.messages (task_id=d1a6bd65)

## 核实结论

全部三个 bug 经代码审计 + Docker MySQL 实测确认属实。另有 1 个新发现的交叉影响。

## 问题总览

| # | 问题 | 路径 | 根因 | 核实 |
|---|---|---|---|---|
| 1 | **消息重复存储** | 两条路径均有 | 子 Agent 回复同时写入子 Agent session 和 Orchestrator session | ✅ **确认** |
| 2 | **身份伪造** | ask_agent 路径 | `skip_user_message=False`，在子 Agent session 伪造 user 消息 | ✅ **确认** |
| 3 | **上下文贫瘠** | 两条路径均有 | Orchestrator 上下文无法结构化传递给子 Agent | ✅ **确认（设计层面）** |
| 新增 | **窗口查询无角色过滤** | 两条路径均有 | `fetchGroupChatWindow` 不过滤 role，伪造 user 消息也注入其他 Agent | 🔴 **新发现** |

---

## Bug 1：消息重复存储 — 已核实 ✅

### 核实方式

通过 Docker MySQL 查询 `task_id=d1a6bd65` 的消息记录，以下为一个完整的 ask_agent 调用周期：

```
id=122  session=4e1747da (god)  role=user   "请介绍一下自己..."                        ← BUG 2: 伪造 user（Orchestrator LLM 写的）
id=123  session=4e1747da (god)  role=agent  agent=god  "```aka_yhy type: html-render..." ← ✅ 第1次: god session 的 agent 消息
id=124  session=aa390b42 (orch) role=agent  agent=god  "```aka_yhy type: html-render..." ← ❌ 第2次: 重复！在 orch session
id=125  session=aa390b42 (orch) role=agent  agent=Orch  "god 回复了~..."                 ← ✅ orch 自己的总结（正常）
```

同一句 god 的回复 `"```aka_yhy type: html-render..."` 在 god session (id=123) 和 orchestrator session (id=124) 各存一份。**所有 4 个 ask_agent 调用周期全部呈现此模式**。

### 两条重复路径 — 均核实

#### 路径 1：ask_agent（群聊主要路径）— 每个 TEXT chunk 都重复

```
graph.py:268-275 _handle_ask_agent_call()
  │
  ├─▶ BackendClient.run_task(session_id=god_session, skip_user_message=False)
  │     → task.go:271-284 创建 agent 消息(Status:streaming)  ← 第1次持久化
  │     → task.go:381 StreamWriter.NewStreamWriter(sessionID=god_session, ...)
  │        处理 god 的 agentend SSE 流，内容写入 god session 的 agent 消息
  │
  └─▶ graph.py:292-361 stream_result() 读取 god 的 SSE 返回流
        → 每个 TEXT 事件 (line 319-331): queue.put(TEXT event, agent_type=claude-code)
        → 每个 DONE 事件 (line 332-345): 如果 done_text 非空，也 queue.put(TEXT event)
        → orchestrator.py:167 yield ask_task.result()  ← 进入 orch 的 SSE 流
          → StreamWriter.go:124 newAgentType != sw.currentAgentType
          → switchAgent (go:189-237): CREATE 新 MySQL 行  ← 第2次持久化（重复！）
```

关键：ask_agent 路径**每个 TEXT chunk 都转发**，流式过程中可能产生多个重复消息（每 500 字符 flush 一次即为一个新行）。

#### 路径 2：ExecutionEngine（任务编排）— 只最终结果重复

```
engine.py:145-153 _execute_task()
  │
  ├─▶ BackendClient.run_task(session_id=child_session) ← skip_user_message 默认 True ✅
  │     → task.go:271-284 创建 agent 消息(Status:streaming)  ← 第1次持久化
  │
  └─▶ engine.py:156-174 stream_result() 读取子 Agent SSE 流
        → 流式内容: yield RUNTIME_TEXT event ← StreamWriter default 分支，不写 MySQL ✅
        → 完成后: yield RUNTIME_COMPLETED event → 由 _stream_wave → _handle_execute 后续处理

orchestrator.py:302-316 _handle_execute()
  → 流转结束后: yield TEXT event (_child_result_text, agent_type=child_agent_type)
    → StreamWriter switchAgent → CREATE 新 MySQL 行 ← 第2次持久化（重复！只一次）
```

关键区别：ExecutionEngine 路径 RUNTIME_TEXT 不写 MySQL，**只有最终 _child_result_text 一个 TEXT 事件触发 switchAgent**。与 ask_agent 的"每个 chunk 都重复"不同。

### 两条路径对比

| | ask_agent | ExecutionEngine |
|---|---|---|
| 触发方式 | reason_node 的 tool call | dispatch → execute 节点 |
| 消息内容 | LLM 写的 question | LLM 写的 task.content |
| `skip_user_message` | **False** → 伪造 user 消息 (Bug 2) | **True** (默认) ✅ |
| 流式事件类型 | **TEXT**（每个 chunk, line 319-331） | **RUNTIME_TEXT** (流式, engine.py:167-174) |
| 最终结果事件 | DONE 也转 TEXT (line 332-345) | **TEXT** (orchestrator.py:311-316) |
| 重复次数 | **每个 flush 一次**（多行） | **1 次**（仅最终结果） |
| ask_agent 重试 | 最多 3 次 (graph.py:266-288) → 可能更多重复 | 无重试 |

### 根因已核实

StreamWriter 的事件路由逻辑 (writer.go:110-127)：

```go
case generated.EventTypeText:
    newAgentType, _ := event.Content["agent_type"].(string)
    // ...
    if newAgentType != sw.currentAgentType || (sourceMessageID... ) {
        sw.flushTextBuffer()
        sw.switchAgent(newAgentType, newAgentName, sourceMessageID)
    }
    sw.appendText(text)
```

- `agent_type="orchestrator"` → `sw.currentAgentType` (go:233 初始化) → 不切换 → 追加到同一消息
- `agent_type="claude-code"` → 与 "orchestrator" 不同 → `switchAgent` → 创建新 MySQL 行 (go:206-217)

### 数据库统计证据

通过 `content` 字段精确匹配，确认 task_id=d1a6bd65 下：

```
精确匹配的重复对数：11 条（content 完全相同）
  - god 回复重复：6 对
  - aa  回复重复：5 对

Orchestrator session 中 agent_type=claude-code 的消息总数：12 条
精确匹配到重复的比例：11/12 = 92%
  （唯一未匹配的是 id=70 [Error] 消息，因为 error 内容与子 Agent session 的失败消息不同）

子 Agent session agent 消息总数：god=7, aa=6
结论：每一次子 Agent 回复，100% 被写入两个 session
```

典型重复对时间线（以 god 的 HTML 回复为例）：

```
id=57  sess=4e1747da (god)  20:04:36.405  "```aka_yhy type: html-render..."  ← 第1次
id=78  sess=aa390b42 (orch) 20:04:54.478  "```aka_yhy type: html-render..."  ← 第2次（延迟 ~18s）

id=107 sess=4e1747da (god)  21:57:32.266  "```aka_yhy type: html-render..."  ← 第1次
id=108 sess=aa390b42 (orch) 21:57:52.718  "```aka_yhy type: html-render..."  ← 第2次（延迟 ~20s）
```

时间差规律：orch session 的第2次写入始终比子 Agent session 的第1次晚 8-20 秒（对应 SSE 流式传输 + switchAgent flush 的延迟）。

### 影响

1. **数据库冗余**：每个子 Agent 回复双倍存储（11/12 = 92% 精确匹配率）
2. **窗口查询重复注入**：`fetchGroupChatWindow` (service/impl/group_chat_window.go) 查其他 session 消息时不区分 role 也不去重，同一回复出现两次，导致 `system_prompt_append` 注入重复内容
3. **子 Agent 感知到重复**：经核实，aa 在回复中提到"还嘲讽我发了两遍"，说明重复内容通过窗口注入反馈给了子 Agent
4. **正反馈循环**：重复存储 → 窗口注入重复 → 子 Agent 感知重复 → 影响输出质量 → 更多重复

### 修复方向

| 方向 | 描述 | 优点 | 缺点 |
|---|---|---|---|
| **A（推荐）** | 子 Agent session 不持久化 agent 消息 | 消息只存一份，窗口查询不重复 | 子 Agent 历史上下文为空 |
| **B** | `fetchGroupChatWindow` 去重 | 改动最小 | 数据库冗余，治标不治本 |
| **C** | StreamWriter 识别转发不创建行 | 最精准 | 改动复杂 |

详见下方"推荐修复方案"章节。

---

## Bug 2：身份伪造（ask_agent 路径）— 已核实 ✅

### 核实方式

数据库查询确认每条 ask_agent 调用都会在子 Agent session 中创建一条 `role=user` 但内容来自 Orchestrator LLM 的消息：

```
id=120  session=aa390b42 (orch) role=user  "请 god 也介绍一下自己"             ← ✅ 真实用户（内容简练）
id=121  session=aa390b42 (orch) role=agent agent=Orch  ask_agent 卡片         ← ✅ orchestrator
id=122  session=4e1747da (god) role=user  "请介绍一下自己，包括..."              ← ❌ 伪造! 内容明显是 LLM 扩写
id=123  session=4e1747da (god) role=agent agent=god  god 的回复               ← ✅ god 真实输出
```

**所有 4 个 ask_agent 调用中都存在此模式。**

### 根因已核实

`graph.py:268-275`：

```python
message_id = await backend_client.run_task(
    task_id=state["task_id"],
    session_id=target_session_id,   # ← 子 Agent 的 session!
    message=question,                # ← Orchestrator LLM 写的内容
    agent_type=agent_type,
    cwd=_cwd_var.get(),
    skip_user_message=False,         # ← ❌ 不跳过，会创建 user 消息
)
```

对比 ExecutionEngine 路径 `engine.py:145-152`，不传 `skip_user_message` 参数，使用默认值 `True` (backend_client.py:44)，所以**不创建 user 消息**。这是为什么只有 ask_agent 有身份伪造问题。

### 新增交叉影响：伪造 user 消息通过 fetchGroupChatWindow 注入其他 Agent

`fetchGroupChatWindow` (service/impl/group_chat_window.go) 的查询条件：

```go
query := db.GetDB().
    Where("task_id = ? AND session_id != ?", taskID, sessionID).  // ← 排除当前 session
    Where("status IN ?", []string{"completed", "streaming"})
    // ⚠️ 没有 role 过滤！user 和 agent 消息都会被包含
```

这意味着：
- aa 的窗口查询会包含 god session 的伪造 user 消息 (id=122)
- 其他 Agent 看到的是 "user 对 god 说：请介绍一下自己..." 
- 但从数据完整链路看，用户从未对 god 说过这句话
- **这是 Bug 1 + Bug 2 的交叉影响：重复消息浪费窗口空间 + 伪造消息污染语义**

### 修复方向

在 `_handle_ask_agent_call()` (graph.py:274) 中将 `skip_user_message=False` 改为 `True`。

---

## Bug 3：子 Agent 上下文贫瘠 — 已核实 ✅（设计层面）

### 核实方式

代码追踪确认了子 Agent 的上下文信息来源和缺口。

### 核实的上下文来源

子 Agent 实际收到的信息共 3 个来源：

**① system_prompt_append (规则引擎注入)**
- SafetyRule / ScopeRule / SoulRule / SkillRule
- GroupChatRule: `fetchGroupChatWindow(task_id, session_id)` 的结果
  - 查询其他 session 的 completed/streaming 消息 (task.go:449-451)
  - **不过滤 role**（用户、agent、伪造 user 全部包含）
  - **不去重**（Bug 1 的重复消息双份出现）

**② message 参数 (task.go:299 → agentend → 传给 Claude CLI 的 -p)**
- ask_agent 路径: `question` (Orchestrator LLM 生成的一个问题)
- ExecutionEngine 路径: `dispatch.content` (Orchestrator LLM 写的任务描述)
- ⚠️ 不包含 plan overview / 用户原始请求 / 其他 Agent 的任务分配

**③ fetchGroupChatWindow (Backend 自动注入，现位于 service/impl/task_service.go)**
- 作为 `GroupChatMessages` 字段传入 agentend
- Wave 1 时为空（无前置历史）
- Wave 2+ 有内容（但可能包含 Bug 1 重复 + Bug 2 伪造）


### 核心矛盾

`dispatch.content` / `question` 是唯一连接 Orchestrator 上下文和子 Agent 的管道，完全依赖 LLM 写任务描述时自觉包含背景信息，无结构化保障。

---

## 深层根因剖析：三层矛盾叠加

这不是"改一行就能修"的 bug，而是**架构层面的角色冲突**。Orchestrator 同时扮演两个不兼容的角色，却没有对"谁负责持久化"做出单一决策。

### 角色冲突：协调者 vs 转发者

```
角色 A: 协调者 (Coordinator)
  → 调用 BackendClient.run_task() 启动子 Agent
  → 子 Agent 在自己的 session 中独立运行
  → Backend RunTask handler 在子 Agent session 创建 agent 消息 (task.go:270-284)
  → StreamWriter B 绑定子 Agent session，消费子 Agent 的 SSE 流
  → 子 Agent 回复被持久化到子 Agent session  ← 第 1 次写入

角色 B: 转发者 (Relay)
  → 把子 Agent 回复透传到 orchestrator 自己的 SSE 流
  → 前端订阅的是 orchestrator session 的 SSE 流
  → 为了让前端看到子 Agent 实时输出，必须转发
```

### 矛盾爆发点：StreamWriter.switchAgent()

StreamWriter 的事件路由 (writer.go:110-127)：

```go
case generated.EventTypeText:
    newAgentType, _ := event.Content["agent_type"].(string)
    if newAgentType != sw.currentAgentType {
        sw.switchAgent(newAgentType, newAgentName, sourceMessageID)
    }
```

`switchAgent` 的设计意图是："同一个 session 里，如果说话的 Agent 变了，创建新消息行"。**这个逻辑本身是正确的**。问题在于它无法区分两种场景：

```
场景 A（正常）：单 Agent 直接对话
  agent_type 从头到尾不变 → 不触发 switchAgent → 不重复 ✅

场景 B（Bug!）：Orchestrator 转发子 Agent 输出
  orch SSE 流:
    TEXT(agent_type="orchestrator") → 正常写入 orch session
    TEXT(agent_type="claude-code")  → switchAgent! 新消息行
         ↑
         │  StreamWriter 认为"同一个 session 里换了人说话"，理所当然创建新消息行
         │  但它不知道：这个内容已经在子 Agent session 里被另一个 StreamWriter 写过一次了！
```

### 三层矛盾

```
┌──────────┬──────────────────────────────────────────────────────────────────┐
│ 第 1 层   │ Session 归属矛盾                                               │
│          │                                                                │
│          │ StreamWriter 只认识"当前 session"。                            │
│          │ 它不知道 TEXT 事件携带的内容已被另一个 session 持久化。          │
│          │ switchAgent 逻辑是正确的——"同一个 session 里换了说话人就该      │
│          │ 创建新消息行"。问题在于 Orchestrator 硬把子 Agent 的输出        │
│          │ 灌进了自己的 session。                                          │
├──────────┼──────────────────────────────────────────────────────────────────┤
│ 第 2 层   │ "谁是消息的真正拥有者" 矛盾                                    │
│          │                                                                │
│          │ 子 Agent 回复的"真正拥有者"是谁？                              │
│          │   如果是子 Agent session → 那 Orchestrator 转发时不该持久化    │
│          │   如果是 Orchestrator session → 那子 Agent session 不该持久化  │
│          │ 当前实现：两边都持久化了。没有任何一层做了去重或                 │
│          │   "选择唯一写入者"的决策。                                     │
├──────────┼──────────────────────────────────────────────────────────────────┤
│ 第 3 层   │ 窗口查询放大矛盾                                              │
│          │                                                                │
│          │ fetchGroupChatWindow (service/impl/group_chat_window.go) 查所有 session 的消息，  │
│          │ 不去重，不过滤 role。由于子 Agent 回复在两个 session 各存一份， │
│          │ 窗口查询把同一内容返回两次 → 注入到 system_prompt_append        │
│          │ → 子 Agent 上下文里出现重复内容 → 浪费 token 预算              │
│          │ → 形成正反馈循环：                                              │
│          │   重复存储 → 窗口注入重复 → 子Agent感知重复 → 影响输出质量     │
└──────────┴──────────────────────────────────────────────────────────────────┘
```

### 完整数据流（两个 StreamWriter 各写各的）

```
                  ┌──────────────────────────────────┐
                  │        用户发消息                  │
                  └──────────────┬───────────────────┘
                                 │
                  ┌──────────────▼───────────────────┐
                  │   Backend RunTask (orch session)  │
                  │   创建 agent 消息到 orch session  │
                  │   StreamWriter A 绑定 orch sess   │
                  └──────────────┬───────────────────┘
                                 │ SSE 流
                  ┌──────────────▼───────────────────┐
                  │   Orchestrator Adapter            │
                  │   reason_node → ask_agent         │
                  │                                   │
                  │   graph.py:268                    │
                  │   BackendClient.run_task(         │
                  │     skip_user_message=False, ← Bug 2
                  │     session_id = "child_session"  │
                  │   )                               │
                  └──────────┬───────────┬───────────┘
                             │           │
        ┌────────────────────▼──┐    ┌───▼────────────────────┐
        │ Backend RunTask       │    │  子 Agent 运行          │
        │ (child session)       │    │  Claude CLI 执行        │
        │                       │    │                        │
        │ 1. 创建 user 消息     │    │  SSE 流输出回复         │
        │    (伪造! Bug 2)      │    │                        │
        │ 2. 创建 agent 消息    │    └───┬────────────────────┘
        │ 3. StreamWriter B     │        │
        │    绑定 child session │        │ SSE text 事件
        │    消费 SSE 流        │        │
        │    → 写入 child sess  │◄───────┘
        │    ← 第 1 次持久化    │
        └───────────────────────┘
                             │
                             │ 同一份 SSE 事件通过
                             │ queue.put() / yield 转发到
                             │ orch 的 SSE 流
                             ▼
        ┌─────────────────────────────────────────────┐
        │ StreamWriter A (orch session)                │
        │                                              │
        │ 收到 TEXT event:                             │
        │   agent_type="claude-code"                   │
        │   != sw.currentAgentType("orchestrator")     │
        │                                              │
        │ → switchAgent("claude-code", ...)            │
        │ → 在 orch session 创建新 MySQL 行             │
        │ → 第 2 次持久化！重复了！                     │
        │                                              │
        │ [writer.go:124-127]                          │
        │ if newAgentType != sw.currentAgentType {     │
        │     sw.switchAgent(...)  ← 创建了重复行       │
        │ }                                            │
        └─────────────────────────────────────────────┘
```

### 本质结论

> **Orchestrator 作为"管道"连接了两个独立的 StreamWriter（子 Agent session 的 和 Orchestrator session 的），但没有对"谁负责持久化"做出单一决策。两边的 StreamWriter 各自忠实执行自己的职责——"收到 TEXT 且 agent_type 变了就创建新行"——导致同一份内容被两个 session 各写一次。switchAgent 本身没有 bug，它的设计是正确的。真正的问题在于 Orchestrator Adapter 层把子 Agent 的输出同时喂给了两个消费者，却没有协调"谁写、谁只转发"。**

---

## 已实施修复方案

核心思路：**子 Agent 回复仍由子 Agent session 负责持久化；Orchestrator session 只持久化“询问/分派卡片”，对子 Agent 正文只做 SSE/Redis 转发，不再创建重复 Message。**

### 实时展示语义

- Orchestrator 执行阶段转发子 Agent 正文前，发送 `ask_card_start` / `ask_card_done`。
- 卡片展示 Orchestrator 向子 Agent 询问/分派的内容（`question` 使用 dispatch task content）。
- 子 Agent 正文继续以 `text` 事件进入 Orchestrator SSE，携带子 Agent 的 `agent` / `agent_type` / 原始 `message_id`，前端按 Agent 切成子 Agent 单独发言。
- 每次子 Agent 正文转发后，下一段 Orchestrator 正文强制拆成新的 Message，保证多轮交互按 `Orchestrator 规划 → SubAgent 回复 → Orchestrator 总结 → SubAgent 回复 → Orchestrator 总结` 的顺序展示。
- ask 卡片按 `question_id` 记录所在 Message：`ask_card_done` 回写同一张卡片所在消息；新的 `ask_card_start` 会消耗子 Agent 回复后的分隔边界，避免刷新后多张卡片聚合到最早的 Orchestrator 消息。

### 持久化语义

- `ExecutionEngine` 保存 Backend 返回的子 Agent 原始 `message_id` 到 `TaskResult`。
- Orchestrator 转发正文时复用该 `message_id`。
- Backend `StreamWriter` 如果发现 `text.content.message_id` 已属于同 task 下其他 session 的 Message，则只写 Redis Stream / Hub，不在 Orchestrator session 新建重复 Message。
- 群聊历史加载读取 task 下可见消息：保留 Orchestrator session 的用户消息/卡片，以及各子 Agent 的 agent 回复；过滤子 Agent 内部 user 提问，避免问题文本既显示在卡片又显示成用户气泡。
- 群聊窗口注入允许 `user` / `agent`、普通 Agent / Orchestrator 消息进入；仅按当前 session、状态、时间窗裁剪，并对旧重复内容做精确去重。
- 兼容旧脏数据：群聊历史过滤 Orchestrator session 中 `agent_type != orchestrator` 的旧重复转发行，避免刷新后把旧转发行渲染成 aa/god 气泡并夹带 ask 卡片。
- 实时流状态切换：当前端正在渲染子 Agent streaming 气泡时收到新的 `ask_card_start`，先结算当前子 Agent 消息，再把新卡片作为 Orchestrator/规划者的新 runtime 消息展示，避免不刷新时卡片挂到上一个子 Agent 气泡内。

### 修改文件

- `agentend/src/orchestrator/models.py` — `TaskResult` 增加 `message_id`
- `agentend/src/orchestrator/planning/graph.py` — ask_agent 不再把 Orchestrator 的问题伪装成子 Agent session 的 user 消息
- `agentend/src/orchestrator/execution/engine.py` — 执行子 Agent 后保存原始回复 `message_id`
- `agentend/src/adapters/orchestrator.py` — 执行阶段输出询问卡片，并用原始 `message_id` 转发子 Agent 正文
- `backend/internal/stream/writer.go` — 外部 session 的已持久化正文只转发，不重复落库；转发后下一段 Orchestrator 正文或新 ask 卡片强制开新 Message；按 `question_id` 回写 ask 卡片完成态
- `frontend/src/hooks/use-chat-stream.ts` — 群聊历史支持读取 task 可见消息
- `frontend/src/components/chat/ChatArea.tsx` — 群聊分页读取 task 可见消息
- `frontend/src/stores/chat.ts` — live ask 卡片到达时按 speaker 切换结算上一条消息
- `backend/internal/controller/impl/task_controller.go` + `backend/internal/service/impl/task_service.go` — 群聊窗口保留 role / agent_type 全量上下文，并去重历史重复内容

### 验证

- `go test ./internal/stream ./internal/controller/... ./internal/service/...`
- `uv run --group dev pytest tests/test_orchestrator_presentation.py`
- `pnpm exec vitest run src/stores/__tests__/chat.test.ts src/lib/__tests__/block-reducer.test.ts`
- `pnpm exec tsc -b`

---

## 历史推荐方案：4 处改动，按优先级排列

核心思路：**让 Orchestrator session 成为群聊场景下子 Agent 回复的唯一持久化位置**。子 Agent session 退化为"执行沙箱"——只做 SSE 实时推送，不写 MySQL。

> 该方案未采用。实际修复选择保留子 Agent session 的原始持久化，让 Orchestrator session 只保留询问卡片并转发正文。

### 修复 1：Bug 2 — 身份伪造（1 行，无风险）

**文件**: `agentend/src/orchestrator/planning/graph.py:274`

```diff
- skip_user_message=False,
+ skip_user_message=True,
```

**为什么安全**：子 Agent 由 Orchestrator 驱动，不存在用户直接对话的场景。ExecutionEngine 路径已经默认 `True`。改为 `True` 后，Backend RunTask 不再在子 Agent session 创建伪造的 `role=user` 消息。

**影响**：子 Agent 的 agentend 仍然收到 `message` 参数（传给 Claude CLI 的 `-p`），只是不再在 MySQL 创建 `role=user` 行。

---

### 修复 2：Bug 1 — 消息重复存储（方向 A：子 Agent session 不持久化）

需要在三层各做一处改动，形成完整的"透传管道"：

#### 层 1：BackendClient 新增 `skip_persistence` 参数

**文件**: `agentend/src/clients/backend_client.py`

```diff
  async def run_task(
      self,
      task_id: str,
      session_id: str,
      message: str,
      agent_type: str,
      cwd: str = "",
      skip_user_message: bool = True,
+     skip_persistence: bool = False,
  ) -> str:
      """POST /api/tasks/:taskId/run → returns message_id."""
      resp = await self._client.post(
          f"{self._base_url}/api/tasks/{task_id}/run",
          json={
              "message": message,
              "session_id": session_id,
              "agent_type": agent_type,
              "cwd": cwd,
              "skip_user_message": skip_user_message,
+             "skip_persistence": skip_persistence,
          },
      )
```

#### 层 2：Backend RunTask handler 支持 `skip_persistence`

**文件**: `backend/internal/controller/impl/task_controller.go` + `service/impl/task_service.go`（原 `handler/task.go`）

```diff
  type RunTaskReq struct {
      Message         string `json:"message" binding:"required"`
      AgentType       string `json:"agent_type"`
      SessionID       string `json:"session_id" binding:"required"`
      Cwd             string `json:"cwd"`
      SkipUserMessage bool   `json:"skip_user_message"`
+     SkipPersistence bool   `json:"skip_persistence"`
  }
```

在 RunTask handler 中，`skip_persistence=True` 时：

```diff
  // Create agent message with streaming status
+ if !req.SkipPersistence {
      messageID := uuid.New().String()
      agentMsg := model.Message{...}
      db.GetDB().Create(&agentMsg)
+ }

  // Launch background goroutine to consume agentend stream
  go func() {
      // ...
-     sw := stream.NewStreamWriter(ctx, taskID, req.SessionID, messageID, agentType)
+     if req.SkipPersistence {
+         // Redis-only relay: publish SSE to Redis/Hub without MySQL writes
+         stream.RelayOnly(ctx, taskID, req.SessionID, resp.Body)
+     } else {
+         sw := stream.NewStreamWriter(ctx, taskID, req.SessionID, messageID, agentType)
+         sw.Run(scanFunc)
+     }
  }()
```

需要新增一个 `stream.RelayOnly()` 函数：只做 Redis SSE 推送（Hub.Publish + Redis XADD），不做 MySQL 写入。StreamWriter 中已有 `publishToRedis()` 方法，提取其 SSE 解析 + Redis 推送逻辑即可。

```
StreamWriter（完整）           RelayOnly（轻量）
├── SSE 行解析                 ├── SSE 行解析
├── 事件路由 (TEXT/RUNTIME...) │   （不做事件路由）
├── switchAgent → MySQL 写入   ✗   （不做）
├── bufferTextLine             ✗   （不做）
├── flushTextBuffer → MySQL    ✗   （不做）
├── publishToRedis → Hub+Redis ✓   publishToRedis → Hub+Redis  ← 保留
└── updateMessageStatus        ✗   （不做）
```

#### 层 3：Orchestrator 调用方传入 `skip_persistence=True`

**文件**: `agentend/src/orchestrator/planning/graph.py:268-275`（ask_agent 路径）

```diff
  message_id = await backend_client.run_task(
      task_id=state["task_id"],
      session_id=target_session_id,
      message=question,
      agent_type=agent_type,
      cwd=_cwd_var.get(),
      skip_user_message=True,          # ← 修复 1
+     skip_persistence=True,           # ← 修复 2
  )
```

**文件**: `agentend/src/orchestrator/execution/engine.py:145-153`（ExecutionEngine 路径）

```diff
  message_id = await asyncio.wait_for(
      self._backend_client.run_task(
          task_id=self._task_id,
          session_id=session_id,
          message=dispatch.content,
          agent_type=agent_type,
          cwd=agent_cwd,
+       skip_persistence=True,
      ),
      timeout=30.0,
  )
```

#### 修复后数据流（只有一条持久化路径）

```
                  ┌──────────────────────────────────┐
                  │        用户发消息                  │
                  └──────────────┬───────────────────┘
                                 │
                  ┌──────────────▼───────────────────┐
                  │   Backend RunTask (orch session)  │
                  │   StreamWriter A 绑定 orch sess   │
                  │   ← 唯一持久化点                   │
                  └──────────────┬───────────────────┘
                                 │ SSE 流
                  ┌──────────────▼───────────────────┐
                  │   Orchestrator Adapter            │
                  │   graph.py:268 run_task(          │
                  │     skip_user_message=True,       │ ← 修复 1
                  │     skip_persistence=True,        │ ← 修复 2
                  │   )                               │
                  └──────────┬───────────┬───────────┘
                             │           │
        ┌────────────────────▼──┐    ┌───▼────────────────────┐
        │ Backend RunTask       │    │  子 Agent 运行          │
        │ (child session)       │    │                        │
        │                       │    │  SSE 流输出回复         │
        │ ❌ 不再创建 user 消息  │    │                        │
        │ ❌ 不再创建 agent 消息 │    └───┬────────────────────┘
        │ ✅ RelayOnly: 只推    │        │
        │    Redis SSE 不写     │        │ SSE text 事件
        │    MySQL              │        │
        └───────────────────────┘◄───────┘
                             │
                             │ 同一份 SSE 事件转发到
                             │ orch 的 SSE 流
                             ▼
        ┌─────────────────────────────────────────────┐
        │ StreamWriter A (orch session)                │
        │                                              │
        │ TEXT event: agent_type="claude-code"         │
        │ → switchAgent → 在 orch session 创建新行     │
        │                                              │
        │ ✅ 唯一一次持久化，不再重复！                  │
        └─────────────────────────────────────────────┘
```

---

### 修复 3：窗口查询保留 role / agent_type 全量上下文

**文件**: `backend/internal/service/impl/group_chat_window.go`

```diff
  query := db.GetDB().
      Where("task_id = ? AND session_id != ?", taskID, sessionID).
+     Where("status IN ?", []string{"completed", "streaming"})
```

**为什么不过滤 `role` / `agent_type`**：窗口查询的目的是提供群聊完整上下文。用户消息、Orchestrator 规划/总结、普通 Agent 回复都可能是下一个 Agent 判断任务状态所需的信息。当前只按 task、session、状态和“当前 Agent 上次发言后的时间窗”裁剪，并保留内容级去重，避免旧重复行放大。

---

### 修复 4：Bug 3 — 上下文贫瘠（结构化上下文注入）

这是设计层面的改进，优先级低于前三个。核心思路：在 Orchestrator 调子 Agent 时，将 plan overview + 用户原始请求 + 任务分工结构化注入 `message` 参数，而非完全依赖 LLM "自觉" 写进去。

**ask_agent 路径** (`graph.py` `_handle_ask_agent_call`):

```python
# 当前：只传 LLM 生成的 question
message = question

# 改进：追加结构化上下文
message = f"""## 任务上下文

### 用户原始请求
{state.get("user_original_message", "")}

### Plan 概览
{plan.overview if plan else ""}

### 你的任务
{question}
"""
```

**ExecutionEngine 路径** (`engine.py` `_execute_task`): 同理，在 `dispatch.content` 前追加结构化上下文。

这个改动的风险在于 token 消耗增加，需要评估上下文窗口预算。

---

### 修复优先级与依赖关系

```
修复 1 (skip_user_message)     ← 独立，可立即做，1 行改动
修复 3 (窗口 role 过滤)        ← 独立，可立即做，1 行改动
    │
    └── 两者互不依赖，但都是防御性修复

修复 2 (skip_persistence)      ← 核心，需要三层联动
    ├── 层 1: BackendClient 新增参数 (Python, 简单)
    ├── 层 2: Backend RunTask + stream.RelayOnly (Go, 中等)
    └── 层 3: graph.py + engine.py 传参 (Python, 简单)

修复 4 (结构化上下文)           ← 独立，设计改进，优先级低
```

**建议实施顺序**：修复 1 → 修复 3 → 修复 2 → 修复 4。修复 1 和 3 可以在 5 分钟内完成并验证，修复 2 需要约 1 小时（含 RelayOnly 实现 + 测试）。

---

## 排查历史

- 2026-05-31 初次排查，尝试在 agentend 层将 TEXT 改为 RUNTIME_TEXT — **错误**，子 Agent 的核心内容丢失持久化
- 2026-05-31 代码审计 + Docker MySQL 核实，确认全部三个 bug，并发现交叉影响（fetchGroupChatWindow 不过滤 role）
- 2026-05-31 数据库精确匹配统计：11/12 条重复（92%），深层根因剖析——三层矛盾叠加（Session 归属 / 消息所有权 / 窗口查询放大）
- 2026-05-31 修复：子 Agent 正文复用原始 `message_id` 转发，Backend 对外部 session 源消息只转发不重复落库；转发后下一段 Orchestrator 正文强制拆成新消息，支持多轮交互顺序；ask_agent 不再伪造子 Agent user 消息；群聊窗口保留 role / agent_type 全量上下文并去重；执行阶段新增询问卡片；群聊历史读取 task 可见消息
