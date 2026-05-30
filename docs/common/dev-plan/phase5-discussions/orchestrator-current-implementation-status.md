# Orchestrator 当前实现状态审计

> 日期: 2026-05-30
> 范围: 对照 `phase5-orchestrator.md` 与 Phase 5 discussions，记录当前 Orchestrator 已满足能力和剩余问题。

## 当前定位

当前 Orchestrator 已从早期线性 Pipeline 进入 **Agent 模式雏形**：

```
用户消息
  ↓
skill_prepare
  ↓
reason（LLM 判断直接回复或 plan_and_dispatch）
  ↓
dispatch（生成 DispatchResult + 写入 shared/.agent）
  ↓
execute（Adapter 接管真实执行）
  ↓
review / evolve / save_mem
```

它现在更接近“协调型 Orchestrator”：

- Orchestrator 自己负责理解需求、拆任务、派发任务、汇总结果。
- Worker Agent 负责在各自 worktree 中读写代码。
- `shared/.agent` 恢复为 Agent 间共享上下文目录。
- `taskctl summary` 可以按真实子 Agent session_id 读取自己的任务。

## 已满足能力

### 1. 闲聊 / 编排双模式

`reason` 节点支持两种输出：

- 普通文本回复：闲聊、简单问答直接由 Orchestrator 返回。
- `plan_and_dispatch` 工具调用：复杂任务进入多 Agent 编排。

这满足 Phase 5 中“Orchestrator 是有记忆的对话 Agent，不是所有请求都走规划管道”的方向。

### 2. Skill 渐进式加载

`skill_prepare` 保留 L1/L2 skill 发现与选择逻辑，并把选中的 skill 指令注入 prompt。这样 Orchestrator 不需要一次性加载所有 skill 内容，符合渐进式披露设计。

### 3. 规划结果恢复写入 shared

dispatch 阶段会将 `PlanOutput` 写入：

```
shared/.agent/
├── config.yaml
└── plans/
    ├── overview.md
    └── task-*.md
```

其中 `config.yaml.tasks[].session_id` 使用真实子 Agent session_id，而不是 LLM 看到的 agent id。这样 `taskctl summary` 可以根据当前 worktree 解析出的 session_id 找到自己的任务。

### 4. Orchestrator 不再拥有代码 worktree

AgentEnd 入口对 `agent_type=orchestrator` 不再自动创建 repo worktree。Orchestrator 规划工具的可读范围也收紧为 `shared_dir`。

这部分修复了 Phase 5 中明确反对的问题：

- Orchestrator 不直接 edit repo。
- 代码读写由 Worker Agent 在独立 worktree 中完成。

### 5. shared_dir 边界校验

Orchestrator 收到 `config.shared_dir` 时，会在有 repo 上下文的情况下校验它必须是当前 task 标准路径：

```
{repo_parent}/worktrees/{task_id}/shared/.agent
```

这降低了 shared_dir 路径注入风险，也避免 Orchestrator 读写错 task 的 shared 目录。

### 6. 子 Agent 独立 worktree 执行

执行阶段仍通过 `ExecutionEngine` 为子 Agent 创建或复用独立 worktree：

```
{repo_parent}/worktrees/{task_id}/{session_id}
```

子 Agent 的代码修改仍被隔离在各自 branch/worktree 中，符合当前 workspace 体系。

### 7. Backend 注入真实编排上下文

Backend 在 `agent_type=orchestrator` 时会构造 config：

- `agents[]`
- `agents[].session_id`
- `task_id`
- `repo_path`
- `shared_dir`

AgentEnd 由此可以完成真实 session 映射、shared 落盘和后续执行。

### 8. 报告时间上下文

Orchestrator 规划工具新增 `current_time()`，可返回当前本地日期、时间和 UTC offset。Aggregator 汇总报告时也会显式注入当前时间，并要求涉及“报告生成时间”“当前日期”等字段时必须使用该值。

这修复了汇总报告中模型自行猜测日期或输出 `[当前日期]` 占位符的问题。

## 仍存在的问题

### P0. Graph 的 execute 仍是 placeholder

当前 `graph.py` 中的 `execute` 节点仍是占位实现，真实执行发生在 `OrchestratorAdapter._handle_execute()`。

后果：

- LangGraph 状态机没有真正掌握执行结果。
- `review` / `evolve` / `save_mem` 看到的状态可能不是最终真实状态。
- 失败重规划链路仍不可靠。

Phase 5 目标是 Execute 子图或 Wave Executor 成为 Graph 的正式节点，而不是 Adapter 外挂执行。

### P0. MemorySaver 不是持久化记忆

当前使用 LangGraph `MemorySaver`，它只适合进程内运行。服务重启后历史会丢失。

Phase 5 目标里的“跨轮次 Memory”还没有达到生产级：

- 没有持久化到文件或 DB。
- 没有按 task/session 做可恢复存储。
- `save_mem` 节点目前没有实际写入逻辑。

### P0. Runtime filesystem layout 尚未落地

Phase 5 discussions 目标结构是：

```
workspaces/{task_id}/
├── orchestrator/
│   ├── runtime.json
│   ├── plan.json
│   └── events/
├── shared/
└── {session_agent_id}/repo/
```

当前实际结构仍是旧 worktree 体系：

```
worktrees/{task_id}/
├── shared/.agent/
└── {session_id}/
```

也就是说：

- 没有 `orchestrator/runtime.json`。
- 没有 `orchestrator/plan.json`。
- 没有 runtime events 日志目录。
- shared 仍是 `.agent` 目录布局，不是完整 SharedContext 对象。

