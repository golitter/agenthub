from __future__ import annotations

import time

from ..digests import canonical_digest
from ..models import GraderResult, GraderStatus
from .base import GradeContext
from .git_diff import changed_paths


class NoOpGrader:
    name = "no_op"
    version = "1.0.0"

    def grade(self, context: GradeContext) -> GraderResult:
        started = time.monotonic()
        changes = changed_paths(context)
        passed = not changes and not context.case.expected.require_change
        return GraderResult(
            grader=self.name,
            version=self.version,
            status=GraderStatus.PASSED if passed else GraderStatus.FAILED,
            score=1.0 if passed else 0.0,
            duration_ms=int((time.monotonic() - started) * 1000),
            evidence_digest=canonical_digest([change.__dict__ for change in changes]),
            summary="zero diff" if passed else f"unexpected changes: {len(changes)}",
        )
