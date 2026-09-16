# Coding Agent Eval Harness

The harness is offline-first and keeps evaluation control-plane assets outside
Agent worktrees. It provides immutable Dataset/Experiment/Trial models,
canonical SHA-256 rules, Git bundle restore, isolated baseline and grading,
SQLite evidence history, Wilson intervals, paired bootstrap, JSONL/CSV/Markdown
reports, AgentHub Run collection, human review records, and the 30-case
`agenthub-coding-v1` regression set.

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

Common commands:

```bash
uv run --directory agentend python -m evals.cli validate \
  evals/datasets/agenthub-coding-v1 --environment-digest sha256:<digest>
uv run --directory agentend python -m evals.cli baseline \
  evals/datasets/agenthub-coding-v1 --environment-digest sha256:<digest>
uv run --directory agentend python -m evals.cli grade --help
uv run --directory agentend python -m evals.cli review --help
uv run --directory agentend python -m evals.cli report --help
```

`CaseManifest` is never sent to an Agent. `agent_visible_task()` is the only
allowlisted projection and excludes scope rules, grader configuration, Hidden
Asset identities, expected facts, and reference evidence.
