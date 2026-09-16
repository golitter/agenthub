import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import { AdminQueryError } from '@/components/admin/AdminQueryError'
import { Button } from '@/components/ui/button'
import {
  compareEvalExperiments,
  createEvalReview,
  type EvalExperiment,
  type EvalTrial,
  getEvalDatasets,
  getEvalExperiments,
  getEvalTrials,
} from '@/lib/api'
import { cn } from '@/lib/utils'

function percent(value: number | null | undefined) {
  return value == null ? '—' : `${(value * 100).toFixed(1)}%`
}

function Metric({ label, value, detail }: { label: string; value: string; detail: string }) {
  return (
    <div className="border-t border-border pt-3">
      <p className="text-[11px] font-medium tracking-[0.08em] text-tertiary uppercase">{label}</p>
      <p className="mt-1 font-mono text-2xl font-semibold tracking-tight text-foreground">
        {value}
      </p>
      <p className="mt-1 text-xs text-text-secondary">{detail}</p>
    </div>
  )
}

function TrialStatus({ trial }: { trial: EvalTrial }) {
  const success = trial.result.task_success === true
  return (
    <span
      className={cn(
        'inline-flex rounded-full border px-2 py-0.5 text-[11px] font-medium',
        success
          ? 'border-success/25 bg-success/10 text-success'
          : trial.state === 'invalid'
            ? 'border-warning/25 bg-warning/10 text-warning'
            : 'border-error/25 bg-error/10 text-error',
      )}
    >
      {success ? '通过' : trial.state === 'invalid' ? '无效' : '失败'}
    </span>
  )
}

export function EvaluationsPage() {
  const queryClient = useQueryClient()
  const [selectedExperimentId, setSelectedExperimentId] = useState('')
  const [baselineId, setBaselineId] = useState('')
  const [selectedTrial, setSelectedTrial] = useState<EvalTrial | null>(null)
  const datasets = useQuery({ queryKey: ['eval-datasets'], queryFn: getEvalDatasets })
  const experiments = useQuery({ queryKey: ['eval-experiments'], queryFn: getEvalExperiments })
  const activeId = selectedExperimentId || experiments.data?.[0]?.experiment_id || ''
  const trials = useQuery({
    queryKey: ['eval-trials', activeId],
    queryFn: () => getEvalTrials(activeId),
    enabled: Boolean(activeId),
  })
  const activeExperiment = useMemo(
    () => experiments.data?.find((item) => item.experiment_id === activeId),
    [activeId, experiments.data],
  )
  const comparison = useQuery({
    queryKey: ['eval-comparison', baselineId, activeId],
    queryFn: () => compareEvalExperiments(baselineId, activeId),
    enabled: Boolean(baselineId && activeId && baselineId !== activeId),
  })
  const review = useMutation({
    mutationFn: ({
      trial,
      decision,
    }: {
      trial: EvalTrial
      decision: Parameters<typeof createEvalReview>[1]
    }) => createEvalReview(trial, decision),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['eval-trials', activeId] }),
  })

  if (experiments.isError || datasets.isError) {
    return (
      <AdminQueryError onRetry={() => Promise.all([experiments.refetch(), datasets.refetch()])} />
    )
  }

  return (
    <div className="mx-auto w-full max-w-[1400px] px-4 py-6 sm:px-6">
      <header className="grid gap-6 border-b border-border pb-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <div>
          <p className="text-[11px] font-medium tracking-[0.12em] text-brand uppercase">
            Evaluation control plane
          </p>
          <h2 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
            Coding Agent 评测
          </h2>
          <p className="mt-2 max-w-[65ch] text-sm leading-6 text-text-secondary">
            查看固定 Dataset
            上的任务成功率、隐藏测试、回归与资源覆盖率。人工审查只记录接受结论，不会改写 Agent
            原始成绩。
          </p>
        </div>
        <div className="grid gap-3 self-end sm:grid-cols-2 lg:grid-cols-1">
          <label className="text-xs font-medium text-text-secondary">
            Candidate
            <select
              value={activeId}
              onChange={(event) => {
                setSelectedExperimentId(event.target.value)
                setSelectedTrial(null)
              }}
              className="mt-2 h-10 w-full rounded-md border border-border bg-background px-3 font-mono text-xs text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {(experiments.data ?? []).map((experiment) => (
                <option key={experiment.experiment_id} value={experiment.experiment_id}>
                  {experiment.experiment_id}
                </option>
              ))}
            </select>
          </label>
          <label className="text-xs font-medium text-text-secondary">
            Paired baseline
            <select
              value={baselineId}
              onChange={(event) => setBaselineId(event.target.value)}
              className="mt-2 h-10 w-full rounded-md border border-border bg-background px-3 font-mono text-xs text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <option value="">不比较</option>
              {(experiments.data ?? [])
                .filter((item) => item.experiment_id !== activeId)
                .map((experiment) => (
                  <option key={experiment.experiment_id} value={experiment.experiment_id}>
                    {experiment.experiment_id}
                  </option>
                ))}
            </select>
          </label>
        </div>
      </header>

      {experiments.isLoading ? (
        <div className="grid gap-6 py-8 lg:grid-cols-[18rem_1fr]" aria-busy="true">
          <div className="h-72 rounded-lg skeleton-sheen" />
          <div className="h-96 rounded-lg skeleton-sheen" />
        </div>
      ) : !activeExperiment ? (
        <div className="py-24 text-center">
          <p className="text-sm font-medium text-foreground">尚无 Experiment</p>
          <p className="mt-2 text-xs text-tertiary">先通过 Eval CLI 创建实验并运行固定 Case。</p>
        </div>
      ) : (
        <EvaluationWorkspace
          experiment={activeExperiment}
          trials={trials.data ?? []}
          trialsLoading={trials.isLoading}
          selectedTrial={selectedTrial}
          onSelectTrial={setSelectedTrial}
          onReview={(trial, decision) => review.mutate({ trial, decision })}
          reviewPending={review.isPending}
          datasetCount={datasets.data?.length ?? 0}
          comparison={comparison.data}
        />
      )}
    </div>
  )
}

