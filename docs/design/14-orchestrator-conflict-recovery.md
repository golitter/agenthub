# 14 — Orchestrator 并行集成与冲突自动恢复规划

> **状态**：核心闭环已实施；人工接管动作与跨进程 Resolver 继续执行仍需后续迭代
> **日期**：2026-08-20
> **范围**：AgentEnd、Backend、Frontend、Contracts、OpenSpec、测试与文档
> **核心决策**：保留多 Agent 乐观并行；合并冲突属于可恢复的集成状态，不等同于 Agent 执行失败；由专用 Resolver 在隔离分支中融合并验证双方改动
> **关联文档**：[Agent 路由与 Orchestrator 自动分派](09-agent-routing-and-dispatch.md)、[SSE 流式输出架构](sse-streaming-architecture.md)、[AgentEnd 执行沙盒](13-agentend-execution-sandbox.md)、[AgentEnd Orchestrator 规划](../../agentend/docs/design/11-orchestrator-planning.md)、[AgentEnd 合并冲突处理](../../agentend/docs/design/15-merge-conflict-resolution.md)、[AgentEnd Workspace](../../agentend/docs/design/08-workspace.md)

## 实施快照（2026-08-20）

已落地：真实 Graph `execute`、执行/集成双状态、原子 `IntegrationResult`、task 级串行集成、隔离 Resolver
分支/worktree、Resolver attempt 事件、唯一根终态、`depends_on` 拓扑校验、Backend/Frontend 状态投影，以及
临时 Git 冲突 fixture。自动恢复耗尽会进入 `awaiting_user` 并保持消息流可重连，不发送根 `done`。

尚未完全覆盖：人工“再试一次/采用 source/采用 target/接受部分结果”操作 API、跨进程 ConflictRecord/Resolver
继续执行。这些不应被误解为已完成的自动能力；Resolver 取消传播、二进制/高风险文件转人工和根 Run 的持久化暂停
状态已在当前实现中收口。

## 1. 背景与问题定义

AgentHub 允许 Orchestrator 把一个用户目标拆分给多个 Agent。各 Agent 在独立 Git worktree 与
agent 分支中并行执行，完成后通过 `taskctl merge` 依次合入同一个 `task/{task_id}` 集成分支。
这套模型能够保留并行吞吐，但多个 Agent 修改同一文件或相邻区域时，后合入的分支可能产生正常的
Git 三方合并冲突。

冲突本身不是系统异常，也不应该通过禁止并行来规避。系统真正需要解决的是：

1. 如何可靠识别冲突，而不是从 Agent 的自然语言回复中猜测。
2. 如何保存已完成的 Agent 产物和已经成功集成的内容。
3. 如何在不污染 task 分支的前提下自动解决冲突。
4. 如何让 Orchestrator 在冲突后继续运行，而不是提前发出最终汇总和 `done`。
5. 如何在自动修复失败后有界重试，并向用户提供可操作的人工兜底入口。

### 1.1 现场案例

2026-08-20 的复现场景中，两个 Agent 从相同基线 `bde9f99` 并行修改 `1.md`：

```text
bde9f99  初始内容：# hell
   │
   ├── 8519c55  Alice 在文件末尾追加 Alice 的使用用途
   │      └── 先成功合入 task 分支
   │
   └── 37269da  阿a 在文件末尾追加阿a的使用用途
          └── 后合入时与 8519c55 冲突，taskctl merge --abort
```

两份修改在业务语义上可以同时保留，但由于都从相同基线在同一文件末尾追加，Git 无法自动判断顺序，
因此产生真实冲突。Agent 分支和提交均未丢失，task 分支也被 `merge --abort` 正确恢复；错误发生在上层
编排：系统把“Agent 已完成、尚未集成”压缩成普通失败，随后输出人工处理建议并结束，没有进入自动修复。

### 1.2 当前实现的主要缺陷

