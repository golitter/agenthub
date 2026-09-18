# 15 — Coding Agent 自动化评测体系实施规划

> **状态**：Phase 0～4 已实施；真实批量运行仍须部署环境通过 strict sandbox readiness
> **日期**：2026-09-16
> **范围**：AgentEnd、Backend、Langfuse、评测数据集、执行隔离、测试与文档
> **核心决策**：平台运行终态与代码任务正确性分离；由 Agent 不可见的确定性 Grader 对 Git 产物和隐藏测试进行最终裁决，Langfuse 用于链路分析而不是作为唯一事实源
> **前置约束**：批量 Eval 必须运行在严格执行沙盒中；当前 `unsafe_process` 仅允许本地开发和单次人工验证，不允许直接承载批量评测
> **关联文档**：[AgentEnd 执行沙盒](13-agentend-execution-sandbox.md)、[Orchestrator 冲突恢复](14-orchestrator-conflict-recovery.md)、[Agent 路由与自动分派](09-agent-routing-and-dispatch.md)、[AgentEnd Run 生命周期](../../agentend/docs/design/22-run-lifecycle-and-sandbox.md)、[Langfuse Trace](../../agentend/docs/design/18-langfuse-trace.md)、[测试与维护地图](../implementation-details/12-testing-and-maintenance.md)

## 实现了什么

本文档定义并实现 Coding Agent 自动化评测闭环：版本化 Dataset/Case、可复现 Experiment/Trial、不可见的隐藏验收资产、确定性 Grader、可审计的结果存储与配对统计报告。实现位于 `agentend/evals/`，并通过 AgentEnd Eval API、Backend 管理代理和 Frontend 评测页提供查询、对比与人工审查。默认 `unsafe_process` 环境继续失败关闭；只有部署好 Bubblewrap、资源限制、短期凭据目录和受控网络命名空间后，真实无人值守批量命令才会开放。

## 怎么实现的

先冻结数据模型、评分口径和资产摘要，再以离线 Fixture 与 Fake Runner 实现确定性评分；真实 Coding CLI 批量执行必须等到 strict ExecutionSandbox 实际强制并通过 readiness 后才开启。以下章节给出领域模型、执行边界、存储结构、指标公式和分阶段验收标准。

## 1. 背景

AgentHub 已经具备 Coding Agent 评测所需的大部分运行基础：

- 四类 Coding CLI 统一适配，能够产生 `text / tool_call / tool_result / done / error` 事件。
- `RunSupervisor`、SQLite RunRepository 和 EventJournal 提供持久化 Run、父子关系、预算、取消与终态。
- Orchestrator 能够拆分任务、按依赖波次并行执行、失败重试或重规划，并记录 `TaskResult.duration` 与 `attempt`。
- Git Worktree 隔离每个 Agent 的修改，`taskctl` 与 IntegrationService 保存结构化 Git 集成事实。
- Langfuse 能够追踪编排节点、CLI 输出、工具调用和 Token Usage。
- 项目已有 pytest、Go test、Vitest 和 Git 集成测试，可复用为评测任务素材。

但现有状态不能直接作为 Agent 效果指标：

1. `Run.state=completed` 仅表示 Runner 正常返回，不表示代码满足用户要求。
2. `TaskResult.success` 当前由执行成功与 Git 集成成功推导，不包含隐藏测试、回归测试和修改范围检查。
3. 现有人工审查只审计划，尚未记录最终 Diff 的接受、修改后接受或拒绝。
4. CLI 工具事件缺少统一稳定的 `tool_call_id`，多个同名调用的结果可能无法精确匹配。
5. 当前回归测试验证平台机制，不等于 Coding Agent 任务成功率评测。
6. 严格 ExecutionSandbox 尚未落地，批量运行自治 Coding CLI 会放大宿主机、凭据、网络与资源风险。

因此需要增加一套独立 Eval Harness，将“任务执行”“Git 产物”“确定性验收”“人工评价”和“统计报告”连接成可复现闭环。

## 2. 目标与非目标

### 2.1 目标

- 建立覆盖代码生成、Bug 修复、重构、测试生成、多文件集成和 No-op 安全判断的评测集。
- 每个 Trial 使用独立、可丢弃的仓库副本和 Worktree，不共享前次运行状态。
- 隐藏测试、评分规则和参考答案在 Agent 执行期间不可见。
- 使用确定性 Grader 判定任务正确性、回归、越界修改和 Git 产物完整性。
- 统一计算任务成功率、修复准确率、测试通过率、误修改率、重试次数、延迟、Token 和成本。
- 支持相同 Dataset、系统版本、模型和参数的重复运行及配对对比。
- 将 Trace、Run、Trial、Dataset Case 和 Git Commit 关联，支持失败归因。
- 支持结果导出为 JSONL、CSV 和 Markdown，简历数据能够追溯到固定评测版本。
- 第一阶段采用 CLI 和文件报告，不依赖评测管理前端。

### 2.2 非目标

- 不把普通单元测试数量包装成 Agent 效果指标。
- 不以 LLM-as-a-Judge 单独决定代码任务是否成功。
- 不允许 Agent 读取隐藏测试、Grader 代码、参考补丁或其他 Trial 产物。
- 不在第一阶段实现在线排行榜、公开 Benchmark 服务或多租户评测平台。
- 不在第一阶段自动修改生产分支、推送远端或部署评测产物。
- 不通过关闭测试、放宽类型检查或修改 Grader 来判定任务成功。
- 不在 `unsafe_process` 模式下启用无人值守批量 Eval。

## 3. 核心概念与成功语义

