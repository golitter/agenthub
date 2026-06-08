# 项目现状与后续开发指南

> 最后更新: 2026-05-28
> 当前阶段: Phase 5 进行中 — AgentEnd Runtime Coordinator + Identity + Workspace Isolation

---

## 一、架构定位（核心认知）

这个项目是一个 **Multi-Agent Runtime Platform** — 轻量级 Agent 操作系统。

三层架构的真实定位：

```
Go Backend   = Transport Gateway（传输网关）
AgentEnd     = Agent Runtime OS（运行时操作系统）
Frontend     = Runtime Visualization（运行时可视化）
```

### 各端职责边界

**Go Backend 只负责：**
- HTTP / SSE 传输
- Auth 认证
- Persistence 持久化（MySQL）
- Redis Buffer

**Go Backend 不理解：**
- orchestrator / task graph / shared / planning / scheduling / agent identity / workspace isolation

**AgentEnd 是所有 Runtime 逻辑的唯一归属地：**

```
AgentEnd Runtime
├── Planner              # 任务规划
├── Scheduler            # 执行调度
├── ExecutionEngine      # 执行引擎
├── RuntimeEventBus      # 统一事件流
├── WorkspaceManager     # 工作区管理（per RuntimeAgent）
├── MergeManager         # 分支合并协调
├── SharedContext        # Agent 间共享上下文
├── PermissionSystem     # 权限系统
├── AgentRegistry        # Agent 注册中心
├── ProfileSystem (SOUL) # Agent 身份与策略
└── AdapterLayer         # 执行后端（Claude/OpenCode/Codex）
```

**Frontend 渲染的是 Runtime Coordination Timeline，不是普通 IM 聊天。**

---

## 二、核心抽象：三层分离

必须明确区分三个概念：

### 2.1 Agent Profile（人格）— 谁来做

```
frontend-engineer
reviewer
architect
tester
```

定义：身份、职责、权限、工具、风格、协作方式。通过 `SOUL.yaml` 描述。

Profile 是长生命周期的身份定义，可以被多个 Task、多个 Runtime 复用。

### 2.2 Adapter Backend（执行后端）— 怎么执行

```
claude-code
opencode
codex
```

定义：如何调用模型、如何解析 stream、如何管理 session。

### 2.3 RuntimeAgent（运行时实例）— 短生命周期 Worker

```
session_agent_id（如 agent-a1b2）
```

定义：当前 task、当前 workspace、当前 branch、当前状态。

这是真正运行中的 Agent Process，**短生命周期**，Task 完成即销毁。

### 真正的关系

```
Frontend Engineer Profile        ← 身份（长生命周期）
        ↓ uses
Claude Code Adapter              ← 执行后端
        ↓ becomes
RuntimeAgent (agent-a1b2)        ← 运行时实例（短生命周期）
```

### 对 ExecutionPlan 的影响

```python
ExecutionTask(
    id="task-001",
    profile="frontend-engineer",   # 身份
    adapter="claude-code",         # 后端
    prompt="实现登录页",
)
```

`adapter` 是实现，`profile` 才是 identity。

---

## 三、Workspace Isolation：Runtime-Centric（非常关键）

### 3.1 隔离粒度

Workspace 绑定的是 **RuntimeAgent 实例**（session_agent_id），不是 Profile。

因为同一个 Profile（如 `frontend-engineer`）可能：
- 被多个 task 使用
- 被多个 runtime 使用
- 同时存在多个 session

如果按 Profile 隔离（`frontend-agent/`），会变成长生命周期 Agent，系统很快混乱。

**正确的隔离是短生命周期 Runtime Worker。**

### 3.2 Runtime Filesystem Layout

```
workspaces/
└── {task_id}/
    │
    ├── orchestrator/                  # Orchestrator 专属空间
    │   ├── runtime.json               #   Runtime 状态
    │   ├── plan.json                  #   ExecutionPlan
    │   └── events/                    #   事件日志
    │
    ├── shared/                        # Agent 间共享上下文
    │   ├── artifacts/                 #   产出物
    │   ├── reviews/                   #   Review 文件
    │   ├── outputs/                   #   Agent 输出
    │   └── memory/                    #   Pin Memory
    │
    ├── {session_agent_id_1}/          # RuntimeAgent 工作区 1
    │   ├── repo/                      #   Git worktree（独立 branch）
    │   ├── state.json                 #   Agent 运行时状态
    │   ├── logs/                      #   执行日志
    │   └── artifacts/                 #   该 Agent 的产出物
    │
    └── {session_agent_id_2}/          # RuntimeAgent 工作区 2
        ├── repo/                      #   Git worktree（独立 branch）
        ├── state.json
        ├── logs/
        └── artifacts/
```

