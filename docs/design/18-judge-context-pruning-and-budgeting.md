# 18 — LLM 评委上下文裁剪与 Token 预算实施规划

> **状态**：实现完成（预算、证据保护、System One 适配、可观测性与单元测试已落地；真实 API shadow/A/B 校准待执行）
> **日期**：2026-09-21
> **范围**：`agentend/evals/**` 的 LLM 评委证据构建、上下文估算、Jev 相关性筛选、确定性预算与可观测性；不修改 Agent 运行时主链路，不改变确定性 grader 与现有评分权重
> **核心决策**：Jev 只负责对非关键证据做相关性排序，最终是否能发送由本地确定性预算器决定；token 使用保守估计值，不引入模型 tokenizer 依赖
> **关联文档**：[评测体系 v2](16-agent-eval-v2-multi-domain-scoring.md)、[Orchestrator 编排评测](17-agent-eval-orchestration-capability.md)、[Coding Agent 自动化评测体系](15-coding-agent-evaluation-harness.md)

## 1. 背景与问题

当前 `llm_quality` 评委把以下内容拼成两条消息后直接发给 DeepSeek：

- system：评分维度、锚点、隐藏 rubric、JSON 返回格式；
- user：case prompt、最终回复、过程 transcript、Git diff。

现有保护主要是字符截断：rubric、最终回复、transcript、diff 各自最多约 20,000 字符。它能限制单字段大小，但不能保证完整请求处于模型上下文窗口内，也存在三类质量问题：

1. **各字段独立截断，没有统一预算**：system、prompt、JSON 序列化与消息开销未计入总量。
2. **截断不理解证据语义**：固定保留 transcript 尾部可能删除更早的失败、重试、越界派发或冲突证据。
3. **模型请求缺少显式输出预算**：当前 Judge 请求没有设置 `max_tokens`，输入和输出之间没有稳定的容量边界。

Jev 可以判断某段工具调用或结果是否值得保留，但 Jev 的阈值是相关性概率，不是 token 配额。因此它不能单独提供“必然不超上下文”的保证。本设计将两者分工：

```
原始评测证据
  → 语义分块与关键证据保护
  → 超过触发线时，Jev 仅筛选非保护块
  → 本地确定性预算器按优先级装箱
  → 对完整请求重新估算
  → Judge 请求
  → 若服务端仍报告上下文超限，缩小目标并重建一次
```

这里的“保证”是工程意义上的：正常路径通过保守估算和安全余量避免超限；估算仍可能与服务端 tokenizer 有偏差，因此必须以“重建后重试一次，仍失败则显式 ERROR”收口，绝不静默删除关键证据或无限重试。

## 2. 历史日志实验结论

使用已保存的真实 Run 日志重建 Orchestrator 与子 Agent 的 LLM 间证据，并对原始输入、Jev `0.3`、Jev `0.5` 三种输入重新调用现有 DeepSeek 评委。实验只向 Jev 发送运行证据与问题描述，隐藏 rubric 不发送给 Jev。

### 2.1 代表性结果

| Case | 原始 Judge prompt tokens | Jev 0.3 | Jev 0.5 | 原始/0.3/0.5 得分 |
|---|---:|---:|---:|---:|
| `orch-conflict-001` | 8,650 | 6,453 | 3,502 | 90 / 92 / 89 |
| `noop-guard-005` | 8,806 | 1,180 | 1,180 | 90 / 88 / 89 |
| `qa-doc-001` | 7,386 | 997 | 997 | 88 / 83 / 88 |
| `bugfix-impl-002` | 6,568 | 3,959 | 1,775 | 90 / 95 / 90 |

四个 case 汇总：

- 原始 Judge 输入：31,410 tokens；
- Jev 0.3：12,589 tokens，Judge 输入减少 59.9%；
- Jev 0.5：7,454 tokens，Judge 输入减少 76.3%；
- 计入 Jev 自身估算输入后，0.3/0.5 的净输入节省约为 7.2%/23.5%。

