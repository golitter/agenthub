# 17 — 评测体系 v2.1：Orchestrator 编排能力评测与并行收益口径

> **状态**：已实施（离线验收全部通过；在线配对实验待批跑执行）
> **日期**：2026-09-18
> **范围**：仅 `agentend/evals/**`（数据集构建器、批跑驱动、指标、评分、评委）+ 根 Makefile evals 目标 + evals 文档；**不改动 `agentend/src/**` 核心代码**
> **来源**：`待优化.md` 第 1 项（高优先级）全部 + 第 2 项的 2.1/2.2 + 第 3 项工程卫生
> **关联文档**：[评测体系 v2](16-agent-eval-v2-multi-domain-scoring.md)（评分模型）、[Coding Agent 自动化评测体系实施规划](15-coding-agent-evaluation-harness.md)（§9.3 并行收益配对实验、§14.3 多 Agent 端到端验收）、[Orchestrator 冲突恢复](14-orchestrator-conflict-recovery.md)（冲突链路）

## 1. 背景

v2 的 30 条 case 中编码类 fixture 仅 1~2 个文件，任务小到 orchestrator 的最优策略就是"派 1 个实现者或自己干"；批跑指令还进一步压制（"简单任务尽量只派一个实现者"）。任务分解、并行波次调度、多实现者协调、Git 冲突恢复等编排层核心能力完全没有被测量，"orchestrator 相比单 agent 的增益"无数据支撑。

## 2. 数据集 v2.1.0：4 条 `orchestrator` 类 case

`models.py` 的 category Literal 增加 `"orchestrator"`；`scoring.py` 把它并入 `CODING_CATEGORIES`（复用 execution .15 / functional .45 / scope .15 / quality .25 权重），功能证据仍来自 public + hidden command grader——编排 case 的价值在分解，判分不依赖编排事实。

### 2.1 Case A：可分解型（测并行收益）

- `orch-parallel-001`（订单汇总管道）：`modules/pricing.py` / `modules/shipping.py` / `modules/tax.py` 三个互不依赖的模块（各含一个缺陷函数 + 一个 `NotImplementedError` 函数），`aggregator.py` 是依赖三者的新契约 `order_total()`（五步组合 + 逐位舍入）。
- `orch-parallel-002`（文本索引管道）：`modules/tokenizer.py` / `modules/stemmer.py` / `modules/scorer.py` + `aggregator.search()`（分词→停用词→词干→计数→排序）。

关键性质：三模块互不 import → orchestrator 可以自然拆成并行波次，aggregator 依赖三者构成集成点。scope 放宽到 `modules/** + aggregator.py`，`max_changed_files=12`。

**规格反猜测**：prompt 逐函数给出完整规格 + 期望 I/O 示例（数值均手算验证避开 .xx5 舍入中点），hidden check 断言边界行为（异常、未知地区、舍入）与 e2e 组合。`orch-parallel-002` 的 stemmer 规格显式写明折叠规则（"去 'ing' 得 'runn'，折叠双写 n"）——gold 验证发现规格若只写"去后缀"则 `'running'→'runn'` 与期望 `'run'` 矛盾，规格已补全。

### 2.2 Case B：冲突型（测 Resolver）

- `orch-conflict-001`（注册表 TTL + 标签）：两项能力（`features/ttl.py` 过期、`features/tags.py` 标签规范化）都要扩展 `shared/registry.py` 的 `register` 与查询逻辑——同一函数的相邻区域。
- `orch-conflict-002`（配置中心 env 展开 + 类型化读取）：两项能力都要扩展 `shared/config.py` 的 `get` 读取逻辑。

两个实现者并行改同一共享文件 → 真实 Git 冲突 → 冲突检测 → Resolver → 合并后 hidden check 全过。与 §14.3 的验收链路一致。

### 2.3 离线验收

