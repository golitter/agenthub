# Coding Agent Eval Harness

The harness is offline-first and keeps evaluation control-plane assets outside
Agent worktrees. It provides immutable Dataset/Experiment/Trial models,
canonical SHA-256 rules, Git bundle restore, isolated baseline and grading,
SQLite evidence history, Wilson intervals, paired bootstrap, JSONL/CSV/Markdown
reports, AgentHub Run collection, human review records, and the 30-case
multi-domain `agenthub-agent-v2` set (coding with real specs, chat,
knowledge QA and no-op guards). The legacy `agenthub-coding-v1` set is
DEPRECATED and kept only for historical comparison.

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

Common commands (or `make evals <validate|baseline|batch|build-dataset>`):

```bash
uv run --directory agentend python -m evals.cli validate \
  evals/datasets/agenthub-agent-v2 --environment-digest sha256:<digest>
uv run --directory agentend python -m evals.cli baseline \
  evals/datasets/agenthub-agent-v2 --environment-digest sha256:<digest>
uv run --directory agentend python -m evals.cli batch \
  evals/datasets/agenthub-agent-v2 --allow-unsafe --limit 6   # pilot
uv run --directory agentend python -m evals.cli grade --help
uv run --directory agentend python -m evals.cli review --help
uv run --directory agentend python -m evals.cli report --help
```

`batch` is the only supported real-Agent batch entry. It reuses the official
grading chain, fails closed without bwrap unless `--allow-unsafe` is given,
and also fails closed when `DS_API_KEY` is missing (every v2 category carries
an LLM-quality weight; a judgeless run would inflate `score_percent`).

Scoring: each case gets a 0-100 composite (`execution` / `functional` /
`scope` / `quality`, weights per category, overridable per case via
`weights`); missing dimensions normalize over the remaining weights and are
reported through `quality_coverage`; a failed `anti_gaming` check caps the
score at 60. `task_success` stays all-or-nothing and `hidden_test_pass` is
reported alongside (null for categories without hidden checks).

`CaseManifest` is never sent to an Agent. `agent_visible_task()` is the only
allowlisted projection and excludes scope rules, grader configuration, Hidden
Asset identities, expected facts, and reference evidence.