另一个 `orch-parallel-001` 样本中，Jev 0.5 将 5 个子 Agent 调用与结果全部删除，Judge 输入由 14,101 降至 6,976 tokens，得分由 91 变为 92；但原始判词中关于集成记录失败和人工同步的负面细节在裁剪后消失。

### 2.2 对设计的约束

1. **不能直接采用 Jev 默认阈值**。在已测样本中，0.5 共删除 32/32 个子 Agent 调用；0.3 在 no-op/QA 中同样会删除全部派发证据。
2. **分数接近不代表证据无损**。总体分可能稳定，但判词依据已经改变，特别是失败过程与越界派发。
3. **Judge 自身存在波动**。完全相同的 0.3 与 0.5 输入在个别 case 上仍相差最多 5 分；正式 A/B 不能用单次评分判定裁剪质量。
4. **中等输入不应默认调用 Jev**。7k～14k token 的样本计入 Jev 成本后净收益有限，还引入外部调用、延迟与偏差。Jev 只在接近预算触发线时启用。

## 3. 目标与非目标

### 3.1 目标

- 用估计 token 控制完整 Judge 请求，而不是分别控制字段字符数；
- 在上下文较长时减少重复、进度类和低价值工具输出；
- 错误、失败、测试、越界、冲突等负面证据不可被 Jev 删除；
- 裁剪过程可审计、可复现，能够解释保留和删除了什么；
- 服务端上下文超限时自动缩小并重建一次，避免原样重试；
- 不改变现有评分维度、权重和隐藏 rubric 的信任边界。

### 3.2 非目标

- 不承诺估计值与服务端实际 tokenizer 完全一致；
- 第一阶段不实现多轮 map-reduce Judge；
- 不把 Jev 引入 Agent 运行时或普通聊天链路；
- 不让 Jev 查看隐藏 rubric、隐藏测试或密钥；
- 不使用裁剪掩盖评测失败，也不修改确定性 grader 的判定。

## 4. 配置设计

新增 Judge 上下文配置，建议使用显式部署配置，不根据模型名称猜测上下文窗口：

```yaml
judge_context:
  context_window_tokens: 65536
  output_reserve_tokens: 2048

  estimator: weighted_chars_v1
  estimator_safety_factor: 1.15
  safety_margin_ratio: 0.10
  min_safety_margin_tokens: 2048

  prune_trigger_ratio: 0.70
  prune_target_ratio: 0.55

  jev_enabled: true
  jev_keep_threshold: 0.30
  jev_result_head_chars: 300

  overflow_retry_ratio: 0.75
  max_overflow_retries: 1
```

约束：

- `context_window_tokens` 必填且大于输出预留与最小安全余量；
- `0 < prune_target_ratio < prune_trigger_ratio <= 1`；
- `overflow_retry_ratio < 1`；
- 关闭 Jev 时，确定性预算器仍然工作；
- `output_reserve_tokens` 同时写入 DeepSeek 请求的 `max_tokens`。

以 65,536 上下文为例：

```
safety_margin = max(65,536 × 10%, 2,048) = 6,554
input_budget  = 65,536 - 2,048 - 6,554 = 56,934
prune_trigger = 56,934 × 0.70 ≈ 39,853
prune_target  = 56,934 × 0.55 ≈ 31,313
```

估算未达到约 39.9k token 时不调用 Jev；触发后将输入压到约 31.3k，为估算误差、请求包装和后续字段变化留下二次余量。

## 5. Token 估算器

新增 `agentend/evals/token_budget.py`，提供 `TokenEstimate`、`ContextBudget` 与 `estimate_messages()`。

### 5.1 `weighted_chars_v1`

对最终序列化请求中的每段文本分类估算：

| 字符类型 | 估算规则 |
|---|---|
| ASCII 字母 | 每 4 字符约 1 token，向上取整 |
| 数字 | 每 2 字符约 1 token，向上取整 |
| 空白 | 不单独计 token |
| CJK、标点、JSON 符号、其他字符 | 每字符 1 token |

然后加入：

- 固定请求开销 256 tokens；
- 每条 message 8 tokens；
- `estimator_safety_factor`，默认乘 1.15；
- 按完整请求计算，包括 system、rubric、用户需求、最终回复、transcript、diff、role 与 JSON response format。

