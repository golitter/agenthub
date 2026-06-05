# Orchestrator 群聊消息碎片化问题

## 问题描述

在群聊 + Orchestrator 场景下，当管理者分派多个子任务给不同 Agent 时，聊天界面出现大量碎片化消息气泡和重复的 Agent 名字标签。例如：

```
管理者 ← 消息气泡标签
管理者 ← ???
管理者 ← ???
实现者 ← runtime_status / ask_card 中的 Agent 名
实现者 ← 消息气泡标签
等待回答  ← ask_card 状态
请介绍一下你自己...
执行者    ← 下一个子 Agent
我是 opencode... ← 执行者的回复
...
```

本应展示为**多条归属明确的独立消息**（管理者编排 / 子 Agent 回复 / 管理者汇总），每条子 Agent 消息上方带 ask_card 指示条（管理者 → 子 Agent），下方是具体回答。实际被拆分为 7+ 条无关联的碎片消息，每条都带有重复的 Agent 标签，且子 Agent 回复缺失持久化（刷新后丢失）。

---

## 根因分析

### SSE 事件交替 → `streamAgentUpdate` 反复拆分消息

Orchestrator 在同一个 SSE 流中交替产出 orchestrator 事件（ASK_CARD_START/DONE、RUNTIME_EXECUTING/COMPLETED）和子 Agent 事件（TEXT with sub-agent agent_type），意图是在**同一条消息**中展示整个编排过程。但前端的 `streamAgentUpdate` 把每次 agent_type 切换都理解为"说话人换了，应该分气泡"，导致反复拆分。

### SSE 事件序列追踪

3 个子任务在同一 wave 中并行执行，事件按以下顺序流出（并行任务的 `RUNTIME_TEXT` 可能交错）：

```
① RUNTIME_EXECUTING  task_id=1  agent="实现者" status="running"
② RUNTIME_EXECUTING  task_id=2  agent="执行者" status="running"
③ RUNTIME_EXECUTING  task_id=3  agent="操作者" status="running"
④ RUNTIME_TEXT       task_id=1  agent="实现者" text="..."（子 Agent 流式 token）
⑤ RUNTIME_TEXT       task_id=2  agent="执行者" text="..."
⑥ RUNTIME_COMPLETED  task_id=1  agent="实现者" success=true
⑦ ASK_CARD_START     source="管理者" → target="实现者" question="请介绍一下..."
⑧ ASK_CARD_DONE      status="completed" summary="..."
⑨ TEXT               agent="实现者" agent_type="claude-code" text="实现者的完整回复"
⑩ RUNTIME_COMPLETED  task_id=2  agent="执行者"
⑪ ASK_CARD_START     source="管理者" → target="执行者" question="请介绍一下..."
⑫ ASK_CARD_DONE      status="completed"
⑬ TEXT               agent="执行者" agent_type="opencode" text="执行者的完整回复"
⑭ RUNTIME_COMPLETED  task_id=3  agent="操作者"
⑮ ASK_CARD_START     source="管理者" → target="操作者" question="请介绍一下..."
⑯ ASK_CARD_DONE      status="completed"
⑰ TEXT               agent="操作者" agent_type="codex" text="操作者的完整回复"
⑱ TEXT               agent="Orchestrator" agent_type="orchestrator" text="汇总报告..."
⑲ DONE
```

来源：
- `RUNTIME_EXECUTING` / `RUNTIME_TEXT` / `RUNTIME_COMPLETED`：`agentend/src/orchestrator/execution/engine.py:106-242`
- `ASK_CARD_START` / `ASK_CARD_DONE` / `TEXT`：`agentend/src/adapters/orchestrator.py:345-375`（`_handle_execute` 方法（起始于第 299 行）中，每个 task result 之后立即 yield）

### Backend 事件处理验证

> 确认 Backend StreamWriter **不是**碎片化的来源。

Backend `StreamWriter`（`backend/internal/stream/writer.go:113-227`）对上述 SSE 事件分 4 类处理：