### P1. SharedContext 仍是文件约定，不是模块化能力

当前 shared 写入只是恢复了 `config.yaml + plans/*.md`，还不是 Phase 5 设想里的 SharedContext 管理器。

缺失能力：

- artifacts / reviews / outputs 的统一写入接口。
- shared 文件 schema/version。
- 并发写入锁。
- shared 读写审计。
- shared 清理与生命周期管理。

### P1. 任务依赖信息未进入 plan_and_dispatch

`DispatchResult` 支持 `depends_on`，`topological_sort` 也支持分波执行，但 `TaskDef` 与 `plan_and_dispatch` 当前没有稳定承载依赖字段。

实际效果：

- 多任务通常会被视为无依赖。
- 波次调度能力存在，但 LLM 生成计划时无法可靠表达依赖。

### P1. MergeManager 尚未实现

当前合并仍依赖已有 workspace / git 操作或 taskctl merge。Phase 5 discussions 中的 MergeManager 还未落地：

- 没有 RuntimeAgent branch → target branch 的统一 MergeResult。
- 没有冲突文件结构化返回。
- 没有 merge 事件。
- 冲突后生成新 Runtime Task 的流程未实现。

### P1. Agent identity / profile / adapter 分层仍不完整

Phase 5 目标区分：

- profile：Agent 身份，如 frontend-engineer、reviewer。
- adapter：执行后端，如 claude-code、opencode、codex。
- session_agent_id：短生命周期 runtime worker。

当前实现仍主要依赖：

- `id`
- `type`
- `name`
- `session_id`

这能跑通当前流程，但还不是完整的 SOUL / RuntimeAgent 身份模型。

### P1. Orchestrator 对代码上下文的获取能力变弱但还没有替代机制

为了安全，Orchestrator 不再读取代码 worktree。这符合设计，但也带来新需求：

- 如果 Orchestrator 需要理解代码结构，应通过子 Agent 探查或受控摘要工具获取。
- 目前没有“让 worker 先探查并回写 shared summary”的标准流程。
- 复杂任务的前置分析可能仍依赖 LLM 猜测。

### P2. Contract schema 没有表达 orchestrator config 结构

当前 `AgentRequest.config` 是可扩展 object，足以承载：

- `agents[]`
- `agents[].session_id`
- `repo_path`
- `shared_dir`
- `task_id`

但 schema 没有约束这些字段。短期可接受，长期会导致 Backend / AgentEnd 对 config 结构的理解分散。

### P2. 测试仍偏单元，缺少端到端验证

已有回归测试覆盖：

- shared 规划落盘。
- 真实 session_id 写入 config。
- 工具相对路径从 shared_dir 解析。
- Orchestrator 不创建代码 worktree。
- shared_dir 越界拒绝。

仍缺少：

- Backend → AgentEnd → 子 Agent → taskctl 的端到端测试。
- 前端群聊消息分离验证。
- 失败重规划验证。
- 多任务依赖波次验证。

## 当前可用流程

当前一条编排请求的可用路径是：

```
Backend RunTask
  ↓
构造 AgentRequest.config
  ↓
AgentEnd Orchestrator reason
  ↓
plan_and_dispatch
  ↓
dispatch + shared 落盘
  ↓
ExecutionEngine 创建子 Agent worktree
  ↓
子 Agent 执行任务，可用 taskctl summary 读取 shared
  ↓
Aggregator 汇总结果
  ↓
Orchestrator 返回最终 TEXT / DONE
```

## 下一步建议

### Step 1: 将真实执行移入 Graph

把 `OrchestratorAdapter._handle_execute()` 的真实执行状态接回 Graph，或实现正式 execute 子图。

目标：

- `review` 能看到真实 TaskResult。
- 失败重规划真正可用。
- `evolve` 只记录真实结果。

### Step 2: 建立 Orchestrator runtime 目录

先落一个最小版本：

```
worktrees/{task_id}/orchestrator/
├── runtime.json
├── plan.json
└── events/
```

即使暂不迁移到 `workspaces/` 新结构，也应先把 Orchestrator 自身状态从内存中落下来。

### Step 3: 抽象 SharedContext

把 shared 读写从散落文件操作升级为模块：

```python
SharedContext.write_plan(plan, dispatch_results)
SharedContext.write_output(agent_id, task_id, content)
SharedContext.write_review(agent_id, task_id, review)
SharedContext.read_summary(session_id)
```

### Step 4: 补齐 plan dependency

扩展 `TaskDef` / `plan_and_dispatch`，允许 LLM 明确输出：

```yaml
depends_on:
  - task-001
```

然后让 `topological_sort` 的能力真正进入运行路径。

### Step 5: 明确 config schema

如果 orchestrator config 稳定下来，考虑在 contracts 中新增结构化定义，避免 Backend / AgentEnd 继续靠松散 map 对齐。

## 结论

当前 Orchestrator 已经修复最严重的 shared 断链和工作区边界问题，能支撑基础多 Agent 编排：

- 会判断闲聊或编排。
- 会将任务写入 shared。
- 子 Agent 能通过真实 session_id 读取自己的任务。
- Orchestrator 不再直接读取代码 worktree。

但它仍不是 Phase 5 终态。最大缺口是 Runtime 化不足：真实执行不在 Graph 内、Memory 不持久、Orchestrator runtime 目录未落地、SharedContext 仍是文件约定。下一阶段应优先把执行状态和运行时状态收回 Orchestrator Runtime。 
