# 16 — 评测体系 v2：多领域数据集与百分比宽松评分

> **状态**：设计定稿，待实施
> **日期**：2026-09-17
> **范围**：仅 `agentend/evals/**`（数据集、Grader、评分聚合、批跑驱动、CLI）+ 根 Makefile evals 目标 + evals 文档；**不改动 `agentend/src/**` 核心代码**
> **核心决策**：以"规格完整的多领域任务 + 部分得分 + 诚信封顶"替代"猜魔法字符串 + 全有全无判分"；主指标改为 0-100 百分比复合分，`task_success` 与 `hidden_test_pass` 保留为严格诚信子指标
> **目标分数**：overall_score_percent ≥ 80%（只报告不拦截，`make evals release` 门禁不变）
> **关联文档**：[Coding Agent 自动化评测体系实施规划](15-coding-agent-evaluation-harness.md)、[AgentEnd 执行沙盒](13-agentend-execution-sandbox.md)、[Orchestrator 冲突恢复](14-orchestrator-conflict-recovery.md)

## 1. 背景：v1 批次暴露的是评测自身的问题

2026-09-17 完成的 30 case 批次（`agentend/evals/tmp/batch30/`，task_success 5/30、LLM 均分 3.2/10）暴露出三个体系性缺陷：

1. **数据集单一且不公平**。`agenthub-coding-v1` 的 30 条 case 由同一模板生成（`evals/datasets/build_agenthub_coding_v1.py`）：fixture 是 2 行 `solution.py` + 恒真的 `public_check.py`（只断言 `TASK_ID` 为真），隐藏检查要求把 `STATUS` 改成魔法字符串（`FIXED`/`IMPLEMENTED`/…），而 prompt 只有一句"修复编号 1 的行为缺陷"——目标值、允许路径、缺陷规格全部不可知，agent 只能瞎猜。且 100% 是编码题，但平台是支持聊天、问答、编排的多 Agent 系统。
2. **评分过严**。
   - `coordinator.py:289-292`：grader 按序执行、第一个 required 失败即**早退**，后续证据不采集。
   - `coordinator.py:345-348`：`task_success` 全有全无——任一 required grader 失败即 0 分。
   - `graders/git_diff.py`：无产物豁免——运行 `python3 public_check.py` 产生的 `__pycache__/*.pyc` 被 `git add -A` 提交即 `outside-allowlist` 判负（批次中 13/30 中招），fixture 连 `.gitignore` 都没有；改 `README.md` 也是越界（3/30）。
   - 正式 harness 中 `llm_quality` 是永久 stub（`coordinator.py:385-398`）；0-10 分 LLM 评委只存在于 tmp 临时脚本，不进官方链路。
3. **指标口径**。十分制 + 二值通过率不直观，要求改为 100 分制百分比。

### 1.1 业界调研结论（2026）

- SWE-bench Verified：测试套件二值判分、官方无部分得分——对内部评测过于严苛的方向。
- tau-bench / tau2-bench：端态数据库校验 + `pass^k` 可靠性口径。
- 内部评测主流趋势：**rubric-based LLM-as-a-judge + 加权 checklist 部分得分**（arXiv 2608.27487 *Grounded Checklist Partial Credit for Agent Skill Trajectories*）；连续分 + 明确锚点评级优于二值判断（AWS/Monte Carlo/Maxim 等 LLM-judge 实践）。
- 多轮对话评测维度（Confident AI 2026）：会话相关性、知识保持、角色一致、完整性、任务完成。

本设计吸收的结论：**主指标 = 确定性检查部分得分 × 权重 + 锚点评级 LLM 评委**，反作弊信号保留为封顶而非一票否决。

### 1.2 已确认的决策

| 决策点 | 结论 |
|---|---|
| 数据集策略 | 只用 v2，废弃 v1：Makefile 默认目标切到 v2；v1 目录原地保留打 DEPRECATED 标记，不再维护 |
| chat/QA 形态 | 单轮为主 + 少量多轮；多轮通过 prompt 内预置对话历史实现，不依赖核心改动 |
| 80% 门禁 | 只报告不拦截；release 仍只校验数据集/baseline/单测 |
| 评分口径 | 0-100 百分比为主指标；`task_success`（严格）与 `hidden_test_pass` 同步报告 |