function EvaluationWorkspace({
  experiment,
  trials,
  trialsLoading,
  selectedTrial,
  onSelectTrial,
  onReview,
  reviewPending,
  datasetCount,
  comparison,
}: {
  experiment: EvalExperiment
  trials: EvalTrial[]
  trialsLoading: boolean
  selectedTrial: EvalTrial | null
  onSelectTrial: (trial: EvalTrial | null) => void
  onReview: (trial: EvalTrial, decision: Parameters<typeof createEvalReview>[1]) => void
  reviewPending: boolean
  datasetCount: number
  comparison?: Awaited<ReturnType<typeof compareEvalExperiments>>
}) {
  const metrics = experiment.metrics
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
  return (
    <div className="grid gap-8 py-7 lg:grid-cols-[17rem_minmax(0,1fr)]">
      <aside className="space-y-5">
        <Metric
          label="Task success"
          value={percent(metrics.task_success.value)}
          detail={`${metrics.task_success.numerator}/${metrics.task_success.denominator} · Wilson 95% ${percent(metrics.task_success.lower_95)}–${percent(metrics.task_success.upper_95)}`}
        />
        <Metric
          label="Usage coverage"
          value={percent(metrics.usage_coverage.value)}
          detail={`${metrics.usage_coverage.numerator}/${metrics.usage_coverage.denominator} trials`}
        />
        <Metric
          label="Latency P50 / P95"
          value={`${metrics.latency_seconds.p50?.toFixed(1) ?? '—'} / ${metrics.latency_seconds.p95?.toFixed(1) ?? '—'}`}
          detail="seconds · valid trials only"
        />
        <div className="border-t border-border pt-3 text-xs leading-5 text-text-secondary">
          <p>
            {experiment.dataset_id}@{experiment.dataset_version}
          </p>
          <p className="font-mono text-[11px] text-tertiary">
            {experiment.system_revision.slice(0, 12)}
          </p>
          <p>
            {datasetCount} registered dataset{datasetCount === 1 ? '' : 's'}
          </p>
        </div>
        {comparison && (
          <div className="border-t border-border pt-3 text-xs leading-5 text-text-secondary">
            <p className="font-medium text-foreground">Paired comparison</p>
            <p className="font-mono">success Δ {percent(comparison.success_delta.delta)}</p>
            <p className="font-mono">
              duration Δ {comparison.duration_delta_seconds.delta?.toFixed(2) ?? '—'}s
            </p>
            <p>{comparison.paired_cases} paired cases</p>
          </div>
        )}
        {failureClusters.length > 0 && (
          <div className="border-t border-border pt-3">
            <p className="text-[11px] font-medium tracking-[0.08em] text-tertiary uppercase">
              Failure clusters
            </p>
            <div className="mt-2 divide-y divide-border text-xs">
              {failureClusters.slice(0, 6).map(([reason, count]) => (
                <div key={reason} className="flex items-center justify-between gap-3 py-1.5">
                  <span className="min-w-0 truncate text-text-secondary">{reason}</span>
                  <span className="font-mono text-foreground">{count}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </aside>

      <section className="min-w-0">
        <div className="mb-3 flex items-end justify-between gap-4">
          <div>
            <h3 className="text-sm font-semibold text-foreground">Trial evidence</h3>
            <p className="mt-1 text-xs text-tertiary">
              按 Case 与 repetition 排序，失败不会从分母静默移除。
            </p>
          </div>
          <span className="font-mono text-xs text-tertiary">{trials.length} trials</span>
        </div>
        <div className="overflow-x-auto border-y border-border">
          <table className="w-full min-w-[760px] text-left text-xs">
            <thead className="bg-bg-canvas text-[11px] tracking-[0.06em] text-tertiary uppercase">
              <tr>
                <th className="px-3 py-2.5 font-medium">Case</th>
                <th className="px-3 py-2.5 font-medium">Rep</th>
                <th className="px-3 py-2.5 font-medium">Result</th>
                <th className="px-3 py-2.5 font-medium">Hidden / Regression</th>
                <th className="px-3 py-2.5 font-medium">Duration</th>
                <th className="px-3 py-2.5 font-medium">Trace / Commit</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {trialsLoading
                ? Array.from({ length: 5 }).map((_, index) => (
                    <tr key={index}>
                      <td colSpan={6} className="p-3">
                        <div className="h-6 rounded skeleton-sheen" />
                      </td>
                    </tr>
                  ))
                : trials.map((trial) => (
                    <tr
                      key={trial.trial_id}
                      tabIndex={0}
                      onClick={() => onSelectTrial(trial)}
                      onKeyDown={(event) => event.key === 'Enter' && onSelectTrial(trial)}
                      className="cursor-pointer transition-colors hover:bg-bg-hover focus-visible:bg-bg-hover focus-visible:outline-none"
                    >
                      <td className="px-3 py-3 font-medium text-foreground">{trial.case_id}</td>
                      <td className="px-3 py-3 font-mono text-text-secondary">
                        {trial.repetition}
                      </td>
                      <td className="px-3 py-3">
                        <TrialStatus trial={trial} />
                      </td>
                      <td className="px-3 py-3 font-mono text-[11px] text-text-secondary">
                        {String(trial.result.hidden_test_pass ?? '—')} /{' '}
                        {String(trial.result.regression_free ?? '—')}
                      </td>
                      <td className="px-3 py-3 font-mono text-text-secondary">
                        {trial.result.duration_seconds?.toFixed(2) ?? '—'}s
                      </td>
                      <td className="px-3 py-3 font-mono text-[11px] text-tertiary">
                        <span className="block">{trial.trace_id?.slice(0, 12) ?? 'no trace'}</span>
                        <span className="block">
                          {trial.final_commit?.slice(0, 12) ?? 'no commit'}
                        </span>
                      </td>
                    </tr>
                  ))}
            </tbody>
          </table>
        </div>

        {selectedTrial && (
          <div className="mt-6 grid gap-5 border-l-2 border-brand/40 pl-5 md:grid-cols-[minmax(0,1fr)_auto]">
            <div>
              <div className="flex items-center gap-3">
                <h4 className="text-sm font-semibold text-foreground">{selectedTrial.case_id}</h4>
                <TrialStatus trial={selectedTrial} />
              </div>
              <p className="mt-2 font-mono text-[11px] text-tertiary">{selectedTrial.trial_id}</p>
              <pre className="mt-4 max-h-48 overflow-auto rounded-md border border-border bg-bg-canvas p-3 font-mono text-[11px] leading-5 text-text-secondary">
                {selectedTrial.result.diff_summary ||
                  JSON.stringify(selectedTrial.result.grader_statuses ?? {}, null, 2)}
              </pre>
              {selectedTrial.result.trace_url && (
                <a
                  href={selectedTrial.result.trace_url}
                  target="_blank"
                  rel="noreferrer"
                  className="mt-3 inline-flex text-xs font-medium text-brand underline-offset-4 hover:underline"
                >
                  打开 Trace
                </a>
              )}
            </div>
            <div className="flex min-w-40 flex-col gap-2">
              <Button
                size="sm"
                disabled={!selectedTrial.final_commit || reviewPending}
                onClick={() => onReview(selectedTrial, 'accepted')}
                className="active:scale-[0.98]"
              >
                直接接受
              </Button>
              <Button
                size="sm"
                variant="secondary"
                disabled={!selectedTrial.final_commit || reviewPending}
                onClick={() => onReview(selectedTrial, 'rejected_incorrect')}
                className="active:scale-[0.98]"
              >
                拒绝：不正确
              </Button>
              <Button size="sm" variant="quiet" onClick={() => onSelectTrial(null)}>
                关闭详情
              </Button>
            </div>
          </div>
        )}
      </section>
    </div>
  )
}