`make evals build-dataset` → 34 case / v2.1.0；`make evals validate` 通过；`make evals baseline --summary` 34/34 valid（新 case 满足 fail-before：fixture 带缺陷/`NotImplementedError`，public 与 hidden 均 failed）；`evals/tmp/verify_v2_gold.py` 补齐 4 条 gold patch 后全部 `[OK]`（pre 双 failed → post 双 pass）。

## 3. 批跑指令分化与串行对照臂（`batch.py`）

`_build_instruction(prompt, category, arm)`：

| 类别 | 指令 |
|---|---|
| chat / knowledge_qa / no_op | 自己直接回答，不派实现者，不改仓库 |
| coding（其余） | 简单任务尽量只派一个实现者（维持 v2 口径） |
| orchestrator + `--arm parallel`（默认） | 中性："按你认为合理的方式分解并执行；任务结构允许时可以并行派多个实现者" |
| orchestrator + `--arm serial` | "一次只派一个实现者……禁止同时派多个实现者" |

串行对照不通过配置开关实现（`agentend/src` 的 OrchestratorConfig 无并行度字段，改核心超出本设计范围），而是指令臂：记录在 `ExperimentSnapshot.prompt_revision = agenthub-agent-v2-{arm}` 与 `max_parallelism`（serial=1，parallel=4）。所有臂共享统一头（只可调度 Claude Code / OpenCode / Pi，禁止调度 Codex）与反作弊尾（不得寻找或访问隐藏测试）。

## 4. 编排事实采集（轮询驱动，零核心改动）

Backend 会把编排事件压平成 orchestrator 消息内容里的 legacy 标记（`backend/internal/stream/writer.go`）：`\ntype: plan\njson: {...}\n` 与 `\ntype: runtime_status\njson: {...}\n`。批跑驱动是纯轮询方，标记是 plan/task/conflict 事实的唯可得来源：

- `orchestration_facts(text)` 解析标记 → `plan_task_ids` / `plan_task_count` / `dispatched_implementers` / `retry_count`（每 task 的 attempt 最大值求和）/ `subtask_final_status` / `conflict_chains`（按 conflict_id 去重连续状态）→ `conflict_count`、`conflict_recovery_count`（链中出现 resolving）、`integration_conflict_seen`、`resolution_completed_seen`（链尾 completed）。
- 并行度来自轮询循环内的 session 状态采样：每 5s GET task detail，非 orchestrator 会话处于 `running|resolving` 计入 active → `max_concurrent_implementers` 与 `implementer_sessions_engaged`。
- 时长来自 Run status 的 `started_at`/`finished_at`（Go RFC3339Nano，`_parse_timestamp` 兼容 Z 后缀与 >6 位小数秒）→ `duration_seconds`。
- 展示文本（`final_text` / `transcript_text`）剥离标记后截断，送 LLM 评委。

`run_facts` → `coordinator._aggregate_result` 透出 `plan_task_count` / `implementer_count` / `max_concurrent_implementers` / `conflict_count` / `integration_conflict_seen` / `resolution_completed_seen`（既有 `conflict_recovery_count` 之后）。

## 5. 并行收益指标（`metrics.parallel_speedup`）

对齐设计 15 §9.3：

- 仅统计 orchestrator 类 case；并行臂存在并发事实时按 `max_concurrent_implementers >= 2` 过滤分支资格（无事实时回退 category_only）；
- 按 `(case_id, repetition)` 配对，case 内取重复中位耗时，`speedup = 串行中位 / 并行中位`；
- 跨 case 几何平均（非正比率回退中位数），case 级 bootstrap 百分位 CI；
- 质量护栏：并行臂 `score_percent` 均值降幅 ≤ 容忍点数（默认 5）且 `task_success` 率不降才 `reportable`，否则给出 `not_reportable_reason`。

CLI：`python -m evals.cli speedup --serial <dir> --parallel <dir> [--score-tolerance N] [--output f.json]`（目录或 results.jsonl 均可；不可报告时 exit 1）。Makefile：`make evals speedup ARGS="--serial ... --parallel ..."`。

