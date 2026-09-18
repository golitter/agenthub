from __future__ import annotations

import sys
from pathlib import Path

import pytest
from pydantic import ValidationError

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from evals import cli as eval_cli
from evals.digests import canonical_digest, dataset_digest_payload, tree_digest
from evals.models import (
    CaseManifest,
    DatasetManifest,
    ExperimentSnapshot,
    FixtureSpec,
    GraderResult,
    TrialRecord,
    TrialState,
    agent_visible_task,
    trial_id_for,
)
from evals.readiness import BatchEvalBlocked, BatchEvalReadiness

DIGEST_A = "sha256:" + "a" * 64
DIGEST_B = "sha256:" + "b" * 64


def dataset() -> DatasetManifest:
    return DatasetManifest.model_validate(
        {
            "schema_version": 1,
            "dataset_id": "agenthub-coding-v1",
            "version": "1.0.0",
            "description": "Core coding evaluation",
            "case_ids": ["bugfix-001"],
        }
    )


def case() -> CaseManifest:
    return CaseManifest.model_validate(
        {
            "schema_version": 1,
            "case_id": "bugfix-001",
            "category": "bugfix",
            "difficulty": "medium",
            "fixture": {
                "source": "fixtures/bugfix-001.bundle",
                "sha256": DIGEST_A,
                "base_ref": "refs/heads/main",
            },
            "prompt": "Fix the parent admission fence.",
            "execution": {"timeout_seconds": 600, "max_turns": 20, "network": "none"},
            "scope": {"allowed_paths": ["agentend/src/**"], "forbidden_paths": ["agentend/evals/hidden/**"]},
            "graders": [
                {"type": "git_diff"},
                {
                    "type": "hidden_command",
                    "asset_id": "parent-fence-v1",
                    "argv": ["uv", "run", "pytest", "/eval-hidden/test_parent.py"],
                },
            ],
            "expected": {"require_change": True, "require_commit": True, "max_changed_files": 4},
        }
    )


def test_models_reject_unknown_fields_and_invalid_no_op() -> None:
    payload = dataset().model_dump()
    payload["agent_type"] = "codex"
    with pytest.raises(ValidationError, match="Extra inputs are not permitted"):
        DatasetManifest.model_validate(payload)

    payload = case().model_dump()
    payload["category"] = "no_op"
    with pytest.raises(ValidationError, match="require_change=false"):
        CaseManifest.model_validate(payload)


def test_agent_visible_task_excludes_control_plane_and_hidden_data() -> None:
    visible = agent_visible_task(case()).model_dump()
    assert set(visible) == {"case_id", "prompt", "timeout_seconds", "max_turns", "network"}
    serialized = str(visible)
    assert "graders" not in serialized
    assert "parent-fence-v1" not in serialized
    assert "allowed_paths" not in serialized


def test_canonical_dataset_digest_is_order_independent_and_complete() -> None:
    value = dataset_digest_payload(
        dataset(),
        {"bugfix-001": case()},
        fixture_digests={"bugfix-001": DIGEST_A},
        hidden_asset_digests={"parent-fence-v1": DIGEST_B},
        grader_set_digest=DIGEST_A,
        environment_digest=DIGEST_B,
    )
    reordered = dict(reversed(list(value.items())))
    assert canonical_digest(value) == canonical_digest(reordered)

    changed = {**value, "environment_digest": DIGEST_A}
    assert canonical_digest(value) != canonical_digest(changed)


def test_tree_digest_is_stable_and_rejects_symlinks(tmp_path: Path) -> None:
    assets = tmp_path / "assets"
    assets.mkdir()
    (assets / "b.txt").write_text("b", encoding="utf-8")
    (assets / "a.txt").write_text("a", encoding="utf-8")
    first = tree_digest(assets)
    assert first == tree_digest(assets)
    (assets / "link").symlink_to(assets / "a.txt")
    with pytest.raises(ValueError, match="symlink"):
        tree_digest(assets)