| 编号 | 缺陷 | 当前表现 | 影响 |
|---|---|---|---|
| P-01 | Graph 与真实执行双状态源 | LangGraph `execute` 是空占位，真实 `_handle_execute` 在 Graph 外运行 | Graph 可基于空结果提前进入 review/evolve/save_mem |
| P-02 | 冲突从自然语言识别 | 搜索“冲突文件”并解析后续文本 | 易误判、漏判、提取错误文件名，无法证明 taskctl 是否真正执行 |
| P-03 | 执行状态与集成状态混合 | `TaskResult.success=False` 同时表示命令失败、超时和 merge conflict | 已完成产物被错误表现为执行失败，恢复策略无法区分 |
| P-04 | Replan 过于泛化 | 把失败文本重新交给 LLM 自由规划 | 可能重复执行已成功任务、丢失失败分支引用或再次制造相同冲突 |
| P-05 | 中间轮次产生终态语义 | 每轮 Aggregator 都输出 `final_summary`，Graph 多节点可发 `done` | UI 看起来已经结束，SSE 可能提前关闭 |
| P-06 | 重试计数不一致 | Adapter 硬编码 3，`review_node` 返回固定 `iteration=1` | 配置不生效，无法可靠限制或审计重试 |
| P-07 | 依赖波次能力名存实亡 | `TaskDef` 没有 `depends_on`，Dispatcher 强制设为空 | 所有任务被当作同一 wave，规格中的依赖执行无法表达 |
| P-08 | 规格和实现漂移 | OpenSpec 描述真实 EXECUTE 节点、MemorySaver 和 REVIEW 回路，代码不一致 | 后续维护和测试基于错误架构认知 |
| P-09 | 失败类型跨端过度简化 | merge conflict 在展示层落成 generic error | 前端无法呈现“修复中”、重试次数和人工接管动作 |

## 2. 目标与非目标

### 2.1 目标

- 保留同一 wave 内多个 Agent 的并行执行能力。
- 对同一 task 分支的集成操作继续串行化，避免并发写 Git refs。
- 将“工作完成”和“工作已集成”建模为两个独立维度。
- `taskctl merge` 为每次调用产生可校验、可关联、机器可读的结果。
- 冲突后自动创建隔离 Resolver 分支和 worktree，保留并融合双方意图。
- 已成功合入的任务不因其他任务冲突而重复执行。
- 自动恢复有明确的尝试次数、分支谱系、错误码和终止条件。
- 中间冲突与修复轮次只产生进度事件；根任务只产生一个最终汇总和一个 `done`。
- SSE 断线重连后能恢复当前 `executing / integrating / resolving / awaiting_user` 状态。
- 规格、代码、契约、设计文档与自动化测试描述同一套生命周期。

### 2.2 非目标

- 不要求在初次规划时禁止多个 Agent 修改同一文件。
- 不追求所有冲突都自动解决；语义不明确或高风险修改必须允许转人工。
- 不允许 Resolver 直接修改主仓库默认分支。
- 不在冲突现场直接修改 task-base worktree，避免把共享集成分支留在未合并状态。
- 不通过简单采用 `ours` 或 `theirs` 自动覆盖一方内容。
- 本阶段不实现通用 CRDT、语义补丁系统或跨仓库分布式事务。
- 本阶段不自动推送远端，也不改变 task 分支合入默认分支时现有的人工审查要求。

## 3. 核心设计原则

### 3.1 乐观并行，串行集成

并行执行与串行集成是两个独立阶段：

```text
执行阶段：Agent A ─┐
                   ├── 并行，互不阻塞
          Agent B ─┘

集成阶段：merge A → task  ── merge B → task
                   串行，由 task 级锁保护
```

并行任务可以基于相同 task 基线开始。某个 Agent 完成后允许立即尝试合入，不必等待整个 wave 全部完成。
如果后续合入发生冲突，已成功进入 task 分支的提交保持不变，失败分支作为 Resolver 的输入继续处理。

### 3.2 冲突是可恢复状态，不是产物失败

Agent 完成代码修改、验证并提交后，即使合入冲突，其执行仍然成功：

```text
execution_status   = completed
integration_status = conflict
```

只有 Agent CLI 错误、执行超时、验证失败或取消才属于执行失败。这个区分决定后续动作：

| 执行状态 | 集成状态 | 后续动作 |
|---|---|---|
| completed | merged | 完成，不再派发 |
| completed | conflict | 创建 Resolver，不重跑原始任务 |
| completed | integration_failed | 根据错误码重试集成或转人工 |
| failed / timeout | pending | 重试原始任务或重新规划执行 |
| cancelled | pending | 随根 Run 终止，不自动恢复 |

### 3.3 Git 事实优先于模型自述

状态必须来自 `taskctl`、Git 与 Run 生命周期的结构化结果。Agent 的文本回复只用于向用户解释，不得作为
`success`、冲突文件、源分支或目标提交的权威来源。

### 3.4 恢复必须保留意图和谱系

每次 Resolver 都必须知道：

- 原始用户目标和原始子任务。
- 已成功集成的任务及其提交。
- 发生冲突的源分支、源提交、目标分支、目标提交和 merge base。
- 冲突文件与 Git 诊断信息。
- 上一次修复尝试的结果。

系统不得只向 Resolver 提供“merge conflict: 1.md”这样的字符串。

### 3.5 单一生命周期所有者