### 3.1 领域对象

| 对象         | 含义                                                                                      |
| ------------ | ----------------------------------------------------------------------------------------- |
| Dataset      | 一组版本固定的评测 Case                                                                   |
| Case         | 一个任务定义，包含 Fixture、Prompt、规则和 Grader 配置                                    |
| Experiment   | 一个不可变的实验配置快照，如系统版本、Agent/模型、Prompt 版本、执行镜像、并行度和重复次数 |
| Trial        | 某个 Experiment 对某个 Case 的一次运行                                                    |
| Fixture      | Agent 可见的初始代码仓库                                                                  |
| Hidden Asset | Agent 不可见的隐藏测试、评分脚本或参考事实                                                |
| Grader       | 对 Run、Diff、测试和人工结果执行评分的组件                                                |
| Eval Result  | Trial 的事实、分数、指标和失败原因                                                        |

### 3.2 三层成功语义

系统必须保留三层状态，不能压缩成一个 `success`：

```text
运行成功：Run 正常终止，未超时、取消或违反预算
集成成功：Agent 产物形成可验证 Commit，并正确集成到评测目标分支
任务成功：隐藏测试、回归、Diff 边界及任务专属断言全部通过
```

权威任务成功条件：

```text
task_success =
    run_state == completed
    AND integration_status IN {merged, not_required}
    AND required_graders_all_passed
    AND forbidden_change_count == 0
    AND baseline_regression_count == 0
```

`Run completed` 或 `TaskResult.success` 只能作为前置事实，不能直接记为 `task_success=true`。

任何由 Agent 行为导致的超时、依赖损坏、测试无法收集、仓库损坏或 Grader 协议违规都是有效 Trial 的失败，不得标记为 `INVALID`。`INVALID` 仅用于能归因到 Agent 执行之外的评测设施或 Fixture 故障。

## 4. 总体架构

```text
Dataset Manifest
      │
      ▼
Eval Coordinator ─────────────── Experiment Store
      │                                │
      ├── 准备只读 Fixture             │
      ├── 创建 Trial Workspace          │
      ├── 生成 Agent 可见 Prompt         │
      ▼                                │
AgentHub Run / Orchestrator             │
      │                                │
      ├── Run/Event Journal ────────────┤
      ├── Langfuse Trace                │
      ├── Tool Events                   │
      └── Git Integration Result        │
      │                                │
      ▼                                │
Evaluation Sandbox                     │
      ├── 挂载 Trial 产物               │
      ├── 单独挂载 Hidden Tests         │
      ├── 禁止模型与外网访问             │
      └── 运行确定性 Graders             │
      │                                │
      ▼                                │
Result Aggregator ─────────────────────┘
      ├── JSONL / SQLite
      ├── CSV / Markdown
      └── Langfuse Scores
```

### 4.1 事实源划分

| 事实                         | 权威来源                          |
| ---------------------------- | --------------------------------- |
| Run 生命周期、时间和终止原因 | RunRepository                     |
| CLI 事件和工具调用           | Run EventJournal                  |
| Token Usage 和 Trace         | 标准化 DONE 事件 + Langfuse       |
| Git 分支、Commit、冲突和合并 | IntegrationRepository / Git probe |
| 测试及任务正确性             | Eval Grader                       |
| 最终人工接受                 | Eval ReviewRecord                 |
| 聚合指标                     | Eval Result Store                 |

Langfuse 是分析和可视化副本；即使 Langfuse 暂时不可用，Trial 仍须完成并保存本地权威结果。

## 5. 代码与目录规划

第一阶段将评测代码放在 AgentEnd 内，但与生产 `src/` 隔离：

```text
agentend/
├── evals/
│   ├── README.md
│   ├── cli.py                # readiness/validate/baseline/report/grade/review/speedup/batch/experiment
│   ├── batch.py              # 真实 Agent 批跑驱动（--arm serial/parallel、断点续跑）
│   ├── coordinator.py
│   ├── loader.py / digests.py / diffutils.py
│   ├── models.py
│   ├── readiness.py          # strict sandbox 批量门禁（fail-closed）
│   ├── repository.py
│   ├── runner.py
│   ├── metrics.py            # aggregate_trials + parallel_speedup
│   ├── scoring.py            # v2 部分得分（见 16 号文档）
│   ├── report.py
│   ├── sandbox.py / transcript.py / langfuse_scores.py
│   ├── graders/
│   │   ├── base.py
│   │   ├── run_state.py
│   │   ├── git_diff.py
│   │   ├── command.py
│   │   ├── anti_gaming.py
│   │   ├── no_op.py
│   │   └── llm_quality.py
│   ├── datasets/
│   │   ├── build_agenthub_coding_v1.py
│   │   ├── build_agenthub_agent_v2.py   # make evals build-dataset
│   │   ├── agenthub-agent-v2/           # 当前默认数据集（v2.1.0，34 case）
│   │   │   ├── dataset.yaml
│   │   │   └── cases|fixtures|hidden/
│   │   └── agenthub-coding-v1/          # DEPRECATED（2026-09-17 起仅原地保留）
│   │       └── dataset.yaml / cases|fixtures|hidden/
│   └── tmp/                   # 批跑输出（gitignore，含 results.jsonl）
└── tests/
    └── evals/                 # make evals release 回归门禁
```

约束：

