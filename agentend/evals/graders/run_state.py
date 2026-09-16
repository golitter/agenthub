from __future__ import annotations

from ..digests import canonical_digest
from ..models import GraderResult, GraderStatus
from .base import GradeContext


class RunStateGrader:
    name = "run_state"
    version = "1.0.0"

    def grade(self, context: GradeContext) -> GraderResult:
        state = context.run_facts.get("run_state")
        integration = context.run_facts.get("integration_status")
        passed = state == "completed" and integration in {"merged", "not_required"}
        facts = {"run_state": state, "integration_status": integration}
        return GraderResult(
            grader=self.name,
            version=self.version,
            status=GraderStatus.PASSED if passed else GraderStatus.FAILED,
            score=1.0 if passed else 0.0,
            duration_ms=0,
            evidence_digest=canonical_digest(facts),
            summary=f"run={state or 'unknown'}, integration={integration or 'unknown'}",
        )