LangGraph 或 Adapter 只能有一个组件拥有状态推进权。本规划选择 **LangGraph 为唯一生命周期所有者**：

- Graph 节点执行真实操作并返回真实状态。
- Adapter 只负责准备上下文、启动 Graph、把 Graph 事件转换为 `StreamEvent`。
- Aggregator、EvolutionStore 和 ConversationMemory 只消费 Graph 已确认的权威终态。

## 4. 目标生命周期

```text
REASON
  │
  ▼
DISPATCH
  │
  ▼
EXECUTE  ── Agent 在独立 worktree 并行执行
  │
  ▼
INTEGRATE ── 按 task 锁串行合入
  │
  ├── 全部 merged ─────────────────────────┐
  │                                        │
  ├── execution failed ── RETRY/REPLAN ────┤
  │                                        │
  └── merge conflict ── PREPARE_RESOLVER   │
                           │                │
                           ▼                │
                        RESOLVE             │
                           │                │
                           ▼                │
                     VERIFY_RESOLUTION      │
                           │                │
                           ├── success ─────┘
                           ├── retry → PREPARE_RESOLVER
                           └── exhausted → AWAIT_USER
                                            │
                                            ▼
                                         REVIEW
                                            │
                                            ▼
                                      FINAL_AGGREGATE
                                            │
                                            ▼
                                      EVOLVE → SAVE_MEM
                                            │
                                            ▼
                                        ONE DONE
```

### 4.1 终态定义

根 Orchestrator Run 只能进入以下终态之一：

| 终态 | 条件 |
|---|---|
| `completed` | 所有必需任务执行完成且产物均已集成，必要验证通过 |
| `partial` | 自动恢复耗尽，部分产物已集成，用户选择接受部分结果 |
| `awaiting_user` | 自动恢复耗尽或语义风险过高，等待人工决策；不是 DONE |
| `failed` | 不可恢复的控制面、Git 元数据或执行错误 |
| `cancelled` | 用户取消或父 Run 取消，所有子 Run 和 Resolver Run 已传播取消 |

`awaiting_user` 是持久化暂停状态。只有用户选择继续、接受部分结果或取消后，根 Run 才进入下一状态。

## 5. 数据模型规划

### 5.1 TaskResult 拆分状态

建议替换单一 `success` 语义，保留只读兼容派生字段：

```python
class TaskResult(BaseModel):
    task_id: str
    root_task_id: str
    agent: str
    attempt: int = 0

    execution_status: Literal[
        "pending", "running", "completed", "failed", "timeout", "cancelled"
    ]
    integration_status: Literal[
        "pending", "merged", "conflict", "failed", "not_required"
    ]

    content: str
    message_id: str
    duration: float

    source_branch: str
    source_commit: str
    target_branch: str
    target_commit: str
    merge_base: str

    error_code: str
    error_message: str
    conflict_files: list[str]

    @property
    def success(self) -> bool:
        return self.execution_status == "completed" and self.integration_status in {
            "merged", "not_required"
        }
```

迁移期内可继续生成 `success` 字段供现有前端和 Aggregator 使用，但内部路由不得再只判断这个布尔值。

### 5.2 IntegrationResult

每次 `taskctl merge` 生成独立结果：

```json
{
  "version": 1,
  "run_id": "child-run-id",
  "task_id": "task-id",
  "session_id": "agent-session-id",
  "attempt": 0,
  "status": "conflict",
  "source_branch": "agent/session-id/task-id",
  "source_commit": "37269da...",
  "target_branch": "task/task-id",
  "target_commit": "8519c55...",
  "merge_base": "bde9f99...",
  "conflict_files": ["1.md"],
  "aborted": true,
  "error_code": "merge_conflict",
  "error_message": "git merge exited with status 1",
  "started_at": "2026-08-20T17:12:00+08:00",
  "finished_at": "2026-08-20T17:12:01+08:00"
}
```

结果文件建议写入：

```text
<shared>/.agent/integration-results/<run_id>.json
```

写入必须使用 staging 文件加原子 rename；读取时校验 `run_id`、task、session、分支和时间，防止误读上一次
Run 的陈旧结果。

### 5.3 ConflictRecord

Orchestrator 为每个未解决冲突维护持久化记录：

```python
class ConflictRecord(BaseModel):
    conflict_id: str
    root_run_id: str
    task_id: str
    failed_task_id: str
    failed_agent: str
    attempt: int

    source_branch: str
    source_commit: str
    target_branch: str
    target_commit: str
    merge_base: str
    conflict_files: list[str]

    resolver_agent: str
    resolver_branch: str
    resolver_session_id: str
    resolver_run_id: str

    status: Literal[
        "detected", "preparing", "resolving", "verifying",
        "resolved", "retryable", "awaiting_user", "cancelled"
    ]
    last_error_code: str
    last_error_message: str
```