- Fixture 使用 Git bundle、归档或固定 Commit，必须带 SHA-256。
- Dataset/Experiment Digest 对排序键的规范化 JSON 计算，内容包含所有 Case、Fixture、Hidden Asset、Grader 和环境 Digest，不依赖 YAML 空白或文件遍历顺序。
- `hidden/` 不复制到 Agent Worktree；只在执行完成后的 Grader Sandbox 中只读挂载。
- `reports/` 和临时 Trial 目录加入 `.gitignore`，Dataset 清单与小型 Fixture 可入库。
- 大型 Fixture 放对象存储时，Manifest 必须固定对象版本和 Digest。
- Eval 模型不进入跨端 Contracts，除非后续需要 Backend/Frontend 展示。

## 6. Dataset 与 Case 设计

### 6.1 Dataset Manifest

```yaml
schema_version: 1
dataset_id: agenthub-coding-v1
version: 1.0.0
description: AgentHub Coding Agent 核心工程能力评测
case_ids:
  - bugfix-run-parent-fence-001
  - bugfix-context-tool-pair-001
  - feature-sse-resume-001
defaults:
  wall_time_seconds: 600
  grader_time_seconds: 180
```

Dataset 只定义任务与验收事实；Agent 类型、模型、Prompt 版本、并行度、重复次数和随机种子属于 Experiment 快照，不写入 Case，避免为比较两个 Agent 而复制 Dataset。

### 6.2 Experiment 快照

```yaml
schema_version: 1
experiment_id: exp-20260916-codex-a
dataset_id: agenthub-coding-v1
dataset_version: 1.0.0
dataset_digest: sha256:<digest>
system_revision: <git-commit>
prompt_revision: <digest-or-version>
agent_type: codex
model: <provider-model-id>
model_parameters: { temperature: 0 }
execution_image: <registry/image@sha256:digest>
dependency_cache_digest: sha256:<digest>
max_parallelism: 1
repetitions: 3
seed_policy: per_case_repetition
```

Experiment 创建后不得原地修改；任何配置变化都创建新 Experiment。如 Provider 不支持种子或完全确定性，必须在报告中标记，不得宣称模型输出可逐字复现。

### 6.3 Case Manifest

```yaml
schema_version: 1
case_id: bugfix-run-parent-fence-001
category: bugfix
difficulty: medium
fixture:
  source: fixtures/bugfix-run-parent-fence-001.bundle
  sha256: <digest>
  base_ref: refs/heads/main
prompt: |
  修复父 Run 进入终态后仍可能接受新子 Run 的问题，并补充必要测试。
execution:
  timeout_seconds: 600
  max_turns: 20
  network: none
scope:
  allowed_paths:
    - agentend/src/execution/**
    - agentend/tests/test_execution.py
  forbidden_paths:
    - agentend/evals/hidden/**
    - agentend/pyproject.toml
graders:
  - type: run_state
  - type: git_diff
  - type: command
    argv: [uv, run, pytest, -q, agentend/tests/test_execution.py]
  - type: hidden_command
    asset_id: parent-fence-tests-v1
    argv: [uv, run, pytest, -q, /eval-hidden/test_parent_fence.py]
expected:
  require_change: true
  require_commit: true
  max_changed_files: 4
```

Case Manifest 是评测控制面资产，不整份传入 Agent。Coordinator 只能从显式允许列表生成 Agent 可见 Prompt，不得包含 `graders`、`expected`、Hidden Asset ID/路径、失败断言名称或参考补丁信息。Grader 命令使用 `argv` 数组和受限 `cwd/env` 配置，不允许 Shell 字符串。

### 6.4 首版 30 条 Case 配比

| 类别             | 数量 | 核心能力                       |
| ---------------- | ---: | ------------------------------ |
| Bug 修复         |    8 | 定位、最小修复、隐藏测试       |
| 功能开发         |    6 | 需求理解、接口与跨文件实现     |
| 重构             |    4 | 行为保持、结构改善、回归控制   |
| 测试生成         |    4 | 边界条件、异常路径、测试有效性 |
| 多文件与集成     |    4 | 依赖修改、Git 集成、冲突处理   |
| No-op / 安全判断 |    4 | 拒绝错误前提、避免无必要修改   |

首版优先从已修复缺陷和现有测试中提取受控变异：

- 父 Run 终态后错误接受子 Run。
- 取消传播未覆盖已登记子 Run。
- Worktree 路径或 symlink 逃逸。
- Context Compactor 拆散 Tool Call 与 Tool Result。
- DAG 循环依赖或不存在依赖未被拒绝。
- SSE 按序号恢复时出现重复或丢失。
- Outbox 旧任务误作用于新安装记录。
- Integration Result 绑定到错误 Workspace。
- Git 冲突污染 task-base Worktree。
- Tool Call 结束但没有匹配 Tool Result。

### 6.5 Case 制作规则

1. 从当前正确代码或历史修复前 Commit 创建 Fixture。
2. 只引入一个主要故障，避免多个根因使判分歧义。
3. 在 Agent 运行前证明可见或隐藏测试至少有一项失败。
4. 保存任务描述、Fixture Digest、测试命令和期望修改范围。
5. 隐藏测试必须验证行为，不匹配特定补丁文本。
6. Case 不得依赖当前开发机的绝对路径、个人密钥或未锁定远程资源。
7. 执行环境必须固定镜像 Digest、语言/工具链版本与离线依赖缓存 Digest；缓存缺失在 Trial 开始前是设施故障，在 Agent 修改后才出现则按 Trial 失败处理。
8. No-op Case 必须明确 `require_change=false`，并用任务专属的确定性断言判定“不应修改”；任何无必要代码修改均计入误修改。自然语言解释的表达质量只进入人工/LLM 软评分。
9. `allowed_paths`/`forbidden_paths` 采用仓库相对 POSIX 路径和固定 glob 语义；规则应同时检查 rename 的旧/新路径、删除、文件模式、symlink、submodule 和二进制文件。
10. Git bundle 必须通过 `git bundle verify`，且 Fixture 能在无网络环境完整恢复；子模块和 Git LFS 对象要么随 Fixture 封装并固定 Digest，要么在 Schema 校验时拒绝。