| 事件类型 | Backend 处理 | MySQL 写入 |
|----------|-------------|-----------|
| `RUNTIME_EXECUTING` / `RUNTIME_TEXT` / `RUNTIME_COMPLETED` | `default` 分支，flush text buffer 后透传 Redis | 无 |
| `ASK_CARD_START` / `ASK_CARD_DONE` | `persistAskCardEvent` 持久化 | 有 |
| 子 Agent `TEXT`（带 `message_id` 来自子 session） | `shouldForwardTextWithoutPersist` → **true** → `publishForwardedText` 仅转发 Redis | **无** |
| Orchestrator 聚合 `TEXT`（无 `message_id`） | `shouldSplitAfterForward` → `switchAgent` → 追加到 orchestrator 原始 Message | 有（同一 Message） |

**关键路径**：子 Agent 的 TEXT 事件携带 `message_id`（来自 `engine.py:147-156` 的 `run_task`，该调用使用子 session 的 `session_id` 但 orchestrator 的 `task_id`）。`FindSessionIDByTaskMessage`（`message_dao.go:68`）查到 `session_id` 与当前 StreamWriter 的 `sessionID` 不同 → `shouldForwardTextWithoutPersist` 返回 true → 文本仅转发到 Redis，**不创建 MySQL sub-Message，不触发 `switchAgent`**。

**结论**：Backend 层面 **不存在消息碎片化问题**。MySQL 中只有 orchestrator 的原始消息（含聚合文本），子 Agent 的详细回复仅通过 Redis 实时推送，不做持久化。碎片化是**纯前端 store 层**的问题，但修复需要三端联动——引入 `group_id` 机制让子 Agent 消息独立持久化，前端将 ask_card block 归属到子 Agent 消息内渲染。

### 前端 Store 逐步响应

#### 阶段 1：runtime 事件（①②③）

```
streamingAgentType = "orchestrator"
streamingAgentName = "管理者"
runtimeBlocks = [
  { type: "runtime_status", agent: "实现者", status: "running" },
  { type: "runtime_status", agent: "执行者", status: "running" },
  { type: "runtime_status", agent: "操作者", status: "running" },
]
```

#### 阶段 2：第一个 ask_card（⑦⑧）

事件⑦ `ASK_CARD_START`：source_agent_type="orchestrator", source_agent="管理者"

进入 `streamAskCardStart`（`frontend/src/stores/message-store.ts:802-868`）：

```typescript
sourceAgentType = "orchestrator"  // 与 streamingAgentType 相同
sourceAgentName = "管理者"         // 与 streamingAgentName 相同
speakerChanged = false  // 不拆分！
```

→ 只在 `runtimeBlocks` 中新增一个 `ask_agent` block（状态="pending" / "等待回答"）

事件⑧ `ASK_CARD_DONE`：更新为 "已回答"

此时 UI 渲染为**一条 [管理者] 消息**，包含：
- 3 个 RuntimeStatus 卡片（实现者 执行中、执行者 执行中、操作者 执行中）
- 1 个 AskAgentCard（管理者 → 实现者 已回答）

#### 阶段 3：第一个 TEXT 事件（⑨）— **第一次拆分！**

事件⑨ `TEXT`：agent="实现者", agent_type="claude-code"

进入 `streamAgentUpdate("claude-code", "实现者", messageId)`（`frontend/src/stores/message-store.ts:485-534`）：

```typescript
agentChanged = "orchestrator" !== "claude-code"  → true
hasContent = runtimeBlocks.length > 0              → true
// → 拆分！
```

**拆分结果**：
1. 当前 streaming 内容固化为 **[管理者] 消息**（含 runtime blocks + ask_card）
2. 新建空的 streaming 状态：`streamingAgentType="claude-code"`, `streamingAgentName="实现者"`

然后 `streamText("实现者的完整回复")` → 拼接到新的 streaming 内容

此时 UI 新增一条 **[实现者] 消息**

#### 阶段 4：第二个 ask_card（⑪）— **第二次拆分！**