例如：

```
workspaces/
└── task-001/
    ├── orchestrator/
    ├── shared/
    ├── agent-a1b2/                    # frontend-engineer, branch: feature/task-001-ui
    │   └── repo/
    └── agent-c3d4/                    # reviewer, branch: feature/task-002-review
        └── repo/
```

### 3.3 Orchestrator 不直接改代码

Orchestrator 的 workspace 只包含：
- plan（规划数据）
- runtime state（运行时状态）
- event logs（事件日志）
- coordination data（协调数据）

**Orchestrator 不拥有代码工作区，不直接 edit repo。**

否则 Worker Agent + Orchestrator 都改代码，系统很快混乱。

Orchestrator 只做 **协调 merge**。

### 3.4 每个 RuntimeAgent 独立 worktree + 独立 branch

```
agent-a1b2
    ↓
feature/task-001-ui

agent-c3d4
    ↓
feature/task-002-review
```

互相隔离，互不干扰。

---

## 四、MergeManager：分支合并协调

### 4.1 Merge 是 Runtime 行为

不是 git command wrapper，而是 Runtime 协调行为。

```python
class MergeManager:
    async def merge_agent_branch(
        self,
        source_agent_id: str,
        target_branch: str = "main",
    ) -> MergeResult:
        """将 RuntimeAgent 的 branch merge 到目标分支"""
        ...
```

### 4.2 冲突处理：生成新的 Runtime Task

不要让 Orchestrator 直接改冲突文件。而是：

```
merge failed (conflict)
    ↓
create conflict-resolution task
    ↓
spawn reviewer agent
    ↓
resolve conflict
    ↓
retry merge
```

这会让系统非常干净 — 冲突解决也是一个被编排的 Task。

### 4.3 Merge 事件

```
workspace.branch.created
workspace.merge.started
workspace.merge.conflict
workspace.merge.completed
```

---

## 五、SOUL System：Agent Identity Layer

### 5.1 SOUL 是 Runtime Identity Contract

不是 prompt 文件，而是 **Agent Capability Descriptor**。

`profiles/frontend-engineer/soul.yaml`：

```yaml
id: frontend-engineer

identity:
  role: Senior Frontend Engineer

responsibilities:
  - react-ui
  - component-architecture
  - tailwind-styling

constraints:
  - no-backend-edit
  - no-db-migration

permissions:
  spawn_agent: false          # 只有 orchestrator 能 spawn
  write_workspace: true
  review_code: false

tools:
  allow:
    - edit
    - grep
    - render

collaboration:
  can_review: false
  can_delegate: false

style:
  concise: true
  production_ready: true
```

### 5.2 Profile 目录结构

```
agentend/src/profiles/
├── frontend-engineer/
│   ├── soul.yaml          # 身份 + 权限 + 约束
│   ├── system.md          # System Prompt 模板
│   └── rules.yaml         # 专属规则
│
├── reviewer/
│   ├── soul.yaml
│   ├── system.md
│   └── rules.yaml
│
├── architect/
│   └── ...
│
└── tester/
    └── ...
```

`profile ≠ adapter`。一个 Profile 可以用不同的 Adapter 后端执行。

### 5.3 Capability-Based Permission

```yaml
# orchestrator/soul.yaml
permissions:
  spawn_agent: true           # 可以创建子 Agent
  write_workspace: true
  review_code: true

# frontend-engineer/soul.yaml
permissions:
  spawn_agent: false          # 不能自己拉 Agent
  write_workspace: true
  review_code: false
```

ExecutionEngine 检查：

```python
if not agent.permissions.spawn_agent:
    raise PermissionDenied()
```

### 5.4 Context Rendering Pipeline

不拼字符串，用 **Runtime Context Assembly**：

```python
class PromptRenderer:
    def render(self, context: AgentContext) -> str:
        return f"""
{render_soul(context.soul)}
{render_rules(context.rules)}
{render_workspace(context.workspace)}
{render_task(context.task)}
{render_memory(context.memory)}
"""
```

