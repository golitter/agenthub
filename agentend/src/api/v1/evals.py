from __future__ import annotations

import statistics

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, ConfigDict, Field

from evals.loader import DatasetValidationError, load_dataset
from evals.metrics import aggregate_trials, paired_bootstrap
from evals.repository import SQLiteEvalRepository
from src.app.config import settings

router = APIRouter(prefix="/v1/evals", tags=["evals"])


class ReviewRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    reviewer_id: str = Field(min_length=1, max_length=200)
    decision: str
    reviewed_commit: str = Field(min_length=7, max_length=128)
    amended_commit: str | None = Field(default=None, min_length=7, max_length=128)
    reason_codes: list[str] = Field(default_factory=list)
    comment: str = Field(default="", max_length=10000)


def _repository() -> SQLiteEvalRepository:
    return SQLiteEvalRepository(settings.evals.resolve_path(settings.evals.database_path))


@router.get("/datasets")
async def list_datasets() -> list[dict]:
    datasets_root = settings.evals.resolve_path(settings.evals.datasets_dir)
    output = []
    if not datasets_root.is_dir():
        return output
    for manifest_path in sorted(datasets_root.glob("*/dataset.yaml")):
        try:
            loaded = load_dataset(
                manifest_path.parent,
                environment_digest=settings.evals.environment_digest,
            )
            output.append(
                {
                    "dataset_id": loaded.manifest.dataset_id,
                    "version": loaded.manifest.version,
                    "description": loaded.manifest.description,
                    "case_count": len(loaded.cases),
                    "digest": loaded.digest,
                }
            )
        except DatasetValidationError as exc:
            output.append({"dataset_id": manifest_path.parent.name, "invalid_reason": str(exc)})
    return output


@router.get("/experiments")
async def list_experiments() -> list[dict]:
    repository = _repository()
    try:
        output = []
        for experiment in repository.list_experiments():
            rows = repository.list_trials(experiment["experiment_id"])
            output.append({**experiment, "metrics": aggregate_trials(rows)})
        return output
    finally:
        repository.close()


@router.get("/experiments/{experiment_id}/trials")
async def list_trials(experiment_id: str) -> list[dict]:
    repository = _repository()
    try:
        if repository.get_experiment(experiment_id) is None:
            raise HTTPException(status_code=404, detail="experiment not found")
        return repository.list_trials(experiment_id)
    finally:
        repository.close()


@router.get("/compare")
async def compare_experiments(baseline: str, candidate: str) -> dict:
    repository = _repository()
    try:
        baseline_rows = repository.list_trials(baseline)
        candidate_rows = repository.list_trials(candidate)
        if repository.get_experiment(baseline) is None or repository.get_experiment(candidate) is None:
            raise HTTPException(status_code=404, detail="experiment not found")
        baseline_by_case = _case_samples(baseline_rows)
        candidate_by_case = _case_samples(candidate_rows)
        case_ids = sorted(set(baseline_by_case) & set(candidate_by_case))
        success_pairs = [
            (baseline_by_case[case_id]["success"], candidate_by_case[case_id]["success"])
            for case_id in case_ids
        ]
        duration_pairs = [
            (baseline_by_case[case_id]["duration"], candidate_by_case[case_id]["duration"])
            for case_id in case_ids
            if baseline_by_case[case_id]["duration"] is not None
            and candidate_by_case[case_id]["duration"] is not None
        ]
        return {
            "baseline": baseline,
            "candidate": candidate,
            "paired_cases": len(case_ids),
            "success_delta": paired_bootstrap(success_pairs),
            "duration_delta_seconds": paired_bootstrap(duration_pairs),
        }
    finally:
        repository.close()


def _case_samples(rows: list[dict]) -> dict[str, dict[str, float | None]]:
    grouped: dict[str, list[dict]] = {}
    for row in rows:
        if row["state"] == "invalid":
            continue
        grouped.setdefault(row["case_id"], []).append(row)
    output = {}
    for case_id, samples in grouped.items():
        durations = [
            float(row["result"]["duration_seconds"])
            for row in samples
            if row["result"].get("duration_seconds") is not None
        ]
        output[case_id] = {
            "success": statistics.mean(float(bool(row["result"].get("task_success"))) for row in samples),
            "duration": statistics.median(durations) if durations else None,
        }
    return output


@router.get("/trials/{trial_id}")
async def get_trial(trial_id: str) -> dict:
    repository = _repository()
    try:
        trial = repository.get_trial(trial_id)
        if trial is None:
            raise HTTPException(status_code=404, detail="trial not found")
        rows = repository.list_trials(trial.experiment_id)
        row = next(item for item in rows if item["trial_id"] == trial_id)
        return {
            **row,
            "grader_history": repository.grader_history(trial_id),
            "reviews": repository.list_reviews(trial_id),
        }
    finally:
        repository.close()


@router.post("/trials/{trial_id}/reviews", status_code=201)
async def create_review(trial_id: str, request: ReviewRequest) -> dict:
    repository = _repository()
    try:
        trial = repository.get_trial(trial_id)
        if trial is None:
            raise HTTPException(status_code=404, detail="trial not found")
        if not trial.final_commit or request.reviewed_commit != trial.final_commit:
            raise HTTPException(status_code=409, detail="reviewed_commit is not the frozen Agent commit")
        review_id = repository.add_review(
            trial_id=trial_id,
            reviewer_id=request.reviewer_id,
            decision=request.decision,
            reviewed_commit=request.reviewed_commit,
            amended_commit=request.amended_commit,
            reason_codes=request.reason_codes,
            comment=request.comment,
        )
        return {"review_id": review_id, "task_success_unchanged": True}
    finally:
        repository.close()
