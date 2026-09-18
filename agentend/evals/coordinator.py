from __future__ import annotations

import asyncio
import shutil
import tempfile
from pathlib import Path
from typing import Any, Protocol

from .diffutils import bounded_diff
from .digests import canonical_digest
from .graders import AntiGamingGrader, CommandGrader, DiffScopeGrader, LLMQualityGrader, NoOpGrader, RunStateGrader
from .graders.base import GradeContext, Grader
from .langfuse_scores import publish_trial_scores
from .loader import LoadedDataset, restore_fixture
from .models import (
    AgentVisibleTask,
    CaseManifest,
    ExperimentSnapshot,
    GraderResult,
    GraderSpec,
    GraderStatus,
    TrialRecord,
    TrialState,
    agent_visible_task,
    trial_id_for,
)
from .repository import SQLiteEvalRepository
from .sandbox import CommandExecutor
from .scoring import score_case


class EvaluationCoordinator:
    def __init__(
        self,
        repository: SQLiteEvalRepository,
        executor: CommandExecutor,
        *,
        langfuse_client: Any | None = None,
        judge_client: Any | None = None,
    ) -> None:
        self.repository = repository
        self.executor = executor
        self.langfuse_client = langfuse_client
        self.judge_client = judge_client

    def baseline(self, dataset: LoadedDataset, case_id: str) -> list[GraderResult]:
        case = dataset.cases[case_id]
        with tempfile.TemporaryDirectory(prefix="agenthub-eval-baseline-") as temporary:
            workspace, base_revision = restore_fixture(dataset, case_id, Path(temporary) / "repo")
            context = GradeContext(
                trial_id=f"baseline-{case_id}",
                case=case,
                repository=workspace,
                base_revision=base_revision,
                final_revision=base_revision,
                hidden_assets=dataset.root / "hidden",
            )
            return [CommandGrader(spec, self.executor).grade(context) for spec in case.baseline]

    def baseline_valid(self, dataset: LoadedDataset, case_id: str) -> tuple[bool, list[GraderResult]]:
        results = self.baseline(dataset, case_id)
        specs = dataset.cases[case_id].baseline
        valid = len(results) == len(specs) and all(
            result.status.value == spec.expect for spec, result in zip(specs, results, strict=True)
        )
        return valid, results

    async def run_experiment(
        self,
        dataset: LoadedDataset,
        snapshot: ExperimentSnapshot,
        trial_executor: TrialExecutor,
        *,
        trial_root: Path,
    ) -> list[TrialRecord]:
        if snapshot.dataset_digest != dataset.digest:
            raise ValueError("experiment dataset digest mismatch")
        self.repository.create_experiment(snapshot)
        trial_root.mkdir(parents=True, exist_ok=True)
        slots = asyncio.Semaphore(snapshot.max_parallelism)

        async def run_one(case_id: str, repetition: int) -> TrialRecord:
            async with slots:
                trial_id = trial_id_for(snapshot.experiment_id, case_id, repetition)
                existing_trial = self.repository.get_trial(trial_id)
                if existing_trial is not None and existing_trial.state in {
                    TrialState.COMPLETED,
                    TrialState.FAILED,
                    TrialState.CANCELLED,
                    TrialState.INVALID,
                }:
                    return existing_trial
                if existing_trial is not None and existing_trial.state in {
                    TrialState.RUNNING,
                    TrialState.COLLECTING,
                    TrialState.GRADING,
                    TrialState.AWAITING_REVIEW,
                }:
                    # The caller must reconcile the authoritative Run before
                    # resuming; an unknown in-flight execution is never replayed.
                    return existing_trial
                if existing_trial is None:
                    self.repository.create_trial(
                        TrialRecord(
                            trial_id=trial_id,
                            experiment_id=snapshot.experiment_id,
                            case_id=case_id,
                            repetition=repetition,
                            fixture_digest=dataset.fixture_digests[case_id],
                        )
                    )
                workspace = trial_root / trial_id / "repo"
                if workspace.parent.exists():
                    shutil.rmtree(workspace.parent)
                workspace.parent.mkdir(parents=True, exist_ok=False)
                agent_started = False
                try:
                    workspace, base_revision = restore_fixture(dataset, case_id, workspace)
                    baseline_valid, _ = await asyncio.to_thread(self.baseline_valid, dataset, case_id)
                    if not baseline_valid:
                        return self._invalid_trial(
                            snapshot.experiment_id,
                            dataset,
                            case_id,
                            repetition,
                            "baseline_mismatch",
                            "fixture",
                        )
                    agent_started = True
                    final_revision, run_facts = await trial_executor.execute(
                        agent_visible_task(dataset.cases[case_id]),
                        workspace,
                        base_revision,
                        repetition,
                    )
                    trial, _, _ = self.grade_existing(
                        dataset,
                        experiment_id=snapshot.experiment_id,
                        case_id=case_id,
                        repetition=repetition,
                        repository_path=workspace,
                        base_revision=base_revision,
                        final_revision=final_revision,
                        run_facts=run_facts,
                        execution_image_digest=snapshot.execution_image,
                    )
                    return trial
                except Exception as exc:
                    existing = self.repository.get_trial(trial_id)
                    if existing is not None:
                        failed = existing.model_copy(
                            update={
                                "state": TrialState.FAILED if agent_started else TrialState.INVALID,
                                "failure_reason": type(exc).__name__ if agent_started else None,
                                "invalid_reason": None if agent_started else type(exc).__name__,
                                "responsibility": "agent" if agent_started else "infrastructure",
                            }
                        )
                        self.repository.update_trial(failed)
                        return failed
                    raise

        tasks = [
            asyncio.create_task(run_one(case_id, repetition))
            for case_id in dataset.manifest.case_ids
            for repetition in range(snapshot.repetitions)
        ]
        return list(await asyncio.gather(*tasks))

    def grade_existing(
        self,
        dataset: LoadedDataset,
        *,
        experiment_id: str,
        case_id: str,
        repetition: int,
        repository_path: Path,
        base_revision: str,
        final_revision: str,
        run_facts: dict[str, Any],
        execution_image_digest: str,
    ) -> tuple[TrialRecord, list[GraderResult], dict[str, Any]]:
        case = dataset.cases[case_id]
        trial_id = trial_id_for(experiment_id, case_id, repetition)
        trial = TrialRecord(
            trial_id=trial_id,
            experiment_id=experiment_id,
            case_id=case_id,
            repetition=repetition,
            fixture_digest=dataset.fixture_digests[case_id],
            state=TrialState.CREATED,
            root_run_id=run_facts.get("root_run_id"),
            trace_id=run_facts.get("trace_id"),
        )
        created = self.repository.create_trial(trial)
        if not created:
            existing = self.repository.get_trial(trial_id)
            if existing and existing.state == TrialState.COMPLETED:
                raise ValueError("completed trials are immutable; use a new experiment or grade rerun")
        for state in (
            TrialState.PREPARING,
            TrialState.BASELINE_CHECKING,
            TrialState.READY,
            TrialState.RUNNING,
            TrialState.COLLECTING,
            TrialState.GRADING,
        ):
            trial = trial.transition(state)
            self.repository.update_trial(trial)
        context = GradeContext(
            trial_id=trial_id,
            case=case,
            repository=repository_path.resolve(strict=True),
            base_revision=base_revision,
            final_revision=final_revision,
            hidden_assets=dataset.root / "hidden",
            run_facts=run_facts,
        )
        results = self._grade(context)
        hidden_digest = canonical_digest(dataset.hidden_asset_digests)
        grader_run_id = self.repository.begin_grader_run(
            trial_id,
            grader_set_digest=dataset.grader_set_digest,
            hidden_asset_digest=hidden_digest,
            execution_image_digest=execution_image_digest,
        )
        try:
            for result in results:
                self.repository.append_grader_result(grader_run_id, trial_id, result)
        finally:
            self.repository.finish_grader_run(grader_run_id)
        result_payload = self._aggregate_result(case, results, run_facts)
        result_payload["diff_summary"] = bounded_diff(
            repository_path,
            base_revision,
            final_revision,
        )
        final_trial = trial.model_copy(
            update={
                "state": TrialState.COMPLETED,
                "final_commit": final_revision,
            }
        )
        self.repository.update_trial(final_trial, result=result_payload)
        publish_trial_scores(
            self.langfuse_client,
            trace_id=final_trial.trace_id,
            result=result_payload,
        )
        return final_trial, results, result_payload

    def rerun_graders(
        self,
        dataset: LoadedDataset,
        trial: TrialRecord,
        *,
        repository_path: Path,
        base_revision: str,
        run_facts: dict[str, Any],
        execution_image_digest: str,
    ) -> list[GraderResult]:
        if not trial.final_commit:
            raise ValueError("trial has no frozen final commit")
        case = dataset.cases[trial.case_id]
        context = GradeContext(
            trial_id=trial.trial_id,
            case=case,
            repository=repository_path.resolve(strict=True),
            base_revision=base_revision,
            final_revision=trial.final_commit,
            hidden_assets=dataset.root / "hidden",
            run_facts=run_facts,
        )
        results = self._grade(context)
        grader_run_id = self.repository.begin_grader_run(
            trial.trial_id,
            grader_set_digest=dataset.grader_set_digest,
            hidden_asset_digest=canonical_digest(dataset.hidden_asset_digests),
            execution_image_digest=execution_image_digest,
        )
        try:
            for result in results:
                self.repository.append_grader_result(grader_run_id, trial.trial_id, result)
        finally:
            self.repository.finish_grader_run(grader_run_id)
        return results

    def _grade(self, context: GradeContext) -> list[GraderResult]:
        # Every grader runs: partial scoring needs the full evidence trail and
        # dataset YAML is expected to order cheap graders before llm_quality.
        results: list[GraderResult] = []
        for spec in context.case.graders:
            grader = self._grader(spec)
            results.append(grader.grade(context))
        return results

    def _grader(self, spec: GraderSpec) -> Grader:
        if spec.type == "run_state" or spec.type == "integration":
            return RunStateGrader()
        if spec.type == "git_diff":
            return DiffScopeGrader()
        if spec.type == "no_op":
            return NoOpGrader()
        if spec.type == "anti_gaming":
            return AntiGamingGrader()
        if spec.type in {"command", "hidden_command", "regression"}:
            return CommandGrader(spec, self.executor)
        if spec.type == "human_review":
            return _DeferredHumanReviewGrader()
        if spec.type == "llm_quality":
            return LLMQualityGrader(spec, client=self.judge_client)
        raise ValueError(f"unsupported grader: {spec.type}")

    def _invalid_trial(
        self,
        experiment_id: str,
        dataset: LoadedDataset,
        case_id: str,
        repetition: int,
        reason: str,
        responsibility: str,
    ) -> TrialRecord:
        trial = TrialRecord(
            trial_id=trial_id_for(experiment_id, case_id, repetition),
            experiment_id=experiment_id,
            case_id=case_id,
            repetition=repetition,
            fixture_digest=dataset.fixture_digests[case_id],
            state=TrialState.INVALID,
            invalid_reason=reason,
            responsibility=responsibility,
        )
        existing = self.repository.get_trial(trial.trial_id)
        if existing is None:
            self.repository.create_trial(trial)
        self.repository.update_trial(trial, result={"case_category": dataset.cases[case_id].category})
        return trial

    @staticmethod
    def _aggregate_result(
        case: CaseManifest,
        results: list[GraderResult],
        run_facts: dict[str, Any],
    ) -> dict[str, Any]:
        statuses = {result.grader: result.status.value for result in results}
        specs = case.graders
        required_passed = all(
            not spec.required or result.status == GraderStatus.PASSED
            for spec, result in zip(specs, results, strict=False)
        ) and len(results) == len(specs)
        scoring = score_case(case.category, results, weights=case.weights)
        has_hidden = any(spec.type == "hidden_command" for spec in specs)
        return {
            "case_category": case.category,
            "task_success": required_passed,
            "hidden_test_pass": statuses.get("hidden_command") == "passed" if has_hidden else None,
            "regression_free": statuses.get("regression") in {None, "passed"},
            "false_modification": statuses.get("git_diff") == "failed" or statuses.get("no_op") == "failed",
            "grader_statuses": statuses,
            "duration_seconds": run_facts.get("duration_seconds"),
            "total_tokens": run_facts.get("total_tokens"),
            "trial_cost": run_facts.get("trial_cost"),
            "provider_price_table_version": run_facts.get("provider_price_table_version"),
            "currency": run_facts.get("currency"),
            "usage_available": run_facts.get("total_tokens") is not None,
            "score_percent": scoring["score_percent"],
            "score_dimensions": scoring["score_dimensions"],
            "missing_dimensions": scoring["missing_dimensions"],
            "anti_gaming_capped": scoring["anti_gaming_capped"],
            "trace_url": run_facts.get("trace_url"),
            "retry_count": run_facts.get("retry_count", 0),
            "replan_count": run_facts.get("replan_count", 0),
            "conflict_recovery_count": run_facts.get("conflict_recovery_count", 0),
            "integration_status": run_facts.get("integration_status"),
            "plan_task_count": run_facts.get("plan_task_count"),
            "implementer_count": run_facts.get("implementer_count"),
            "max_concurrent_implementers": run_facts.get("max_concurrent_implementers"),
            "conflict_count": run_facts.get("conflict_count", 0),
            "integration_conflict_seen": run_facts.get("integration_conflict_seen", False),
            "resolution_completed_seen": run_facts.get("resolution_completed_seen", False),
        }


class _DeferredHumanReviewGrader:
    name = "human_review"
    version = "1.0.0"

    def grade(self, context: GradeContext) -> GraderResult:
        return GraderResult(
            grader=self.name,
            version=self.version,
            status=GraderStatus.SKIPPED,
            score=None,
            duration_ms=0,
            evidence_digest=canonical_digest({"trial_id": context.trial_id, "status": "awaiting_review"}),
            summary="awaiting human review",
        )


class TrialExecutor(Protocol):
    async def execute(
        self,
        task: AgentVisibleTask,
        workspace: Path,
        base_revision: str,
        repetition: int,
    ) -> tuple[str, dict[str, Any]]: ...


def copy_repository(source: Path, destination: Path) -> Path:
    if destination.exists():
        raise ValueError("destination already exists")
    shutil.copytree(source, destination, symlinks=True)
    return destination
