# Phase 5: Orchestrator 群聊协作 — Agent 模式重构

> 目标: Orchestrator 重构为有记忆的 Agent，自主判断闲聊直接回复 vs 任务编排派发，通过 LangGraph 状态图管理完整生命周期。
> 预估: 5-6 天
> 前置: Phase 4 完成 (产物卡片渲染可用)
> 状态: ✅ 核心闭环已完成

## 核心设计变更

### 旧设计（Pipeline 模式）

```
discover → select → load_l2 → plan → write_shared
(线性管道，无记忆，所有请求都走完整规划流程)
```

### 新设计（Agent 模式）— 已实现

Orchestrator 是一个**有记忆的对话 Agent**：
- 闲聊 → LLM 直接回复文本
- 任务 → 调用 `plan_and_dispatch` 工具触发编排
- 每轮对话的历史、工具调用、结果都记入 Memory

```
┌──────────────────────────────────────────────────────┐
│             Orchestrator Agent Graph                  │
│                                                      │
│              ┌──────────────┐                        │
│              │ skill_prepare │ ← 渐进式 Skill 加载    │
│              └──────┬───────┘                        │
│                     │                                │
│              ┌──────┴───────┐                        │
│              │    REASON    │ ← LLM + Memory + Skills│
│              └──────┬───────┘                        │
│                     │                                │
│              输出类型?                                │
│          ┌────────┴────────┐                         │
│          │                 │                         │
│       文本回复          plan_and_dispatch             │
│       (闲聊/直接回答)    (任务编排工具调用)            │
│          │                 │                         │
│          ▼                 ▼                         │
│     ┌─────────┐     ┌──────────┐                    │
│     │  SAVE   │     │ DISPATCH │                    │
│     │  MEM    │     └────┬─────┘                    │
│     └────┬────┘          │                          │
│          │               ▼                          │
│          │        ┌──────────┐                       │
│          │        │ EXECUTE  │ (子图: wave executor) │
│          │        └────┬─────┘                       │
│          │             │                            │
│          │             ▼                            │
│          │       ┌──────────┐                        │
│          │       │  REVIEW  │                        │
│          │       └────┬─────┘                        │
│          │            │                              │
│          │       ┌────┴────┐                         │
│          │    ok │    replan│                        │
│          │       ▼        ▼                          │
│          │ ┌─────────┐ ┌────────┐                    │
│          │ │ EVOLVE  │ │ REASON │ ← 带失败上下文     │
│          │ └────┬────┘ └────────┘                    │
│          │      │                                    │
│          │      ▼                                    │
│          │  ┌─────────┐                              │
│          └─▶│ SAVE MEM│                              │
│             └────┬────┘                              │
│                  │                                   │
│                  ▼                                   │
│                 END                                  │
└──────────────────────────────────────────────────────┘
```

## 交付标准

### AgentEnd 验证

- ✅ 闲聊测试：Orchestrator 直接文本回复，不触发编排
- ✅ 任务编排测试：规划 → 派发 → 执行 → 汇总
- ✅ 上下文引用测试：从 Memory 中引用历史结果

### 前端群聊 UI 验证

- ✅ 选择 Orchestrator Agent
- ✅ 闲聊 → Orchestrator 直接回复
- ✅ 任务编排 → 规划进度 → 多 Agent 执行 → 结果汇总
- ✅ 不同 Agent 用不同颜色标签（Claude: orange / OpenCode: green / Orchestrator: yellow / Codex: indigo）

## 实际实现

### AgentEnd — Orchestrator 模块

```
agentend/src/
├── orchestrator/
│   ├── planning/
│   │   ├── graph.py              # ✅ LangGraph 状态图（skill_prepare → reason → dispatch → review → evolve → save_mem）
│   │   ├── tools.py              # ✅ plan_and_dispatch + 辅助工具
│   │   ├── prompts.py            # ✅ System Prompt + Reasoning 模板
│   │   └── skill_loader.py       # ✅ 渐进式 Skill 发现 + 加载
│   ├── execution/
│   │   ├── engine.py             # ✅ 任务执行引擎
│   │   ├── dispatcher.py         # ✅ 任务派发 + Agent 映射
│   │   ├── wave.py               # ✅ Wave Executor（依赖拓扑排序）
│   │   ├── coordination.py       # ✅ 多 Agent 协调
│   │   └── state.py              # ✅ 执行状态追踪
│   ├── memory/
│   │   ├── evolution.py          # ✅ 规划经验存储
│   │   └── pin_memory.py         # ✅ 用户约束/记忆
│   ├── reporting/
│   │   └── aggregator.py         # ✅ 结果汇总
│   └── models.py                 # ✅ PlanOutput / TaskDef / TaskResult / DispatchResult
├── adapters/
│   └── orchestrator.py           # ✅ OrchestratorAdapter（串联 Graph 全生命周期）
├── workspace/
│   ├── manager.py                # ✅ Workspace 生命周期管理
│   ├── git_ops.py                # ✅ Git 操作（worktree + branch）
│   ├── store.py                  # ✅ Workspace 持久化
│   └── recovery.py               # ✅ 恢复机制
├── skills/
│   └── provisioner.py            # ✅ Skill 分发
├── session/
│   └── manager.py                # ✅ 会话状态机（IDLE → ACTIVE → INTERRUPTED → DESTROYED）
├── rules/
│   ├── engine.py                 # ✅ 规则引擎
│   ├── builtin.py                # ✅ Safety / Scope / Taskctl 规则
│   └── registry.py               # ✅ 规则注册
├── clients/
│   └── backend_client.py         # ✅ Go Backend 通信
├── preview/
│   └── server.py                 # ✅ 预览服务器（aiohttp 静态文件）
└── schemas/
    └── events.py                 # ✅ StreamEvent / EventType
```

