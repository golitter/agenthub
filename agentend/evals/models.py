from __future__ import annotations

import hashlib
import json
import re
from enum import Enum
from pathlib import PurePosixPath
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

Identifier = Annotated[str, Field(pattern=r"^[a-z0-9][a-z0-9._-]{0,127}$")]
Digest = Annotated[str, Field(pattern=r"^sha256:[0-9a-f]{64}$")]


class FrozenModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class DatasetDefaults(FrozenModel):
    wall_time_seconds: int = Field(default=600, ge=1, le=86400)
    grader_time_seconds: int = Field(default=180, ge=1, le=86400)


class DatasetManifest(FrozenModel):
    schema_version: Literal[1]
    dataset_id: Identifier
    version: Annotated[str, Field(pattern=r"^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$")]
    description: str = Field(min_length=1, max_length=2000)
    case_ids: list[Identifier] = Field(min_length=1)
    defaults: DatasetDefaults = Field(default_factory=DatasetDefaults)

    @field_validator("case_ids")
    @classmethod
    def unique_case_ids(cls, value: list[str]) -> list[str]:
        if len(value) != len(set(value)):
            raise ValueError("case_ids must be unique")
        return value


class FixtureSpec(FrozenModel):
    source: str = Field(min_length=1)
    sha256: Digest
    base_ref: str = Field(min_length=1, max_length=512)

    @field_validator("source")
    @classmethod
    def relative_fixture_path(cls, value: str) -> str:
        _validate_relative_posix_path(value, "fixture source")
        return value


class ExecutionSpec(FrozenModel):
    timeout_seconds: int = Field(default=600, ge=1, le=86400)
    max_turns: int = Field(default=20, ge=1, le=1000)
    network: Literal["none", "controlled"] = "none"


class ScopeSpec(FrozenModel):
    allowed_paths: list[str] = Field(min_length=1)
    forbidden_paths: list[str] = Field(default_factory=list)

    @field_validator("allowed_paths", "forbidden_paths")
    @classmethod
    def repository_relative_globs(cls, values: list[str]) -> list[str]:
        for value in values:
            _validate_relative_posix_path(value, "scope pattern", allow_glob=True)
        return values


class GraderSpec(FrozenModel):
    type: Literal[
        "run_state",
        "integration",
        "git_diff",
        "command",
        "hidden_command",
        "regression",
        "anti_gaming",
        "no_op",
        "human_review",
        "llm_quality",
    ]
    version: str = "1.0.0"
    required: bool = True
    argv: list[str] | None = None
    asset_id: Identifier | None = None
    cwd: str | None = None
    env: dict[str, str] = Field(default_factory=dict)
    timeout_seconds: int | None = Field(default=None, ge=1, le=86400)
    expect: Literal["passed", "failed"] = "passed"

    @model_validator(mode="after")
    def validate_command_shape(self) -> GraderSpec:
        command_types = {"command", "hidden_command", "regression"}
        if self.type in command_types and not self.argv:
            raise ValueError(f"{self.type} grader requires argv")
        if self.type not in command_types and self.argv is not None:
            raise ValueError(f"{self.type} grader does not accept argv")
        if self.type == "hidden_command" and self.asset_id is None:
            raise ValueError("hidden_command grader requires asset_id")
        if self.type != "hidden_command" and self.asset_id is not None:
            raise ValueError("asset_id is only valid for hidden_command")
        if self.cwd is not None:
            _validate_relative_posix_path(self.cwd, "grader cwd")
        if any("\x00" in item for item in (self.argv or [])):
            raise ValueError("grader argv cannot contain NUL")
        return self


class ExpectedSpec(FrozenModel):
    require_change: bool = True
    require_commit: bool = True
    max_changed_files: int | None = Field(default=None, ge=0, le=10000)


class CaseManifest(FrozenModel):
    schema_version: Literal[1]
    case_id: Identifier
    category: Literal["bugfix", "feature", "refactor", "test_generation", "integration", "no_op"]
    difficulty: Literal["easy", "medium", "hard"]
    owner: str = Field(default="agentend", min_length=1, max_length=200)
    fixture: FixtureSpec
    prompt: str = Field(min_length=1, max_length=100000)
    execution: ExecutionSpec = Field(default_factory=ExecutionSpec)
    scope: ScopeSpec
    baseline: list[GraderSpec] = Field(default_factory=list)
    graders: list[GraderSpec] = Field(min_length=1)
    expected: ExpectedSpec = Field(default_factory=ExpectedSpec)

    @model_validator(mode="after")
    def no_op_requires_zero_diff(self) -> CaseManifest:
        if self.category == "no_op" and self.expected.require_change:
            raise ValueError("no_op cases must set expected.require_change=false")
        return self


