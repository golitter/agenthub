from __future__ import annotations

import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from evals.digests import canonical_digest
from evals.graders.git_diff import DiffScopeGrader, scope_score
from evals.models import CaseManifest, GraderResult, GraderStatus
from evals.scoring import ANTI_GAMING_CAP, CATEGORY_WEIGHTS, score_case

DIGEST = "sha256:" + "0" * 64


def result(grader: str, status: GraderStatus, score: float | None = None, **extra) -> GraderResult:
    return GraderResult(
        grader=grader,
        version="1.0.0",
        status=status,
        score=score,
        duration_ms=1,
        evidence_digest=DIGEST,
        summary="test",
        **extra,
    )


def make_case(category: str, weights: dict[str, float] | None = None) -> CaseManifest:
    return CaseManifest(
        schema_version=1,
        case_id=f"case-{category}",
        category=category,
        difficulty="easy",
        fixture={"source": "fixtures/a.bundle", "sha256": DIGEST, "base_ref": "main"},
        prompt="do the thing",
        scope={"allowed_paths": ["solution.py"], "forbidden_paths": []},
        graders=[{"type": "run_state"}],
        expected={"require_change": category not in {"chat", "knowledge_qa", "no_op"}},
        weights=weights,
    )


def test_category_weight_rows_sum_to_one() -> None:
    for category, weights in CATEGORY_WEIGHTS.items():
        assert set(weights) == {"execution", "functional", "scope", "quality"}
        assert abs(sum(weights.values()) - 1.0) < 1e-9, category
    assert set(CATEGORY_WEIGHTS) == {
        "bugfix", "feature", "refactor", "test_generation", "integration",
        "chat", "knowledge_qa", "no_op",
    }


def test_case_weights_override_and_validation() -> None:
    case = make_case("bugfix", weights={"execution": 0.2, "functional": 0.4, "scope": 0.2, "quality": 0.2})
    assert case.weights is not None and abs(sum(case.weights.values()) - 1.0) < 0.001
    with pytest.raises(Exception):
        make_case("bugfix", weights={"execution": 0.9})
    with pytest.raises(Exception):
        make_case("bugfix", weights={"execution": 0.5, "bogus": 0.5})


def test_coding_partial_functional_uses_public_counts() -> None:
    results = [
        result("run_state", GraderStatus.PASSED, 1.0),
        result("git_diff", GraderStatus.PASSED, 1.0),
        result("command", GraderStatus.FAILED, 0.0, passed=3, failed=1),
        result("hidden_command", GraderStatus.PASSED, 1.0),
        result("llm_quality", GraderStatus.PASSED, 0.8),
    ]
    payload = score_case("bugfix", results)
    # functional = 0.35 * (3/4) + 0.65 * 1.0 = 0.9125
    assert payload["score_dimensions"]["functional"] == pytest.approx(0.9125)
    expected = 100 * (0.15 * 1.0 + 0.45 * 0.9125 + 0.15 * 1.0 + 0.25 * 0.8)
    assert payload["score_percent"] == pytest.approx(expected, abs=0.01)
    assert payload["missing_dimensions"] == []
    assert payload["anti_gaming_capped"] is False


def test_public_partial_falls_back_to_exit_code_without_counts() -> None:
    results = [
        result("run_state", GraderStatus.PASSED, 1.0),
        result("git_diff", GraderStatus.PASSED, 1.0),
        result("command", GraderStatus.FAILED, 0.0),
        result("hidden_command", GraderStatus.FAILED, 0.0),
    ]
    payload = score_case("bugfix", results)
    assert payload["score_dimensions"]["functional"] == 0.0
    # quality missing -> normalize over execution/functional/scope
    expected = 100 * (0.15 * 1.0 + 0.45 * 0.0 + 0.15 * 1.0) / 0.75
    assert payload["score_percent"] == pytest.approx(expected, abs=0.01)
    assert payload["missing_dimensions"] == ["quality"]


def test_zero_diff_functional_combines_noop_and_anti_gaming() -> None:
    results = [
        result("run_state", GraderStatus.PASSED, 1.0),
        result("no_op", GraderStatus.PASSED, 1.0),
        result("git_diff", GraderStatus.PASSED, 1.0),
        result("anti_gaming", GraderStatus.PASSED, 1.0),
        result("llm_quality", GraderStatus.PASSED, 0.9),
    ]
    payload = score_case("chat", results)
    assert payload["score_dimensions"]["functional"] == pytest.approx(0.8 * 1.0 + 0.2 * 1.0)
    assert payload["score_percent"] == pytest.approx(100 * (0.2 + 0.2 + 0.6 * 0.9), abs=0.01)


def test_anti_gaming_failure_caps_score() -> None:
    results = [
        result("run_state", GraderStatus.PASSED, 1.0),
        result("no_op", GraderStatus.PASSED, 1.0),
        result("git_diff", GraderStatus.PASSED, 1.0),
        result("anti_gaming", GraderStatus.FAILED, 0.0),
        result("llm_quality", GraderStatus.PASSED, 1.0),
    ]
    payload = score_case("chat", results)
    assert payload["score_percent"] == ANTI_GAMING_CAP
    assert payload["anti_gaming_capped"] is True


def test_all_dimensions_missing_scores_zero() -> None:
    payload = score_case("no_op", [result("run_state", GraderStatus.ERROR)])
    assert payload["score_percent"] == 0.0
    # ERROR (unlike FAILED) contributes no dimension value at all.
    assert set(payload["missing_dimensions"]) == {"execution", "functional", "quality", "scope"}


