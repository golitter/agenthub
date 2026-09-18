"""Composite 0-100 scoring with partial credit per dimension.

`task_success` stays all-or-nothing; the percentage score is the lenient
headline metric and integrity signals act as caps, not vetoes.
"""

from __future__ import annotations

from typing import Any

from .models import GraderResult, GraderStatus

# `orchestrator` cases are coding tasks whose value is in the decomposition:
# functional evidence still comes from public + hidden command graders.
CODING_CATEGORIES = {
    "bugfix", "feature", "refactor", "test_generation", "integration", "orchestrator",
}
ZERO_DIFF_CATEGORIES = {"chat", "knowledge_qa", "no_op"}

# category -> (execution, functional, scope, quality)
CATEGORY_WEIGHTS: dict[str, dict[str, float]] = {
    **{
        category: {"execution": 0.15, "functional": 0.45, "scope": 0.15, "quality": 0.25}
        for category in CODING_CATEGORIES
    },
    "chat": {"execution": 0.20, "functional": 0.00, "scope": 0.20, "quality": 0.60},
    "knowledge_qa": {"execution": 0.15, "functional": 0.00, "scope": 0.15, "quality": 0.70},
    "no_op": {"execution": 0.20, "functional": 0.60, "scope": 0.00, "quality": 0.20},
}

ANTI_GAMING_CAP = 60.0


def score_case(
    category: str,
    results: list[GraderResult],
    *,
    weights: dict[str, float] | None = None,
) -> dict[str, Any]:
    by_grader = {result.grader: result for result in results}
    resolved = weights if weights is not None else CATEGORY_WEIGHTS.get(category)
    if resolved is None:
        raise ValueError(f"no default weights for category: {category}")
    dimensions = {
        "execution": _execution(by_grader),
        "functional": _functional(category, by_grader),
        "scope": _scope(by_grader),
        "quality": _quality(by_grader),
    }
    missing = sorted(name for name, value in dimensions.items() if value is None)
    present = {name: value for name, value in dimensions.items() if value is not None}
    total_weight = sum(resolved.get(name, 0.0) for name in present)
    if present and total_weight > 0:
        score_percent = 100.0 * sum(resolved.get(name, 0.0) * value for name, value in present.items()) / total_weight
    else:
        score_percent = 0.0
    anti_gaming = by_grader.get("anti_gaming")
    capped = anti_gaming is not None and anti_gaming.status == GraderStatus.FAILED
    if capped:
        score_percent = min(score_percent, ANTI_GAMING_CAP)
    return {
        "score_percent": round(score_percent, 2),
        "score_dimensions": dimensions,
        "missing_dimensions": missing,
        "anti_gaming_capped": capped,
    }


def _execution(by_grader: dict[str, GraderResult]) -> float | None:
    result = by_grader.get("run_state")
    if result is None or result.status not in {GraderStatus.PASSED, GraderStatus.FAILED}:
        return None
    return 1.0 if result.status == GraderStatus.PASSED else 0.0


def _functional(category: str, by_grader: dict[str, GraderResult]) -> float | None:
    if category in CODING_CATEGORIES:
        public = _public_partial(by_grader.get("command"))
        hidden = _binary(by_grader.get("hidden_command"))
        parts = [(0.35, public), (0.65, hidden)]
        available = [(weight, value) for weight, value in parts if value is not None]
        if not available:
            return None
        weight_sum = sum(weight for weight, _ in available)
        return sum(weight * value for weight, value in available) / weight_sum
    if category in ZERO_DIFF_CATEGORIES:
        zero_diff = _binary(by_grader.get("no_op"))
        anti_gaming = _binary(by_grader.get("anti_gaming"))
        parts = [(0.8, zero_diff), (0.2, anti_gaming)]
        available = [(weight, value) for weight, value in parts if value is not None]
        if not available:
            return None
        weight_sum = sum(weight for weight, _ in available)
        return sum(weight * value for weight, value in available) / weight_sum
    return None


def _scope(by_grader: dict[str, GraderResult]) -> float | None:
    result = by_grader.get("git_diff")
    if result is None or result.status not in {GraderStatus.PASSED, GraderStatus.FAILED}:
        return None
    if result.score is not None:
        return result.score
    return 1.0 if result.status == GraderStatus.PASSED else 0.0


def _quality(by_grader: dict[str, GraderResult]) -> float | None:
    result = by_grader.get("llm_quality")
    if result is None or result.status != GraderStatus.PASSED:
        return None
    return result.score


def _binary(result: GraderResult | None) -> float | None:
    if result is None or result.status not in {GraderStatus.PASSED, GraderStatus.FAILED}:
        return None
    return 1.0 if result.status == GraderStatus.PASSED else 0.0


def _public_partial(result: GraderResult | None) -> float | None:
    if result is None or result.status not in {GraderStatus.PASSED, GraderStatus.FAILED}:
        return None
    passed, failed = result.passed or 0, result.failed or 0
    if passed + failed > 0:
        return passed / (passed + failed)
    return 1.0 if result.status == GraderStatus.PASSED else 0.0
