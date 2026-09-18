from __future__ import annotations

import logging
from typing import Any

logger = logging.getLogger(__name__)

_BOOLEAN_SCORES = (
    "task_success",
    "hidden_test_pass",
    "regression_free",
    "false_modification",
    "usage_available",
)
_NUMERIC_SCORES = ("duration_seconds", "total_tokens", "trial_cost", "score_percent")


def publish_trial_scores(client: Any, *, trace_id: str | None, result: dict[str, Any]) -> bool:
    """Best-effort Langfuse copy; the local Eval store remains authoritative."""

    if client is None or not trace_id:
        return False
    try:
        for name in _BOOLEAN_SCORES:
            value = result.get(name)
            if isinstance(value, bool):
                client.create_score(
                    trace_id=trace_id,
                    name=name,
                    value=value,
                    data_type="BOOLEAN",
                )
        for name in _NUMERIC_SCORES:
            value = result.get(name)
            if isinstance(value, (int, float)) and not isinstance(value, bool):
                client.create_score(
                    trace_id=trace_id,
                    name=name,
                    value=float(value),
                    data_type="NUMERIC",
                )
        return True
    except Exception:
        logger.warning("Langfuse score publication failed; local Eval result is preserved", exc_info=True)
        return False