事件⑪ `ASK_CARD_START`：source_agent_type="orchestrator", source_agent="管理者"

进入 `streamAskCardStart`：

```typescript
sourceAgentType = "orchestrator"     // 当前 streamingAgentType = "claude-code"
sourceAgentName = "管理者"            // 当前 streamingAgentName = "实现者"
speakerChanged = ("orchestrator" !== "claude-code") → true
shouldCloseCurrent = true (有 streaming content)
// → 又拆分！
```

**拆分结果**：
1. 固化 **[实现者] 消息**（实现者的回复文本）
2. 新建 streaming：`streamingAgentType="orchestrator"`, `streamingAgentName="管理者"`
3. 添加 ask_agent block（管理者 → 执行者 等待回答）

此时 UI 又多了一条 **[管理者] 消息**

#### 阶段 5：第二个 TEXT 事件（⑬）— **第三次拆分！**

同理：agent_type 从 "orchestrator" → "opencode"，再拆一次

→ 固化 [管理者] 消息（含第二个 ask_card）
→ 新建 [执行者] 消息

#### 重复以上模式...

#### 最终消息列表（7+ 条）

```
[1] 管理者 ─ runtime_status × 3 + ask_card(管理者→实现者 已回答)
[2] 实现者 ─ 实现者的完整回复文本
[3] 管理者 ─ ask_card(管理者→执行者 已回答)
[4] 执行者 ─ 执行者的完整回复文本
[5] 管理者 ─ ask_card(管理者→操作者 已回答)
[6] 操作者 ─ 操作者的完整回复文本
[7] 管理者 ─ 汇总报告（aggregated summary）
```

### 拆分触发点总结

| 触发位置 | 代码 | 条件 |
|----------|------|------|
| TEXT 事件 agent_type 变化 | `frontend/src/stores/message-store.ts:489-497` | `streamAgentUpdate` 检测到 `agentChanged` 且有内容 |
| ASK_CARD_START source 与当前 streaming agent 不同 | `frontend/src/stores/message-store.ts:806-816` | `streamAskCardStart` 检测到 `speakerChanged` 且有内容 |

在 Orchestrator 场景下，SSE 事件**反复交替**于 "管理者/orchestrator" 和子 Agent 之间：

```
orchestrator → claude-code → orchestrator → opencode → orchestrator → codex → orchestrator
```

每次切换都触发一次消息拆分，导致产生 N×2+1 条碎片消息（N = 子任务数）。

---

## 根本原因

这不是某个单点的 bug，而是**架构层面的设计冲突**：

1. **`streamAgentUpdate` 的设计初衷**是为「单 Agent 单轮对话」设计的——检测到 agent 变化时拆分消息，让每个 Agent 的回复显示为独立气泡
2. **Orchestrator 的 SSE 事件模式**是在同一个 SSE 流中交替产出 orchestrator 事件和子 Agent 事件，意图是在**同一条消息**中展示整个编排过程
3. 但前端把每次 agent_type 切换都理解为"说话人换了，应该分气泡"，导致一条本应统一的编排消息被拆成了 7+ 条碎片

---

## 修复方案：子 Agent 独立消息 + ask_card 内嵌

### 设计目标

1. **消息归属明确**：每条消息的说话人就是该消息的实际产出者——管理者的编排归管理者，子 Agent 的回复归子 Agent，汇总归管理者
2. **上下文清晰**：每条子 Agent 消息上方带 ask_card 指示条（管理者 → 子 Agent），明确回答的上下文归属
3. **持久化可查**：子 Agent 的回复持久化到 MySQL，刷新后仍可查看

### 目标渲染效果

所有消息在同一个聊天流中，每条子 Agent 消息上方带 ask_card 指示条，下方是具体回答：