此前 14 次真实 DeepSeek 调用中，简单的 `ceil(chars / 2) + 1024` 已比服务端 `prompt_tokens` 高 26.1%～93.7%，未出现低估。`weighted_chars_v1` 用于减少中英文比例变化造成的偏差，但仍需长期校准。

### 5.2 估算器校准

每次成功调用记录：

- `estimated_prompt_tokens`；
- 服务端返回的 `usage.prompt_tokens`（可用时）；
- `estimate_error_ratio = estimated / actual`；
- `estimator_version` 与 safety factor。

验收时要求校准集不出现低估。线上一旦出现 `estimated < actual`，发出告警并提高 safety factor；历史记录保留估算器版本，避免口径漂移无法追溯。

## 6. 证据分块与硬保护

新增 `agentend/evals/evidence.py`，不再先拼成一整段字符串再裁剪，而是构造语义块：

```python
class EvidenceChunk:
    id: str
    kind: EvidenceKind
    text: str
    protected: bool
    priority: int
    tool_call_id: str | None
    estimated_tokens: int
    jev_keep_probability: float | None
    jev_drop_probability: float | None
```

建议的 `EvidenceKind` 包括：case prompt、rubric、最终回复、Agent 派发、Agent 结果、错误、测试摘要、diff hunk、进度信息、重复信息。

### 6.1 无条件保护规则

以下证据标记为 `protected=True`，Jev 不得处理，预算器也不得静默删除：

- error、timeout、cancelled、failed run、非零退出码；
- public/hidden test 结果与测试失败摘要；
- forbidden path、allowlist 越界、symlink/submodule 等违规 diff hunk；
- Orchestrator retry、集成冲突、Resolver 过程与未完成状态；
- `no_op`、`chat`、`knowledge_qa` 中的任何子 Agent/工具派发——派发本身可能就是负面证据；
- system prompt、隐藏 rubric、case prompt、输出 JSON schema。

### 6.2 默认优先级

| 优先级 | 证据 |
|---:|---|
| 100 | Judge 指令、case prompt、rubric、输出 schema |
| 95 | 错误、失败、超时、违规、冲突、重试 |
| 90 | 测试结果、非零退出、最终运行状态 |
| 80 | Agent 最终回复、关键 diff hunk |
| 60 | 成功的子 Agent 独立结果 |
| 40 | 普通工具/Agent 调用 |
| 20 | 进度播报与中间说明 |
| 0 | 可确认的重复内容 |

优先级解决“什么先进入预算”，`protected` 解决“什么绝对不能被自动删除”，二者不可合并为一个字段。

## 7. Jev 相关性筛选

Jev 只接收未保护块，并采用评测专用问题，而不是通用聊天压缩提示：

- 对调用：**知道这个动作发生过，是否可能改变任一评分维度？**
- 对结果：**其中是否包含无法从最终回复、测试或 diff 恢复的独立评分证据？**

第一阶段默认阈值为 0.30，仅作为候选块的相关性信号。Jev 结果不能覆盖本地保护规则。

处理策略：

- 高于阈值：保留完整块；
- 低于阈值但调用/结果对仍有结构价值：保留调用摘要，将结果截为前 300 字符并加明确标记；
- 明确低价值且非保护：允许整对删除；
- Jev 调用失败、超时或返回异常：fail-open，跳过 Jev，进入本地确定性预算；
- 隐藏 rubric、隐藏测试内容、API key 和环境变量永不发送给 Jev。

调用与结果通过 `tool_call_id` 绑定。除“保留调用摘要 + 截短结果”这一显式状态外，不允许产生孤立 result 或无法解释的半对证据。

## 8. 确定性预算器

新增 `agentend/evals/context_budgeter.py`，负责最终硬边界。流程如下：

1. 计算完整原始请求估算值；低于 `prune_trigger` 时直接发送，不调用 Jev。
2. 超过触发线时，先应用保护规则，再让 Jev 为非保护块产生相关性信号。
3. 所有保护块先入箱；非保护块按以下顺序装箱：
   - priority 降序；
   - Jev keep probability 降序；
   - 单位 token 信息价值降序；
   - 原始时间顺序作为稳定 tie-breaker。
