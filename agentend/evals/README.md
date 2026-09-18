# Coding Agent Eval Harness

The harness is offline-first and keeps evaluation control-plane assets outside
Agent worktrees. It provides immutable Dataset/Experiment/Trial models,
canonical SHA-256 rules, Git bundle restore, isolated baseline and grading,
SQLite evidence history, Wilson intervals, paired bootstrap, JSONL/CSV/Markdown
reports, AgentHub Run collection, human review records, and the 34-case
multi-domain `agenthub-agent-v2` set (v2.1: coding with real specs, chat,
knowledge QA, no-op guards, and four `orchestrator` cases — two decomposable
pipelines for parallel benefit and two shared-core conflicts for the Resolver
path). The legacy `agenthub-coding-v1` set is DEPRECATED and kept only for
historical comparison.

Unattended real-Agent experiments are fail-closed until every strict sandbox
capability is actively probed. Check the gate with:

```bash
uv run --directory agentend python -m evals.cli readiness
```

The default `unsafe_process` configuration must report `ready: false`. A strict
deployment uses Bubblewrap, `prlimit`, an isolated Git clone, a read-only
short-lived credential directory, and either no network or a pre-provisioned
managed network namespace whose egress allowlist is maintained outside the
Agent process.

Common commands (or `make evals <validate|baseline|batch|speedup|build-dataset>`):

```bash
uv run --directory agentend python -m evals.cli validate \
  evals/datasets/agenthub-agent-v2 --environment-digest sha256:<digest>
uv run --directory agentend python -m evals.cli baseline \
  evals/datasets/agenthub-agent-v2 --environment-digest sha256:<digest>
uv run --directory agentend python -m evals.cli batch \
  evals/datasets/agenthub-agent-v2 --allow-unsafe --limit 6   # pilot
uv run --directory agentend python -m evals.cli batch \
  evals/datasets/agenthub-agent-v2 --arm serial --repetitions 3 \
  --output agentend/evals/tmp/batch-serial                   # speedup control arm
uv run --directory agentend python -m evals.cli speedup \
  --serial agentend/evals/tmp/batch-serial \
  --parallel agentend/evals/tmp/batch-parallel                # paired speedup + CI
uv run --directory agentend python -m evals.cli grade --help
uv run --directory agentend python -m evals.cli review --help
uv run --directory agentend python -m evals.cli report --help
```

`batch` is the only supported real-Agent batch entry. It reuses the official
grading chain, fails closed without bwrap unless `--allow-unsafe` is given,
and also fails closed when `DS_API_KEY` is missing (every v2 category carries
an LLM-quality weight; a judgeless run would inflate `score_percent`).
`--repetitions N` reruns each case N times (resume dedupes by
`case_id + repetition`); `--arm serial|parallel` selects the dispatch
instruction arm recorded in the experiment snapshot. Instructions are
category-differentiated: conversation cases must stay zero-touch, small coding
tasks stay single-implementer, and `orchestrator` cases get a neutral
decomposition instruction (serial arm forces one implementer at a time).

Scoring: each case gets a 0-100 composite (`execution` / `functional` /
`scope` / `quality`, weights per category, overridable per case via
`weights`); missing dimensions normalize over the remaining weights and are
reported through `quality_coverage`; a failed `anti_gaming` check caps the
score at 60. `task_success` stays all-or-nothing and `hidden_test_pass` is
reported alongside (null for categories without hidden checks). Aggregation
averages within a case before across cases, so uneven repetition counts
cannot skew the overall weight.

Orchestration facts: the batch driver parses the legacy runtime markers the
Backend flattens into orchestrator message content (plan / runtime_status) and
samples implementer session states while polling, surfacing
`plan_task_count`, `retry_count`, `conflict_recovery_count`,
`max_concurrent_implementers` and conflict chains in each result row.
`speedup` pairs a serial arm against a parallel arm per
`(case_id, repetition)` (case-level medians, geometric mean, bootstrap CI) and
only reports when the parallel arm's quality stays within tolerance. Judge
identity (model + prompt version) is embedded in every `llm_quality`
evidence digest and summary header.

`CaseManifest` is never sent to an Agent. `agent_visible_task()` is the only
allowlisted projection and excludes scope rules, grader configuration, Hidden
Asset identities, expected facts, and reference evidence.