```
[用户]  请帮我实现一个用户注册功能...

[管理者]                                          ← runtime_status 消息
  ✅ 实现者 completed ⏱ 12.3s
  ✅ 执行者 completed ⏱ 8.7s
  ✅ 操作者 completed ⏱ 15.1s

[实现者]                                          ← 子 Agent 独立消息
  ┌ 管理者 → 实现者  ✓ 已回答  「请介绍一下你自己…」 ┐  ← ask_card 指示条
  └──────────────────────────────────────────────┘
  好的，我来实现前端注册表单...                       ← 具体回答

[执行者]
  ┌ 管理者 → 执行者  ✓ 已回答  「请介绍一下你自己…」 ┐
  └──────────────────────────────────────────────┘
  后端 API 已实现...

[操作者]
  ┌ 管理者 → 操作者  ✓ 已回答  「请介绍一下你自己…」 ┐
  └──────────────────────────────────────────────┘
  数据库表设计完成...

[管理者]                                          ← 汇总消息
  📋 编排汇总...
```

**渲染语义**：

- 管理者的消息包含 `runtime_status` blocks（任务执行状态）
- 子 Agent 的消息是独立消息，上方带 ask_card 指示条——**管理者问了什么、子 Agent 回答了**
- ask_card 指示条和回答文本同属一条子 Agent 消息，不是拆开的碎片
- 管理者的汇总消息也是独立消息，归属管理者

### 数据模型

```
Message { agent: 管理者, agent_type: orchestrator,
          blocks: [runtime_status×3] }                    ← runtime_status 消息

Message { agent: 实现者, agent_type: claude-code,
          blocks: [ask_agent(管理者→实现者 已回答)],
          content: "实现者的回复..." }                    ← ask_card 指示条 + 回答

Message { agent: 执行者, agent_type: opencode,
          blocks: [ask_agent(管理者→执行者 已回答)],
          content: "执行者的回复..." }

Message { agent: 操作者, agent_type: codex,
          blocks: [ask_agent(管理者→操作者 已回答)],
          content: "操作者的回复..." }

Message { agent: 管理者, agent_type: orchestrator,
          content: "汇总..." }                            ← 汇总消息
```

每条 Message 在 MySQL 中独立存储。子 Agent 的 Message 同时携带 ask_card block 和文本内容，渲染时 ask_card 显示在文本上方。`group_id` 用于 Backend 关联同一编排轮次的消息。

### 第一层：Contracts

新增 `group_id` 字段到 StreamEvent 和 Message 契约。

**`contracts/schemas/stream-event.yaml`**：
```yaml
# StreamEvent.content 中新增可选字段
group_id:
  type: string
  required: false
  description: "编排轮次标识，orchestrator 场景下所有事件共享同一 group_id"
```

**`contracts/schemas/message.yaml`**：
```yaml
# Message 新增字段
group_id:
  type: string
  required: false
  description: "编排组标识，同一编排轮次的所有消息共享此 ID"
```

运行 `make generate` 重新生成三端类型。

### 第二层：Agentend

**改动文件**：`agentend/src/adapters/orchestrator.py`、`agentend/src/orchestrator/execution/engine.py`

改动很小：在编排开始时生成 `group_id`，注入所有 yield 的事件。

`_handle_execute`（`orchestrator.py:299`）：

```python
async def _handle_execute(self, ...):
    group_id = f"orch-{self._task_id}-{uuid4().hex[:8]}"  # ← 新增

    for wave in execution_waves:
        async for event, result in self._stream_wave(engine, wave):
            event.content["group_id"] = group_id          # ← 注入
            yield event
            if result is not None:
                # ASK_CARD_START / ASK_CARD_DONE / TEXT
                for sub_event in (ask_card_start, ask_card_done, text_event):
                    sub_event.content["group_id"] = group_id  # ← 注入
                    yield sub_event

    # 汇总 TEXT 也带 group_id
    summary_event.content["group_id"] = group_id
    yield summary_event
```

RUNTIME 事件由 `_execute_task`（`engine.py:106`）产出，在 `_stream_wave` 层面统一注入 `group_id`（或由 `_handle_execute` 在 yield 前统一注入）。

