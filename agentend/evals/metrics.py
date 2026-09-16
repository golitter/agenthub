from __future__ import annotations

import math
import random
import statistics
from collections.abc import Callable, Iterable, Sequence
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class Proportion:
    numerator: int
    denominator: int
    value: float | None
    lower_95: float | None
    upper_95: float | None


def wilson_interval(successes: int, total: int, z: float = 1.959963984540054) -> Proportion:
    if successes < 0 or total < 0 or successes > total:
        raise ValueError("invalid proportion counts")
    if total == 0:
        return Proportion(successes, total, None, None, None)
    p = successes / total
    denominator = 1 + (z * z / total)
    center = (p + z * z / (2 * total)) / denominator
    margin = z * math.sqrt((p * (1 - p) + z * z / (4 * total)) / total) / denominator
    return Proportion(successes, total, p, max(0.0, center - margin), min(1.0, center + margin))


def percentile(values: Sequence[float], percentile_value: float) -> float | None:
    if not values:
        return None
    if not 0 <= percentile_value <= 100:
        raise ValueError("percentile must be between 0 and 100")
    ordered = sorted(values)
    position = (len(ordered) - 1) * percentile_value / 100
    lower = math.floor(position)
    upper = math.ceil(position)
    if lower == upper:
        return ordered[lower]
    weight = position - lower
    return ordered[lower] * (1 - weight) + ordered[upper] * weight


def paired_bootstrap(
    pairs: Sequence[tuple[float, float]],
    *,
    samples: int = 10000,
    seed: int = 0,
    statistic: Callable[[Iterable[float]], float] = statistics.mean,
) -> dict[str, float | int | None]:
    if not pairs:
        return {"pairs": 0, "delta": None, "lower_95": None, "upper_95": None}
    rng = random.Random(seed)
    differences = [candidate - baseline for baseline, candidate in pairs]
    observed = statistic(differences)
    bootstrapped = []
    for _ in range(samples):
        bootstrapped.append(statistic(rng.choice(differences) for _ in differences))
    return {
        "pairs": len(pairs),
        "delta": observed,
        "lower_95": percentile(bootstrapped, 2.5),
        "upper_95": percentile(bootstrapped, 97.5),
    }


def aggregate_trials(rows: Sequence[dict[str, Any]]) -> dict[str, Any]:
    valid = [row for row in rows if row.get("state") != "invalid"]
    successful = [row for row in valid if row.get("result", {}).get("task_success") is True]
    bugfix = [row for row in valid if row.get("result", {}).get("case_category") == "bugfix"]
    bugfix_success = [row for row in bugfix if row.get("result", {}).get("task_success") is True]
    usage_rows = [row for row in valid if row.get("result", {}).get("usage_available") is True]
    durations = [
        float(row["result"]["duration_seconds"])
        for row in valid
        if row.get("result", {}).get("duration_seconds") is not None
    ]
    return {
        "trials": len(rows),
        "valid_trials": len(valid),
        "invalid_trials": len(rows) - len(valid),
        "task_success": wilson_interval(len(successful), len(valid)).__dict__,
        "bugfix_accuracy": wilson_interval(len(bugfix_success), len(bugfix)).__dict__,
        "usage_coverage": wilson_interval(len(usage_rows), len(valid)).__dict__,
        "latency_seconds": {
            "p50": percentile(durations, 50),
            "p95": percentile(durations, 95),
        },
    }