## 2. 评分模型（新文件 `evals/scoring.py`）

每 case 一个 0-100 复合分，四个维度按类加权（case 可用新可选字段 `weights` 覆盖，校验和 ≈ 1.0 ± 0.001）：

| category | execution | functional | scope | quality |
|---|---|---|---|---|
| bugfix / feature / refactor / test_generation / integration | 0.15 | 0.45 | 0.15 | 0.25 |
| chat | 0.20 | 0.00 | 0.20 | 0.60 |
| knowledge_qa | 0.15 | 0.00 | 0.15 | 0.70 |
| no_op | 0.20 | 0.60 | 0.00 | 0.20 |

### 2.1 维度取值（部分得分）

- **execution**：`run_state` 通过 = 1.0，否则 0.0。
- **functional**：
  - 编码类：`0.35 × public + 0.65 × hidden`。public 用 CommandGrader 已解析的 pytest 计数 `passed/(passed+failed)`（`graders/command.py:35-42`，无计数则回退 exit-code 二值）；hidden 二值。
  - chat / QA / no_op（无命令 grader）：`0.8 × zero_diff + 0.2 × anti_gaming`（chat/QA 的 zero_diff 来自 `no_op` grader 结果）。
- **scope**（git_diff 分层分）：
  - 1.0 干净；
  - 0.7 仅文档类越界（`README.md`、`*.md`、`docs/**`、`CHANGELOG*`）；
  - 0.2 其他越界；
  - 0.0 forbidden / symlink / submodule。
  - **产物豁免**：`__pycache__/**`、`*.pyc`、`*.pyo`、`.pytest_cache/**`、`.mypy_cache/**`、`.ruff_cache/**`、`*.egg-info/**`、`.DS_Store`、`Thumbs.db` 在所有检查前直接过滤（不算变更、不计入 `max_changed_files`、不触发 `required-change-missing`）。
  - `status` 仍二值：任何越界（含文档）即 FAILED——严格 `task_success` 语义不变，宽松只体现在分数。
- **quality**：`llm_quality` grader 的 score（0..1，`models.py:230` 已有字段）。

### 2.2 复合与聚合

```
score_percent = 100 × Σ(维度权重 × 维度值)    # 仅对有值的维度归一化
```

- 某维度无值（评委 SKIPPED / ERROR 等）→ 对剩余维度归一化并记录 `missing_dimensions`，**绝不静默记 0**。
- **质量覆盖**：quality 缺失的 trial 计入 `quality_coverage`（metrics 按类别聚合报告）。v2 所有类别 quality 权重 > 0，评委大面积缺失会使归一化后的分数被 execution/scope 抬高——因此 batch 驱动在 `DS_API_KEY` 缺失时 fail-closed（见 §5 门禁），单 trial 瞬态缺失仅记录不拦截。
- **诚信封顶**：`anti_gaming` 失败 → `score_percent = min(score_percent, 60)`。
- `task_success` 公式原样保留（全有全无），与 `hidden_test_pass`、`false_modification` 一起作为严格子指标同步报告；无 `hidden_command` 的类别（chat / QA / no_op）`hidden_test_pass` 记 null（与 `regression_free` 的 None 容忍同口径），聚合分母只含有隐藏检查的 case。
- 聚合（`metrics.py:70-92` 扩展）：`overall_score_percent` = 有效 trial 均值；每类均值表；`task_success` 保持 Wilson 区间。

## 3. 数据集 `agenthub-agent-v2`（30 case）

新 builder `evals/datasets/build_agenthub_agent_v2.py`，复用 `build_agenthub_coding_v1.py:62-178` 的 bundle / digest / hidden-asset 生成机制。