### 第三层：Backend

核心改动：让子 Agent TEXT 走 `switchAgent` 创建独立 MySQL Message，而非 `shouldForwardTextWithoutPersist` 仅转发。

#### 3.1 Message Model 新增 `group_id`

**`backend/internal/model/message.go`**：
```go
type Message struct {
    // ... 现有字段
    GroupID string `json:"group_id" gorm:"column:group_id;index"`
}
```

数据库迁移：
```sql
ALTER TABLE messages ADD COLUMN group_id VARCHAR(64) DEFAULT '';
CREATE INDEX idx_messages_group_id ON messages(group_id);
```

#### 3.2 StreamWriter 改动

**`backend/internal/stream/writer.go`**：

**改动点 1**：`Run` 方法中 TEXT 事件处理——检测 `group_id`，编排场景走 `switchAgent` 持久化：

```go
case generated.EventTypeText:
    if text, ok := event.Content["text"].(string); ok {
        // ... 现有 agent_type 解析 ...
        groupID, _ := event.Content["group_id"].(string)

        if groupID != "" {
            // ═══ 编排场景：子 Agent TEXT → 持久化为独立 Message ═══
            sw.flushTextBuffer()
            if newAgentType != sw.currentAgentType {
                sw.switchAgent(newAgentType, newAgentName, sourceMessageID)
            }
            sw.writeText(text)
            sw.setGroupID(groupID)
        } else if sw.shouldForwardTextWithoutPersist(sourceMessageID) {
            // 现有逻辑：非编排场景的子 session TEXT 仅转发
            sw.flushTextBuffer()
            sw.publishForwardedText(...)
        } else {
            // 现有逻辑：普通 agent 切换
            // ...
        }
    }
```

**关键变化**：有 `group_id` 的 TEXT 事件**跳过** `shouldForwardTextWithoutPersist`，直接走 `switchAgent` → 创建独立 MySQL Message（带子 Agent 的 `agent_type` / `agent_name`）。

**改动点 2**：`switchAgent` 方法——新 Message 带 `group_id`：

```go
func (sw *StreamWriter) switchAgent(newAgentType, newAgentName, sourceMessageID string) {
    // ... 现有 flush + finalize 逻辑 ...
    newMsg := model.Message{
        // ... 现有字段
        GroupID: sw.groupID,  // ← 新增
    }
    // ...
}
```

**改动点 3**：ASK_CARD / RUNTIME 事件——保持在 orchestrator 原始 Message 的 blocks 中，`group_id` 通过 `content` 传递，不影响现有 `persistAskCardEvent` 逻辑。

#### 3.3 Message DAO

新增按 `group_id` 查询方法（`backend/internal/dao/gorm/message_dao.go`）：

```go
func (dao *MessageDao) ListByGroupID(groupID string) ([]model.Message, error) {
    var messages []model.Message
    if err := db.GetDB().Where("group_id = ?", groupID).Order("created_at ASC").Find(&messages).Error; err != nil {
        return nil, err
    }
    return messages, nil
}
```

### 第四层：Frontend Store

#### 4.1 状态模型扩展

**`frontend/src/stores/session-store.ts`** 的 `SessionChatState` 新增：

```typescript
streamingGroupId?: string  // 当前编排轮次的 group_id
```

`ChatMessage` 类型新增：

```typescript
interface ChatMessage {
    // ... 现有字段
    groupId?: string
}
```

#### 4.2 `streamAgentUpdate` 改动

**`frontend/src/stores/message-store.ts:485-534`**：

在编排场景下（有 `groupId`），**允许拆分**但标记 `groupId`：