### Go Backend 适配

- ✅ SSE 透传新事件类型（planning / dispatch / execute / review / evolve 等）
- ✅ Orchestrator config.agents 构建
- ✅ StreamWriter 支持 Agent 切换创建子消息

### 前端群聊 UI

- ✅ **PlanCard**：任务规划进度展示（任务列表 + Agent 分配 + 状态图标）
- ✅ **CoordChannel**：多 Agent 协调通道（轮次展示 + 结论摘要）
- ✅ **Agent 标签 + 颜色**：Claude: orange / OpenCode: green / Orchestrator: yellow / Codex: indigo
- ✅ **RuntimeStatus**：执行状态实时更新
- ✅ **FinalSummaryCard**：任务完成摘要
- ✅ **TaskFailureCard**：任务失败通知
- ✅ **AskAgentCard**：Agent 间交互

### Graph 节点实现状态

| 节点 | 状态 | 说明 |
|------|------|------|
| `skill_prepare_node` | ✅ | 渐进式 Skill 发现 + L1/L2 加载 |
| `reason_node` | ✅ | LLM tool-calling 循环 + 闲聊/编排路由 |
| `dispatch_node` | ✅ | 任务派发 + Agent 映射 |
| `execute_node` | ✅ | Wave Executor 子图（依赖拓扑排序） |
| `review_node` | ✅ | 结果审查 + 重规划判断 |
| `evolve_node` | ✅ | 规划经验记录 |
| `save_mem_node` | ✅ | Memory 持久化 |

### GraphState

```python
class GraphState(TypedDict):
    # 输入层（不可变）
    message: str
    agents: list[dict]
    task_id: str
    shared_dir: str
    allowed_read_dirs: list[str]

    # REASON 产出
    output_type: str                            # "text" | "plan"
    text: str                                   # 文本回复
    plan: PlanOutput | None                     # 编排计划

    # DISPATCH 产出
    dispatch_results: list[DispatchResult]
    execution_waves: list[list[DispatchResult]] # 拓扑分波

    # EXECUTE 产出（累积）
    task_results: Annotated[list[TaskResult], add]
    task_status: dict[str, str]                 # task_id → RUNNING|DONE|FAILED

    # REVIEW 决策
    needs_replan: bool
    replan_reason: str

    # 聚合
    summary: str

    # 元信息
    iteration: Annotated[int, add_one]          # 防无限重规划
    max_iterations: int                         # 默认 3

    # Memory
    memory_messages: Annotated[list, add]       # 累积对话历史
```

## 验证流程

```bash
# 1. 启动三端
make all

# 2. AgentEnd 单元验证
# 闲聊测试
curl -X POST http://localhost:8001/v1/agent/execute \
  -H "Content-Type: application/json" \
  -d '{ "message": "你好", "agent_type": "orchestrator", ... }'
# 预期: 直接文本回复

# 编排测试
curl -X POST http://localhost:8001/v1/agent/execute \
  -H "Content-Type: application/json" \
  -d '{ "message": "写登录页并审查", "agent_type": "orchestrator", ... }'
# 预期: PLANNING → DISPATCH → EXECUTE → REVIEW → AGGREGATE → DONE

# 3. 全链路验证
# 浏览器打开 → 选 Orchestrator → 测试闲聊 + 任务编排 + 上下文引用
```

## 待优化项（Phase 6+）

| 项目 | 状态 | 说明 |
|------|------|------|
| MemorySaver 持久化 | ⚠️ 内存级 | 当前为进程内存储，重启丢失 |
| Profile System (SOUL) | 🔧 部分实现 | Soul MD 可编辑，但 Profile 目录未完整 |
| Workspace per-RuntimeAgent | ✅ 基本可用 | Git worktree 隔离 |
| MergeManager | 🔧 基础版 | 成功路径可用，冲突处理待完善 |
| Scheduler 并行 | 📋 待实现 | Wave Executor 已预留 DAG 拓扑 |
| Conflict-Resolution Task | 📋 待实现 | 冲突自动 spawn reviewer |
| Retry / Cancellation | 📋 待实现 | 失败重试和取消机制 |
| Durable Resume | 📋 待实现 | 断线恢复 |
