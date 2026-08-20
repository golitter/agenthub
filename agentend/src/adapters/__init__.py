from src.adapters.base import BaseAgentAdapter
from src.adapters.registry import AdapterRegistry

__all__ = [
    "BaseAgentAdapter",
    "AdapterRegistry",
    "ClaudeCodeAdapter",
    "OpenCodeAdapter",
    "OrchestratorAdapter",
    "PiAdapter",
]


def __getattr__(name: str):
    """Load concrete adapters lazily to keep submodule imports cycle-free."""
    if name == "ClaudeCodeAdapter":
        from src.adapters.claude import ClaudeCodeAdapter

        return ClaudeCodeAdapter
    if name == "OpenCodeAdapter":
        from src.adapters.opencode import OpenCodeAdapter

        return OpenCodeAdapter
    if name == "OrchestratorAdapter":
        from src.adapters.orchestrator import OrchestratorAdapter

        return OrchestratorAdapter
    if name == "PiAdapter":
        from src.adapters.pi import PiAdapter

        return PiAdapter
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
