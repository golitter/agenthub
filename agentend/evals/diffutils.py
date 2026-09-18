from __future__ import annotations

import subprocess
from pathlib import Path


def bounded_diff(
    repository: Path,
    base_revision: str,
    final_revision: str,
    limit: int = 200_000,
    timeout_seconds: float = 300.0,
) -> str:
    completed = subprocess.run(
        ["git", "-C", str(repository), "diff", "--no-ext-diff", base_revision, final_revision, "--"],
        capture_output=True,
        check=True,
        timeout=timeout_seconds,
    )
    raw = completed.stdout
    if len(raw) <= limit:
        return raw.decode("utf-8", errors="replace")
    # Head+tail sampling: changes in later files must stay visible to graders
    # and judges even when the full diff exceeds the budget.
    half = limit // 2
    head = raw[:half].decode("utf-8", errors="replace")
    tail = raw[len(raw) - half :].decode("utf-8", errors="replace")
    return head + f"\n…[{len(raw) - limit} bytes omitted from the middle of the diff]…\n" + tail