## 7. Trial 生命周期

```text
CREATED
  → PREPARING
  → BASELINE_CHECKING
  → READY
  → RUNNING
  → COLLECTING
  → GRADING
  → AWAITING_REVIEW（可选）
  → COMPLETED

任意阶段可进入 FAILED / CANCELLED / INVALID
```

### 7.1 执行步骤

1. 校验 Dataset、Case Schema 和所有 Digest。
2. Coordinator 在专用临时根目录中恢复 Fixture，校验路径与 Git 对象后创建干净 Trial 仓库；此阶段不执行 Fixture 代码。
3. 执行 baseline checks：
   - Bug Case 的目标失败测试必须失败。
   - 重构、测试生成和 No-op Case 的原有测试必须通过。
   - 所有 baseline 命令都在与 Agent 执行环境分离的 Grader Sandbox 中运行。
4. 创建 AgentHub Run，设置 `requested_by=eval`；由 Eval Store 保存 `experiment_id / trial_id / case_id → root_run_id`的权威映射，并将同样的可观测允许列表元数据传给 Langfuse。现有 `RunSpec` 不为此虚构额外字段。
5. 等待 Run 终态，并保存事件游标、终止原因、Token Usage 和 Trace ID。
6. 读取 IntegrationResult，并使用 Git probe 验证提交谱系。
7. 冻结最终 Commit，不允许 Agent 继续写入。
8. 创建不联网的 Grader Sandbox，只读挂载 Hidden Assets。
9. 按固定顺序运行 Grader，写入每项事实和证据摘要。
10. 如 Case 需要人工审查，则生成 Diff ReviewRecord。
11. 聚合 TrialResult，写入 SQLite 与 JSONL。
12. 将数值 Score 和分类 Tag 回写 Langfuse，失败不影响本地结果提交。

### 7.2 幂等与恢复

- `trial_id` 由 `experiment_id + case_id + repetition` 的规范化编码确定性生成；Experiment 本身必须已固定全部执行配置。
- 相同 Trial 配置重复提交时返回已有结果，不重复消耗模型。
- Trial 每个阶段在 SQLite 中持久化，重启后从安全边界恢复。
- `RUNNING` 阶段重启后先查询 RunRepository；未知执行状态不得直接重跑。
- `GRADING` 可安全重复，但每次都新增不可变的 GraderRun，记录 Grader 版本、执行镜像和 Hidden Asset Digest，不覆盖旧结果。
- 已完成 Trial 不允许覆盖，只能创建新的 Experiment 或 Dataset 版本。

## 8. Grader 设计

### 8.1 执行顺序

Grader 按成本和确定性排序：

1. `RunStateGrader`：Run、预算、终止原因。
2. `IntegrationGrader`：Commit、合并状态、Git 谱系。
3. `DiffScopeGrader`：允许路径、禁止路径、文件数量和敏感文件。
4. `VisibleCommandGrader`：项目公开测试、lint、build。
5. `HiddenCommandGrader`：Agent 不可见的行为测试。
6. `RegressionGrader`：基线原有测试与跨模块回归。
7. `NoOpGrader`：无需修改时是否保持零 Diff。
8. `HumanReviewGrader`：最终 Diff 的人工接受状态。
9. `LLMQualityGrader`：可选，只评价可维护性等主观维度，不决定硬成功。

### 8.2 硬门槛与软评分

硬门槛任何一项失败，`task_success=false`：

- Run 未正常完成。
- 必需 Git 产物缺失或集成失败。
- 隐藏测试失败。
- 原有测试产生回归。
- 修改禁止路径。
- 通过删除、跳过或弱化测试规避验收。

软评分用于排序和分析：

- 修改规模与最小性。
- 代码复杂度变化。
- 新增测试质量。
- 文档和错误信息质量。
- 人工可维护性评分。

### 8.3 Grader 输出

```json
{
  "grader": "hidden_command",
  "version": "1.0.0",
  "status": "passed",
  "score": 1.0,
  "duration_ms": 8421,
  "argv": ["uv", "run", "pytest", "-q", "/eval-hidden/test_parent_fence.py"],
  "exit_code": 0,
  "passed": 6,
  "failed": 0,
  "evidence_digest": "sha256:...",
  "summary": "6 passed"
}
```

原始输出必须有大小上限并脱敏；报告默认保存摘要和 Digest，大日志通过受控 Artifact 保存。

## 9. 指标口径

### 9.1 质量指标

| 指标             | 公式                                                                    |
| ---------------- | ----------------------------------------------------------------------- |
| 任务成功率       | `成功 Trial 数 / 有效 Trial 数`                                         |
| Bug 修复准确率   | `task_success=true 的 Bugfix Trial / 有效 Bugfix Trial`                 |
| 任务级测试通过率 | `全部必需测试通过的 Trial / 至少定义一项必需测试的有效 Trial`           |
| 断言级测试通过率 | `通过断言数 / 执行断言总数`                                             |
| 硬误修改率       | `修改禁止/受保护路径，或 No-op Case 出现 Diff 的 Trial / 有效 Trial`    |
| 审查型过度修改率 | `人工决策为 rejected_overbroad 的 Trial / 完成人工审查的 Trial`         |
| 回归率           | `引入原有测试失败的 Trial / 有效 Trial`                                 |
| No-op 正确率     | `零 Diff 且任务专属确定性断言通过的有效 No-op Trial / 有效 No-op Trial` |
| 直接人工接受率   | `直接接受 / 完成人工审查的 Trial`                                       |
| 宽松人工接受率   | `(直接接受 + 修改后接受) / 完成人工审查的 Trial`                        |

