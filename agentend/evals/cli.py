from __future__ import annotations

import argparse
import json
import tempfile
from pathlib import Path

import yaml

from src.app.config import settings

from .batch import add_parser as add_batch_parser
from .coordinator import EvaluationCoordinator
from .loader import DatasetValidationError, load_dataset
from .metrics import aggregate_trials, parallel_speedup
from .models import ExperimentSnapshot
from .readiness import BatchEvalBlocked, current_batch_eval_readiness
from .report import write_csv, write_jsonl, write_markdown
from .repository import SQLiteEvalRepository
from .sandbox import BubblewrapGraderSandbox, GraderSandboxUnavailable


def _readiness_payload() -> dict[str, object]:
    readiness = current_batch_eval_readiness(
        sandbox_mode=settings.execution.sandbox.mode,
        sandbox_backend=settings.execution.sandbox.backend,
    )
    return {
        "ready": readiness.ready,
        "sandbox_mode": readiness.sandbox_mode,
        "sandbox_backend": readiness.sandbox_backend,
        "missing_capabilities": readiness.missing_capabilities,
        "capabilities": dict(readiness.capabilities),
    }


def _load_result_rows(path: Path) -> list[dict]:
    resolved = path / "results.jsonl" if path.is_dir() else path
    rows = []
    for line in resolved.read_text(encoding="utf-8").splitlines():
        if line.strip():
            rows.append(json.loads(line))
    return rows