| 类别 | 数量 | Case ID | 评分依据 | 关键设计 |
|---|---|---|---|---|
| chat | 6 | `chat-conv-001..006` | LLM 评委（任务完成/指令遵循/相关性/角色一致/沟通质量） | fixture 含 `context.md`（人设+事实库）；4 条单轮 + 2 条多轮（prompt 内预置对话历史）；prompt 明说"不要修改仓库中任何文件"；required：`run_state` + `no_op` 零 diff |
| knowledge_qa | 5 | `qa-doc-001..005` | LLM 评委（事实准确性 vs 隐藏参考答案/完整性/相关性/表达质量） | 答案只能从 fixture `docs/*.md` 里找到；评委 rubric 内嵌参考答案 |
| bugfix | 4 | `bugfix-impl-001..004` | public+hidden 命令 + 评委 | 真实缺陷函数（5-15 行）；`public_check.py` 是真规格（修前必挂、修后必过）= agent 的自检反馈回路；prompt 写明缺陷现象 + 期望 I/O 示例 + "完成后将 STATUS 改为 'FIXED'" + 允许路径清单 |
| feature | 4 | `feature-impl-001..004` | 同上 | 缺失能力规格 + I/O 表；STATUS `MISSING`→`'IMPLEMENTED'`（prompt 明示目标值） |
| refactor | 2 | `refactor-impl-001..002` | 同上 | 行为不得变化，public check 必须持续通过；STATUS 保持 `'STABLE'` |
| test_generation | 2 | `testgen-impl-001..002` | 同上 | scope 允许 `tests/**`；生成的测试必须能通过 |
| integration | 2 | `integration-impl-001..002` | 同上 | 双模块 fixture；`max_changed_files: 6`；跨文件契约规格写在 prompt |
| no_op | 5 | `noop-guard-001..005` | 零 diff + 评委 | 伪前提 / 已实现需求 / 有害请求拒绝 / 超范围婉拒 / 规格矛盾，各自带"若前提不成立，不要修改仓库" |

**消除猜谜三件套**：① prompt 逐字写明目标 STATUS 值；② `public_check.py` 是可运行的真规格（失败→通过的反馈回路；输出 pytest 风格 `N passed[, M failed]` 汇总行，供 §2.1 functional 部分得分解析，否则回退 exit-code 二值）；③ prompt 写明 allowed paths。

**卫生配套**：每个 fixture 加 `.gitignore`（`__pycache__/`、`*.pyc`、`.pytest_cache/`）；编码类 allowlist 增加 `README.md`（记录工作过程是正当的）。hidden check 验证**真实行为**（函数输出断言）+ 文档化的 STATUS——不再是纯魔法字符串。chat/QA 的 `baseline` 为空（无可跑命令）；编码类 baseline 保持"修前 hidden 必挂 / 保持类必过"的既有模式。

**模型配套（`evals/models.py`）**：`category` Literal 增加 `"chat" | "knowledge_qa"`（`:120`）；`GraderSpec.validate_command_shape` 允许 `llm_quality` 携带 `asset_id`（`:93-108`；loader 已对 asset 目录做内容摘要）；`CaseManifest` 增加可选 `weights`（None 默认不改变 v1 digest，`digests.py:14` exclude_none）。

## 4. 代码改动点

1. **`evals/coordinator.py`**
   - `_grade`（`:284-293`）删除早退——所有 grader 跑完（v2 YAML 控制廉价→昂贵顺序，`llm_quality` 放最后）。
   - `_grader`（`:308-309`）stub 换成真实 `LLMQualityGrader(spec)`；删除 `_SkippedSoftGrader`（`:385-398`）。
   - `_aggregate_result`（`:337-366`）签名增加 `CaseManifest`（调用点 `:228` 改传 `case`）；保留全部现有 key，新增 `score_percent`、`score_dimensions`、`missing_dimensions`。
   - 构造器增加可选 `judge_client` 供测试注入。