class ExperimentSnapshot(FrozenModel):
    schema_version: Literal[1]
    experiment_id: Identifier
    dataset_id: Identifier
    dataset_version: str
    dataset_digest: Digest
    system_revision: str = Field(min_length=7, max_length=128)
    prompt_revision: str = Field(min_length=1, max_length=256)
    agent_type: Identifier
    model: str = Field(min_length=1, max_length=256)
    model_parameters: dict[str, bool | int | float | str | None] = Field(default_factory=dict)
    execution_image: str = Field(min_length=1, max_length=1000)
    dependency_cache_digest: Digest
    max_parallelism: int = Field(default=1, ge=1, le=128)
    repetitions: int = Field(default=1, ge=1, le=1000)
    seed_policy: Literal["per_case_repetition", "provider_unseeded"] = "per_case_repetition"

    @field_validator("execution_image")
    @classmethod
    def immutable_execution_image(cls, value: str) -> str:
        if re.fullmatch(r".+@sha256:[0-9a-f]{64}", value) is None:
            raise ValueError("execution_image must be pinned by sha256 digest")
        return value


class TrialState(str, Enum):
    CREATED = "created"
    PREPARING = "preparing"
    BASELINE_CHECKING = "baseline_checking"
    READY = "ready"
    RUNNING = "running"
    COLLECTING = "collecting"
    GRADING = "grading"
    AWAITING_REVIEW = "awaiting_review"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"
    INVALID = "invalid"


_TERMINAL_STATES = {TrialState.COMPLETED, TrialState.FAILED, TrialState.CANCELLED, TrialState.INVALID}
_NEXT_STATES = {
    TrialState.CREATED: {TrialState.PREPARING},
    TrialState.PREPARING: {TrialState.BASELINE_CHECKING},
    TrialState.BASELINE_CHECKING: {TrialState.READY},
    TrialState.READY: {TrialState.RUNNING},
    TrialState.RUNNING: {TrialState.COLLECTING},
    TrialState.COLLECTING: {TrialState.GRADING},
    TrialState.GRADING: {TrialState.AWAITING_REVIEW, TrialState.COMPLETED},
    TrialState.AWAITING_REVIEW: {TrialState.COMPLETED},
}


class TrialRecord(FrozenModel):
    trial_id: Identifier
    experiment_id: Identifier
    case_id: Identifier
    repetition: int = Field(ge=0)
    seed: int | None = None
    state: TrialState = TrialState.CREATED
    root_run_id: str | None = None
    trace_id: str | None = None
    fixture_digest: Digest
    final_commit: str | None = None
    failure_reason: str | None = None
    invalid_reason: str | None = None
    responsibility: Literal["agent", "fixture", "infrastructure", "operator"] | None = None

    def transition(self, target: TrialState) -> TrialRecord:
        if self.state in _TERMINAL_STATES:
            raise ValueError(f"terminal trial cannot transition from {self.state.value}")
        allowed = set(_NEXT_STATES.get(self.state, set())) | {
            TrialState.FAILED,
            TrialState.CANCELLED,
            TrialState.INVALID,
        }
        if target not in allowed:
            raise ValueError(f"invalid trial transition: {self.state.value} -> {target.value}")
        return self.model_copy(update={"state": target})


class GraderStatus(str, Enum):
    PASSED = "passed"
    FAILED = "failed"
    ERROR = "error"
    SKIPPED = "skipped"


class GraderResult(FrozenModel):
    grader: Identifier
    version: str = Field(min_length=1, max_length=128)
    status: GraderStatus
    score: float | None = Field(default=None, ge=0.0, le=1.0)
    duration_ms: int = Field(ge=0)
    argv: list[str] | None = None
    exit_code: int | None = None
    passed: int | None = Field(default=None, ge=0)
    failed: int | None = Field(default=None, ge=0)
    evidence_digest: Digest
    summary: str = Field(max_length=4000)


def trial_id_for(experiment_id: str, case_id: str, repetition: int) -> str:
    if repetition < 0:
        raise ValueError("repetition must be non-negative")
    payload = json.dumps(
        [experiment_id, case_id, repetition],
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode("utf-8")
    return f"trial-{hashlib.sha256(payload).hexdigest()[:32]}"


class AgentVisibleTask(FrozenModel):
    """The only Case fields permitted to cross into an Agent prompt."""

    case_id: Identifier
    prompt: str
    timeout_seconds: int
    max_turns: int
    network: Literal["none", "controlled"]


def agent_visible_task(case: CaseManifest) -> AgentVisibleTask:
    return AgentVisibleTask(
        case_id=case.case_id,
        prompt=case.prompt,
        timeout_seconds=case.execution.timeout_seconds,
        max_turns=case.execution.max_turns,
        network=case.execution.network,
    )


def _validate_relative_posix_path(value: str, label: str, *, allow_glob: bool = False) -> None:
    if not value or "\\" in value or "\x00" in value:
        raise ValueError(f"{label} must be a non-empty POSIX path")
    candidate = value
    if allow_glob:
        candidate = candidate.replace("**", "placeholder").replace("*", "x").replace("?", "x")
    path = PurePosixPath(candidate)
    if path.is_absolute() or any(part in {"", ".", ".."} for part in path.parts):
        raise ValueError(f"{label} must be repository-relative without traversal")