def run_speedup(args: argparse.Namespace) -> int:
    payload = parallel_speedup(
        _load_result_rows(args.serial),
        _load_result_rows(args.parallel),
        score_tolerance_points=args.score_tolerance,
    )
    rendered = json.dumps(payload, ensure_ascii=False, sort_keys=True)
    if args.output is not None:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(rendered + "\n", encoding="utf-8")
    print(rendered)
    return 0 if payload["reportable"] else 1


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m evals.cli")
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("readiness", help="show whether unattended batch evaluation is safe")
    validate = subparsers.add_parser("validate", help="validate a versioned dataset and all asset digests")
    validate.add_argument("dataset", type=Path)
    validate.add_argument("--environment-digest", required=True)
    baseline = subparsers.add_parser("baseline", help="run isolated baseline checks without an Agent")
    baseline.add_argument("dataset", type=Path)
    baseline.add_argument("--environment-digest", required=True)
    baseline.add_argument("--case")
    baseline.add_argument("--summary", action="store_true")
    report = subparsers.add_parser("report", help="export one experiment from the local result store")
    report.add_argument("--database", type=Path, required=True)
    report.add_argument("--experiment", required=True)
    report.add_argument("--output", type=Path, required=True)
    grade = subparsers.add_parser("grade", help="append an immutable grader rerun for a frozen Trial")
    grade.add_argument("--database", type=Path, required=True)
    grade.add_argument("--dataset", type=Path, required=True)
    grade.add_argument("--environment-digest", required=True)
    grade.add_argument("--trial", required=True)
    grade.add_argument("--repository", type=Path, required=True)
    grade.add_argument("--base-revision", required=True)
    grade.add_argument("--execution-image-digest", required=True)
    grade.add_argument("--run-facts", type=Path)
    review = subparsers.add_parser("review", help="append a human Diff review without changing Agent score")
    review.add_argument("--database", type=Path, required=True)
    review.add_argument("--trial", required=True)
    review.add_argument("--reviewer", required=True)
    review.add_argument("--decision", required=True)
    review.add_argument("--reviewed-commit", required=True)
    review.add_argument("--amended-commit")
    review.add_argument("--comment", default="")
    speedup = subparsers.add_parser(
        "speedup",
        help="paired serial-vs-parallel speedup over two batch arms (offline math, no gates)",
    )
    speedup.add_argument("--serial", type=Path, required=True,
                         help="results.jsonl of the --arm serial batch (or its output directory)")
    speedup.add_argument("--parallel", type=Path, required=True,
                         help="results.jsonl of the --arm parallel batch (or its output directory)")
    speedup.add_argument("--score-tolerance", type=float, default=5.0,
                         help="max tolerated score_percent drop (points) before speedup is withheld")
    speedup.add_argument("--output", type=Path, default=None, help="also write the JSON payload here")
    speedup.set_defaults(func=run_speedup)
    add_batch_parser(subparsers)
    experiment = subparsers.add_parser("experiment", help="start a batch experiment after strict readiness")
    experiment.add_argument("--dataset", type=Path, required=True)
    experiment.add_argument("--environment-digest", required=True)
    experiment.add_argument("--config", type=Path, required=True)
    experiment.add_argument("--database", type=Path, required=True)
    args = parser.parse_args(argv)

    if getattr(args, "func", None) is not None:
        return args.func(args)

    if args.command == "validate":
        try:
            dataset = load_dataset(args.dataset, environment_digest=args.environment_digest)
        except DatasetValidationError as exc:
            parser.error(str(exc))
        print(
            json.dumps(
                {
                    "dataset_id": dataset.manifest.dataset_id,
                    "version": dataset.manifest.version,
                    "case_count": len(dataset.cases),
                    "digest": dataset.digest,
                },
                ensure_ascii=False,
                sort_keys=True,
            )
        )
        return 0

    if args.command == "baseline":
        try:
            dataset = load_dataset(args.dataset, environment_digest=args.environment_digest)
            case_ids = [args.case] if args.case else dataset.manifest.case_ids
            if any(case_id not in dataset.cases for case_id in case_ids):
                parser.error("unknown case id")
            sandbox = BubblewrapGraderSandbox()
            sandbox.require_ready()
            with tempfile.TemporaryDirectory(prefix="agenthub-eval-cli-") as temporary:
                repository = SQLiteEvalRepository(Path(temporary) / "evals.sqlite3")
                try:
                    coordinator = EvaluationCoordinator(repository, sandbox)
                    output = {}
                    valid = True
                    for case_id in case_ids:
                        case_valid, results = coordinator.baseline_valid(dataset, case_id)
                        valid = valid and case_valid
                        output[case_id] = {
                            "valid": case_valid,
                            "results": [result.model_dump(mode="json") for result in results],
                        }
                finally:
                    repository.close()
        except (DatasetValidationError, GraderSandboxUnavailable) as exc:
            parser.error(str(exc))
        if args.summary:
            print(
                json.dumps(
                    {
                        "dataset_id": dataset.manifest.dataset_id,
                        "cases": len(output),
                        "valid": sum(1 for item in output.values() if item["valid"]),
                    },
                    ensure_ascii=False,
                    sort_keys=True,
                )
            )
        else:
            print(json.dumps(output, ensure_ascii=False, sort_keys=True))
        return 0 if valid else 1

    if args.command == "report":
        repository = SQLiteEvalRepository(args.database)
        try:
            experiments = {
                item["experiment_id"]: item for item in repository.list_experiments()
            }
            selected = experiments.get(args.experiment)
            if selected is None:
                parser.error("experiment not found")
            rows = repository.list_trials(args.experiment)
            output = args.output
            write_jsonl(rows, output.with_suffix(".jsonl"))
            write_csv(rows, output.with_suffix(".csv"))
            write_markdown(selected, rows, output.with_suffix(".md"))
            print(json.dumps(aggregate_trials(rows), ensure_ascii=False, sort_keys=True))
            return 0
        finally:
            repository.close()

    if args.command == "grade":
        dataset = load_dataset(args.dataset, environment_digest=args.environment_digest)
        repository = SQLiteEvalRepository(args.database)
        try:
            trial = repository.get_trial(args.trial)
            if trial is None:
                parser.error("trial not found")
            facts = json.loads(args.run_facts.read_text(encoding="utf-8")) if args.run_facts else {}
            coordinator = EvaluationCoordinator(repository, BubblewrapGraderSandbox())
            results = coordinator.rerun_graders(
                dataset,
                trial,
                repository_path=args.repository,
                base_revision=args.base_revision,
                run_facts=facts,
                execution_image_digest=args.execution_image_digest,
            )
            print(json.dumps([result.model_dump(mode="json") for result in results], ensure_ascii=False))
            return 0
        finally:
            repository.close()

    if args.command == "review":
        repository = SQLiteEvalRepository(args.database)
        try:
            trial = repository.get_trial(args.trial)
            if trial is None:
                parser.error("trial not found")
            if trial.final_commit != args.reviewed_commit:
                parser.error("reviewed commit is not the frozen Agent commit")
            review_id = repository.add_review(
                trial_id=args.trial,
                reviewer_id=args.reviewer,
                decision=args.decision,
                reviewed_commit=args.reviewed_commit,
                amended_commit=args.amended_commit,
                comment=args.comment,
            )
            print(json.dumps({"review_id": review_id, "task_success_unchanged": True}))
            return 0
        finally:
            repository.close()

    payload = _readiness_payload()
    print(json.dumps(payload, ensure_ascii=False, sort_keys=True))
    if args.command == "readiness":
        return 0 if payload["ready"] else 2

    readiness = current_batch_eval_readiness(
        sandbox_mode=settings.execution.sandbox.mode,
        sandbox_backend=settings.execution.sandbox.backend,
    )
    try:
        readiness.require()
    except BatchEvalBlocked as exc:
        parser.error(str(exc))
    try:
        snapshot = ExperimentSnapshot.model_validate(
            yaml.safe_load(args.config.read_text(encoding="utf-8"))
        )
        dataset = load_dataset(args.dataset, environment_digest=args.environment_digest)
    except (OSError, ValueError, DatasetValidationError, yaml.YAMLError) as exc:
        parser.error(str(exc))
    if snapshot.dataset_digest != dataset.digest:
        parser.error("experiment dataset_digest does not match validated dataset")
    repository = SQLiteEvalRepository(args.database)
    try:
        created = repository.create_experiment(snapshot)
    finally:
        repository.close()
    print(json.dumps({"experiment_id": snapshot.experiment_id, "created": created, "status": "created"}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