该记录用于重启恢复、SSE 重连、重试去重和人工接管。

### 5.4 任务依赖

`TaskDef` 增加：

```python
depends_on: list[str] = Field(default_factory=list)
```

Dispatcher 必须原样传递并校验：

- 依赖 ID 必须存在。
- 不能依赖自身。
- 依赖图出现环时返回 `invalid_plan_cycle`，不得把环中任务塞进同一 wave。
- 上游执行失败时，下游标记 `blocked`；上游仅发生集成冲突时，下游是否可执行由任务的
  `requires_integrated_dependencies` 决定，第一版默认 `true`。

## 6. 结构化 taskctl 结果

### 6.1 调用关联

Backend 创建 child Run 时已经拥有 `run_id / root_run_id / parent_run_id`。AgentEnd 应把当前 `run_id`
通过受控环境变量注入 Agent worktree：

```text
AGENTHUB_RUN_ID
AGENTHUB_ROOT_RUN_ID
AGENTHUB_PARENT_RUN_ID
```

`taskctl merge` 使用 `AGENTHUB_RUN_ID` 命名结果文件，不新增可改变目标分支的命令参数，继续满足
“目标只能由 worktree 路径推导”的安全约束。

### 6.2 结果分类

| error_code | 含义 | 默认处理 |
|---|---|---|
| 空 | 合入成功 | 标记 `merged` |
| `merge_conflict` | Git 返回未合并文件，已 abort | 创建 Resolver |
| `dirty_target` | task-base 在合入前不干净 | 控制面错误，禁止 Resolver，先修复 workspace |
| `source_missing` | 源分支或提交不存在 | 不可恢复失败或人工检查 |
| `target_moved` | 结果写入后目标提交已变化 | 重新计算并尝试集成，不复用旧结论 |
| `commit_failed` | Agent 未提交改动且自动提交失败 | 作为执行/集成错误重试原 Agent |
| `merge_aborted_failed` | 冲突后未能安全 abort | P0 控制面错误，隔离 task worktree并停止自动操作 |
| `integration_missing` | Agent 回复结束但没有对应结果 | 提醒 Agent 执行 taskctl；有界重试后失败 |
| `result_invalid` | 结果 schema 或关联校验失败 | 拒绝采用并记录安全审计 |

### 6.3 兼容迁移

第一阶段保留 `_detect_reported_merge_conflict()` 作为兼容兜底，但必须：

1. 优先读取结构化结果。
2. 文本检测命中时记录 `legacy_conflict_detection=true`。
3. 文本检测不得伪造 source/target commit；缺失事实必须显式为空。
4. 所有内置 taskctl 分发稳定后删除文本检测。

## 7. Resolver 设计

### 7.1 为什么需要专用 Resolver

通用 replan 适合处理执行失败、Agent 不可用或任务拆分错误；Git 冲突已经拥有明确的双方提交和冲突
文件，更适合走确定性恢复。重新执行原始任务可能：

- 基于旧基线再次生成相同冲突。
- 重复已经成功合入的工作。
- 覆盖另一 Agent 的内容。
- 失去原失败提交与用户意图之间的可审计关联。

因此 `merge_conflict` 默认进入 Resolver，不先进入自由 replan。

### 7.2 Resolver 分支和 worktree

分支命名：

```text
resolve/<task-id>/<conflict-id>/<attempt>
```

准备流程：

1. 获取 task 级 merge 锁。
2. 读取并校验当前 `task/{task_id}` HEAD。
3. 从最新 task HEAD 创建 resolver 分支和独立 worktree。
4. 在 resolver worktree 执行 `git merge --no-commit --no-ff <source_branch>`。
5. 如果 Git 自动合并成功，直接验证、提交并合入 task，无需调用 LLM。
6. 如果发生冲突，保留 resolver worktree 的冲突状态，释放 task 级锁。
7. 启动 Resolver Agent，只允许在 resolver worktree 和当前 task shared 目录内工作。

冲突绝不能留在 task-base worktree；task 分支在整个修复期间保持可读、可继续审查的干净状态。

### 7.3 Resolver 输入

Resolver prompt 必须由结构化模板构建，至少包含：

```text
原始用户目标
原始失败子任务及 Agent 回复摘要
已成功集成任务及提交列表
source_branch / source_commit
target_branch / target_commit
merge_base
conflict_files
每个冲突文件的 base / ours / theirs diff
必须保留双方业务意图的约束
验证命令与验收条件
禁止修改无关文件、禁止丢弃一方内容、禁止直接操作 task/main 的约束
```