def test_scope_score_layers() -> None:
    assert scope_score([]) == 1.0
    assert scope_score(["outside-allowlist:README.md"]) == 0.7
    assert scope_score(["outside-allowlist:docs/a.md", "outside-allowlist:CHANGELOG.md"]) == 0.7
    assert scope_score(["outside-allowlist:app.py"]) == 0.2
    assert scope_score(["outside-allowlist:README.md", "required-change-missing"]) == 0.2
    assert scope_score(["forbidden:secrets/key"]) == 0.0
    assert scope_score(["symlink:link"]) == 0.0
    assert scope_score(["submodule:vendor"]) == 0.0
    assert scope_score(["outside-allowlist:README.md", "forbidden:x"]) == 0.0


def _git(repository: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repository), *args], check=True, capture_output=True, text=True
    ).stdout.strip()


@pytest.fixture()
def scoped_repo(tmp_path: Path):
    from evals.graders.base import GradeContext

    repository = tmp_path / "repo"
    repository.mkdir()
    _git(repository, "init", "-q", "-b", "main")
    _git(repository, "config", "user.email", "e@x")
    _git(repository, "config", "user.name", "t")
    (repository / "solution.py").write_text("VALUE = 1\n", encoding="utf-8")
    (repository / "README.md").write_text("readme\n", encoding="utf-8")
    _git(repository, "add", ".")
    _git(repository, "commit", "-q", "-m", "base")
    base = _git(repository, "rev-parse", "HEAD")

    def context_for(case: CaseManifest, final: str) -> GradeContext:
        return GradeContext(
            trial_id="t", case=case, repository=repository,
            base_revision=base, final_revision=final,
        )

    return repository, context_for


def _diff_case(allowed: list[str]) -> CaseManifest:
    return CaseManifest(
        schema_version=1,
        case_id="scope-check",
        category="bugfix",
        difficulty="easy",
        fixture={"source": "fixtures/a.bundle", "sha256": DIGEST, "base_ref": "main"},
        prompt="fix",
        scope={"allowed_paths": allowed, "forbidden_paths": []},
        graders=[{"type": "run_state"}],
        expected={"require_change": False},
    )


def test_artifact_only_diff_is_exempt_and_passes(scoped_repo) -> None:
    repository, context_for = scoped_repo
    (repository / "__pycache__").mkdir()
    (repository / "__pycache__" / "solution.cpython-310.pyc").write_bytes(b"\x00pyc")
    (repository / ".pytest_cache").mkdir()
    (repository / ".pytest_cache" / "v").write_text("cache\n", encoding="utf-8")
    _git(repository, "add", "-A")
    _git(repository, "commit", "-q", "-m", "artifacts")
    final = _git(repository, "rev-parse", "HEAD")
    graded = DiffScopeGrader().grade(context_for(_diff_case(["solution.py"]), final))
    assert graded.status == GraderStatus.PASSED
    assert graded.score == 1.0


def test_readme_escape_fails_but_scores_partial(scoped_repo) -> None:
    repository, context_for = scoped_repo
    (repository / "README.md").write_text("updated notes\n", encoding="utf-8")
    _git(repository, "add", "-A")
    _git(repository, "commit", "-q", "-m", "docs")
    final = _git(repository, "rev-parse", "HEAD")
    graded = DiffScopeGrader().grade(context_for(_diff_case(["solution.py"]), final))
    assert graded.status == GraderStatus.FAILED
    assert graded.score == 0.7


def test_code_escape_scores_lower_than_docs(scoped_repo) -> None:
    repository, context_for = scoped_repo
    (repository / "app.py").write_text("VALUE = 2\n", encoding="utf-8")
    _git(repository, "add", "-A")
    _git(repository, "commit", "-q", "-m", "code")
    final = _git(repository, "rev-parse", "HEAD")
    graded = DiffScopeGrader().grade(context_for(_diff_case(["solution.py"]), final))
    assert graded.status == GraderStatus.FAILED
    assert graded.score == 0.2


def test_aggregate_result_hidden_test_pass_null_without_hidden() -> None:
    from evals.coordinator import EvaluationCoordinator

    case = make_case("chat")
    payload = EvaluationCoordinator._aggregate_result(
        case,
        [
            result("run_state", GraderStatus.PASSED, 1.0),
            result("no_op", GraderStatus.PASSED, 1.0),
        ],
        {},
    )
    assert payload["hidden_test_pass"] is None
    assert payload["score_percent"] is not None
    assert "hidden_command" not in payload["grader_statuses"]


def test_quality_coverage_and_overall_score_in_aggregation() -> None:
    from evals.metrics import aggregate_trials

    rows = [
        {"state": "completed", "result": {
            "case_category": "chat", "task_success": True, "usage_available": False,
            "score_percent": 80.0, "missing_dimensions": [],
        }},
        {"state": "completed", "result": {
            "case_category": "chat", "task_success": False, "usage_available": False,
            "score_percent": 60.0, "missing_dimensions": ["quality"],
        }},
        {"state": "invalid", "result": {}},
    ]
    metrics = aggregate_trials(rows)
    assert metrics["overall_score_percent"] == pytest.approx(70.0)
    assert metrics["scored_trials"] == 2
    assert metrics["quality_coverage"]["numerator"] == 1
    assert metrics["quality_coverage"]["denominator"] == 2
    assert metrics["category_scores"]["chat"]["mean_score"] == pytest.approx(70.0)
    assert metrics["hidden_test_pass"]["denominator"] == 0
    assert metrics["hidden_test_pass"]["value"] is None


def test_evidence_digest_stable_for_none_weights() -> None:
    # Digest serialization drops None model fields, so weights=None leaves the
    # v1 case digests untouched.
    dumped = make_case("bugfix").model_dump(mode="json", exclude_none=True)
    assert "weights" not in dumped
    assert canonical_digest(make_case("bugfix")) == canonical_digest(make_case("bugfix"))