```typescript
streamAgentUpdate: (sessionId, agentType, agentName, messageId, groupId?) => {
    _flushTextBuf(useSessionStore.setState as SessionSet)
    useSessionStore.setState((s) => {
        const session = ensureSession(s, sessionId)
        const agentChanged = ...
        const messageChanged = ...

        if ((agentChanged || messageChanged) && hasContent) {
            const prevMessage = buildAgentMessage(session, sessionId, {
                keepRuntimeStreamingText: false,
            })

            // ← 新增：编排场景下标记 groupId
            if (session.streamingGroupId || groupId) {
                prevMessage.groupId = session.streamingGroupId ?? groupId
            }

            return {
                sessions: {
                    ...s.sessions,
                    [sessionId]: {
                        ...session,
                        messages: [...session.messages, prevMessage],
                        streamingContent: '',
                        runtimeBlocks: [],
                        streamingAgentType: agentType,
                        streamingAgentName: agentName,
                        streamingMessageId: messageId,
                        streamingGroupId: groupId ?? session.streamingGroupId,
                    },
                },
            }
        }
        // ... 无拆分时的现有逻辑 ...
    })
},
```

#### 4.3 `streamAskCardStart` 改动

**`frontend/src/stores/message-store.ts:802-868`**：

编排场景下，ASK_CARD_START 事件在子 Agent TEXT **之前**到达（事件⑦在事件⑨之前）。当 `speakerChanged` 为 true 时（当前 streaming 是子 Agent，而 ask_card source 是管理者），需要固化当前内容、切换到新 streaming，将 ask_card block 放入**下一个子 Agent 消息**的 blocks 中：

```typescript
streamAskCardStart: (sessionId, event) => {
    // ... 解析 sourceAgentType/sourceAgentName/groupId ...

    const speakerChanged = ...

    if (speakerChanged && hasContent) {
        // 固化当前 streaming 内容为独立消息
        const prevMessage = buildAgentMessage(session, sessionId, {
            keepRuntimeStreamingText: false,
        })
        prevMessage.groupId = session.streamingGroupId ?? groupId

        // 新的 streaming 不切回 orchestrator，
        // 而是切到 ask_card 的 target agent，ask_card block 作为该消息的 header
        return {
            sessions: {
                ...s.sessions,
                [sessionId]: {
                    ...session,
                    messages: [...session.messages, prevMessage],
                    streamingContent: '',
                    runtimeBlocks: [askAgentBlock],  // ask_card 指示条 → 子 Agent 消息的 header
                    streamingAgentType: targetAgentType,   // 切到 ask_card 的 target
                    streamingAgentName: targetAgentName,
                    streamingGroupId: groupId ?? session.streamingGroupId,
                },
            },
        }
    }
    // ... speakerChanged 为 false 时，直接追加 block（现有逻辑）...
}
```
```

#### 4.4 SSE 事件路由

**`frontend/src/hooks/use-chat-stream.ts`**：

TEXT 和 ASK_CARD 事件分发时提取并传递 `group_id`：

```typescript
const groupId = event.content?.group_id
streamAgentUpdate(sessionId, agentType, agentName, messageId, groupId)
// ASK_CARD 事件同理
streamAskCardStart(sessionId, { ...event.content, group_id: groupId })
```

### 第五层：Frontend 渲染

#### 5.1 MessageBubble 渲染

**`frontend/src/components/chat/MessageBubble.tsx`**：

每条消息独立渲染，不再需要 `OrchestrationGroup` 容器或 `groupByOrchestration` 分组逻辑。子 Agent 消息的 blocks 中含有 `ask_agent` block 时，在文本内容上方渲染 ask_card 指示条：

```tsx
function MessageBubble({ message }) {
    return (
        <div className="flex gap-3">
            <AgentAvatar agent={message.agent} type={message.agentType} />
            <div className="flex-1">
                <AgentNameTag agent={message.agent} type={message.agentType} />
                {/* ask_card 指示条 — 来自 blocks */}
                {message.blocks?.filter(b => b.type === 'ask_agent').map(block => (
                    <AskCardBar key={block.id} block={block} />
                ))}
                {/* runtime_status blocks */}
                {message.blocks?.filter(b => b.type === 'runtime_status').map(block => (
                    <RuntimeStatusCard key={block.id} block={block} />
                ))}
                {/* 文本内容 */}
                {message.content && <div className="msg-text">{message.content}</div>}
            </div>
        </div>
    )
}
```

**`AskCardBar` 组件**：

```tsx
function AskCardBar({ block }) {
    return (
        <div className={cn(
            "flex items-center gap-1.5 px-2.5 py-1.5 rounded text-xs mb-2.5",
            "bg-agent-color/8 border border-agent-color/20",
        )}>
            <span className="font-semibold text-purple">{block.source}</span>
            <span className="text-muted">→</span>
            <span className="font-semibold text-agent-color">{block.target}</span>
            <span className="badge">{block.status === 'answered' ? '✓ 已回答' : '⏳ 等待回答'}</span>
            {block.question && (
                <span className="text-muted truncate max-w-60">「{block.question}」</span>
            )}
        </div>
    )
}
```

#### 5.2 MessageList 无需分组

**`frontend/src/components/chat/MessageList.tsx`**：

所有消息扁平渲染，无需分组逻辑：

```tsx
function MessageList({ messages }) {
    return messages.map(msg => (
        <MessageBubble key={msg.id} message={msg} />
    ))
}
```

### SSE 事件流对比

#### 修复前（7 条碎片，无编组）

```
RUNTIME_EXECUTING ×3  → streaming [管理者] runtime_status blocks
ASK_CARD_START        → 追加 ask_agent block
ASK_CARD_DONE         → 更新状态
TEXT(实现者)           → agentChanged → 拆分 → 固化[管理者]消息① → 新建[实现者]streaming
TEXT(执行者)           → (同上，无 groupId 上下文) → 固化[实现者]消息② → 固化[管理者]消息③...
... 最终 7+ 条独立消息，无关联 ...
```

#### 修复后（5 条消息，ask_card 内嵌子 Agent 消息）

```
RUNTIME_EXECUTING ×3  → streaming [管理者] runtime_status blocks       groupId="orch-xxx"
ASK_CARD_START(实现者) → 追加 ask_agent block                         groupId="orch-xxx"
ASK_CARD_DONE         → 更新状态
TEXT(实现者)           → agentChanged + groupId → 拆分
                      → 固化[管理者]消息① groupId="orch-xxx"（含 runtime_status）
                      → 新建[实现者]streaming groupId="orch-xxx"
                      → ask_card block 从管理者消息迁移到实现者消息 blocks
                      → 实现者文本追加到 streaming