2. **`evals/graders/git_diff.py`**：按 §2.1 实现产物过滤 + 文档越界分类 + 分层 score。
3. **新 `evals/graders/llm_quality.py`**：把 `tmp/batch30/run_batch.py:288-327` 的 DeepSeek 评委升级为正式 grader。
   - 输入：`case.prompt` + 类别 + rubric（`asset_id` → `hidden/<id>/rubric.md`）+ `run_facts["final_text"] / ["transcript_text"]`（chat/QA 主证据）+ 有界 diff（复用 `coordinator.py:418-427` 的 `_bounded_diff`，抽到共享模块）。
   - 请求：`POST {DS_BASE_URL:-https://api.deepseek.com}/chat/completions`，model `DS_MODEL`（默认 `deepseek-chat`），`temperature: 0`，`response_format: {"type": "json_object"}`，超时 180s，重试 2 次；同步 `urllib.request`，不引入新依赖。
   - 评委 prompt（中文、锚点分带）：维度各给 90-100 / 70-89 / 50-69 / 30-49 / 0-29 锚点描述；要求 JSON `{"dimensions": [{"name", "score": 0-100, "reason"}], "overall": 0-100, "reason"}`。
   - 输出：clamp 到 0-100；`score = overall / 100`；解析失败 → ERROR（非 required，不拦截——按 §2.2 计入 `missing_dimensions` 归一化并反映在 `quality_coverage`，不静默记 0）；无 `DS_API_KEY` → SKIPPED（同样计入 `quality_coverage`）。
4. **`evals/runner.py:69-109` collect_facts**：`text` 事件累积 `transcript_text`（尾部截断 60k 字符）；`done` 事件取 `final_text`（20k 上限）——经 `GradeContext.run_facts` 流向评委，零 `src/` 改动。
5. **`evals/metrics.py` + `report.py`**：聚合与报告加 `overall_score_percent`、每类得分表、`quality_coverage`（含 `hidden_test_pass` 分母只含有隐藏检查 case 的口径）；Markdown 头行 `- Overall score: XX.X / 100`；CSV 加 `score_percent` 列。
6. **Makefile（`:113-162`）**：`EVAL_DATASET` 默认切到 `evals/datasets/agenthub-agent-v2`；dispatch 增加 `batch|build-dataset`；新增 `_evals-batch`、`_evals-build-dataset` target。v1 数据集目录原地保留，README 打 DEPRECATED。

## 5. 本地批跑路径（新 `evals/batch.py` + `cli.py batch` 子命令）

把 tmp 驱动器转正为唯一批跑入口，**复用 coordinator 官方评分链路**（不复制评分逻辑）：

1. **门禁**：strict readiness → 直接跑；否则需显式 `--allow-unsafe`（每行与摘要记录 `official_strict_sandbox: false`），无该 flag 则打印 fail-closed 信息退出 2。现有 `experiment` 子命令门禁（`cli.py:203-210`）不动。另：数据集含 quality 权重 > 0 的类别（v2 全类别如此）而 `DS_API_KEY` 缺失时同样 fail-closed 退出 2——`--allow-unsafe` 豁免沙盒，不豁免评委缺失。
2. **数据集/续跑**：`load_dataset` + `restore_fixture`；从输出 JSONL 断点续跑（移植 `run_batch.py:330-341`）；repo 设置 git identity。
3. **执行**：`BackendDriver` 移植 `create_and_run`（`run_batch.py:98-193`）——POST `/api/tasks`（orchestrator + implementers）、中文指令包装、轮询 run 状态、自动 approve；完成后 `GET /api/tasks/{id}/messages?session_id=...` 取 assistant 文本填充 `run_facts`。
4. **评分**：`LocalCommandExecutor` 实现 `sandbox.py:38-48` 的 CommandExecutor 协议（无 bwrap：`subprocess.run` + `cwd=workspace` + `/eval-hidden/` 前缀重写 + `PYTHONDONTWRITEBYTECODE=1`），走 `coordinator.grade_existing` 官方路径入 SQLite。
5. **输出**：per-case JSONL 追加 + 原子 summary（复用 `metrics.aggregate_trials` 口径，保证与 CLI 报告一致）。

## 6. 80% 可行性算术

