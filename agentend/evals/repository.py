from __future__ import annotations

import json
import sqlite3
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .digests import canonical_digest
from .models import ExperimentSnapshot, GraderResult, TrialRecord, TrialState


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


class EvalConflictError(ValueError):
    pass


class SQLiteEvalRepository:
    """Single-writer authoritative store for immutable evaluation evidence."""

    def __init__(self, path: str | Path) -> None:
        self.path = Path(path)
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self._db = sqlite3.connect(self.path, timeout=5.0)
        self._db.row_factory = sqlite3.Row
        self._db.execute("PRAGMA journal_mode=WAL")
        self._db.execute("PRAGMA foreign_keys=ON")
        self._db.execute("PRAGMA busy_timeout=5000")
        self._init_schema()

    def _init_schema(self) -> None:
        self._db.executescript(
            """
            CREATE TABLE IF NOT EXISTS experiments (
                experiment_id TEXT PRIMARY KEY,
                dataset_id TEXT NOT NULL,
                dataset_version TEXT NOT NULL,
                dataset_digest TEXT NOT NULL,
                system_revision TEXT NOT NULL,
                prompt_revision TEXT NOT NULL,
                model_config_json TEXT NOT NULL,
                execution_config_json TEXT NOT NULL,
                config_digest TEXT NOT NULL,
                execution_image_digest TEXT NOT NULL,
                dependency_cache_digest TEXT NOT NULL,
                status TEXT NOT NULL,
                created_at TEXT NOT NULL,
                finished_at TEXT
            );
            CREATE TABLE IF NOT EXISTS trials (
                trial_id TEXT PRIMARY KEY,
                experiment_id TEXT NOT NULL REFERENCES experiments(experiment_id),
                case_id TEXT NOT NULL,
                repetition INTEGER NOT NULL,
                seed INTEGER,
                root_run_id TEXT,
                trace_id TEXT,
                fixture_digest TEXT NOT NULL,
                final_commit TEXT,
                state TEXT NOT NULL,
                failure_reason TEXT,
                invalid_reason TEXT,
                responsibility TEXT,
                started_at TEXT NOT NULL,
                finished_at TEXT,
                result_json TEXT NOT NULL DEFAULT '{}',
                UNIQUE(experiment_id, case_id, repetition)
            );
            CREATE INDEX IF NOT EXISTS idx_trials_experiment ON trials(experiment_id, case_id, repetition);
            CREATE TABLE IF NOT EXISTS grader_runs (
                grader_run_id TEXT PRIMARY KEY,
                trial_id TEXT NOT NULL REFERENCES trials(trial_id),
                grader_set_digest TEXT NOT NULL,
                hidden_asset_digest TEXT NOT NULL,
                execution_image_digest TEXT NOT NULL,
                started_at TEXT NOT NULL,
                finished_at TEXT
            );
            CREATE TABLE IF NOT EXISTS grader_results (
                grader_result_id TEXT PRIMARY KEY,
                grader_run_id TEXT NOT NULL REFERENCES grader_runs(grader_run_id),
                trial_id TEXT NOT NULL REFERENCES trials(trial_id),
                grader_name TEXT NOT NULL,
                grader_version TEXT NOT NULL,
                status TEXT NOT NULL,
                score REAL,
                evidence_digest TEXT NOT NULL,
                summary TEXT NOT NULL,
                duration_ms INTEGER NOT NULL,
                result_json TEXT NOT NULL,
                UNIQUE(grader_run_id, grader_name)
            );
            CREATE TABLE IF NOT EXISTS review_records (
                review_id TEXT PRIMARY KEY,
                trial_id TEXT NOT NULL REFERENCES trials(trial_id),
                reviewer_id TEXT NOT NULL,
                decision TEXT NOT NULL,
                reviewed_commit TEXT NOT NULL,
                amended_commit TEXT,
                reason_codes_json TEXT NOT NULL,
                comment TEXT NOT NULL,
                created_at TEXT NOT NULL
            );
            """
        )
        self._db.commit()

    def close(self) -> None:
        self._db.close()

    def create_experiment(self, snapshot: ExperimentSnapshot) -> bool:
        payload = snapshot.model_dump(mode="json")
        model_config = {
            "agent_type": snapshot.agent_type,
            "model": snapshot.model,
            "model_parameters": snapshot.model_parameters,
            "seed_policy": snapshot.seed_policy,
        }
        execution_config = {
            "max_parallelism": snapshot.max_parallelism,
            "repetitions": snapshot.repetitions,
        }
        try:
            self._db.execute(
                """INSERT INTO experiments (
                       experiment_id, dataset_id, dataset_version, dataset_digest,
                       system_revision, prompt_revision, model_config_json,
                       execution_config_json, config_digest, execution_image_digest,
                       dependency_cache_digest, status, created_at
                   ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'created', ?)""",
                (
                    snapshot.experiment_id,
                    snapshot.dataset_id,
                    snapshot.dataset_version,
                    snapshot.dataset_digest,
                    snapshot.system_revision,
                    snapshot.prompt_revision,
                    json.dumps(model_config, sort_keys=True),
                    json.dumps(execution_config, sort_keys=True),
                    canonical_digest(payload),
                    snapshot.execution_image,
                    snapshot.dependency_cache_digest,
                    utc_now(),
                ),
            )
            self._db.commit()
            return True
        except sqlite3.IntegrityError:
            row = self._db.execute(
                "SELECT config_digest FROM experiments WHERE experiment_id = ?",
                (snapshot.experiment_id,),
            ).fetchone()
            if not row or row["config_digest"] != canonical_digest(payload):
                raise EvalConflictError("experiment_id already exists with different immutable config")
            return False

    def create_trial(self, trial: TrialRecord) -> bool:
        try:
            self._db.execute(
                """INSERT INTO trials (
                       trial_id, experiment_id, case_id, repetition, seed, root_run_id,
                       trace_id, fixture_digest, final_commit, state, failure_reason,
                       invalid_reason, responsibility, started_at
                   ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                (
                    trial.trial_id,
                    trial.experiment_id,
                    trial.case_id,
                    trial.repetition,
                    trial.seed,
                    trial.root_run_id,
                    trial.trace_id,
                    trial.fixture_digest,
                    trial.final_commit,
                    trial.state.value,
                    trial.failure_reason,
                    trial.invalid_reason,
                    trial.responsibility,
                    utc_now(),
                ),
            )
            self._db.commit()
            return True
        except sqlite3.IntegrityError:
            existing = self.get_trial(trial.trial_id)
            if existing is None:
                # Either a non-identity constraint failure (e.g. FOREIGN KEY for
                # a missing experiment: surface the original error) or the
                # natural key (experiment_id, case_id, repetition) already holds
                # a row under a different trial_id (a genuine identity clash).
                clash = self._db.execute(
                    "SELECT trial_id FROM trials WHERE experiment_id = ? AND case_id = ? AND repetition = ?",
                    (trial.experiment_id, trial.case_id, trial.repetition),
                ).fetchone()
                if clash is not None:
                    raise EvalConflictError(
                        "trial identity already exists with different immutable facts "
                        f"(existing trial_id: {clash['trial_id']})"
                    )
                raise
            # Only immutable identity facts participate in the conflict check:
            # state / root_run_id / trace_id / final_commit evolve via
            # update_trial, so run_experiment -> grade_existing must stay
            # idempotent even when run_facts add the run identity later.
            immutable = ("trial_id", "experiment_id", "case_id", "repetition", "fixture_digest", "seed")
            if any(getattr(existing, name) != getattr(trial, name) for name in immutable):
                raise EvalConflictError("trial identity already exists with different immutable facts")
            return False

    def get_trial(self, trial_id: str) -> TrialRecord | None:
        row = self._db.execute("SELECT * FROM trials WHERE trial_id = ?", (trial_id,)).fetchone()
        return self._trial_from_row(row) if row else None

    def update_trial(self, trial: TrialRecord, *, result: dict[str, Any] | None = None) -> None:
        finished_at = utc_now() if trial.state in {
            TrialState.COMPLETED,
            TrialState.FAILED,
            TrialState.CANCELLED,
            TrialState.INVALID,
        } else None
        cursor = self._db.execute(
            """UPDATE trials SET root_run_id = ?, trace_id = ?, final_commit = ?, state = ?,
                      failure_reason = ?, invalid_reason = ?, responsibility = ?,
                      finished_at = COALESCE(?, finished_at), result_json = COALESCE(?, result_json)
               WHERE trial_id = ?""",
            (
                trial.root_run_id,
                trial.trace_id,
                trial.final_commit,
                trial.state.value,
                trial.failure_reason,
                trial.invalid_reason,
                trial.responsibility,
                finished_at,
                json.dumps(result, ensure_ascii=False, sort_keys=True) if result is not None else None,
                trial.trial_id,
            ),
        )
        if cursor.rowcount != 1:
            raise KeyError(trial.trial_id)
        self._db.commit()

    def begin_grader_run(
        self,
        trial_id: str,
        *,
        grader_set_digest: str,
        hidden_asset_digest: str,
        execution_image_digest: str,
    ) -> str:
        grader_run_id = f"grader-{uuid.uuid4().hex}"
        self._db.execute(
            """INSERT INTO grader_runs (
                   grader_run_id, trial_id, grader_set_digest, hidden_asset_digest,
                   execution_image_digest, started_at
               ) VALUES (?, ?, ?, ?, ?, ?)""",
            (
                grader_run_id,
                trial_id,
                grader_set_digest,
                hidden_asset_digest,
                execution_image_digest,
                utc_now(),
            ),
        )
        self._db.commit()
        return grader_run_id

    def append_grader_result(self, grader_run_id: str, trial_id: str, result: GraderResult) -> None:
        self._db.execute(
            """INSERT INTO grader_results (
                   grader_result_id, grader_run_id, trial_id, grader_name, grader_version,
                   status, score, evidence_digest, summary, duration_ms, result_json
               ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            (
                f"result-{uuid.uuid4().hex}",
                grader_run_id,
                trial_id,
                result.grader,
                result.version,
                result.status.value,
                result.score,
                result.evidence_digest,
                result.summary,
                result.duration_ms,
                result.model_dump_json(),
            ),
        )
        self._db.commit()

    def finish_grader_run(self, grader_run_id: str) -> None:
        self._db.execute(
            "UPDATE grader_runs SET finished_at = ? WHERE grader_run_id = ? AND finished_at IS NULL",
            (utc_now(), grader_run_id),
        )
        self._db.commit()

    def add_review(
        self,
        *,
        trial_id: str,
        reviewer_id: str,
        decision: str,
        reviewed_commit: str,
        amended_commit: str | None = None,
        reason_codes: list[str] | None = None,
        comment: str = "",
    ) -> str:
        allowed = {
            "accepted",
            "accepted_with_changes",
            "rejected_incorrect",
            "rejected_regression",
            "rejected_overbroad",
            "rejected_unmaintainable",
        }
        if decision not in allowed:
            raise ValueError("unsupported review decision")
        if decision == "accepted_with_changes" and not amended_commit:
            raise ValueError("accepted_with_changes requires amended_commit")
        review_id = f"review-{uuid.uuid4().hex}"
        self._db.execute(
            """INSERT INTO review_records (
                   review_id, trial_id, reviewer_id, decision, reviewed_commit,
                   amended_commit, reason_codes_json, comment, created_at
               ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            (
                review_id,
                trial_id,
                reviewer_id,
                decision,
                reviewed_commit,
                amended_commit,
                json.dumps(reason_codes or [], ensure_ascii=False),
                comment,
                utc_now(),
            ),
        )
        self._db.commit()
        return review_id

    def list_experiments(self) -> list[dict[str, Any]]:
        rows = self._db.execute("SELECT * FROM experiments ORDER BY created_at DESC").fetchall()
        return [dict(row) for row in rows]

    def get_experiment(self, experiment_id: str) -> dict[str, Any] | None:
        row = self._db.execute(
            "SELECT * FROM experiments WHERE experiment_id = ?", (experiment_id,)
        ).fetchone()
        return dict(row) if row else None

    def list_trials(self, experiment_id: str) -> list[dict[str, Any]]:
        rows = self._db.execute(
            "SELECT * FROM trials WHERE experiment_id = ? ORDER BY case_id, repetition",
            (experiment_id,),
        ).fetchall()
        return [self._decode_trial_row(row) for row in rows]

    def grader_history(self, trial_id: str) -> list[dict[str, Any]]:
        rows = self._db.execute(
            """SELECT gr.*, COALESCE(json_group_array(gres.result_json), '[]') AS results_json
               FROM grader_runs gr
               LEFT JOIN grader_results gres ON gres.grader_run_id = gr.grader_run_id
               WHERE gr.trial_id = ? GROUP BY gr.grader_run_id ORDER BY gr.started_at""",
            (trial_id,),
        ).fetchall()
        output = []
        for row in rows:
            item = dict(row)
            item["results"] = [
                json.loads(value)
                for value in json.loads(item.pop("results_json"))
                if isinstance(value, str) and value
            ]
            output.append(item)
        return output

    def list_reviews(self, trial_id: str) -> list[dict[str, Any]]:
        rows = self._db.execute(
            "SELECT * FROM review_records WHERE trial_id = ? ORDER BY created_at", (trial_id,)
        ).fetchall()
        output = []
        for row in rows:
            item = dict(row)
            item["reason_codes"] = json.loads(item.pop("reason_codes_json"))
            output.append(item)
        return output

    @staticmethod
    def _trial_from_row(row: sqlite3.Row) -> TrialRecord:
        return TrialRecord(
            trial_id=row["trial_id"],
            experiment_id=row["experiment_id"],
            case_id=row["case_id"],
            repetition=row["repetition"],
            seed=row["seed"],
            state=row["state"],
            root_run_id=row["root_run_id"],
            trace_id=row["trace_id"],
            fixture_digest=row["fixture_digest"],
            final_commit=row["final_commit"],
            failure_reason=row["failure_reason"],
            invalid_reason=row["invalid_reason"],
            responsibility=row["responsibility"],
        )

    @staticmethod
    def _decode_trial_row(row: sqlite3.Row) -> dict[str, Any]:
        item = dict(row)
        item["result"] = json.loads(item.pop("result_json"))
        return item