“有效 Trial”不包含在 Agent 启动前已确认的 Fixture 损坏、锁定依赖缓存缺失或评测基础设施故障等 `INVALID` 结果。状态必须记录机器可读的责任方和原因码，报告单独展示无效率；禁止静默删除失败样本，也禁止将 Agent 导致的环境损坏转成 `INVALID`。

断言级通过率只用于同一 Dataset 内的诊断，不作为跨 Dataset 排名主指标，避免断言拆分粒度不同导致权重失真。

### 9.2 效率指标

| 指标             | 口径                                                                      |
| ---------------- | ------------------------------------------------------------------------- |
| 执行耗时         | 根 Run `started_at → finished_at`                                         |
| 端到端耗时       | Trial `PREPARING → COMPLETED/FAILED/INVALID`，单独报告执行与 Grading 耗时 |
| TTFA             | 根 Run 开始到首个 `text/tool_call/error/done` 有效动作事件                |
| TTFT             | 根 Run 开始到首个 `text` 事件；无文本输出时为 `null`，不记为 0            |
| 子任务耗时       | `TaskResult.duration`                                                     |
| 平均重试次数     | 每 Trial 的执行重试次数平均值                                             |
| 平均重规划次数   | Orchestrator replan iteration 平均值                                      |
| 冲突恢复次数     | ResolutionAttempt 数量                                                    |
| P50/P95 延迟     | 对有效 Trial 的执行耗时和端到端耗时分别计算分位数                         |
| 每成功任务 Token | `总 Token / 成功 Trial 数`                                                |
| 每成功任务成本   | `总模型成本 / 成功 Trial 数`                                              |

执行重试、重规划和冲突恢复必须分开统计。`attempt` 当前从 0 开始：每个逻辑 `plan_task_id` 的执行重试数是其 `max(attempt)`，Trial 重试数是各逻辑任务重试数之和，不能用整个 Trial 的一个 `max(attempt)` 丢失多子任务重试。冲突恢复次数按去重后的 `ResolutionAttempt` 记录计数。

Token 与成本报告必须同时展示 Usage 覆盖率、Provider 价格表版本和币种；Usage 缺失的 Trial 不得当作 0 Token/0 成本。

### 9.3 并行收益

并行效率必须使用配对实验：

- Dataset、Fixture、Prompt、模型和系统版本相同。
- 对照组 `max_parallelism=1`，实验组使用目标并行度。
- 仅对含至少两个可并行分支的 Orchestrator Case 统计并行收益。
- 每个 Case 至少重复 3 次，按 `case_id + repetition` 配对比较中位数与成功率。

```text
speedup = 串行组中位耗时 / 并行组中位耗时
```

只有实验组质量指标不劣于预设容忍区间时，才能报告加速收益。

### 9.4 统计不确定性

- 所有比例指标同时报告分子、分母和 95% 置信区间；小样本使用 Wilson 区间，不使用正态近似。
- 系统对比以 Case 为采样单位做配对 bootstrap，不将同一 Case 的多次 repetition 当作完全独立样本。
- 30 条 Case 是首版工程回归集，不支持对所有 Coding Agent 任务的普遍性外推；报告必须限定结论适用的 Dataset 和系统版本。

### 9.5 平台可靠性指标

以下指标单独进入 Reliability Suite，不与 Coding 质量成功率混合：

- SSE 断线恢复成功率、重复事件率、丢失事件率、恢复 P95。
- Run 取消收敛率和进程树回收率。
- AgentEnd 重启后的 Run、Workspace 和 Integration 恢复率。
- Git 冲突检测、Resolver 成功率和人工接管率。
- 工具调用成功率和未匹配 Tool Result 比例。

## 10. 工具事件与可观测性改造

### 10.1 统一工具调用身份

所有 Adapter 的 Tool Call 与 Tool Result 增加：

```json
{
  "tool_call_id": "provider-or-generated-id",
  "tool": "command_execution",
  "status": "started|success|failed|cancelled|incomplete",
  "started_at": "...",
  "finished_at": "...",
  "exit_code": 0
}
```

要求：

- 优先保留 Provider 原始调用 ID；没有时由 Adapter 使用 `run_id + 单调序号` 生成在 Run 内唯一、可重放的 ID，不使用工具名作为身份。
- Tool Result 必须引用同一个 `tool_call_id`，不得仅按工具名匹配。
- Stream 结束时仍未完成的 Tool Call 标记为 `incomplete`。
- Transport Sanitizer 可以裁剪参数和结果，但不能移除 ID、工具名、状态、时间和退出码。
- 如果字段进入 SSE 跨端契约，先修改 `contracts/schemas/event-types.yaml` 并执行 `make generate`。

### 10.2 Eval Trace 元数据

Langfuse Trace 增加允许列表字段：

```text
dataset_id
dataset_version
experiment_id
trial_id
case_id
case_category
repetition
system_revision
prompt_revision
model
agent_type
```

Trial 完成后回写：

- `task_success`
- `hidden_test_pass`
- `regression_free`
- `false_modification`
- `human_acceptance`
- `duration_seconds`
- `total_tokens`
- `trial_cost`
- `usage_available`

