# Coding Agent Eval Harness

This package is intentionally offline-first. Phase 0 freezes strict Pydantic
models, canonical SHA-256 rules, the Agent-visible Case allowlist, the Trial
state machine, and the fail-closed batch safety gate.

Unattended real-Agent experiments remain disabled until every strict sandbox
capability is actively probed. Check the gate with:

```bash
uv run --directory agentend python -m evals.cli readiness
```

The current `unsafe_process` configuration must report `ready: false`. Offline
dataset validation, deterministic graders, and result storage are added in the
next phase; this package must not be used to expose Case manifests or hidden
assets to an Agent process.
