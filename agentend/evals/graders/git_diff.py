from __future__ import annotations

import fnmatch
import subprocess
import time
from dataclasses import dataclass

from ..digests import canonical_digest
from ..models import GraderResult, GraderStatus
from .base import GradeContext

# Interpreter/tool byproducts that running the agent-visible checks is expected
# to produce. They are dropped before any scope rule fires: not counted as
# changes, not against max_changed_files, and not as required-change evidence.
ARTIFACT_PATTERNS = (
    "__pycache__/**",
    "*.pyc",
    "*.pyo",
    ".pytest_cache/**",
    ".mypy_cache/**",
    ".ruff_cache/**",
    "*.egg-info/**",
    ".DS_Store",
    "Thumbs.db",
)

# Documentation-only escapes earn partial scope credit instead of a hard zero.
DOC_PATTERNS = ("README.md", "*.md", "docs/**", "CHANGELOG*")

SEVERE_PREFIXES = ("forbidden:", "symlink:", "submodule:")


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
    version = "1.1.0"

    def grade(self, context: GradeContext) -> GraderResult:
        started = time.monotonic()
        raw_changes = changed_paths(context)
        changes = [change for change in raw_changes if not is_artifact_change(change)]
        exempt = [change for change in raw_changes if is_artifact_change(change)]
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
            "exempt_artifacts": [change.__dict__ for change in exempt],
            "violations": violations,
        }
        return GraderResult(
            grader=self.name,
            version=self.version,
            status=GraderStatus.PASSED if passed else GraderStatus.FAILED,
            score=scope_score(violations),
            duration_ms=int((time.monotonic() - started) * 1000),
            evidence_digest=canonical_digest(evidence),
            summary="diff scope passed" if passed else "; ".join(violations)[:4000],
        )


def scope_score(violations: list[str]) -> float:
    """Layered scope credit: strict status stays binary, the score does not."""

    if not violations:
        return 1.0
    if any(violation.startswith(SEVERE_PREFIXES) for violation in violations):
        return 0.0
    if all(_is_doc_violation(violation) for violation in violations):
        return 0.7
    return 0.2


def _is_doc_violation(violation: str) -> bool:
    if not violation.startswith("outside-allowlist:"):
        return False
    path = violation[len("outside-allowlist:"):]
    return any(_matches(path, pattern) for pattern in DOC_PATTERNS)


def is_artifact_change(change: ChangedPath) -> bool:
    paths = change.paths
    return bool(paths) and all(any(_matches(path, pattern) for pattern in ARTIFACT_PATTERNS) for path in paths)


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