隐私规则继续生效；源码正文、密钥、绝对宿主路径和完整隐藏测试不得上传 Langfuse。`cost_per_success` 是 Experiment 聚合值，不回写为单个 Trial Score；现有 `agentend/src/observability/privacy.py` 的 `ALLOWED_METADATA_KEYS` 必须与上述字段同步扩展后，这些元数据才会实际上报。

## 11. 持久化模型

第一阶段使用独立 SQLite WAL 数据库，例如 `agentend/data/evals.sqlite3`。

### 11.1 核心表

```text
experiments
  experiment_id PK
  dataset_id / dataset_version / dataset_digest
  system_revision / prompt_revision
  model_config_json / execution_config_json / config_digest
  execution_image_digest / dependency_cache_digest
  status
  created_at / finished_at

trials
  trial_id PK
  experiment_id FK
  case_id / repetition / seed
  root_run_id / trace_id
  fixture_digest / final_commit
  state / failure_reason / invalid_reason / responsibility
  started_at / finished_at
  result_json

grader_runs
  grader_run_id PK
  trial_id FK
  grader_set_digest / hidden_asset_digest / execution_image_digest
  started_at / finished_at

grader_results
  grader_result_id PK
  grader_run_id FK / trial_id FK
  grader_name / grader_version
  status / score
  evidence_digest / summary
  duration_ms
  UNIQUE(grader_run_id, grader_name)

review_records
  review_id PK
  trial_id FK
  reviewer_id
  decision
  reviewed_commit / amended_commit
  reason_codes_json
  comment
  created_at
```

### 11.2 人工审查决策

```text
accepted
accepted_with_changes
rejected_incorrect
rejected_regression
rejected_overbroad
rejected_unmaintainable
```

首版可以通过 CLI 写入 ReviewRecord；产品化后再增加 Backend API 和前端 Diff 审查界面。`accepted_with_changes` 必须同时记录 Agent 原始 `reviewed_commit` 和人工修改后 `amended_commit`；人工修改后的通过不能回写或提高原 Trial 的 `task_success`，只进入“修改后接受率”和人工修改量统计。

SQLite 仅支持单 Coordinator 进程持有写入所有权，通过事务和有界 `busy_timeout` 序列化状态转换；数据库不放在 NFS/对象存储上。如后续需要多 Coordinator，先迁移到支持行级锁和唯一约束的共享数据库。

## 12. 安全与隔离

### 12.1 批量 Eval 上线门槛

当前严格 ExecutionSandbox 未完成，Eval 批量执行必须保持关闭。允许实施的内容包括：

- Dataset、Fixture、Grader、Repository 和报告代码。
- 使用伪 Runner 的自动化测试。
- 对已存在 Git 产物进行离线 Grading。
- 单次、人工监督、使用可丢弃仓库的开发验证。

以下能力完成前，不允许无人值守批量运行真实 Coding CLI：

- 严格文件系统隔离，Agent 不能看到 Hidden Assets 和其他 Trial。
- 独立进程与资源限制，支持可靠进程树回收。
- 网络隔离和受控模型/依赖出口。
- 短期凭据或凭据代理，禁止暴露宿主长期凭据。
- 独立 Git 元数据，不能修改其他 Task refs、hooks 或 config。
- CPU、内存、进程、磁盘、时间和输出预算。
- Eval 模式禁止自动降级到 `unsafe_process`。

### 12.2 Grader 隔离

- Grader 与 Agent 执行环境分离。
- Grader 默认无网络，不注入模型密钥和 Agent CLI 凭据。
- Hidden Assets 只读挂载，且不位于 Trial Worktree 路径下。
- Grader 命令使用参数数组，不经过 Shell 拼接。
- 每个 Grader 有独立超时、输出上限和进程限制。
- Fixture 内测试脚本按不可信代码处理，必须在沙盒内运行。
- Grader 执行 Agent 产物时仍按敌意代码处理：禁止网络、禁止可写宿主挂载，不向 Trial 产物传递 Hidden Asset 原始输出，销毁临时文件系统后只导出有界、脱敏的 Grader 摘要。

## 13. CLI 设计

第一阶段提供以下入口：

```bash
# 查看 strict sandbox readiness 是否允许无人值守批量评测
uv run --directory agentend python -m evals.cli readiness

# 校验 Dataset、Case、Fixture 和 Hidden Asset Digest（Makefile 默认数据集 evals/datasets/agenthub-agent-v2）
make evals validate
# 等价：uv run --directory agentend python -m evals.cli validate <dataset-dir> --environment-digest <sha256>

# 仅运行 Fixture 基线检查，不调用 Agent
make evals baseline

# 真实 Agent 批跑（复用 coordinator 官方评分链路）；strict readiness 未通过时
# 必须显式 --allow-unsafe 才能在本地信任环境执行，否则失败关闭退出
# --case 限定 case、--limit 试点、--repetitions 重复、--arm 选择 parallel/serial 对照臂
make evals batch ARGS="--allow-unsafe --limit 6"

# 串行/并行配对加速比（对两臂批跑输出离线计算，见 17 号文档）
make evals speedup ARGS="--serial <dir> --parallel <dir>"

# 运行完整 Experiment；必须通过 strict sandbox readiness
uv run --directory agentend python -m evals.cli experiment \
  --dataset <dataset-dir> \
  --environment-digest <sha256> \
  --config <experiment.yaml> \
  --database <sqlite-path>

# 对已有 Trial 重新执行 Grader
uv run --directory agentend python -m evals.cli grade \
  --database <sqlite-path> --dataset <dataset-dir> --environment-digest <sha256> \
  --trial <trial-id> --repository <repo> --base-revision <rev> \
  --execution-image-digest <sha256>

# 追加人工 Diff 审查记录（不改变 Agent 得分）
uv run --directory agentend python -m evals.cli review \
  --database <sqlite-path> --trial <trial-id> --reviewer <id> \
  --decision <decision> --reviewed-commit <sha>

# 生成报告
uv run --directory agentend python -m evals.cli report \
  --database <sqlite-path> --experiment <experiment-id> --output <dir>
```

