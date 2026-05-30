# Phase 5.1: Orchestrator 向 Subagent 提问（双写流式）

> 目标: Orchestrator 规划时可通过 `ask_agent` 工具向指定 subagent 提问，subagent 在自己的 session 中流式回答，同时 orchestrator 框内展示提问卡片。
> 预估: 3-4 天
> 前置: Phase 5 完成（LangGraph Agent 模式 + 群聊 UI 可用）
> 视觉参考: [ask-agent-demo.html](ask-agent-demo.html)

## 核心流程

```
User → Orchestrator Session
  │
  ├── REASON node
  │     ├── LLM 调用 ask_agent(agent="claude-code", question="...")
  │     │     │
  │     │     │  ① 发送消息到 subagent session（BackendClient.run_task）
  │     │     │  ② 后端存储消息 → 触发 agentend 处理
  │     │     │  ③ subagent 开始流式回答
  │     │     │
  │     │     ├── [subagent session SSE] → 前端 subagent ChatArea 显示流式回答
  │     │     └── [orchestrator SSE] → 推送 ask_card 事件，显示提问卡片（expanded + pending）
  │     │
  │     │  同步阻塞：等待 subagent 回答完毕
  │     │  回答内容作为 ToolMessage 返回 LLM
  │     │
  │     │  回答完成后 → ask_card 折叠（collapsed + answered），子 agent 回答作为独立消息紧随其后
  │     │
  │     └── LLM 拿到回答继续 reasoning → 决定 text / plan_and_dispatch
  │
  ├── [可选] CONSULT node（reason → dispatch 之间）
  │     └── 批量向多个 subagent 提问，并发执行，全部完成后进入 dispatch
  │
  └── DISPATCH → EXECUTE → REVIEW → ...
```

## 关键设计决策

### 1. 双写路径

| 路径 | 说明 | 事件类型 |
|------|------|----------|
| Subagent session | 正常消息流，持久化到 DB，前端 SSE 推送 | `text`, `done` |
| Orchestrator session | 提问卡片 + 状态同步 | `ask_card_start`, `ask_card_done` |

### 2. Ask Card 三态生命周期

基于 [ask-agent-demo.html](ask-agent-demo.html) 的视觉设计：

| 状态 | 卡片样式 | 问题 body | 状态 badge |
|------|----------|-----------|------------|
| **pending** | 展开完整显示 | 完整问题文本 | 紫色脉冲圆点 + "等待回答" |
| **answered（collapsed）** | 折叠为一行 header | `display: none` 隐藏，header 显示 30 字摘要 | 绿色圆点 + "已回答" |
| **answered（expanded）** | 点击展开 | 恢复完整问题文本 | 绿色圆点 + "已回答" |

交互规则：
- 回答完成后自动折叠，header 显示问题摘要（前 30 字 + `...`）
- 折叠态 hover 时边框高亮（`cursor: pointer`）
- 点击 toggle 展开/收起，可反复切换
- 子 agent 的回答作为**独立消息**紧跟在卡片后面（非内嵌）

### 3. 同步阻塞实现

`ask_agent` 工具在 agentend 内部：
1. 调用 `BackendClient.run_task(session_id=subagent_session, message=question)`
2. 通过 `BackendClient.stream_result()` 读取 subagent 的回答 SSE
3. 收集完整回答内容作为 ToolMessage 返回给 LLM
4. 同时向 orchestrator 的 SSE stream 推送 `ask_card_start` / `ask_card_done` 事件

### 4. 触发时机

- **reason_node**：LLM 可随时调用 `ask_agent`，拿回答后继续 reasoning
- **consult_node**（新增）：dispatch 前批量咨询，graph 中 reason → consult → dispatch

## 三端改动

### Contracts — 新增事件类型

```yaml
# contracts/schemas/events.yaml — 新增
- ask_card_start    # Orchestrator 向 subagent 发起提问（卡片展开态）
- ask_card_done     # Subagent 回答完成（卡片折叠态 + 摘要）
```

**ask_card_start content**:
```json
{
  "question_id": "q-001",
  "target_agent": "claude-code",
  "target_session_id": "sess-xxx",
  "question": "数据库 schema 是什么？"
}
```

**ask_card_done content**:
```json
{
  "question_id": "q-001",
  "target_agent": "claude-code",
  "target_session_id": "sess-xxx",
  "summary": "当前 schema 包含 users, tasks, messages 三张表...",
  "status": "completed"
}
```

### AgentEnd — 新 tool + consult node

**新 tool** (`agentend/src/orchestrator/planning/tools.py`):
```python
@tool
def ask_agent(agent: str, question: str) -> str:
    """向指定 Agent 提问，阻塞等待回答。用于规划阶段收集信息。"""
    # 实际执行在 reason_node / consult_node 中拦截处理
    return "ask_pending"
```

**reason_node 改动** (`agentend/src/orchestrator/planning/graph.py`):
- 检测到 `ask_agent` tool call 时，不直接执行，而是：
  1. 向 orchestrator SSE 推 `ask_card_start`（卡片展开，pending 状态）
  2. 调用 BackendClient 向 subagent session 发消息
  3. 阻塞等待 stream_result
  4. 推 `ask_card_done`（卡片折叠为摘要行，answered 状态）
  5. 将回答作为 ToolMessage 追加到 messages，继续 LLM 循环

**新增 consult_node**:
```python
def consult_node(state: GraphState) -> dict:
    """dispatch 前的批量咨询阶段。"""
    pending_questions = state.get("pending_questions", [])
    # 并发向多个 subagent 提问
    # 收集所有回答 → 注入 state
    return {"consult_results": [...]}
```