ASK_CARD_START(执行者) → speakerChanged → 拆分
                      → 固化[实现者]消息② groupId="orch-xxx"（含 ask_card + 回答文本）
                      → 新建 streaming，ask_card(管理者→执行者) 作为 blocks header
ASK_CARD_DONE         → 更新状态
TEXT(执行者)           → agentChanged → 追加到执行者 streaming
... 类似 ...
TEXT(管理者汇总)       → agentChanged → 切换回管理者 streaming          groupId="orch-xxx"
DONE                  → 固化[管理者]消息⑤ groupId="orch-xxx"

最终 messages = [
    { 管理者, groupId: "orch-xxx", blocks: [runtime_status×3] },
    { 实现者, groupId: "orch-xxx", blocks: [ask_agent(管理者→实现者)], content: "实现者回复" },
    { 执行者, groupId: "orch-xxx", blocks: [ask_agent(管理者→执行者)], content: "执行者回复" },
    { 操作者, groupId: "orch-xxx", blocks: [ask_agent(管理者→操作者)], content: "操作者回复" },
    { 管理者, groupId: "orch-xxx", content: "汇总" },
]
```

渲染时每条消息独立显示，子 Agent 消息的 ask_card block 渲染为文本上方的指示条。

### 改动范围总结

| 层 | 文件 | 改动 | 量级 |
|---|---|---|---|
| Contracts | `schemas/stream-event.yaml`, `schemas/message.yaml` | 新增 `group_id` 字段 | 小 |
| Agentend | `adapters/orchestrator.py`, `execution/engine.py` | 所有 yield 事件注入 `group_id` | 小 |
| Backend | `stream/writer.go` | TEXT 事件检测 `group_id` → 走 `switchAgent` 持久化 | 中 |
| Backend | `model/message.go` | 新增 `GroupID` 字段 + GORM 迁移 | 小 |
| Backend | `dao/gorm/message_dao.go` | 新增 `ListByGroupID` 查询 | 小 |
| Frontend | `stores/message-store.ts` | `streamAgentUpdate` / `streamAskCardStart` 加入 `groupId` + ask_card 归属子 Agent 逻辑 | 中 |
| Frontend | `stores/session-store.ts` | `SessionChatState` 新增 `streamingGroupId` | 小 |
| Frontend | `hooks/use-chat-stream.ts` | 提取 `group_id` 传入 store 方法 | 小 |
| Frontend | `components/chat/MessageBubble.tsx` | ask_card block 渲染为文本上方指示条 | 小 |

### 实施顺序

1. **Contracts**：`group_id` 字段 + `make generate`
2. **Agentend**：事件注入 `group_id`
3. **Backend**：`Message.GroupID` + `StreamWriter` 持久化路径
4. **Frontend Store**：`streamingGroupId` + `streamAgentUpdate` / `streamAskCardStart` 编组逻辑（ask_card 归属子 Agent 消息）
5. **Frontend 渲染**：`MessageBubble` 中 ask_card block 渲染为文本上方指示条

### 废弃方案

#### 方案 A'：前端不拆分 + block 级内容组织

在 `streamAgentUpdate` / `streamAskCardStart` 中，orchestrator 场景下**不拆分消息**，将子 Agent 文本固化为 `agent_text` block 嵌入同一条 streaming 消息中。

**废弃原因**：
1. 子 Agent 文本被嵌入管理者气泡内，语义上是"管理者转述"而非"子 Agent 自己说"
2. Backend 不持久化子 Agent TEXT，刷新后子 Agent 回复丢失
3. 新增 `agent_text` block 类型增加了渲染复杂度，与现有 `text` block 语义重叠

#### 方案 B'：Agentend 合并事件

让 Orchestrator 把 ASK_CARD + 子 Agent TEXT 合并为更少的事件，避免 agent_type 交替。

**废弃原因**：会丢失子 Agent 的实时流式输出能力；ask_card 的"等待回答 → 已回答"动态变化也无法展示。

---

## 相关文件

| 端 | 文件 | 职责 |
|----|------|------|
| Contracts | `contracts/schemas/stream-event.yaml` | StreamEvent 契约定义（新增 `group_id` 字段） |
| Contracts | `contracts/schemas/message.yaml` | Message 契约定义（新增 `group_id` 字段） |
| Agentend | `agentend/src/adapters/orchestrator.py` | `_handle_execute` 中注入 `group_id` 并 yield 事件 |
| Agentend | `agentend/src/orchestrator/execution/engine.py` | ExecutionEngine 产出 RUNTIME_EXECUTING/TEXT/COMPLETED |
| Backend | `backend/internal/stream/writer.go` | StreamWriter 消费 SSE，编排场景走 `switchAgent` 持久化子 Agent Message（**主要改动点**） |
| Backend | `backend/internal/model/message.go` | Message Model 新增 `GroupID` 字段 |
| Backend | `backend/internal/dao/gorm/message_dao.go` | 新增 `ListByGroupID` 查询 |
| Frontend | `frontend/src/stores/message-store.ts` | `streamAgentUpdate` / `streamAskCardStart` 加入 `groupId` + ask_card 归属子 Agent 逻辑（**主要改动点**） |
| Frontend | `frontend/src/stores/session-store.ts` | `SessionChatState` 新增 `streamingGroupId` |
| Frontend | `frontend/src/hooks/use-chat-stream.ts` | SSE 事件路由，提取 `group_id` 传入 store 方法 |
| Frontend | `frontend/src/components/chat/MessageBubble.tsx` | ask_card block 渲染为文本上方指示条 |
