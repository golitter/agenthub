from __future__ import annotations

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from evals.models import ExperimentSnapshot, TrialRecord, TrialState
from evals.repository import SQLiteEvalRepository
from src.api.v1.evals import ReviewRequest, create_review, get_trial
from src.app.config import SandboxConfig, settings
from src.execution.sandbox import ExecutionSandboxUnavailable, prepare_agent_subprocess

DIGEST = "sha256:" + "a" * 64


@pytest.mark.asyncio
async def test_review_api_preserves_original_trial_score(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    database = tmp_path / "evals.sqlite3"
    monkeypatch.setattr(settings.evals, "database_path", str(database))
    repository = SQLiteEvalRepository(database)
    repository.create_experiment(
        ExperimentSnapshot(
            schema_version=1,
            experiment_id="exp-api",
            dataset_id="dataset-api",
            dataset_version="1.0.0",
            dataset_digest=DIGEST,
            system_revision="1234567",
            prompt_revision="v1",
            agent_type="fake",
            model="fake/model",
            execution_image="registry/eval@sha256:" + "b" * 64,
            dependency_cache_digest=DIGEST,
        )
    )
    trial = TrialRecord(
        trial_id="trial-api",
        experiment_id="exp-api",
        case_id="case-api",
        repetition=0,
        fixture_digest=DIGEST,
        state=TrialState.COMPLETED,
        final_commit="1" * 40,
    )
    repository.create_trial(trial)
    repository.update_trial(trial, result={"task_success": False})
    repository.close()

    response = await create_review(
        "trial-api",
        ReviewRequest(
            reviewer_id="reviewer",
            decision="accepted_with_changes",
            reviewed_commit="1" * 40,
            amended_commit="2" * 40,
        ),
    )
    assert response["task_success_unchanged"] is True
    detail = await get_trial("trial-api")
    assert detail["result"]["task_success"] is False
    assert detail["reviews"][0]["amended_commit"] == "2" * 40


def test_unsafe_process_passthrough_and_strict_missing_credentials_fail_closed(tmp_path: Path) -> None:
    argv, cwd, env = prepare_agent_subprocess(
        ["/usr/bin/true"],
        cwd=str(tmp_path),
        env={"PATH": "/usr/bin"},
        config=SandboxConfig(),
    )
    assert argv == ["/usr/bin/true"]
    assert cwd == str(tmp_path)
    assert env == {"PATH": "/usr/bin"}

    strict = SandboxConfig(mode="strict", backend="bubblewrap", credential_broker_dir="")
    with pytest.raises(ExecutionSandboxUnavailable, match="strict execution sandbox"):
        prepare_agent_subprocess(
            ["/usr/bin/true"],
            cwd=str(tmp_path),
            env={"PATH": "/usr/bin"},
            config=strict,
        )
