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


def _case_key(row: dict[str, Any], index: int) -> str:
    # Repetitions of one case are not independent samples (design 15 §9.4):
    # score aggregation averages within a case first, then across cases, so an
    # uneven repetition count cannot skew the overall weight.
    return str(row.get("case_id") or f"__row_{index}")


def _per_case_mean(values_by_case: dict[str, list[float]]) -> float | None:
    if not values_by_case:
        return None
    return statistics.mean(statistics.mean(items) for items in values_by_case.values())


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
    score_rows = [row for row in valid if row.get("result", {}).get("score_percent") is not None]
    hidden_rows = [row for row in valid if row.get("result", {}).get("hidden_test_pass") is not None]
    hidden_passed = [row for row in hidden_rows if row["result"]["hidden_test_pass"] is True]
    quality_present = [
        row
        for row in valid
        if "quality" not in (row.get("result", {}).get("missing_dimensions") or [])
    ]
    case_scores: dict[str, dict[str, list[float]]] = {}
    for index, row in enumerate(score_rows):
        category = str(row["result"].get("case_category", "unknown"))
        case_scores.setdefault(category, {}).setdefault(_case_key(row, index), []).append(
            float(row["result"]["score_percent"])
        )
    category_scores: dict[str, dict[str, Any]] = {}
    for category, cases in case_scores.items():
        trial_count = sum(len(items) for items in cases.values())
        category_scores[category] = {
            "count": trial_count,
            "cases": len(cases),
            "mean_score": _per_case_mean(cases),
        }
    all_scores_by_case: dict[str, list[float]] = {}
    for index, row in enumerate(score_rows):
        all_scores_by_case.setdefault(_case_key(row, index), []).append(
            float(row["result"]["score_percent"])
        )
    return {
        "trials": len(rows),
        "valid_trials": len(valid),
        "invalid_trials": len(rows) - len(valid),
        "task_success": wilson_interval(len(successful), len(valid)).__dict__,
        "bugfix_accuracy": wilson_interval(len(bugfix_success), len(bugfix)).__dict__,
        "usage_coverage": wilson_interval(len(usage_rows), len(valid)).__dict__,
        "overall_score_percent": _per_case_mean(all_scores_by_case),
        "scored_trials": len(score_rows),
        "category_scores": category_scores,
        "hidden_test_pass": wilson_interval(len(hidden_passed), len(hidden_rows)).__dict__,
        "quality_coverage": wilson_interval(len(quality_present), len(valid)).__dict__,
        "latency_seconds": {
            "p50": percentile(durations, 50),
            "p95": percentile(durations, 95),
        },
    }


def _speedup_row(row: dict[str, Any]) -> dict[str, Any] | None:
    """Normalize a batch JSONL row or a repository trial row for speedup math."""

    state = row.get("state") or ("completed" if row.get("status") == "completed" else "invalid")
    result = row.get("result") or {}
    if state == "invalid" or not row.get("case_id"):
        return None
    duration = result.get("duration_seconds")
    if duration is None:
        return None
    return {
        "case_id": str(row["case_id"]),
        "repetition": int(row.get("repetition") or 0),
        "category": str(result.get("case_category") or row.get("case_category") or ""),
        "duration": float(duration),
        "score": float(result["score_percent"]) if result.get("score_percent") is not None else None,
        "task_success": result.get("task_success"),
        "max_concurrent_implementers": result.get("max_concurrent_implementers"),
    }


