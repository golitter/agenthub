from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Protocol

from ..models import CaseManifest, GraderResult


@dataclass(frozen=True)
class GradeContext:
    trial_id: str
    case: CaseManifest
    repository: Path
    base_revision: str
    final_revision: str
    hidden_assets: Path | None = None
    run_facts: dict[str, Any] = field(default_factory=dict)


class Grader(Protocol):
    name: str
    version: str

    def grade(self, context: GradeContext) -> GraderResult: ...
