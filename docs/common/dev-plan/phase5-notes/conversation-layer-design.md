# Conversation Layer 设计

> Phase 5 新增层：在 Runtime 之上、API 之下，处理消息路由、回复、引用、重新生成。
> 核心原则：Conversation 语义不泄漏到 Runtime，Adapter 不暴露给 Conversation。

---

## 一、为什么需要独立的一层

现在的请求流：

```
API → AdapterRegistry.get(agent_type) → Adapter
```

这是 **1 session = 1 adapter** 的模型，无法支持：
- 同一对话中多个 Agent 参与
- @mention 指定回复目标
- 引用/回复消息
- 重新生成（同一消息多次执行）

加上 Conversation Layer 后：

```
API
  ↓
Conversation Service          ← 新增：消息图、参与者、上下文
  ↓
Routing Policy                ← 新增：简单路由 vs Orchestrator 编排
  ↓
Runtime Execution Service     ← 已有：Scheduler + ExecutionEngine
  ↓
Adapter                       ← 已有：Claude/OpenCode/Codex
```

**关键信号：Adapter 不需要改。** 说明 Conversation 与 Runtime 解耦成功。

---

## 二、三层职责

### Conversation Layer

```text
负责：消息图（message graph）、回复（reply）、引用（quote）、
     @mention、重新生成（regenerate）、参与者管理
不负责：不知道 adapter、不知道 runtime、不知道 workspace
```

### Routing Layer

```text
负责：简单路由（单 Agent）、复杂路由（Orchestrator 编排）、fallback
不负责：不知道消息图、不知道 workspace
```

### Runtime Layer（已有）

```text
负责：planning、execution、scheduling、events、workspace、merge
不负责：不知道 conversation 语义
```

---

## 三、路由规则：profile 不暴露 adapter

### @mention 用 profile 名称

```
@frontend        → profile="frontend-engineer"
@reviewer        → profile="reviewer"
@architect       → profile="architect"
```

**不用 adapter 名称：**

```
@claude-code     ← ❌ 暴露了 implementation detail
@opencode        ← ❌ 用户不应该知道 provider
```

否则未来 provider migration（claude-code → gemini）会炸。

### 路由关系

```
conversation
    ↓
profile (身份)
    ↓
runtime registry
    ↓
adapter (实现)
```

`target_profile` 是路由对象，不是 `target_agent`。

---

## 四、数据结构

### AgentRequest 扩展

```python
class AgentRequest(BaseModel):
    # 现有
    task_id: str
    session_id: str
    message: str
    agent_type: AgentType
    stream: bool = True
    config: dict | None = None

    # 新增：conversation 层
    conversation_id: str                       # 对话 ID（替代前端传 history）
    target_profile: str | None = None          # @mention 指定 profile
    reply_to_message_id: str | None = None     # 回复的消息 ID
    quote_message_ids: list[str] = []          # 引用的消息 ID 列表（不是文本）
    regenerate_message_id: str | None = None   # 重新生成目标消息 ID
```

### 关键设计决策

| 字段 | 正确做法 | 错误做法 | 原因 |
|------|----------|----------|------|
| `target_profile` | 用 profile 名称 | 用 adapter 名称 | adapter 是 implementation detail |
| `conversation_id` | 只传 ID | 传 `conversation_history` | 服务端自己加载上下文，避免 token 无限增长、replay 不一致 |
| `quote_message_ids` | 传消息 ID 列表 | 传 `quote: str` 文本 | quote 是 reference 不是文本，edited message 会崩 |
| `regenerate_message_id` | 传消息 ID | 用 `regenerate: bool` flag | regenerate 是新 Execution，不是请求标记 |

### 为什么前端不传 conversation_history

1. **token 无限增长** — 对话越来越长，前端传不动
2. **前后端 context 不一致** — 前端看到的 ≠ 服务端实际存的
3. **regenerate 不可重放** — 历史消息无法从客户端重建
4. **Runtime Replay 不成立** — 服务端无法独立重放对话

服务端通过 `conversation_id` 自己加载：

```python
context = ConversationStore.load_context(conversation_id)
```

---

## 五、Message ≠ Execution