**Graph 变更**:
```
skill_prepare → reason ──┬── text ──→ save_mem → END
                         ├── plan ──→ consult → dispatch → execute → review → ...
                         └── error → END
```

### Backend — 无新增

现有 `POST /api/tasks/{taskId}/run` + `GET /api/tasks/{taskId}/stream` 已足够：
- Orchestrator 通过 `BackendClient.run_task` 向 subagent session 发消息
- Subagent 的回答通过 SSE 正常推送到 subagent session
- Orchestrator 的 ask 事件通过 orchestrator session 的 SSE 推送

### Frontend — Ask Card 组件 + 折叠交互

**新增 block 类型** (`frontend/src/lib/block-types.ts`):
```typescript
| { type: 'ask_agent'; id: string; question_id: string; target_agent: string;
    target_session_id: string; question: string; status: 'pending' | 'answered';
    collapsed: boolean; summary?: string }
```

**AskAgentCard 组件三态渲染** (`frontend/src/components/chat/AskAgentCard.tsx`):

```
┌─ Ask Card (pending) ──────────────────────────┐
│ [项] → [前] 前端小助手          ●脉冲 等待回答 │
│ ─────────────────────────────────────────────── │
│ 登录页需要支持哪些登录方式？                     │
│ 邮箱表单和 OAuth 按钮的 UI 布局有什么建议？     │
└────────────────────────────────────────────────┘

            ↓ 回答完成后自动折叠

┌─ Ask Card (collapsed, 可点击) ─────────────────┐
│ [项] → [前] 前端小助手 登录页需要支持哪些登录方... ● 已回答 │
└────────────────────────────────────────────────┘

            ↓ 点击展开

┌─ Ask Card (expanded, 可再次点击收起) ──────────┐
│ [项] → [前] 前端小助手                ● 已回答 │
│ ─────────────────────────────────────────────── │
│ 登录页需要支持哪些登录方式？                     │
│ 邮箱表单和 OAuth 按钮的 UI 布局有什么建议？     │
└────────────────────────────────────────────────┘
```

**交互行为**:
- `ask_card_start` → 渲染卡片（expanded + pending），子 agent 回答作为独立流式消息紧跟其后
- `ask_card_done` → 卡片折叠（collapsed + answered），header 显示 30 字摘要
- 折叠态：`cursor: pointer`，hover 边框高亮，点击 toggle 展开/收起
- 展开态：完整显示问题 body，再次点击收起
- 子 agent 回答始终作为独立消息保留在聊天流中，不嵌入卡片

**subagent session 联动**:
- 当 orchestrator 发起 ask 时，subagent session 自动触发 SSE 连接
- 前端侧边栏 subagent session 显示"新消息"提示
- 用户点击可切换到 subagent 查看完整流式回答

## 文件清单

```
Contracts:
├── contracts/schemas/events.yaml              # ✏️ 新增 ask_card_start / ask_card_done

AgentEnd:
├── src/orchestrator/planning/tools.py         # ✏️ 新增 ask_agent tool
├── src/orchestrator/planning/graph.py         # ✏️ reason_node 拦截 ask_agent + 新增 consult_node
├── src/orchestrator/planning/prompts.py       # ✏️ prompt 中说明 ask_agent 用法
├── src/adapters/orchestrator.py               # ✏️ _handle_reason 支持 ask_agent 双写

Backend:
└── (无改动 — 现有 API 已足够)

Frontend:
├── src/lib/block-types.ts                     # ✏️ 新增 ask_agent block 类型（含 collapsed 字段）
├── src/lib/block-reducer.ts                   # ✏️ 解析 ask_card_start / ask_card_done 事件
├── src/stores/chat.ts                         # ✏️ streamAskCardStart / streamAskCardDone
├── src/hooks/use-chat-stream.ts               # ✏️ 处理新事件类型
├── src/components/chat/AskAgentCard.tsx        # 🆕 提问卡片组件（三态 + 折叠交互）
└── src/components/chat/MessageRenderer.tsx     # ✏️ 渲染 ask_agent block
```

## 验证流程

```bash
# 1. 启动三端
make run-backend && make run-agentend && make run-frontend

# 2. 场景 1：reason_node 中提问
# 用户: "帮我看看这个项目能不能用 Docker 部署"
# 预期:
#   - Orchestrator: "正在分析..." → [Ask Card expanded] "项目有 Dockerfile 吗？" (pending)
#   - Claude Code session: 流式回答 "有 Dockerfile，在根目录..."
#   - Orchestrator: [Ask Card collapsed] "项目有 Dockerfile 吗..." (answered) → 回答消息紧随
#   - Orchestrator: "收到回复，项目有 Dockerfile..." → 直接回复用户

# 3. 场景 2：consult_node 批量咨询
# 用户: "重构整个认证系统"
# 预期:
#   - Orchestrator REASON → plan_and_dispatch
#   - CONSULT: 并发向多个 subagent 提问，每个 Ask Card 展示完整问题
#   - 每个 subagent 回答后，对应 Ask Card 折叠，回答消息保留
#   - Orchestrator 汇总回答 → DISPATCH

# 4. 前端交互验证
# - Ask Card pending: 完整展示问题，紫色脉冲 "等待回答"
# - Ask Card answered: 自动折叠为一行摘要，绿色 "已回答"
# - 点击折叠态: 展开完整问题，再次点击收起
# - hover 折叠态: 边框高亮提示可点击
# - 子 agent 回答始终作为独立消息显示在 Ask Card 下方
# - 切换 session 后，历史消息正确加载，Ask Card 保持折叠态
```
