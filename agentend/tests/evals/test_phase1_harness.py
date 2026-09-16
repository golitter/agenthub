from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from evals.coordinator import EvaluationCoordinator
from evals.digests import canonical_digest, file_digest
from evals.loader import load_dataset
from evals.langfuse_scores import publish_trial_scores
from evals.metrics import aggregate_trials, paired_bootstrap, wilson_interval
from evals.models import ExperimentSnapshot, GraderStatus
from evals.repository import EvalConflictError, SQLiteEvalRepository
from evals.sandbox import CommandResult

DIGEST_A = "sha256:" + "a" * 64
DIGEST_B = "sha256:" + "b" * 64


class FakeExecutor:
    def __init__(self, result: CommandResult | None = None) -> None:
        self.result = result or CommandResult(("pytest",), 0, "2 passed", "", 5)

    def run(self, argv, **_kwargs) -> CommandResult:
        return CommandResult(tuple(argv), self.result.exit_code, self.result.stdout, self.result.stderr, 5)


def git(repository: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repository), *args],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()


def make_dataset(tmp_path: Path) -> tuple[Path, Path, str]:
    source = tmp_path / "source"
    source.mkdir()
    git(source, "init", "-q", "-b", "main")
    git(source, "config", "user.email", "eval@example.invalid")
    git(source, "config", "user.name", "Eval Fixture")
    (source / "app.py").write_text("VALUE = 1\n", encoding="utf-8")
    git(source, "add", "app.py")
    git(source, "commit", "-q", "-m", "fixture")
    base = git(source, "rev-parse", "HEAD")

    dataset_root = tmp_path / "dataset"
    (dataset_root / "cases").mkdir(parents=True)
    (dataset_root / "fixtures").mkdir()
    (dataset_root / "hidden" / "behavior-v1").mkdir(parents=True)
    (dataset_root / "hidden" / "behavior-v1" / "test_hidden.py").write_text(
        "def test_hidden(): assert True\n", encoding="utf-8"
    )
    bundle = dataset_root / "fixtures" / "bugfix-001.bundle"
    subprocess.run(
        ["git", "-C", str(source), "bundle", "create", str(bundle), "main"],
        check=True,
        capture_output=True,
    )
    fixture_digest = file_digest(bundle)
    (dataset_root / "dataset.yaml").write_text(
        """schema_version: 1
dataset_id: test-coding-v1
version: 1.0.0
description: Test dataset
case_ids: [bugfix-001]
""",
        encoding="utf-8",
    )
    (dataset_root / "cases" / "bugfix-001.yaml").write_text(
        f"""schema_version: 1
case_id: bugfix-001
category: bugfix
difficulty: easy
fixture:
  source: fixtures/bugfix-001.bundle
  sha256: {fixture_digest}
  base_ref: main
prompt: Fix VALUE.
scope:
  allowed_paths: [app.py]
  forbidden_paths: [secrets/**]
graders:
  - type: run_state
  - type: git_diff
  - type: hidden_command
    asset_id: behavior-v1
    argv: [python, /eval-hidden/test_hidden.py]
expected:
  require_change: true
  require_commit: true
  max_changed_files: 1
""",
        encoding="utf-8",
    )
    return dataset_root, source, base


def experiment(dataset_digest: str) -> ExperimentSnapshot:
    return ExperimentSnapshot(
        schema_version=1,
        experiment_id="exp-test-a",
        dataset_id="test-coding-v1",
        dataset_version="1.0.0",
        dataset_digest=dataset_digest,
        system_revision="1234567",
        prompt_revision="v1",
        agent_type="fake",
        model="fake/model",
        execution_image="example/eval@sha256:" + "c" * 64,
        dependency_cache_digest=DIGEST_B,
    )


def test_dataset_load_grade_store_and_rerun_history(tmp_path: Path) -> None:
    dataset_root, repository_path, base = make_dataset(tmp_path)
    loaded = load_dataset(dataset_root, environment_digest=DIGEST_A)
    repository = SQLiteEvalRepository(tmp_path / "evals.sqlite3")
    assert repository.create_experiment(experiment(loaded.digest)) is True

    (repository_path / "app.py").write_text("VALUE = 2\n", encoding="utf-8")
    git(repository_path, "add", "app.py")
    git(repository_path, "commit", "-q", "-m", "fix")
    final = git(repository_path, "rev-parse", "HEAD")
    coordinator = EvaluationCoordinator(repository, FakeExecutor())
    trial, results, payload = coordinator.grade_existing(
        loaded,
        experiment_id="exp-test-a",
        case_id="bugfix-001",
        repetition=0,
        repository_path=repository_path,
        base_revision=base,
        final_revision=final,
        run_facts={"run_state": "completed", "integration_status": "merged", "total_tokens": 10},
        execution_image_digest=DIGEST_A,
    )
    assert payload["task_success"] is True
    assert all(result.status == GraderStatus.PASSED for result in results)
    assert len(repository.grader_history(trial.trial_id)) == 1

    coordinator.rerun_graders(
        loaded,
        trial,
        repository_path=repository_path,
        base_revision=base,
        run_facts={"run_state": "completed", "integration_status": "merged"},
        execution_image_digest=DIGEST_A,
    )
    history = repository.grader_history(trial.trial_id)
    assert len(history) == 2
    assert history[0]["grader_run_id"] != history[1]["grader_run_id"]
    repository.close()