def parallel_speedup(
    serial_rows: Sequence[dict[str, Any]],
    parallel_rows: Sequence[dict[str, Any]],
    *,
    score_tolerance_points: float = 5.0,
    success_tolerance: float = 0.0,
    samples: int = 10_000,
    seed: int = 0,
) -> dict[str, Any]:
    """Paired speedup between a serial arm and a parallel arm (design 15 §9.3).

    Only orchestrator-category cases count; a case is branch-eligible when its
    parallel arm actually ran >= 2 concurrent implementers (falling back to the
    category alone when concurrency facts are absent). Speedup is reported only
    when the parallel arm's quality stays within the tolerated degradation.
    """
    serial = [row for row in (_speedup_row(item) for item in serial_rows) if row]
    parallel = [row for row in (_speedup_row(item) for item in parallel_rows) if row]

    parallel_cases = {row["case_id"] for row in parallel if row["category"] == "orchestrator"}
    concurrency_facts = any(
        row["max_concurrent_implementers"] is not None
        for row in parallel
        if row["case_id"] in parallel_cases
    )
    eligible: dict[str, bool] = {}
    for case_id in sorted(parallel_cases):
        if concurrency_facts:
            eligible[case_id] = any(
                (row["case_id"] == case_id and (row["max_concurrent_implementers"] or 0) >= 2)
                for row in parallel
            )
        else:
            eligible[case_id] = True

    per_case: dict[str, dict[str, Any]] = {}
    excluded: dict[str, str] = {}
    for case_id, is_eligible in eligible.items():
        if not is_eligible:
            excluded[case_id] = "fewer than 2 concurrent implementers in the parallel arm"
            continue
        serial_durations = _durations_by_case(serial, case_id)
        parallel_durations = _durations_by_case(parallel, case_id)
        paired_reps = sorted(set(serial_durations) & set(parallel_durations))
        if not paired_reps:
            excluded[case_id] = "no (case_id, repetition) pair with durations on both arms"
            continue
        serial_median = statistics.median(serial_durations[rep] for rep in paired_reps)
        parallel_median = statistics.median(parallel_durations[rep] for rep in paired_reps)
        if parallel_median <= 0:
            excluded[case_id] = "non-positive parallel duration"
            continue
        per_case[case_id] = {
            "pairs": len(paired_reps),
            "serial_median_seconds": round(serial_median, 3),
            "parallel_median_seconds": round(parallel_median, 3),
            "speedup": round(serial_median / parallel_median, 4),
        }

    speedups = [item["speedup"] for item in per_case.values()]
    geomean = _geometric_mean(speedups)
    lower = upper = None
    if speedups:
        rng = random.Random(seed)
        case_ids = list(per_case)
        bootstrapped = []
        for _ in range(samples):
            drawn = [per_case[rng.choice(case_ids)]["speedup"] for _ in case_ids]
            value = _geometric_mean(drawn)
            if value is not None:
                bootstrapped.append(value)
        lower = percentile(bootstrapped, 2.5)
        upper = percentile(bootstrapped, 97.5)

    guard = _quality_guard(
        serial, parallel, parallel_cases,
        score_tolerance_points=score_tolerance_points, success_tolerance=success_tolerance,
    )
    reportable = bool(speedups) and guard["within_tolerance"]
    return {
        "eligible_cases": sorted(per_case),
        "excluded_cases": excluded,
        "branch_filter": "concurrency_facts" if concurrency_facts else "category_only",
        "pairs": sum(item["pairs"] for item in per_case.values()),
        "speedup_geomean": geomean,
        "speedup_lower_95": lower,
        "speedup_upper_95": upper,
        "speedup_median": statistics.median(speedups) if speedups else None,
        "per_case": per_case,
        "quality_guard": guard,
        "reportable": reportable,
        "not_reportable_reason": None if reportable else _speedup_block_reason(per_case, guard),
    }


def _speedup_block_reason(
    per_case: dict[str, dict[str, Any]], guard: dict[str, Any]
) -> str:
    if not per_case:
        return "no eligible paired orchestrator cases"
    if not guard["within_tolerance"]:
        return "parallel arm quality degraded beyond tolerance: " + "; ".join(guard["violations"])
    return ""


def _durations_by_case(rows: Sequence[dict[str, Any]], case_id: str) -> dict[int, float]:
    return {
        row["repetition"]: row["duration"]
        for row in rows
        if row["case_id"] == case_id and row["duration"] is not None
    }


def _geometric_mean(values: Sequence[float]) -> float | None:
    if not values:
        return None
    if any(value <= 0 for value in values):
        # A non-positive ratio has no multiplicative meaning; report the plain
        # median instead of silently dropping the case.
        return statistics.median(values)
    return round(math.exp(statistics.mean(math.log(value) for value in values)), 4)


def _quality_guard(
    serial: Sequence[dict[str, Any]],
    parallel: Sequence[dict[str, Any]],
    case_ids: set[str],
    *,
    score_tolerance_points: float,
    success_tolerance: float,
) -> dict[str, Any]:
    serial_sel = [row for row in serial if row["case_id"] in case_ids]
    parallel_sel = [row for row in parallel if row["case_id"] in case_ids]
    serial_scores = [row["score"] for row in serial_sel if row["score"] is not None]
    parallel_scores = [row["score"] for row in parallel_sel if row["score"] is not None]
    serial_success = [row for row in serial_sel if row["task_success"] is True]
    parallel_success = [row for row in parallel_sel if row["task_success"] is True]
    violations: list[str] = []
    score_delta = (
        statistics.mean(parallel_scores) - statistics.mean(serial_scores)
        if serial_scores and parallel_scores
        else None
    )
    if score_delta is not None and score_delta < -score_tolerance_points:
        violations.append(
            f"score dropped {abs(score_delta):.2f} points (> {score_tolerance_points:.2f})"
        )
    serial_rate = len(serial_success) / len(serial_sel) if serial_sel else None
    parallel_rate = len(parallel_success) / len(parallel_sel) if parallel_sel else None
    if serial_rate is not None and parallel_rate is not None and parallel_rate < serial_rate - success_tolerance:
        violations.append(
            f"task_success rate {parallel_rate:.3f} < serial {serial_rate:.3f} - {success_tolerance:.3f}"
        )
    return {
        "serial_mean_score": round(statistics.mean(serial_scores), 2) if serial_scores else None,
        "parallel_mean_score": round(statistics.mean(parallel_scores), 2) if parallel_scores else None,
        "score_delta_points": round(score_delta, 2) if score_delta is not None else None,
        "serial_success_rate": serial_rate,
        "parallel_success_rate": parallel_rate,
        "violations": violations,
        "within_tolerance": not violations,
    }