一条消息可以有多次执行（重新生成）：

```
message-001
    ├── execution-a1          ← 第一次生成
    ├── execution-a2          ← 第二次重新生成
    └── execution-a3          ← 第三次重新生成
```

### ExecutionRecord

```python
@dataclass
class ExecutionRecord:
    execution_id: str
    conversation_id: str
    trigger_message_id: str              # 触发执行的用户消息
    runtime_id: str | None               # 关联的 Runtime 实例
    target_profile: str                  # 执行者 profile
    state: str                           # pending / running / completed / failed
    created_at: datetime
    completed_at: datetime | None
```

Message 是对话语义，Execution 是运行时语义。它们是多对一关系。

---

## 六、Agent 间上下文共享：Skill-based（不是 Prompt-based）

**核心认知：上下文共享的主要机制是 Skill（taskctl），不是 ContextBuilder 往 prompt 里塞。**

Agent 间共享上下文分三层，**主要靠 Agent 自己通过 skill 按需拉取**：

| 层 | 机制 | 谁主动 | 优先级 |
|---|---|---|---|
| **Skill（主要）** | taskctl summary / common-memory / ls | Agent 按需拉取 | 主要 |
| **Git（文件）** | worktree merge 到 task 分支 | 自动可见 | 主要 |
| **Prompt（辅助）** | ContextBuilder 注入最少必要信息 | Runtime 推送 | 辅助 |

### Skill 层：taskctl（已实现）

每个 Agent 启动时 workspace 已有 taskctl（SkillProvisioner 自动注入）。
Agent 自主调用：`taskctl summary`（看前序 Agent 做了什么）、`taskctl common-memory`（读共享记忆）、`taskctl ls`（看文件结构）。

### Git 层：worktree 分支策略（已实现）

Agent A 执行完 → `taskctl merge` → 合并到 task 分支。Agent B 的 worktree 基于 task 分支，自动能看到 A 的代码。

### Prompt 层：ContextBuilder（辅助）

只注入 Agent 无法通过 skill 获取的信息（reply/quote 等对话级上下文）。
不注入：Agent 执行历史、产物文件列表、共享记忆 — Agent 自己通过 taskctl 获取。

---

## 七、Routing Policy：不要把简单路由放 Orchestrator

### 两种路由模式

**SimpleRoutingPolicy（不经过 Orchestrator）：**

```
"@frontend 写登录页"           → frontend-engineer profile → 直接路由
"@reviewer 这段代码有问题吗"    → reviewer profile → 直接路由
"这个 hook 有问题吗"           → SimpleRouter 判断 → reviewer（单 Agent）
```

**OrchestratorRouter（复杂任务）：**

```
"实现登录系统并审查安全性"       → Orchestrator 拆解为多 Task
"用 Claude 写组件，OpenCode 审查" → Orchestrator 编排
```

### 为什么不让 Orchestrator 做简单路由

如果所有请求都过 Orchestrator，Orchestrator 会退化成 **message router**。
这是危险的 — Orchestrator 应该只做复杂编排。

### 路由判断逻辑

```python
class RoutingPolicy:
    def route(self, message: str, target_profile: str | None) -> RouteResult:
        # 1. 有 @mention → 直接路由到指定 profile
        if target_profile:
            return RouteResult(mode=RoutingMode.DIRECT, profile=target_profile)

        # 2. 简单问题 → 路由到最合适的单 Agent
        if self._is_simple(message):
            profile = self._pick_best_agent(message)
            return RouteResult(mode=RoutingMode.DIRECT, profile=profile)

        # 3. 复杂任务 → 走 Orchestrator
        return RouteResult(mode=RoutingMode.ORCHESTRATOR)

    def _is_simple(self, message: str) -> bool:
        """简单启发式判断，或用 LLM 快速分类"""
        ...
```

---

## 八、Regenerate 实现方向

Regenerate 不是 `regenerate: bool`，而是**基于同一触发消息创建新 Execution**：

```python
# 收到 regenerate_message_id 时
original_message = MessageStore.get(regenerate_message_id)
original_trigger = original_message.trigger_message_id   # 找到原始用户输入

# 创建新 Execution
execution = ExecutionRecord(
    conversation_id=conversation_id,
    trigger_message_id=original_trigger,     # 复用原始输入
    target_profile=original_message.profile, # 同一 profile
)
```