def test_repository_experiment_idempotency_and_review_does_not_mutate_score(tmp_path: Path) -> None:
    repository = SQLiteEvalRepository(tmp_path / "evals.sqlite3")
    snapshot = experiment(DIGEST_A)
    assert repository.create_experiment(snapshot) is True
    assert repository.create_experiment(snapshot) is False
    with pytest.raises(EvalConflictError):
        repository.create_experiment(snapshot.model_copy(update={"model": "other/model"}))
    repository.close()


def test_metrics_exclude_invalid_and_report_uncertainty() -> None:
    rows = [
        {"state": "completed", "result": {"task_success": True, "usage_available": True, "duration_seconds": 1}},
        {"state": "failed", "result": {"task_success": False, "usage_available": False, "duration_seconds": 3}},
        {"state": "invalid", "result": {}},
    ]
    metrics = aggregate_trials(rows)
    assert metrics["valid_trials"] == 2
    assert metrics["task_success"]["numerator"] == 1
    assert metrics["task_success"]["denominator"] == 2
    assert metrics["latency_seconds"]["p50"] == 2.0
    assert metrics["latency_seconds"]["p95"] == pytest.approx(2.9)
    assert wilson_interval(0, 0).value is None

    bootstrap = paired_bootstrap([(10, 8), (20, 15), (30, 29)], samples=100, seed=7)
    assert bootstrap["pairs"] == 3
    assert bootstrap["delta"] < 0


def test_langfuse_scores_are_best_effort_and_exclude_aggregate_cost_per_success() -> None:
    class Client:
        def __init__(self) -> None:
            self.scores = []

        def create_score(self, **kwargs) -> None:
            self.scores.append(kwargs)

    client = Client()
    assert publish_trial_scores(
        client,
        trace_id="trace-a",
        result={
            "task_success": True,
            "hidden_test_pass": False,
            "duration_seconds": 1.5,
            "total_tokens": 12,
            "cost_per_success": 99,
        },
    )
    names = {score["name"] for score in client.scores}
    assert names == {"task_success", "hidden_test_pass", "duration_seconds", "total_tokens"}


def test_diff_scope_rejects_forbidden_change(tmp_path: Path) -> None:
    dataset_root, repository_path, base = make_dataset(tmp_path)
    case_path = dataset_root / "cases" / "bugfix-001.yaml"
    case_path.write_text(
        case_path.read_text(encoding="utf-8").replace("allowed_paths: [app.py]", "allowed_paths: ['**']"),
        encoding="utf-8",
    )
    loaded = load_dataset(dataset_root, environment_digest=DIGEST_A)
    repository = SQLiteEvalRepository(tmp_path / "evals.sqlite3")
    repository.create_experiment(experiment(loaded.digest))
    (repository_path / "secrets").mkdir()
    (repository_path / "secrets" / "key.txt").write_text("not-a-secret", encoding="utf-8")
    git(repository_path, "add", "secrets/key.txt")
    git(repository_path, "commit", "-q", "-m", "bad")
    final = git(repository_path, "rev-parse", "HEAD")
    coordinator = EvaluationCoordinator(repository, FakeExecutor())
    _, results, payload = coordinator.grade_existing(
        loaded,
        experiment_id="exp-test-a",
        case_id="bugfix-001",
        repetition=0,
        repository_path=repository_path,
        base_revision=base,
        final_revision=final,
        run_facts={"run_state": "completed", "integration_status": "merged"},
        execution_image_digest=DIGEST_A,
    )
    assert results[-1].grader == "git_diff"
    assert results[-1].status == GraderStatus.FAILED
    assert payload["task_success"] is False
    repository.close()


@pytest.mark.asyncio
async def test_experiment_coordinator_runs_paired_repetitions(tmp_path: Path) -> None:
    dataset_root, _source, _base = make_dataset(tmp_path)
    loaded = load_dataset(dataset_root, environment_digest=DIGEST_A)
    repository = SQLiteEvalRepository(tmp_path / "evals.sqlite3")
    coordinator = EvaluationCoordinator(repository, FakeExecutor())

    class FakeTrialExecutor:
        async def execute(self, task, workspace, _base_revision, repetition):
            assert set(task.model_dump()) == {
                "case_id",
                "prompt",
                "timeout_seconds",
                "max_turns",
                "network",
            }
            (workspace / "app.py").write_text(f"VALUE = {repetition + 2}\n", encoding="utf-8")
            git(workspace, "config", "user.email", "eval@example.invalid")
            git(workspace, "config", "user.name", "Eval Agent")
            git(workspace, "add", "app.py")
            git(workspace, "commit", "-q", "-m", "fake agent patch")
            return git(workspace, "rev-parse", "HEAD"), {
                "run_state": "completed",
                "integration_status": "merged",
                "total_tokens": 7,
            }

    snapshot = experiment(loaded.digest).model_copy(update={"repetitions": 3})
    trials = await coordinator.run_experiment(
        loaded,
        snapshot,
        FakeTrialExecutor(),
        trial_root=tmp_path / "trials",
    )
    assert len(trials) == 3
    assert len({trial.trial_id for trial in trials}) == 3
    assert all(row["result"]["task_success"] for row in repository.list_trials(snapshot.experiment_id))
    repository.close()