### 5.1 重复次数与聚合口径

`batch.py --repetitions N`：pending 按 `(case_id, repetition)` 去重（`_completed_keys`），断点续跑天然幂等（trial_id = sha256(experiment+case+repetition)）。`aggregate_trials` 改为先 case 内求均值再跨 case（`overall_score_percent` 与 `category_scores.mean_score`；无 case_id 的行回退 `__row_N` 保持旧行为）——同 case 重复不独立（设计 15 §9.4）。

## 6. 评委身份入证据（`graders/llm_quality.py`）

`PROMPT_VERSION = "1.1.0"`；`judge_identity(client)` 读 `client.model`（缺省 `injected-stub`）。PASSED 与 ERROR 两条路径的 `evidence_digest` 输入均加入 `judge_model` / `judge_prompt_version`，summary 头部 `[judge=<model> prompt=<version>]`——评委快照漂移后历史结果可追溯。

## 7. 工程卫生

- `runner.py` `EvalRunTimeout` → `EvalRunTimeoutError`（ruff N818）；
- `cli.py` import 排序修正（I001）；
- `agentend/pyproject.toml` 增加 `[tool.pytest.ini_options]`（`testpaths=["tests"]` + `norecursedirs=["evals/tmp", ...]`）：裸 `pytest` 此前会被 `evals/tmp` 历史批跑工作区里的重名 `test_solution.py` 卡在收集阶段，现在 `uv run pytest` 与 `uv run pytest tests/` 等价（284 passed）；
- `batch.py` 残留目录防护提示保留在操作注意：批跑从 `agentend/` 目录启动或使用绝对 `--output`。

## 8. 验证记录（2026-09-18）

| 项 | 结果 |
|---|---|
| `make evals validate` | 34 case，v2.1.0，digest 一致 |
| `make evals baseline --summary` | 34/34 valid（4 条新 case fail-before 成立） |
| `evals/tmp/verify_v2_gold.py` | 全部编码 case `[OK]`（pre failed → gold 后 pass，双 grader） |
| `uv run pytest -q tests/evals` | 68 passed（新增编排标记解析、指令分化、去重、speedup、评委身份共 10 个用例） |
| `uv run pytest`（agentend 全量） | 284 passed（新增 `testpaths` 配置后裸 pytest 不再扫入 `evals/tmp`） |
| ruff | 变更文件无新增告警（数据集构建器存量 E501 与 HEAD 持平） |
| 既有 30 case 评分口径 | 不受影响（CODING_CATEGORIES 追加不改动既有类别权重；聚合对无 case_id 行为回退兼容，旧用例继续通过） |

## 9. 在线配对实验（待执行的操作步骤）

前置：eval 拓扑 Backend（`SERVER_PORT=38080` / `AGENTEND_PORT=38081`）+ `DS_API_KEY` 就绪。

```bash
make evals batch ARGS="--arm serial   --repetitions 3 --output agentend/evals/tmp/batch-serial"
make evals batch ARGS="--arm parallel --repetitions 3 --output agentend/evals/tmp/batch-parallel"
make evals speedup ARGS="--serial agentend/evals/tmp/batch-serial --parallel agentend/evals/tmp/batch-parallel"
```

验收（对应原待优化第 1 项验收标准）：

- [ ] 批跑 run 事件观察到 ≥2 实现者并行（`max_concurrent_implementers >= 2`）；
- [ ] 冲突型 case 产出 `integration_conflict` → `resolution_completed` 链（`conflict_chains`）且 hidden 测试通过；
- [ ] speedup 及置信区间可输出（`reportable: true`）；
- [x] 既有 30 case 评分口径不受影响（离线已验证）。

## 10. 未包含（留在待办）

Agent-as-a-Judge 原型（原第 2.3 项）：前置的确定性结构压缩（`evals/transcript.py`）与 `rerun_graders` 离线 A/B 通路已具备，待单发评委 vs agent 评委对照数据后决定是否转正。
