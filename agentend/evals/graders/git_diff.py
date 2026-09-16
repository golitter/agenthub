from __future__ import annotations

import fnmatch
import subprocess
import time
from dataclasses import dataclass

from ..digests import canonical_digest
from ..models import GraderResult, GraderStatus
from .base import GradeContext


@dataclass(frozen=True)
class ChangedPath:
    status: str
    old_path: str | None
    new_path: str | None
    old_mode: str
    new_mode: str

    @property
    def paths(self) -> tuple[str, ...]:
        return tuple(path for path in (self.old_path, self.new_path) if path)


class DiffScopeGrader:
    name = "git_diff"
    version = "1.0.0"

    def grade(self, context: GradeContext) -> GraderResult:
        started = time.monotonic()
        changes = changed_paths(context)
        violations: list[str] = []
        for change in changes:
            for path in change.paths:
                if any(_matches(path, pattern) for pattern in context.case.scope.forbidden_paths):
                    violations.append(f"forbidden:{path}")
                if not any(_matches(path, pattern) for pattern in context.case.scope.allowed_paths):
                    violations.append(f"outside-allowlist:{path}")
            if change.new_mode == "120000":
                violations.append(f"symlink:{change.new_path}")
            if change.new_mode == "160000" or change.old_mode == "160000":
                violations.append(f"submodule:{change.new_path or change.old_path}")
        maximum = context.case.expected.max_changed_files
        if maximum is not None and len(changes) > maximum:
            violations.append(f"too-many-files:{len(changes)}>{maximum}")
        if context.case.expected.require_change and not changes:
            violations.append("required-change-missing")
        passed = not violations
        evidence = {
            "changes": [change.__dict__ for change in changes],
            "violations": violations,
        }
        return GraderResult(
            grader=self.name,
            version=self.version,
            status=GraderStatus.PASSED if passed else GraderStatus.FAILED,
            score=1.0 if passed else 0.0,
            duration_ms=int((time.monotonic() - started) * 1000),
            evidence_digest=canonical_digest(evidence),
            summary="diff scope passed" if passed else "; ".join(violations)[:4000],
        )


def changed_paths(context: GradeContext) -> list[ChangedPath]:
    completed = subprocess.run(
        [
            "git",
            "-C",
            str(context.repository),
            "diff",
            "--raw",
            "-z",
            "--no-abbrev",
            "--find-renames",
            context.base_revision,
            context.final_revision,
            "--",
        ],
        capture_output=True,
        check=True,
    )
    fields = completed.stdout.decode("utf-8", errors="surrogateescape").split("\0")
    changes: list[ChangedPath] = []
    index = 0
    while index < len(fields) and fields[index]:
        metadata = fields[index]
        index += 1
        parts = metadata.split()
        if len(parts) != 5 or not parts[0].startswith(":"):
            raise ValueError("unexpected git diff --raw record")
        old_mode = parts[0][1:]
        new_mode = parts[1]
        status = parts[4]
        old_path = fields[index]
        index += 1
        if status.startswith(("R", "C")):
            new_path = fields[index]
            index += 1
        elif status.startswith("D"):
            new_path = None
        else:
            new_path = old_path
            old_path = None if status.startswith("A") else old_path
        changes.append(ChangedPath(status, old_path, new_path, old_mode, new_mode))
    return changes


def _matches(path: str, pattern: str) -> bool:
    if fnmatch.fnmatchcase(path, pattern):
        return True
    if pattern.endswith("/**") and path == pattern[:-3]:
        return True
    return False