每层独立渲染，可测试，可组合。

---

## 六、目前已实现的功能

### 6.1 AgentEnd (Python, ~85%)

| 模块 | 说明 |
|------|------|
| FastAPI 应用骨架 | main.py、CORS、依赖注入、Lifespan |
| Claude Code 适配器 | Claude CLI 集成、session 持久化、流式 JSON 解析、进程管理 |
| OpenCode 适配器 | OpenCode CLI 集成、NDJSON 流式解析 |
| Codex 适配器 | Codex CLI 集成、事件映射 |
| Orchestrator Planner | LangGraph plan → write_shared |
| 会话管理 | 状态机（IDLE → RUNNING → COMPLETED/ERROR） |
| 工作区隔离 | Git worktree 隔离（按 session_agent_id） |
| 规则引擎 | System Prompt 注入、工具白/黑名单、最大轮次限制 |
| 技能供给 | 内置 skillctl 和 render |
| 资源监控 | 磁盘/内存查询 |
| 配置系统 | config.yaml + 环境变量 |

**Orchestrator 已有模块：**

| 模块 | 文件 | 状态 |
|------|------|------|
| LangGraph Graph | `orchestrator/graph.py` | ✅ plan → write_shared |
| Models | `orchestrator/models.py` | ✅ PlanOutput, TaskDef, TaskResult, DispatchResult |
| Prompts | `orchestrator/prompts.py` | ✅ PLAN_PROMPT + RuntimeState |
| Dispatcher | `orchestrator/dispatcher.py` | ✅ 任务映射 |
| Aggregator | `orchestrator/aggregator.py` | ✅ LLM 汇总结果 |
| Evolution Store | `orchestrator/evolution.py` | ✅ 编排经验 |
| Pin Memory | `orchestrator/pin_memory.py` | ✅ 用户约束 |
| Adapter | `adapters/orchestrator.py` | ✅ 串联 5 阶段 |

### 6.2 Backend (Go, ~80%)

**Transport Gateway**，只做 SSE 透传和 CRUD。

| 模块 | 说明 |
|------|------|
| Task/Session/Message 管理 | CRUD + 多 Agent 绑定 |
| SSE 流式系统 | Redis Stream 缓冲 + MySQL 批量持久化 |
| 工作区代理 | 代理 AgentEnd 文件操作、Git diff/commit/revert |
| Admin Dashboard | 6 个管理页面 |

### 6.3 Frontend (React, ~70%)

**Runtime Visualization**，QQ 风格三栏 IM 界面。

| 模块 | 说明 |
|------|------|
| 三栏布局 | 左侧导航 + 中间会话列表 + 右侧聊天区域 |
| 聊天核心 | 消息发送、流式接收、多类型 Block 渲染 |
| SSE 连接 | 自动重连、事件解析 |
| Block 渲染 | text / html-render / image / attachment / diff |
| Diff 卡片 | 多文件 diff、Split/Unified、CodeMirror 编辑 |
| 管理面板 | 6 个管理页面 |

### 6.4 契约层 + 工程设施