批量命令启动时必须验证：

- ExecutionSandbox 为 strict 且 readiness 通过。
- Dataset 和 Fixture Digest 全部匹配。
- 当前 Git revision 已记录。
- Langfuse 可选；关闭时打印提示但不阻塞。
- 并发不超过 RunBudget 和宿主 Eval 配额。

## 14. 自动化测试策略

### 14.1 单元测试

- Dataset/Case Schema 校验与未知字段拒绝。
- Digest 不匹配失败关闭。
- Trial 状态机合法和非法转换。
- 指标分母、INVALID 排除和 0 样本处理。
- Diff allowlist/denylist、rename、delete、symlink 和二进制文件。
- Grader 超时、输出截断、退出码和解析失败。
- Tool Call/Result ID 配对和不完整调用。
- Result Repository 幂等、并发和崩溃恢复。

### 14.2 集成测试

- 使用 Fake Agent 产生正确补丁，验证完整成功路径。
- Fake Agent 不修改代码，Bug Case 必须失败。
- Fake Agent 修改禁止路径，必须判定误修改。
- 可见测试通过但隐藏测试失败，任务必须失败。
- Run completed 但集成失败，任务必须失败。
- Agent 删除测试或增加 skip，Anti-gaming Grader 必须拒绝。
- Grader 重跑产生相同事实，不覆盖原始 Trial。
- Langfuse 不可用时本地结果仍正常完成。

### 14.3 端到端验收

严格沙盒完成后，使用 3 个最小 Case 验证：

1. 单 Agent Bug 修复成功。
2. 多 Agent 两个独立子任务并行执行并成功集成。
3. 两个 Agent 制造真实冲突，经 Resolver 修复后隐藏测试通过。

端到端验收同时检查 Run/Event、Git、Langfuse、Eval Store 和 Markdown 报告之间的 ID 可关联性。

## 15. 分阶段实施计划

### Phase 0 — 模型冻结与安全门禁

- [x] 冻结 Eval Dataset、Case、Experiment、Trial 和 Grader 领域模型。
- [x] 明确 Fixture、Hidden Assets、执行镜像和依赖缓存的 Digest 规则。
- [x] 实现 Eval 启动门禁：非 strict/readiness 环境拒绝批量命令，且不得自动降级到 `unsafe_process`。
- [x] 记录 strict ExecutionSandbox 的批量 Eval 能力缺口，并与 `13-agentend-execution-sandbox.md` 的实施项对齐。

**退出标准**：领域模型与摘要规则通过 Schema 测试；批量命令在当前 `unsafe_process` 环境下可验证地失败关闭。这一退出标准只允许进入离线 Phase 1，不允许真实批量 Agent 执行。

### Phase 1 — 离线 Dataset 与确定性 Grader

- [x] 建立 `agentend/evals/` 目录和 Schema。
- [x] 实现无网络、只读 Hidden Assets、无宿主凭据的 Grader Sandbox，并将 Fixture/Agent 产物按敌意代码执行。
- [x] 实现 Fixture 恢复、Baseline Check、DiffScope 和 Command Grader。
- [x] 实现 SQLite Result Store、JSONL、CSV 和 Markdown 报告。
- [x] 建立开发用 Case，覆盖成功、失败、No-op 和越界修改。
- [x] 使用 Fake Runner 完成全链路自动化测试。

**退出标准**：Grader Sandbox readiness 通过；对固定 Git 产物重复评分结果一致；10 条 Case baseline 全部有效。

### Phase 2 — AgentHub Run 接入

- [x] 完成 strict ExecutionSandbox 的批量 Eval 最小能力；Agent 无法读取 Hidden Assets、宿主凭据和其他 Trial。
- [x] Eval Runner 创建 `requested_by=eval` 的 Run。
- [x] 关联 Experiment、Trial、Case、Run、Trace 和 Commit。
- [x] 收集 Run 终态、TaskResult、IntegrationResult、事件和 Token Usage。
- [x] 统一 Adapter 的 `tool_call_id` 与 Tool Result 状态。
- [x] 增加 Trial 恢复、取消和超时处理。

**退出标准**：strict sandbox readiness 通过；单 Case 可通过真实 AgentHub 执行，并生成可追溯的完整 EvalResult。

### Phase 3 — 30 条核心回归集

- [x] 扩充到 30 条 Case，并完成难度、类别和所有权标注。
- [x] 每个 Bug Case 至少有一个隐藏行为测试。
- [x] 增加 Anti-gaming、No-op 和 Regression Grader。
- [x] 支持目标系统配置 3 次重复实验，并按 Case 配对统计。
- [x] 建立失败原因分类和人工复核流程。

**退出标准**：30 条 Case 的 baseline 无 INVALID；正式实验如出现 INVALID 必须展示原因与分母，不以“必须为 0”驱动错误重分类。重复运行可生成 P50/P95 和预定义置信区间，所有失败均可定位到 Trace、Diff 或 Grader 证据。

### Phase 4 — 人工验收与产品化