前端展示时按 `execution_id` 选择显示哪次执行的结果。

---

## 九、新增目录结构

```
agentend/src/
├── conversation/                 # 🆕 Conversation Layer
│   ├── models.py                 #   Message, ExecutionRecord, Conversation
│   ├── store.py                  #   ConversationStore（服务端加载上下文）
│   ├── router.py                 #   MessageRouter（解析 @mention）
│   ├── mentions.py               #   @mention 解析 + profile 映射
│   ├── context_builder.py        #   构建 Agent 可用的上下文
│   ├── regenerate.py             #   Regenerate 逻辑
│   └── service.py                #   ConversationService（串联以上）
│
├── routing/                      # 🆕 Routing Layer
│   ├── policies.py               #   RoutingPolicy + RoutingMode
│   ├── simple_router.py          #   SimpleRoutingPolicy（单 Agent 直接路由）
│   └── orchestrator_router.py    #   OrchestratorRouter（复杂任务编排）
│
├── runtime/                      # 🆕 Runtime Layer（已有的 orchestrator 重构）
│   ├── events.py
│   ├── models.py
│   ├── registry.py
│   └── context.py
│
├── orchestrator/                 # ✏️ 重构
├── adapters/                     # ✅ 不改
├── workspace/                    # ✅ 不改
├── session/                      # ✅ Phase 5 保留，不继续强化
└── ...
```

**不放 `runtime/router.py`** — router 是 conversation 语义，不是 runtime。

---

## 十、API 路由变更

```python
# agentend/src/api/v1/agent.py

@router.post("/stream")
async def agent_stream(request: AgentRequest, ...):
    # 1. Conversation 层：加载上下文
    context = await conversation_service.load(request.conversation_id)

    # 2. Routing 层：决定路由
    route = routing_policy.route(request.message, request.target_profile)

    if route.mode == RoutingMode.DIRECT:
        # 简单路由 → 直接找 profile 对应的 adapter
        profile = registry.get_profile(route.profile)
        adapter = registry.get_adapter(profile.adapter)

    elif route.mode == RoutingMode.ORCHESTRATOR:
        # 复杂路由 → 走 Orchestrator
        adapter = registry.get_adapter(AgentType.ORCHESTRATOR)

    # 3. 构建增强消息
    enhanced = context_builder.build(request, context)

    # 4. 执行（和现在一样）
    ...
```

---

## 十一、Session 的定位变化

```
当前：1 session = 1 adapter（核心业务对象）
未来：session 只是 provider runtime transport（传输层概念）
终极：conversation → execution → runtime agent（session 退化为内部细节）
```

Phase 5 保留 session，**不继续强化 session 概念**。核心业务对象转移到 conversation + execution。

---

## 十二、最终请求流

```
用户消息 "@frontend 写一个登录页，参考之前的讨论"
    ↓
API（AgentRequest: conversation_id, target_profile, quote_message_ids）
    ↓
ConversationService
    ├── 加载对话上下文
    ├── 解析 @frontend → profile=frontend-engineer
    └── 构建 enhanced message（含引用的之前的讨论）
    ↓
RoutingPolicy
    └── target_profile 存在 → DIRECT 模式
    ↓
RuntimeRegistry
    └── profile=frontend-engineer → adapter=claude-code
    ↓
ExecutionEngine
    └── 创建 ExecutionRecord
    ↓
Claude Adapter
    └── stream_chat(enhanced_message, ...)
    ↓
RuntimeEvent Stream → SSE → 前端
```

```
用户消息 "实现登录系统并审查安全性"
    ↓
API（AgentRequest: conversation_id, 无 target_profile）
    ↓
ConversationService → 加载上下文
    ↓
RoutingPolicy
    └── 复杂任务 → ORCHESTRATOR 模式
    ↓
Orchestrator
    └── Planner 拆解 → Scheduler → ExecutionEngine → Multi-Agent
    ↓
RuntimeEvent Stream → SSE → 前端
```
