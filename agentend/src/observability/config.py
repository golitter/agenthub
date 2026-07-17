"""Environment-driven observability settings.

These settings intentionally stay independent from the required YAML application
configuration: observability must never become an AgentEnd startup dependency.
"""

from __future__ import annotations

import logging
import os
from dataclasses import dataclass
from functools import lru_cache

logger = logging.getLogger(__name__)

TOKYO_CLOUD_URL = "https://jp.cloud.langfuse.com"


def _parse_bool(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    if raw is None or not raw.strip():
        return default
    normalized = raw.strip().lower()
    if normalized in {"1", "true", "yes", "on"}:
        return True
    if normalized in {"0", "false", "no", "off"}:
        return False
    logger.warning("Invalid boolean for %s; using %s", name, default)
    return default


def _parse_sample_rate() -> float:
    raw = os.getenv("LANGFUSE_SAMPLE_RATE", "1.0")
    try:
        value = float(raw)
    except ValueError:
        logger.warning("Invalid LANGFUSE_SAMPLE_RATE; using 1.0")
        return 1.0
    if not 0.0 <= value <= 1.0:
        logger.warning("LANGFUSE_SAMPLE_RATE must be between 0 and 1; using 1.0")
        return 1.0
    return value


def _parse_payload_limit() -> int:
    raw = os.getenv("LANGFUSE_MAX_PAYLOAD_CHARS", "4000")
    try:
        value = int(raw)
    except ValueError:
        logger.warning("Invalid LANGFUSE_MAX_PAYLOAD_CHARS; using 4000")
        return 4000
    if value < 256:
        logger.warning("LANGFUSE_MAX_PAYLOAD_CHARS is too small; using 4000")
        return 4000
    return value


@dataclass(frozen=True)
class ObservabilitySettings:
    tracing_enabled: bool
    public_key: str
    secret_key: str
    base_url: str
    environment: str
    release: str | None
    sample_rate: float
    capture_content: bool
    capture_tool_input: bool
    capture_tool_output: bool
    max_payload_chars: int
    mask_patterns: tuple[str, ...]

    @property
    def active(self) -> bool:
        return self.tracing_enabled and bool(self.public_key and self.secret_key)

    @classmethod
    def from_env(cls) -> ObservabilitySettings:
        patterns = tuple(
            item.strip() for item in os.getenv("LANGFUSE_MASK_PATTERNS", "").split(",") if item.strip()
        )
        return cls(
            tracing_enabled=_parse_bool("LANGFUSE_TRACING_ENABLED", True),
            public_key=os.getenv("LANGFUSE_PUBLIC_KEY", "").strip(),
            secret_key=os.getenv("LANGFUSE_SECRET_KEY", "").strip(),
            base_url=os.getenv("LANGFUSE_BASE_URL", TOKYO_CLOUD_URL).strip() or TOKYO_CLOUD_URL,
            environment=os.getenv("LANGFUSE_TRACING_ENVIRONMENT", "development").strip() or "development",
            release=os.getenv("LANGFUSE_RELEASE") or None,
            sample_rate=_parse_sample_rate(),
            capture_content=_parse_bool("LANGFUSE_CAPTURE_CONTENT", False),
            capture_tool_input=_parse_bool("LANGFUSE_CAPTURE_TOOL_INPUT", False),
            capture_tool_output=_parse_bool("LANGFUSE_CAPTURE_TOOL_OUTPUT", False),
            max_payload_chars=_parse_payload_limit(),
            mask_patterns=patterns,
        )


@lru_cache(maxsize=1)
def get_observability_settings() -> ObservabilitySettings:
    return ObservabilitySettings.from_env()


def reset_observability_settings() -> None:
    """Clear cached environment settings (used by tests and explicit reloads)."""

    get_observability_settings.cache_clear()