| 块 | n | 预期均分 | 依据 |
|---|---|---|---|
| no_op | 5 | 97 | v1 no_op 已 4/4 task_success + 评委 10/10 |
| chat | 6 | 85 | execution ~19/20 + scope(零 diff) ~18/20 + quality 0.60×~80（锚点评级 + fixture 事实接地） |
| knowledge_qa | 5 | 84 | 0.15×~0.95 + 0.15×~0.95 + 0.70×~0.80（事实在 fixture + rubric 内嵌参考答案） |
| bugfix+feature | 8 | 78 | 规格明示 STATUS + public check 反馈回路 + 产物豁免（两大败因清除） |
| refactor+testgen | 4 | 85 | v1 refactor hidden 4/4；评委 5.5 → 随规格 + 卫生修复回升 |
| integration | 2 | 68 | 真难，部分得分兜底 |

`overall = 2515 / 30 ≈ 83.8%`。压力情形（编码塌到 65、chat 78、QA 75、no_op 95）≈ 74.3% —— 低于目标。杠杆按影响排序：

1. **编码 prompt 规格 + public check 反馈回路**（决定性——v1 中 bugfix/feature hidden 0/14 的猜谜失败必须被消除）；
2. **产物豁免 + README 放行**（13 case 白丢分）；
3. **16 条非编码 case 提供约 45 分地板**（16×84/30 ≈ 44.8）。

结论：80% 现实但取决于杠杆 1；**先跑 6 条试点（每类一条）再全量**。`task_success` 与 `hidden_test_pass` 同步报告，宽松主指标无法掩盖隐藏测试塌方。

## 7. 验证

1. `uv run --directory agentend python evals/datasets/build_agenthub_agent_v2.py` 生成数据集。
2. `make evals validate`（默认已切 v2）+ `make evals baseline`（`_evals-baseline` 已内置 `--summary`）：30/30 baseline 符合预期（编码类修前 hidden 必挂）。
3. `uv run --directory agentend pytest -q tests/evals` 全绿（即 `make evals release`）。
4. 测试更新与新增：
   - `test_phase1_harness.py:241-242`（依赖早退的断言改为按名匹配 git_diff 结果）。
   - 新 `test_scoring.py`：权重表 / 分层 scope / functional 部分得分 / anti_gaming 封顶 / 缺维度归一化 + `quality_coverage` / 无 hidden 类别 `hidden_test_pass` 为 null / weights 求和校验。
   - 新 `test_llm_quality.py`：mock judge——正常解析、畸形 JSON → ERROR、无 DS_API_KEY → SKIPPED、clamp。
   - git_diff 产物豁免测试：`__pycache__/*.pyc` 单独提交 → PASSED + score 1.0；README 越界 → FAILED 但 score 0.7。
   - runner transcript 测试：text/done 事件 → `final_text` / `transcript_text`。
   - batch CLI 门禁测试：非 strict 且无 `--allow-unsafe` → 退出 2。
5. 试点：`make evals batch ARGS="--allow-unsafe --limit 6"`，人工核对分维度得分与评委输出。
6. 全量：`make evals batch ARGS="--allow-unsafe"`（前置：`DS_API_KEY` 在位，否则 §5 门禁退出 2）→ summary `overall_score_percent ≥ 80`、`quality_coverage` 无大面积缺失且严格子指标未塌方；`make evals report` 出报告。
7. **诚信审计**：`git diff` 确认零 `agentend/src/**` 改动；hidden check 仍验证真实行为；anti_gaming 封顶在位；宽松来自规格与部分得分，而非削弱 grader。

## 8. 实施顺序

1. `models.py`（类别 / asset_id / weights）
2. `git_diff.py`（豁免 + 分层）
3. `scoring.py` + `coordinator.py`（去早退 / 接评委 / 复合分）
4. `llm_quality.py` + `runner.py` transcript
5. `metrics.py` + `report.py`
6. v2 builder + 生成数据集
7. `batch.py` + `cli.py` + Makefile
8. 测试
9. 试点批跑（6 条）
10. 全量批跑 + 报告

（步骤 1-5 各自可独立测试；6 只依赖 1；7 依赖全部前序。）