- [x] 增加最终 Diff 接受、修改后接受和拒绝记录。
- [x] Backend 提供 Dataset/Experiment/Trial 查询 API。
- [x] Frontend 展示对比报告、Diff、Trace 跳转和失败状态。
- [x] 支持基线系统与候选系统配对对比。
- [x] 将稳定 Case 加入发布前回归门禁（`make evals release`）。

**退出标准**：能够从评测报告下钻到 Run、工具调用、Git Diff、隐藏测试和人工决策。

## 16. 里程碑验收指标

第一版评测系统自身的验收标准：

| 项目       | 标准                                                            |
| ---------- | --------------------------------------------------------------- |
| 可复现性   | 相同最终 Commit 和 Grader 版本重复评分一致                      |
| 隐藏性     | Agent Worktree 中无法发现 Hidden Assets 路径或内容              |
| 可追溯性   | 每个结果可关联 Case、Trial、Run、Trace、Commit 和 Grader Digest |
| 失败关闭   | Digest、Sandbox、Fixture 或 Grader异常时不产生成功结果          |
| 幂等性     | 相同 Trial 重复提交不重复调用模型                               |
| 统计正确性 | INVALID 不进入质量指标分母，并在报告中单列                      |
| 安全性     | 批量 Eval 在非 strict sandbox 下拒绝启动                        |
| 报告完整性 | 输出质量、效率、成本、可靠性及失败原因分布                      |

## 17. 评测报告格式

报告头部必须固定以下上下文：

```text
Dataset ID / Version
Dataset Digest
System Git Revision
Prompt Revision
Model / Provider
Agent Type
Execution Image / Dependency Cache Digest
Execution Mode / Sandbox Readiness
Parallelism
Repetitions
Usage Coverage / Price Table Version / Currency
Started / Finished At
Valid / Invalid Trials
```

核心表格：

| 配置 | 有效 Trial | 任务成功率 | 修复准确率 | 误修改率 | 回归率 | 平均重试 | P95 | Token/成功任务 |
| ---- | ---------: | ---------: | ---------: | -------: | -----: | -------: | --: | -------------: |

报告还必须包含：

- 按类别和难度拆分的成功率。
- 失败原因 Top N。
- 可见测试通过但隐藏测试失败的数量。
- No-op 错误修改数量。
- 平台故障与 INVALID Trial。
- 人工接受率及评审一致性。
- 与上一稳定基线的差值，而不只展示绝对值。

## 18. 简历数据使用规范

只有满足以下条件的数据才能进入简历：

- Dataset 和版本已固定。
- Case 数量、类别和重复次数可说明。
- 所有指标有明确分母。
- 结果来自 strict sandbox 下的正式 Experiment。
- 报告记录 Git revision、模型、Prompt 版本和日期。
- 不选择性删除失败 Case。
- 不把平台运行成功率写成任务成功率。
- 不把计划审批率写成人工代码接受率。
- 不把单次最好结果写成稳定平均结果。

推荐表述模板：

> 构建覆盖 Bug 修复、功能开发、重构、测试生成及多文件修改的 X 条 Coding Agent 回归评测集，通过隔离 Worktree、隐藏测试和 Git Diff 约束自动验收；在 `<dataset-version>` 上任务成功率 X%、Bug 修复准确率 X%、误修改率 X%，并使用 Langfuse 追踪工具调用、Token 成本和失败模式。

## 19. 已知风险与缓解

| 风险                           | 缓解措施                                           |
| ------------------------------ | -------------------------------------------------- |
| Agent 通过仓库线索猜到隐藏测试 | 隐藏测试独立挂载；行为断言不依赖固定补丁           |
| Case 被模型或开发者记忆        | 使用多个等价变体，报告 Dataset 版本与污染风险      |
| LLM 非确定性导致指标波动       | 固定配置、至少 3 次重复、报告区间而非单点          |
| 测试通过但实现不可维护         | 硬门槛后增加人工审查和可维护性软评分               |
| Agent 修改测试逃避验收         | 保存 baseline Digest，检测测试删除、skip 和弱化    |
| Langfuse 故障导致结果丢失      | 本地 SQLite/JSONL 为权威，异步补传 Score           |
| Fixture 依赖失效               | 依赖锁定、离线缓存、Baseline Check 和 INVALID 分类 |
| Eval 批量运行危害宿主机        | strict sandbox、无降级、资源预算和网络隔离         |
| 指标被简历过度解读             | 固定口径、保留报告、区分平台可靠性与任务质量       |

## 20. 实施优先级结论

离线建设与执行安全可并行推进，但真实 Agent 批量运行的门禁不可颠倒：

1. 先冻结 Dataset/Case/Experiment/Trial Schema、失败归因与 Digest 口径，并实现非 strict 环境下的批量失败关闭。
2. 在独立 Grader Sandbox 中实现 Fixture、Baseline Check、确定性 Grader、Trial Store 和报告。
3. 完成严格 Agent 执行沙盒与隐藏资产隔离；readiness 通过前不进入真实批量运行。
4. 接入 AgentHub Run、Git Integration 和 Langfuse，同步补强工具调用 ID、Token、TTFA/TTFT 和责任归因。
5. 扩展到 30 条正式评测集，做配对重复实验与置信区间。
6. 最后增加人工 Diff 验收和前端产品化。

第一阶段最有价值的交付不是评测界面，而是能够对一个固定 Case 可靠回答：**Agent 看到了什么、修改了什么、隐藏测试是否通过、是否产生回归、消耗了多少资源，以及这个结论能否在相同版本上复现。**