对于文本、配置和普通代码文件，可由 Resolver 自动处理。以下情况默认直接转人工：

- 二进制文件冲突。
- lockfile 与依赖清单同时发生大范围不一致且无法重新生成。
- 数据库迁移版本号冲突涉及已发布迁移。
- 安全策略、权限或生产配置冲突无法从需求判断优先级。
- Resolver 需要删除一方大部分代码才能通过验证。

### 7.4 Resolver Agent 选择

默认选择顺序：

1. 第一次尝试使用发生冲突的原 Agent，最大化其对自身修改的理解。
2. 第二次尝试使用不同的可用 Agent作为独立 Resolver。
3. 后续不再无界换 Agent，达到配置上限后进入 `awaiting_user`。

若原 Agent 已离线、取消或不可用，直接选择其他可用 Agent。Agent 选择结果写入 `ConflictRecord`。

### 7.5 验证与重新集成

Resolver 完成后必须：

1. 确认不存在 Git 未合并项和冲突标记。
2. 确认 source 与 target 两侧的预期变更均存在。
3. 运行规划提供的验证命令；若无命令，至少执行仓库级轻量检查。
4. 提交 resolver 分支，提交信息包含 `conflict_id` 和双方 commit 短 SHA。
5. 重新获取 task merge 锁。
6. 检查 task HEAD 是否仍等于 Resolver 创建时的目标提交。
7. 若未移动，合入 resolver 分支。
8. 若已移动，先在 resolver 分支重新合入最新 task HEAD；发生新冲突则创建下一 attempt。
9. 成功后标记原任务 `integration_status=merged`、ConflictRecord `resolved`。

### 7.6 重试与去重

默认配置建议：

```yaml
orchestrator:
  execution_retry_max_attempts: 2
  conflict_resolver_max_attempts: 2
  replan_max_iterations: 3
```

三个计数器语义分离：

- `execution_retry_max_attempts`：Agent CLI 或验证执行失败。
- `conflict_resolver_max_attempts`：同一个 ConflictRecord 的自动融合次数。
- `replan_max_iterations`：任务计划本身无法执行时重新拆解的次数。

相同 `(source_commit, target_commit, conflict_files, resolver_agent)` 的失败结果不得重复执行；必须改变
Resolver Agent、目标提交或修复策略，否则直接视为重复失败。

## 8. LangGraph 与流式事件改造

### 8.1 真实 Execute Node

删除 `_execute_placeholder`。真实节点职责：

```text
execute_node
  ├── 按 wave 启动 ExecutionEngine
  ├── 并行收集 TaskResult
  ├── 通过 custom stream writer 发布 runtime_* 事件
  ├── 等待所有允许执行的任务结束
  └── 返回权威 task_results
```

Graph state 不再由 Adapter 在节点返回后就地篡改。Graph 的 `review_node`、`evolve_node` 与
`save_mem_node` 必须看到同一份真实结果。

### 8.2 建议节点

```text
skill_prepare
reason
human_review
dispatch
execute
integrate
classify_failures
prepare_resolver
resolve
verify_resolution
review
final_aggregate
evolve
save_mem
```

`classify_failures` 将结果分成：

- `execution_retryable`
- `execution_terminal`
- `integration_conflicts`
- `integration_retryable`
- `completed`

只有 `execution_retryable` 或规划本身失效才进入通用 replan；`integration_conflicts` 进入 Resolver。

### 8.3 唯一终态事件

事件约束：

- `evolve` 不发送 `done`。
- `save_mem` 不发送 `done`。
- `final_aggregate` 只生成最终内容，不关闭流。
- Graph 正常到达 END 后由 Adapter 发送唯一 `done`。
- `awaiting_user` 发送结构化暂停事件并保持根 Run 可恢复，不发送 `done`。
- fatal error 和 cancel 各发送一个结构化终态事件，然后关闭流。

### 8.4 SSE 事件规划

建议在契约中增加或扩展以下事件：

| 事件 | 用途 |
|---|---|
| `integration_started` | 某 Agent 产物开始合入 task |
| `integration_completed` | 合入成功 |
| `integration_conflict` | 结构化冲突事实与冲突文件 |
| `resolution_started` | Resolver attempt 启动 |
| `resolution_progress` | Resolver 修复、验证或重新集成进度 |
| `resolution_completed` | 冲突已解决并合入 |
| `resolution_failed` | 本次 attempt 失败，注明是否还会重试 |
| `orchestrator_paused` | 自动恢复耗尽，等待人工决策 |

