from __future__ import annotations

import re

from ..models import GraderResult, GraderSpec, GraderStatus
from ..sandbox import CommandExecutor
from .base import GradeContext

_PYTEST_SUMMARY = re.compile(r"(?P<count>\d+) (?P<kind>passed|failed|error|errors|skipped)")


class CommandGrader:
    version = "1.0.0"

    def __init__(self, spec: GraderSpec, executor: CommandExecutor) -> None:
        if not spec.argv:
            raise ValueError("command grader requires argv")
        self.spec = spec
        self.executor = executor
        self.name = spec.type

    def grade(self, context: GradeContext) -> GraderResult:
        result = self.executor.run(
            self.spec.argv or [],
            workspace=context.repository,
            hidden_assets=(
                context.hidden_assets / self.spec.asset_id
                if self.spec.type == "hidden_command" and context.hidden_assets and self.spec.asset_id
                else None
            ),
            cwd=self.spec.cwd,
            env=self.spec.env,
            timeout_seconds=self.spec.timeout_seconds or context.case.execution.timeout_seconds,
        )
        passed_assertions = 0
        failed_assertions = 0
        for match in _PYTEST_SUMMARY.finditer(result.stdout + "\n" + result.stderr):
            count = int(match.group("count"))
            if match.group("kind") == "passed":
                passed_assertions += count
            elif match.group("kind") in {"failed", "error", "errors"}:
                failed_assertions += count
        passed = result.exit_code == 0 and not result.timed_out
        summary = _summary(result)
        return GraderResult(
            grader=self.name,
            version=self.version,
            status=GraderStatus.PASSED if passed else GraderStatus.FAILED,
            score=1.0 if passed else 0.0,
            duration_ms=result.duration_ms,
            argv=list(result.argv),
            exit_code=result.exit_code,
            passed=passed_assertions,
            failed=failed_assertions,
            evidence_digest=result.evidence_digest,
            summary=summary,
        )


def _summary(result: object) -> str:
    stdout = getattr(result, "stdout", "").strip()
    stderr = getattr(result, "stderr", "").strip()
    if getattr(result, "timed_out", False):
        return "command timed out"
    text = stdout or stderr or f"exit code {getattr(result, 'exit_code', None)}"
    last_line = text.splitlines()[-1]
    return last_line[:4000]