4. 调用/结果按原子组装箱；过大的普通 result 可转为 300 字符摘要状态。
5. 生成最终 system/user messages 与完整 JSON 请求，再次估算。
6. 若仍高于目标，逐个移除最低价值的非保护块并重建，直到 `estimated <= prune_target`。

预算器输出包含最终消息及一份 `BudgetDecision`，记录各块的保留、截短、删除原因。

### 8.1 保护证据本身超限

如果仅保护块就超过输入预算，第一阶段不自动删除，也不伪装成正常评分：

- 抛出 `JudgeEvidenceOverflow`；
- `llm_quality` 返回 `ERROR`；
- summary 标记 `protected_evidence_overflow`；
- quality 维度按现有缺失维度规则处理，并在批次报告中计入覆盖率异常。

后续如该错误频繁出现，再单独设计多阶段 Judge；本计划不在 V1 中引入可能改变评分语义的 map-reduce。

## 9. 服务端超限恢复

`DeepSeekJudgeClient` 需要区分普通 HTTP 400 与明确的 context-length 错误，并让上层执行一次有意义的恢复：

1. 首次请求前设置 `max_tokens = output_reserve_tokens`；
2. 若返回上下文超限，将 `prune_target` 乘 `overflow_retry_ratio`（默认 0.75）；
3. 使用原始语义块重新装箱并生成新请求；
4. 最多重建重试一次；
5. 仍失败则返回 `ERROR`，不得原样重试，不得无限降级。

网络超时、408、429 仍沿用现有有限重试；认证失败、其他确定性 4xx 不进入上下文重建逻辑。

## 10. 可观测性与结果记录

建议为 `GraderResult` 增加可选 `metadata: dict[str, JSONValue]`，至少记录：

```json
{
  "estimator_version": "weighted_chars_v1",
  "estimated_raw_tokens": 52140,
  "estimated_final_tokens": 30880,
  "actual_prompt_tokens": 27120,
  "input_budget_tokens": 56934,
  "prune_trigger_tokens": 39853,
  "prune_target_tokens": 31313,
  "jev_invoked": true,
  "jev_threshold": 0.3,
  "chunks_total": 42,
  "chunks_protected": 9,
  "chunks_kept": 24,
  "chunks_truncated": 4,
  "chunks_dropped": 14,
  "overflow_retries": 0
}
```

以下配置必须进入 `evidence_digest` 或 Judge 身份：估算器版本、安全系数、预算参数、Jev 是否启用及阈值、裁剪决策摘要。这样同一运行证据在不同裁剪策略下不会被误认为同一评委结果。

日志中只记录 chunk id、kind、大小和决策，不复制隐藏 rubric 或完整敏感文本。

## 11. 代码改动清单

1. **新增 `evals/token_budget.py`**
   - `weighted_chars_v1`；
   - 完整请求估算；
   - `ContextBudget` 配置校验与阈值计算。
2. **新增 `evals/evidence.py`**
   - 语义块模型；
   - 类别相关保护规则；
   - transcript、diff、测试与编排事实的分块器。
3. **新增 `evals/jev_client.py`**
   - TypeSafe/Jev 请求适配；
   - 超时、异常 fail-open；
   - 外发字段 allowlist 与响应校验。
4. **新增 `evals/context_budgeter.py`**
   - 保护块优先装箱；
   - Jev 排序与调用/结果完整性；
   - 最终重估和决策审计。
5. **修改 `evals/graders/llm_quality.py`**
   - `_evidence()` 改为构建证据块并调用预算器；
   - Judge 请求添加 `max_tokens`；
   - 捕获 context overflow，缩小目标并重建一次；
   - `PROMPT_VERSION` 与 grader version 升级。
6. **修改 `evals/models.py` 与结果存储/报告**
   - `GraderResult.metadata` 可选字段；
   - JSONL/报告透出裁剪与估算统计，保持旧数据兼容。
7. **配置与文档**
   - 在评测配置示例中增加 `judge_context`；
   - `TYPESAFE_API_KEY` 继续仅从环境读取，不入库；
   - 更新 `agentend/evals/README.md` 的运行与隐私说明。