**6 个 YAML Schema** + `make generate` 自动生成三端类型（18 个文件）。
**Makefile** + **scripts/** 统一三端服务管理。

---

## 七、当前进度总览

```
Phase 1 — Go 胶水层          ✅ 完成
Phase 2 — 最小聊天界面        ✅ 完成
Phase 3 — IM 体验补全         ✅ 完成
Phase 4 — 产物与打磨          ✅ 完成
Phase 5 — Agent 模式重构      🔧 进行中
  ├─ REASON 节点 (Agent 核心)   ✏️ 重构中
  ├─ plan_and_dispatch 工具     🆕 待实现
  ├─ Memory (LangGraph)         🆕 待实现
  ├─ 生命周期图 (Dispatch→Execute→Review→Evolve)  🆕 待实现
  ├─ Wave Executor 子图         🆕 待实现
  ├─ OrchestratorAdapter 重构   ✏️ 待实现
  ├─ 前端群聊 UI                ❌ 未开始
  └─ Go Backend                 🔧 需少量改动（SSE 透传新事件）
Phase 6 — 预览 + 部署         📋 待开始
Phase 7 — 演示 + 交付         📋 待开始
```

---

## 八、Phase 5 核心架构

### 8.1 Orchestrator = 有记忆的 Agent

Orchestrator 不是一个 Pipeline，而是一个有记忆的对话 Agent。
- 闲聊 → LLM 直接回复文本
- 任务 → 调用 `plan_and_dispatch` 工具触发编排
- Memory 跨轮次持久化（LangGraph MemorySaver）

```
REASON 节点内部:
  messages = [System(prompt)] + memory_messages + [Human(user_input)]

  LLM tool-calling 循环:
    if response.tool_calls:
      if tc.name == "plan_and_dispatch":
        → 编排信号，提取 PlanOutput，走 DISPATCH
      else:
        → 普通工具（read_file 等），继续循环
    else:
      → 纯文本回复（闲聊/直接回答），走 SAVE_MEM
```

### 8.2 执行流（LangGraph 状态图）

```
POST /v1/agent/stream
    ↓
OrchestratorAdapter (config={"thread_id": session_id})
    ↓
LangGraph Agent Graph:
    REASON (LLM + Memory + Skills)
      ├── 文本回复 → SAVE_MEM → END
      └── plan_and_dispatch → DISPATCH → EXECUTE → REVIEW
                                                    ├── ok → EVOLVE → SAVE_MEM → END
                                                    └── replan → REASON (带失败上下文)
```

### 8.3 GraphState

```python
class GraphState(TypedDict):
    # 输入层
    message: str
    agents: list[dict]
    task_id: str
    shared_dir: str
    allowed_read_dirs: list[str]

    # REASON 产出
    output_type: str                            # "text" | "plan"
    text: str
    plan: PlanOutput | None

    # DISPATCH 产出
    dispatch_results: list[DispatchResult]
    execution_waves: list[list[DispatchResult]] # 拓扑分波

    # EXECUTE 产出（累积）
    task_results: Annotated[list[TaskResult], add]
    task_status: dict[str, str]

    # REVIEW 决策
    needs_replan: bool
    replan_reason: str

    # 元信息
    iteration: Annotated[int, add_one]
    max_iterations: int                         # 默认 3

    # Memory
    memory_messages: Annotated[list, add]
```

### 8.4 Execute 子图 (Wave Executor)

按依赖关系拓扑排序，分波次执行：

```
Wave 0: [Task-A]           ← 无依赖，立即执行
Wave 1: [Task-B, Task-C]   ← 依赖 A，A 完成后并行
Wave 2: [Task-D]           ← 依赖 B+C
```

### 8.5 Memory 设计

```
Orchestrator Memory (per session, LangGraph MemorySaver)
├── messages: 累积对话 + 工具调用 + 结果
├── evolution: 规划经验（evolution.yaml，已有）
└── pins: 共享约束（_pins.yaml，已有）
```

REASON 节点从 checkpointer 加载历史 messages，注入 pin + evolution context。

---

## 九、Phase 5 实施计划

> 详细实施步骤见 [phase5-orchestrator.md](../phase5-orchestrator.md)

### 9.1 实施步骤概要

1. **新增 `plan_and_dispatch` 工具** — LLM 编排信号
2. **重构 REASON 节点** — 合并 skill 加载 + LLM tool-calling 循环
3. **生命周期节点** — Dispatch / Execute / Review / Evolve / SaveMem
4. **构建新 Graph** — LangGraph StateGraph + MemorySaver
5. **Memory 集成** — LangGraph checkpointer
6. **Execute 子图** — Wave executor
7. **重构 OrchestratorAdapter** — 适配新图结构
8. **Go Backend 适配** — SSE 透传新事件
9. **前端群聊 UI** — PlanningCard + Agent 标签

### 9.2 Phase 5 MVP 范围

**支持：**
- REASON 节点（闲聊直接回复 + 任务编排）
- 跨轮次 Memory（LangGraph MemorySaver）
- 渐进式 Skill 加载
- `plan_and_dispatch` 工具信号
- Execute 子图（Wave Executor，支持依赖分波）
- REVIEW 重规划（失败时带回上下文）
- Evolution + Pin 知识管理
- 群聊 UI（Agent 标签 + 规划卡片）

**不支持（Phase 6+）：**
- Parallel Execution（Wave Executor 已预留）
- Conflict-Resolution Task
- Retry / Cancellation
- Profile System (SOUL)（留 Phase 6）
- Durable Resume

### 9.4 验证方式

```bash
# 启动三端
make all

# AgentEnd 验证
curl -X POST http://localhost:8001/v1/agent/stream \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "test-001",
    "session_id": "orch-1",
    "message": "用 Claude Code 写一个 React 登录页，然后用 OpenCode 审查代码质量",
    "agent_type": "orchestrator",
    "config": {
      "agents": [
        {"id": "claude-code", "profile": "frontend-engineer", "name": "Claude Code"},
        {"id": "opencode", "profile": "reviewer", "name": "OpenCode"}
      ],
      "task_id": "test-001",
      "shared_dir": "/tmp/orch-test"
    }
  }'

# 预期事件序列:
# runtime.started
# runtime.planning
# runtime.plan.completed
# runtime.agent.spawned (agent-a1b2, profile=frontend-engineer)
# workspace.branch.created (feature/task-001-ui)
# task.started (task-001)
# agent.delta (Frontend Engineer 输出...)
# task.completed (task-001)
# runtime.agent.spawned (agent-c3d4, profile=reviewer)
# workspace.branch.created (feature/task-002-review)
# task.started (task-002)
# agent.delta (Reviewer 输出...)
# task.completed (task-002)
# workspace.merge.started
# workspace.merge.completed
# runtime.completed

# 验证 workspace 结构:
ls /tmp/orch-test/
# → orchestrator/ shared/ agent-a1b2/ agent-c3d4/
```

**前端验证：**

1. 选择 Orchestrator Agent
2. 发消息 "用 Claude Code 写登录页，OpenCode 审查"
3. 看到 [Orchestrator] 规划进度
4. 看到 [Frontend Engineer] 标签 + 蓝色 → Creating worktree → 流式输出
5. 看到 [Reviewer] 标签 + 绿色 → Creating worktree → 流式输出
6. 看到 [Runtime] Merging branches → Completed

---

## 十、Phase 6-7 展望

### Phase 6

**Runtime 升级：**
- Scheduler 并行执行（DAG 拓扑排序）
- Conflict-Resolution Task（自动 spawn reviewer 解冲突）
- Retry / Cancellation
- Dynamic Replanning
- 更多 Profile（architect、tester）

**产物预览：**
- 图片预览、代码预览、iframe sandbox

**部署：**
- Docker Compose + Nginx

### Phase 7：演示 + 交付

| 交付物 | 状态 |
|--------|------|
| 产品设计文档 | 需整理 |
| 技术文档 | 需整理 |
| 可运行 Demo | ✅ `make all` 基本可用 |
| AI 协作记录 | ✅ CLAUDE.md / Skills / Git history |
| 3 分钟 Demo 视频 | 需录制 |

**Demo 场景：**

1. 单聊代码生成（30s）
2. 多会话并行（30s）
3. Orchestrator Runtime（60s）— 规划 → [Frontend Engineer] → [Reviewer] → Merge → Completed
4. 产物预览（30s）

---

## 十一、开发纪律

### 11.1 三端职责不可逾越

```
Go Backend   → HTTP/SSE/Auth/Persistence，不碰 Runtime
AgentEnd     → 所有 Runtime 逻辑唯一归属
Frontend     → Runtime Visualization，不做业务逻辑
```

### 11.2 Profile ≠ Adapter ≠ RuntimeAgent

```
Profile      = 身份（长生命周期定义）
Adapter      = 执行后端（怎么调 CLI）
RuntimeAgent = 运行时实例（短生命周期 Worker）
Workspace    = 绑定 RuntimeAgent，不绑定 Profile
```

### 11.3 Orchestrator 只协调，不改代码

Orchestrator workspace 只有 plan / runtime state / event logs / coordination data。
代码修改只由 Worker RuntimeAgent 完成。

### 11.4 契约优先

1. 先更新 `contracts/schemas/*.yaml`
2. `make generate`
3. `contracts/logs/` 写变更记录

### 11.5 串行叠代

每个 Phase 有可演示成果。先跑通再优化。

### 11.6 开发启动

```bash
make all       # 一键三端热重载
make status    # 查看状态
make stop      # 停止
```

### 11.7 技术栈

| 端 | 框架 | 语言 | 端口 | 热重载 |
|----|------|------|------|--------|
| Frontend | React 19 + Vite 8 | TypeScript | 5173 | Vite HMR |
| Backend | Gin | Go | 8080 | Air |
| AgentEnd | FastAPI + LangGraph | Python | 8001 | Uvicorn |

| 中间件 | 用途 |
|--------|------|
| MySQL 8.0 + GORM | 持久化 |
| Redis 6.0+ Stream | SSE 缓冲 |
| 七牛云 | 文件存储 |

---

## 十二、关键文件索引

### AgentEnd（Agent Runtime OS）
```
agentend/src/
├── profiles/                          # 🆕 Profile System (SOUL)
│   ├── frontend-engineer/
│   │   ├── soul.yaml
│   │   ├── system.md
│   │   └── rules.yaml
│   └── reviewer/
│       ├── soul.yaml
│       ├── system.md
│       └── rules.yaml
│
├── runtime/                           # 🆕 Runtime 核心
│   ├── registry.py                    #   AgentRegistry
│   ├── events.py                      #   RuntimeEvent
│   ├── models.py                      #   RuntimeAgent, Permissions
│   └── context.py                     #   PromptRenderer
│
├── orchestrator/
│   ├── coordinator.py                 # 🆕 RuntimeCoordinator
│   ├── runtime.py                     # 🆕 OrchestratorRuntime + TaskRuntime
│   ├── scheduler.py                   # 🆕 Scheduler
│   ├── execution.py                   # 🆕 ExecutionEngine + normalize
│   ├── merge.py                       # 🆕 MergeManager
│   ├── graph.py                       # ✅ LangGraph Planner
│   ├── models.py                      # ✏️ ExecutionPlan + ExecutionTask
│   ├── prompts.py                     # ✅
│   ├── dispatcher.py                  # ✅
│   ├── aggregator.py                  # ✅
│   ├── evolution.py                   # ✅
│   └── pin_memory.py                  # ✅
│
├── adapters/
│   ├── orchestrator.py                # ✏️ 重构
│   ├── claude.py / opencode.py / codex.py  # ✅
│   ├── base.py / registry.py          # ✅
│
├── workspace/
│   └── manager.py                     # ✏️ 扩展 per-RuntimeAgent 工作区
│
├── session/ / rules/ / schemas/ / api/v1/ / app/  # ✅
```

### Backend（Transport Gateway）
```
backend/
├── cmd/server/main.go
├── internal/
│   ├── handler/
│   ├── model/
│   ├── stream/
│   ├── middleware/
│   └── service/
```

### Frontend（Runtime Visualization）
```
frontend/src/
├── pages/
├── components/
│   ├── im/
│   ├── chat/                          # + PlanningCard
│   ├── cards/
│   └── admin/
├── stores/chat.ts                     # ✏️ 扩展 Runtime + Workspace 事件
├── lib/
│   ├── api.ts
│   └── sse.ts
└── main.tsx
```

---

## 十三、风险与建议

### 当前风险

1. **新增模块多** — Profile + Runtime + ExecutionEngine + Scheduler + MergeManager，需控制节奏
2. **事件统一** — 3 种 Adapter 的 normalize 逻辑需确保一致
3. **Workspace 隔离** — per-RuntimeAgent 的 worktree 创建和清理
4. **测试覆盖** — 无自动化测试

### 建议

1. **先定 Profile + Workspace Layout** — 这是基础，其他模块依赖它
2. **先跑通统一事件流** — RuntimeEvent 模型定下来后，Scheduler 和 Engine 自然就位
3. **MergeManager 先做基础版** — merge 成功的路径，冲突处理留 Phase 6
4. **Go 少量改动** — Phase 5 Runtime 逻辑在 AgentEnd 内闭环，但 Go Backend 需：新增 EventType 常量、RunTask 构建 orchestrator config.agents、StreamWriter 扩展 switch case（详见 agentend-group-chat-impl.md 第六节）
5. **Phase 6 并行 + 冲突几乎免费** — ExecutionPlan DAG-ready + Conflict-Resolution Task
6. **预估** — Phase 5 约 5-6 天（含 Profile + Workspace + Merge），Phase 6-7 约 3-5 天

---

## 十四、项目的真正核心

```
execution lifecycle        — 执行生命周期
runtime coordination       — 运行时协调
identity system            — Agent 身份系统（SOUL）
capability system          — Agent 能力系统
workspace isolation        — 工作区隔离（per RuntimeAgent）
branch coordination        — 分支协调
merge management           — 合并管理
event consistency          — 事件一致性
agent collaboration        — Agent 协作
stream durability          — 流持久化
```

这个系统已经是 **Lightweight Agent Operating System**。