若不希望一次增加过多 EventType，可先复用 `planning` 事件并增加稳定的 `node/status` 枚举；但最终应避免
依赖任意字符串。所有跨端字段以 `contracts/schemas/` 为单一来源。

## 9. 汇总、UI 与人工接管

### 9.1 区分中间汇总和最终汇总

现有 `final_summary` 只允许在根任务终止时生成。中间轮次使用：

```text
iteration_summary  本轮执行和集成情况
repair_status      当前 ConflictRecord 和 Resolver attempt
final_summary      根任务最终结果
```

冲突发生时的推荐文案：

> Alice 的修改已成功合入。阿a 已完成内容并提交，但与当前 task 分支存在冲突；双方提交均已保留，
> 系统正在创建隔离修复任务。

不得在自动恢复尚未耗尽时显示“请人工重试”。

### 9.2 前端状态

任务级 UI 增加：

```text
执行中 → 集成中 → 解决冲突中 → 验证中 → 已完成
                               └→ 等待人工处理
```

Agent 子任务卡片分别展示：

- 执行：完成 / 失败 / 超时 / 已取消。
- 集成：待合入 / 已合入 / 冲突修复中 / 等待人工。
- attempt、Resolver Agent、冲突文件和最近错误。

### 9.3 人工操作

进入 `awaiting_user` 后提供以下动作：

| 动作 | 行为 |
|---|---|
| 再试一次 | 创建新的 resolver attempt，可选择 Agent |
| 查看冲突 | 展示 base/ours/theirs、冲突文件和两边提交 |
| 接受当前 task | 放弃未集成分支，根任务以 `partial` 结束，需明确确认 |
| 采用 source | 高风险覆盖动作，只能在 diff 审查后确认 |
| 采用 target | 明确放弃失败 Agent 产物，只能在 diff 审查后确认 |
| 取消任务 | 取消根 Run、子 Run 与 Resolver Run，保留分支供审计 |

“采用 source/target”不能作为默认自动策略。

## 10. Backend 与持久化改造

Backend 需要：

1. 持久化根 Run、child Run、resolver Run 的父子关系。
2. 把 integration/resolution 事件写入 EventJournal，支持 `after_seq` 重放。
3. 根消息在中间 Agent `done` 或 iteration summary 后继续保持 `streaming`。
4. 只有根 Orchestrator Graph 终止时把根消息设为 `completed / failed / cancelled`。
5. `awaiting_user` 时把 session 标记为相应暂停态，并允许重连，不关闭生命周期。
6. 用户恢复操作必须校验 task、session、root run、conflict ID 和当前 attempt。
7. 父 Run 取消必须传播到正在运行的 Resolver Agent，并清理未完成 resolver worktree。

需要评估 session-state 契约是否增加 `resolving` 与 `awaiting_resolution`。如果不新增 session 状态，至少在
Run 状态和 runtime block 中持久化，避免仅存在于前端内存。

## 11. 配置规划

建议增加：

```yaml
orchestrator:
  execution_retry_max_attempts: 2
  conflict_resolver_enabled: true
  conflict_resolver_max_attempts: 2
  conflict_resolver_timeout: 600
  conflict_auto_resolve_text: true
  conflict_auto_resolve_binary: false
```

现有：

```yaml
orchestrator:
  replan_max_iterations: 3
```

必须由 Adapter/Graph 实际读取，不允许存在另一个硬编码上限。Config Center 的 example/actual 双栏、
配置 schema、启动校验和文档需要同步。

## 12. OpenSpec 与文档同步

建议创建一个独立 OpenSpec change，例如：

```text
orchestrator-conflict-auto-recovery
```

至少更新或新增能力：

- `lifecycle-graph`：真实 EXECUTE、INTEGRATE、RESOLVE、唯一 DONE。
- `wave-executor`：真实 depends_on、失败传播、并行执行和串行集成。
- `taskctl-merge`：结构化 IntegrationResult、Run 关联、abort 安全保证。
- `workspace-management`：resolver 分支/worktree 生命周期。
- `agent-run`：resolver Run 父子关系、取消和恢复。
- `orchestrator-presentation`：中间状态与最终状态语义。

现有规格中关于 MemorySaver、REVIEW 路由和真实执行节点的描述必须与最终设计统一，不能只追加新条目而
保留互相矛盾的旧要求。

## 13. 实施阶段

### Phase 0：规格与回归基线

- [ ] 为本次 `1.md` 双 Agent 追加场景建立可重复的 Git fixture。
- [ ] 固化当前失败行为：真实冲突、abort 成功、未发生 replan、提前终态。
- [ ] 创建 OpenSpec change，明确状态模型和事件契约。
- [ ] 确认兼容策略与数据库迁移范围。