## 12. 测试计划

### 12.1 单元测试

- 估算器覆盖纯英文、中文、数字、JSON、混合文本与空输入；
- `ContextBudget` 拒绝非法窗口、负余量、target ≥ trigger；
- packer 输出的最终估算始终不超过目标；
- 错误、测试、违规、冲突等保护块全部保留；
- no-op/chat/QA 的派发证据不能被 Jev 删除；
- tool call/result 保持原子关系，截短时有稳定标记；
- Jev 超时、畸形响应时 fail-open；
- 仅保护证据超限时返回 `protected_evidence_overflow`；
- 模拟服务端 context 400：以 75% 目标重建且只重试一次；
- 普通 400 不误判为上下文超限；
- 新配置和裁剪决策会改变 evidence digest；
- 测试全部使用 stub，不调用真实 Jev 或 DeepSeek。

### 12.2 校准与回归实验

- 从历史数据按类别分层抽样，不只测长 Orchestrator case；
- 每个输入臂至少重复 Judge 3 次，报告均值、中位数、标准差；
- 对比 original、deterministic-only、Jev 0.3 + deterministic 三臂；
- 人工审查失败、越界、冲突、无谓派发是否仍出现在判词依据中；
- 记录 Jev 成本、Judge 节省、总延迟和净 token 节省。

不得仅用“裁剪前后单次总分相近”作为通过标准。

## 13. 分阶段上线

### 阶段 A：仅观测，不改变 Judge 输入

- 实现估算器、分块器和预算决策；
- 仍发送原始请求，只记录“如果启用会如何裁剪”；
- 用真实 `usage.prompt_tokens` 校准 safety factor。

### 阶段 B：确定性预算生效，Jev 关闭

- 对超过触发线的输入执行保护 + 本地装箱；
- 验证上下文超限率、quality coverage 与评分漂移；
- 证明不依赖外部相关性服务也能安全降载。

### 阶段 C：Jev shadow

- Jev 产生决策但不影响 Judge 输入；
- 对比其建议与本地保护规则，统计它试图删除关键证据的比例；
- 隐藏 rubric 不外发。

### 阶段 D：Jev 小流量启用

- 仅对超过触发线且非保护证据占比较高的 trial 启用；
- 先采用 0.30 阈值；
- 发现关键负面证据丢失、净 token 收益为负或评分漂移超标时自动回退到 deterministic-only。

## 14. 验收标准

- [ ] 发送前完整请求的估算值不超过当前目标；
- [ ] DeepSeek 请求显式设置输出 token 上限；
- [ ] 校准样本中估算值不低于服务端实际 prompt tokens；
- [ ] 上下文超限时只重建重试一次，仍失败显式 ERROR；
- [ ] 错误、失败、测试、违规、冲突和 no-op/QA 越界派发证据 100% 保留；
- [ ] 隐藏 rubric、隐藏测试与密钥从不发送给 Jev；
- [ ] 每个裁剪结果可追溯到 chunk 级决策和配置版本；
- [ ] A/B 每臂至少 3 次 Judge，评分中位数漂移不超过预先设定阈值（建议 3 分）；
- [ ] 长上下文样本获得正向净 token 节省；中等输入不会无条件调用 Jev；
- [ ] `quality_coverage` 不因裁剪错误显著下降；
- [ ] `uv run --directory agentend pytest -q tests/evals` 全绿。

## 15. 实施顺序

1. token 估算器、配置模型与校准元数据；
2. 证据语义分块和保护规则；
3. deterministic-only 预算器与保护证据超限错误；
4. Judge `max_tokens`、上下文错误识别与重建重试；
5. Jev client、外发 allowlist 与 shadow 决策；
6. 历史日志三臂重复实验；
7. 小流量启用与报告指标；
8. 达到验收标准后再考虑默认开启。

第一阶段最重要的交付不是“接入 Jev”，而是建立统一、可审计的 Judge 上下文预算。Jev 是可插拔的优化层；即使它关闭或不可用，评测也必须保持有界、可解释并安全失败。
