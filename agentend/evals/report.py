from __future__ import annotations

import csv
import json
from pathlib import Path
from typing import Any

from .metrics import aggregate_trials


def write_jsonl(rows: list[dict[str, Any]], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as output:
        for row in rows:
            output.write(json.dumps(row, ensure_ascii=False, sort_keys=True) + "\n")


def write_csv(rows: list[dict[str, Any]], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fields = [
        "trial_id",
        "case_id",
        "repetition",
        "state",
        "task_success",
        "score_percent",
        "duration_seconds",
        "total_tokens",
        "trial_cost",
        "failure_reason",
        "invalid_reason",
    ]
    with path.open("w", encoding="utf-8", newline="") as output:
        writer = csv.DictWriter(output, fieldnames=fields)
        writer.writeheader()
        for row in rows:
            result = row.get("result", {})
            writer.writerow({field: result.get(field, row.get(field)) for field in fields})


def write_markdown(experiment: dict[str, Any], rows: list[dict[str, Any]], path: Path) -> None:
    metrics = aggregate_trials(rows)
    success = metrics["task_success"]
    coverage = metrics["usage_coverage"]
    quality = metrics["quality_coverage"]
    category_scores = metrics.get("category_scores", {})
    category_lines = [
        f"| {category} | {bucket['count']} | {bucket['mean_score']:.1f} |"
        for category, bucket in sorted(category_scores.items())
    ]
    price_versions = sorted(
        {
            str(row.get("result", {}).get("provider_price_table_version"))
            for row in rows
            if row.get("result", {}).get("provider_price_table_version")
        }
    )
    currencies = sorted(
        {
            str(row.get("result", {}).get("currency"))
            for row in rows
            if row.get("result", {}).get("currency")
        }
    )
    lines = [
        f"# Evaluation report — {experiment['experiment_id']}",
        "",
        f"- Dataset: `{experiment['dataset_id']}@{experiment['dataset_version']}`",
        f"- Dataset digest: `{experiment['dataset_digest']}`",
        f"- System revision: `{experiment['system_revision']}`",
        f"- Valid trials: {metrics['valid_trials']} / {metrics['trials']}",
        (
            f"- Task success: {success['numerator']}/{success['denominator']} "
            f"({_percent(success['value'])}, 95% CI "
            f"{_percent(success['lower_95'])}–{_percent(success['upper_95'])})"
        ),
        (
            f"- Usage coverage: {coverage['numerator']}/{coverage['denominator']} "
            f"({_percent(coverage['value'])})"
        ),
        f"- Overall score: {_number(metrics.get('overall_score_percent'))} / 100",
        (
            f"- Quality coverage: {quality['numerator']}/{quality['denominator']} "
            f"({_percent(quality['value'])})"
        ),
        f"- Provider price table: {', '.join(price_versions) if price_versions else 'not reported'}",
        f"- Currency: {', '.join(currencies) if currencies else 'not reported'}",
        (
            f"- Execution latency P50/P95: {_number(metrics['latency_seconds']['p50'])} / "
            f"{_number(metrics['latency_seconds']['p95'])} s"
        ),
        "",
        "| Category | Scored | Mean score / 100 |",
        "|---|---:|---:|",
        *category_lines,
        "",
        "| Case | Rep | State | Success | Score | Failure |",
        "|---|---:|---|---|---:|---|",
    ]
    for row in rows:
        result = row.get("result", {})
        score = result.get("score_percent")
        lines.append(
            f"| {row['case_id']} | {row['repetition']} | {row['state']} | "
            f"{result.get('task_success', '')} | {f'{score:.1f}' if score is not None else ''} | "
            f"{row.get('failure_reason') or row.get('invalid_reason') or ''} |"
        )
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def _percent(value: float | None) -> str:
    return "n/a" if value is None else f"{value * 100:.1f}%"


def _number(value: float | None) -> str:
    return "n/a" if value is None else f"{value:.2f}"
