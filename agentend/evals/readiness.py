from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping

from src.security.startup_validation import sandbox_capabilities, strict_sandbox_enforced


class BatchEvalBlocked(RuntimeError):
    pass


@dataclass(frozen=True)
class BatchEvalReadiness:
    sandbox_mode: str
    sandbox_backend: str
    capabilities: Mapping[str, bool]

    @property
    def missing_capabilities(self) -> tuple[str, ...]:
        return tuple(sorted(name for name, available in self.capabilities.items() if not available))

    @property
    def ready(self) -> bool:
        return (
            self.sandbox_mode == "strict"
            and self.sandbox_backend != "unsafe_process"
            and strict_sandbox_enforced(dict(self.capabilities))
        )

    def require(self) -> None:
        if self.ready:
            return
        missing = ", ".join(self.missing_capabilities) or "strict sandbox selection"
        raise BatchEvalBlocked(
            "batch evaluation requires an enforced strict sandbox; "
            f"mode={self.sandbox_mode}, backend={self.sandbox_backend}, missing={missing}"
        )


def current_batch_eval_readiness(*, sandbox_mode: str, sandbox_backend: str) -> BatchEvalReadiness:
    return BatchEvalReadiness(
        sandbox_mode=sandbox_mode,
        sandbox_backend=sandbox_backend,
        capabilities=sandbox_capabilities(),
    )