def test_experiment_is_frozen_and_requires_environment_digests() -> None:
    experiment = ExperimentSnapshot.model_validate(
        {
            "schema_version": 1,
            "experiment_id": "exp-20260916-a",
            "dataset_id": dataset().dataset_id,
            "dataset_version": dataset().version,
            "dataset_digest": DIGEST_A,
            "system_revision": "764203f",
            "prompt_revision": DIGEST_B,
            "agent_type": "codex",
            "model": "provider/model",
            "execution_image": "registry/agentend@sha256:" + "c" * 64,
            "dependency_cache_digest": DIGEST_B,
        }
    )
    with pytest.raises(ValidationError, match="frozen"):
        experiment.repetitions = 3  # type: ignore[misc]

    payload = experiment.model_dump()
    payload["execution_image"] = "registry/agentend:latest"
    with pytest.raises(ValidationError, match="pinned by sha256"):
        ExperimentSnapshot.model_validate(payload)


def test_trial_state_machine_rejects_skips_and_terminal_rewrites() -> None:
    trial = TrialRecord(
        trial_id="exp-a-bugfix-001-0",
        experiment_id="exp-a",
        case_id="bugfix-001",
        repetition=0,
        fixture_digest=DIGEST_A,
    )
    with pytest.raises(ValueError, match="invalid trial transition"):
        trial.transition(TrialState.RUNNING)
    failed = trial.transition(TrialState.FAILED)
    with pytest.raises(ValueError, match="terminal trial"):
        failed.transition(TrialState.PREPARING)


def test_trial_identity_is_deterministic_and_grader_result_is_strict() -> None:
    assert trial_id_for("exp-a", "case-a", 0) == trial_id_for("exp-a", "case-a", 0)
    assert trial_id_for("exp-a", "case-a", 0) != trial_id_for("exp-a", "case-a", 1)
    result = GraderResult(
        grader="hidden_command",
        version="1.0.0",
        status="passed",
        score=1.0,
        duration_ms=12,
        evidence_digest=DIGEST_A,
        summary="6 passed",
    )
    assert result.status.value == "passed"
    with pytest.raises(ValidationError, match="less than or equal to 1"):
        GraderResult.model_validate({**result.model_dump(), "score": 1.1})


def test_batch_gate_fails_closed_for_unsafe_or_incomplete_strict_sandbox() -> None:
    capabilities = {"filesystem_isolation": True, "controlled_egress": True}
    unsafe = BatchEvalReadiness("unsafe_process", "unsafe_process", capabilities)
    strict_but_incomplete = BatchEvalReadiness(
        "strict", "bubblewrap", {**capabilities, "credential_broker": False}
    )
    with pytest.raises(BatchEvalBlocked, match="enforced strict sandbox"):
        unsafe.require()
    with pytest.raises(BatchEvalBlocked, match="credential_broker"):
        strict_but_incomplete.require()


def test_batch_gate_accepts_only_enforced_strict_backend() -> None:
    readiness = BatchEvalReadiness(
        "strict",
        "bubblewrap",
        {"filesystem_isolation": True, "controlled_egress": True},
    )
    readiness.require()
    assert readiness.ready is True


def test_experiment_command_fails_closed_before_coordinator(monkeypatch: pytest.MonkeyPatch) -> None:
    readiness = BatchEvalReadiness(
        "unsafe_process",
        "unsafe_process",
        {"controlled_egress": False},
    )
    monkeypatch.setattr(eval_cli, "current_batch_eval_readiness", lambda **_: readiness)
    with pytest.raises(SystemExit) as exc_info:
        eval_cli.main(["experiment"])
    assert exc_info.value.code == 2


def test_fixture_base_ref_rejects_git_options() -> None:
    # `git checkout <base_ref>` must never receive an option as the revision.
    with pytest.raises(ValidationError, match="not a git option"):
        FixtureSpec(
            source="fixtures/a.bundle",
            sha256=DIGEST_A,
            base_ref="--upload-pack=touch /tmp/pwned",
        )
    assert FixtureSpec(source="fixtures/a.bundle", sha256=DIGEST_A, base_ref="main").base_ref == "main"