**退出条件**：测试可以稳定重现问题，规格评审通过。

### Phase 1：统一 Orchestrator 生命周期

- [ ] 把真实执行迁入 LangGraph execute node。
- [ ] 删除 `_execute_placeholder` 和 Adapter 外部双状态推进。
- [ ] 增加唯一 Graph state、真实 review 输入和统一 iteration 计数。
- [ ] 移除 evolve/save_mem 的 DONE，保证根任务唯一 DONE。
- [ ] 使用配置中的 replan 上限。
- [ ] 增加 Graph 级失败、取消和断线恢复测试。

**退出条件**：真实 TaskResult 必须先进入 REVIEW；任何中间节点都不能终结根流。

### Phase 2：结构化集成结果

- [ ] 定义 IntegrationResult schema 与错误码。
- [ ] 注入 Run ID，并让 taskctl 原子写结果文件。
- [ ] ExecutionEngine 读取、校验并消费结果。
- [ ] 拆分 execution/integration 状态。
- [ ] 保留文本检测兼容路径并增加弃用日志。
- [ ] 更新 contracts、生成代码和契约日志。

**退出条件**：冲突判断不再依赖 Agent 回复中的关键词；结果可关联到唯一 child Run。

### Phase 3：Resolver 自动恢复

- [ ] 实现 resolver 分支和隔离 worktree 创建、登记与清理。
- [ ] 在 resolver worktree 准备冲突现场。
- [ ] 实现结构化 Resolver prompt 和 Agent 选择策略。
- [ ] 实现验证、重新集成、target moved 检查和 attempt 去重。
- [ ] 实现重试耗尽后的 `awaiting_user`。
- [ ] 将父 Run 取消传播到 Resolver Run。

**退出条件**：`1.md` fixture 能自动保留 Alice 与阿a两段内容并成功合入 task 分支。

### Phase 4：跨端状态与人工接管

- [ ] 增加 integration/resolution 事件或稳定 planning 子状态。
- [ ] Backend EventJournal 和消息状态支持 repairing/paused 生命周期。
- [ ] Frontend 展示执行状态与集成状态。
- [ ] 增加查看冲突、重试、接受部分结果和取消入口。
- [ ] final_summary 只在真正终止时生成。
- [ ] SSE 重连可恢复 Resolver 进度。

**退出条件**：用户可以区分“Agent 没干完”和“已经干完但正在解决冲突”，刷新页面不丢状态。

### Phase 5：依赖波次与规格收敛

- [ ] 为 TaskDef 增加 depends_on 并完成契约生成。
- [ ] Dispatcher 传递依赖，拓扑排序对环失败关闭。
- [ ] 定义上游失败、冲突和取消对下游任务的传播规则。
- [ ] 更新 OpenSpec、AgentEnd 设计文档、测试手册和根文档索引。
- [ ] 删除 legacy 文本冲突检测。

**退出条件**：OpenSpec、设计文档、代码和测试对生命周期、依赖与冲突恢复没有矛盾描述。

## 14. 测试与验收矩阵

### 14.1 taskctl 与 Git

- [ ] 无改动时跳过自动提交并成功合入。
- [ ] 有改动时自动提交并成功合入。
- [ ] 冲突时准确返回 source/target/base、冲突文件和 `aborted=true`。
- [ ] abort 后 task-base 干净、task HEAD 不变、Agent 分支提交保留。
- [ ] 结果文件写入中断时不会被读取为完整 JSON。
- [ ] 陈旧、伪造或 run_id 不匹配的结果被拒绝。
- [ ] target moved 时旧结果不会被错误复用。

### 14.2 Orchestrator Graph

- [ ] 同一 wave 的 Agent 真正并行执行。
- [ ] task 分支 merge 使用 task 级串行锁。
- [ ] REVIEW 读取真实结果，不会先记录空成功。
- [ ] 执行失败进入 retry/replan，merge conflict 进入 Resolver。
- [ ] 已 merged 任务不会在 Resolver 轮次重复执行。
- [ ] iteration/attempt 分别递增并遵循配置。
- [ ] 重试耗尽进入 awaiting_user，不发送 DONE。
- [ ] 最终成功、失败或取消均只发送一个 DONE/终止事件。

### 14.3 Resolver

