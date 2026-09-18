# Eval Admin — Agent 评测管理页面

## 实现了什么

管理面板第 8 个模块（`/admin/evals` 路由，`ADMIN_PAGES.evals` → `EvaluationsPage`）：浏览 Coding Agent 评测数据集与实验指标、下钻 Trial 明细、成对对比两次实验、对 Trial 提交人工复核结论。指标展示遵循「失败不静默移除出分母」的口径：成功率带 Wilson 95% 置信区间，invalid trial 单独标记而非剔除。

## 怎么实现的

### 页面数据流 (`src/pages/admin/EvaluationsPage.tsx`)

四个 React Query 查询 + 一个 mutation，queryKey 均为页面内联 key（未进 `lib/query-keys.ts` 的 `queryKeys`）：

```tsx
const datasets = useQuery({ queryKey: ['eval-datasets'], queryFn: getEvalDatasets })
const experiments = useQuery({ queryKey: ['eval-experiments'], queryFn: getEvalExperiments })
const activeId = selectedExperimentId || experiments.data?.[0].experiment_id || ''
const trials = useQuery({
  queryKey: ['eval-trials', activeId],
  queryFn: () => getEvalTrials(activeId),
  enabled: Boolean(activeId),
})
const comparison = useQuery({
  queryKey: ['eval-comparison', baselineId, activeId],
  queryFn: () => compareEvalExperiments(baselineId, activeId),
  enabled: Boolean(baselineId && activeId && baselineId !== activeId),
})
const review = useMutation({
  mutationFn: ({ trial, decision }) => createEvalReview(trial, decision),
  onSuccess: () => queryClient.invalidateQueries({ queryKey: ['eval-trials', activeId] }),
})
```

- 未显式选择时 `activeId` 回退到实验列表首项；切换 Candidate 时清空已选 Trial。
- experiments 或 datasets 查询失败时整页渲染共享的 `AdminQueryError`（重试并发两个 refetch）。
- 无实验时显示空态（「先通过 Eval CLI 创建实验并运行固定 Case」）。

### Header 与布局

`max-w-[1400px]` 容器；Header 左侧为标题区（"Evaluation control plane" 大写眉标 + 说明文案），右侧两个 `<select>`：

- **Candidate**：当前展示的实验（`activeId`）。
- **Paired baseline**：可选的对比基线（默认「不比较」，选项排除 Candidate 自身）。

工作区 `EvaluationWorkspace` 采用 `lg:grid-cols-[17rem_minmax(0,1fr)]` 双栏：左侧指标栏，右侧 Trial 证据表。

### 左侧指标栏

`Metric` 子组件（大写小标签 + mono 大数值 + 说明行）依次展示：

- **Task success**：`percent(metrics.task_success.value)` + `numerator/denominator · Wilson 95% lower–upper`。
- **Usage coverage**：同结构占比。
- **Latency P50 / P95**：秒，valid trials only。
- 数据集脚注：`dataset_id@dataset_version`、`system_revision` 前 12 位、注册数据集数量。
- **Paired comparison**（选了 baseline 时）：success Δ、duration Δ（秒）、成对 case 数。
- **Failure clusters**：由 `trials` 前端聚合——失败 Trial 按 `invalid_reason || failure_reason || 首个 failed/error 的 grader 名 || 'unclassified'` 计数，按数量倒序取前 6：

```tsx
const failureClusters = useMemo(() => {
  const counts = new Map<string, number>()
  for (const trial of trials) {
    if (trial.result.task_success === true) continue
    const failedGrader = Object.entries(trial.result.grader_statuses ?? {}).find(
      ([, status]) => status === 'failed' || status === 'error',
    )?.[0]
    const reason = trial.invalid_reason || trial.failure_reason || failedGrader || 'unclassified'
    counts.set(reason, (counts.get(reason) ?? 0) + 1)
  }
  return [...counts.entries()].sort((left, right) => right[1] - left[1])
}, [trials])
```

### Trial 证据表

六列表格（Case / Rep / Result / Hidden & Regression / Duration / Trace & Commit），行可点击（`tabIndex={0}` + Enter 键支持）选中 Trial；加载态为 5 行骨架行。`TrialStatus` 三态徽章：`task_success === true` → 「通过」（success token），`state === 'invalid'` → 「无效」（warning token），否则「失败」（error token）。数值格式化统一走 `percent()`：`null` 显示 `—`，否则 `(v * 100).toFixed(1)%`。

### Trial 详情与人工复核

选中 Trial 后在表格下方展开详情面板（`border-l-2 border-brand/40` 引导线）：case_id + 状态徽章、trial_id、`diff_summary`（无则回退 `grader_statuses` JSON）、可选 Trace 外链。右侧操作列：

- 「直接接受」→ `createEvalReview(trial, 'accepted')`
- 「拒绝：不正确」→ `createEvalReview(trial, 'rejected_incorrect')`
- 「关闭详情」清空选择

复核按钮在 `!trial.final_commit || reviewPending` 时禁用（无 final_commit 的 Trial 不可复核）；mutation 成功后失效 `['eval-trials', activeId]` 刷新。决策枚举完整含四种（`accepted / rejected_incorrect / rejected_regression / rejected_overbroad`），当前 UI 仅暴露前两个按钮。

### 类型与 API (`src/lib/api.ts`)

契约之外的前端本地类型（admin fetch，JWT 鉴权）：

```typescript
export interface EvalProportion {
  numerator: number
  denominator: number
  value: number | null
  lower_95: number | null
  upper_95: number | null
}

export interface EvalMetrics {
  trials: number
  valid_trials: number
  invalid_trials: number
  task_success: EvalProportion
  bugfix_accuracy: EvalProportion
  usage_coverage: EvalProportion
  latency_seconds: { p50: number | null; p95: number | null }
}

export interface EvalExperiment {
  experiment_id: string
  dataset_id: string
  dataset_version: string
  dataset_digest: string
  system_revision: string
  status: string
  created_at: string
  metrics: EvalMetrics
}

export interface EvalTrial {
  trial_id: string
  experiment_id: string
  case_id: string
  repetition: number
  root_run_id?: string | null
  trace_id?: string | null
  final_commit?: string | null
  state: string
  failure_reason?: string | null
  invalid_reason?: string | null
  result: {
    task_success?: boolean
    hidden_test_pass?: boolean
    regression_free?: boolean
    false_modification?: boolean
    duration_seconds?: number | null
    total_tokens?: number | null
    grader_statuses?: Record<string, string>
    diff_summary?: string
    trace_url?: string | null
  }
}
```

| 函数 | 方法 | 路径 | 说明 |
|------|------|------|------|
| `getEvalDatasets` | GET | `/api/admin/evals/datasets` | 数据集列表（id/version/digest/case_count/invalid_reason） |
| `getEvalExperiments` | GET | `/api/admin/evals/experiments` | 实验列表（含 `EvalMetrics`） |
| `getEvalTrials` | GET | `/api/admin/evals/experiments/:experimentId/trials` | Trial 明细 |
| `compareEvalExperiments` | GET | `/api/admin/evals/compare?baseline=&candidate=` | 成对差异（success/duration delta + 置信区间） |
| `createEvalReview` | POST | `/api/admin/evals/trials/:trialId/reviews` | 人工复核（reviewer_id 固定 `admin`，回传 `reviewed_commit = trial.final_commit`） |
