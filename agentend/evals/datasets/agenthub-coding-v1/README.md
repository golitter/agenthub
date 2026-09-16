# AgentHub Coding v1

This is the checked-in 30-case engineering regression set. Every Case owns an
immutable Git bundle and a hidden behavioral assertion. The small fixtures are
deliberately dependency-free so baseline validation remains offline and
reproducible; production-derived cases can replace them only in a new Dataset
version.

Regenerate deterministic manifests and bundles after editing the catalog:

```bash
uv run --directory agentend python evals/datasets/build_agenthub_coding_v1.py
```