- [ ] 两个 Agent 从相同基线在同一文件 EOF 追加不同内容，自动保留双方内容。
- [ ] 两个 Agent 修改同一函数不同语义，Resolver 按验收要求融合并通过测试。
- [ ] Resolver 分支自动合并成功时不调用 LLM。
- [ ] 二进制冲突直接转人工。
- [ ] Resolver 修改无关文件超过策略范围时验证失败。
- [ ] Resolver 运行时 task HEAD 移动，系统重新计算而不是覆盖新提交。
- [ ] Resolver Agent 失败后可换 Agent 重试，且不会重复完全相同尝试。

### 14.4 Backend 与 Frontend

- [ ] child Agent DONE 不关闭根 Orchestrator SSE。
- [ ] iteration summary 不将根消息标记 completed。
- [ ] UI 展示 `completed + conflict` 为“内容完成，冲突修复中”。
- [ ] 页面刷新后从 EventJournal 恢复 resolver attempt。
- [ ] awaiting_user 可以查看双方 diff 并执行明确操作。
- [ ] 接受部分结果需要二次确认并记录被放弃的分支/提交。
- [ ] 根 Run 取消后所有 child/resolver Run 终止且 UI 收到统一取消状态。

### 14.5 固定端到端验收场景

输入：

```text
让两个 Agent 分别把自己的使用用途追加到项目根目录 1.md。
```

期望：

1. 两个 Agent 并行启动。
2. 两个 Agent 都基于 `# hell` 完成各自提交。
3. 第一个 Agent 合入成功。
4. 第二个 Agent 合入产生真实冲突并安全 abort。
5. UI 显示“内容完成，正在解决集成冲突”，根流保持连接。
6. 系统创建 Resolver 分支，保留两边提交与 conflict record。
7. Resolver 输出包含两个 Agent 的独立章节。
8. 验证通过后 resolver 分支合入 task。
9. 最终 task 分支中的 `1.md` 同时包含 Alice 与阿a内容。
10. 已成功 Agent 不被重复派发。
11. 全过程只有一个 `final_summary` 和一个根 `done`。

## 15. 可观测性与审计

每个冲突和 attempt 至少记录：

```text
root_run_id
parent_run_id
child_run_id
conflict_id
task_id / failed_task_id
source_branch / source_commit
target_branch / target_commit
merge_base
conflict_files
resolver_agent / resolver_run_id / resolver_branch
attempt
status / error_code
duration
validation_commands / validation_result
```

建议指标：

- `orchestrator_integration_total{status}`
- `orchestrator_merge_conflicts_total`
- `orchestrator_conflicts_resolved_total{resolver}`
- `orchestrator_conflicts_escalated_total{reason}`
- `orchestrator_resolution_attempts`
- `orchestrator_resolution_duration_seconds`
- `orchestrator_premature_done_total`
- `orchestrator_legacy_conflict_detection_total`

Langfuse trace 中根 Orchestrator、child Agent 与 Resolver Agent 必须通过 root/parent Run ID 关联，且每个
attempt 有独立 span，避免把多次修复折叠成一次不可解释的 LLM 调用。

## 16. 风险与缓解

| 风险 | 缓解措施 |
|---|---|
| Resolver 语义上错误融合 | 结构化输入、双方意图约束、验证命令、高风险文件转人工 |
| task HEAD 在修复期间变化 | 合入前校验 target commit；变化时在 resolver 分支重新集成 |
| 自动重试循环 | 分离计数器、attempt 去重、固定上限、相同输入禁止原样重试 |
| Resolver 污染 task 分支 | 所有冲突只存在于 resolver worktree；task-base 始终失败关闭 |
| 结构化结果文件被伪造或陈旧 | run_id、task、session、路径和时间校验；原子写入；授权目录限制 |
| 兼容迁移期间双重判断不一致 | 结构化结果绝对优先，文本兜底只记录告警，不覆盖结构化事实 |
| 状态字段扩展影响三端 | 契约优先、派生 legacy success、分阶段迁移、生成代码不可手改 |
| awaiting_user 长期占资源 | Resolver 进程结束后释放计算资源，仅保留分支、记录和可恢复状态 |

## 17. 完成定义

本规划只有同时满足以下条件才算完成：

- 多 Agent 同文件修改仍可并行执行。
- 合并冲突通过结构化 Git 结果识别。
- Agent 执行状态与集成状态已拆分。
- LangGraph 拥有唯一且真实的执行状态，不再存在 placeholder 双状态源。
- 冲突在隔离 Resolver worktree 中自动处理，task 分支持续保持干净。
- 已成功合入任务不会被重复执行。
- 自动恢复失败后进入可恢复的人工审查，而不是静默结束。
- 根任务只有一个最终汇总和一个 DONE。
- 本次 `1.md` 现场用例成为端到端自动化回归测试并通过。
- Contracts、OpenSpec、三端生成类型、设计文档和测试手册全部同步。
