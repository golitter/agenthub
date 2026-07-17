"""Allowlisting, masking, and bounded serialization for telemetry payloads."""

from __future__ import annotations

import re
from collections.abc import Mapping, Sequence
from typing import Any

from src.observability.config import ObservabilitySettings

REDACTED = "[REDACTED]"
_OMIT = object()

ALLOWED_METADATA_KEYS = frozenset(
    {
        "agent_type",
        "cli_session_id",
        "environment",
        "iteration",
        "duration_ms",
        "model",
        "ended_at",
        "release",
        "request_id",
        "session_id",
        "latency_ms",
        "status",
        "task_id",
        "trace_name",
        "usage",
        "started_at",
        "terminal_status",
    }
)
_SENSITIVE_KEY = re.compile(r"(?:api[_-]?key|authorization|cookie|credential|password|secret|token)", re.I)
_ABSOLUTE_PATH_KEY = re.compile(r"(?:cwd|path|repo|workspace|directory|dir)$", re.I)
_WINDOWS_ABSOLUTE_PATH = re.compile(r"^[A-Za-z]:[\\/]")
_SECRET_TEXT_PATTERNS = (
    re.compile(r"(?i)bearer\s+[A-Za-z0-9._~+/=-]+"),
    re.compile(r"(?i)\b(?:sk|pk|lsv2_[a-z]+|lf_sk|lf_pk)[-_][A-Za-z0-9_-]{8,}\b"),
)


def mask_text(value: str, settings: ObservabilitySettings) -> str:
    masked = value
    for pattern in _SECRET_TEXT_PATTERNS:
        masked = pattern.sub(REDACTED, masked)
    for raw_pattern in settings.mask_patterns:
        try:
            masked = re.sub(raw_pattern, REDACTED, masked)
        except re.error:
            continue
    if len(masked) > settings.max_payload_chars:
        return masked[: settings.max_payload_chars] + "…[truncated]"
    return masked


def _is_absolute_path(value: str) -> bool:
    return value.startswith(("/", "\\\\")) or bool(_WINDOWS_ABSOLUTE_PATH.match(value))


def _safe_value(value: Any, settings: ObservabilitySettings, *, key: str = "", depth: int = 0) -> Any:
    if depth > 6:
        return _OMIT
    if _SENSITIVE_KEY.search(key):
        return REDACTED
    if value is None or isinstance(value, (bool, int, float)):
        return value
    if isinstance(value, str):
        if _ABSOLUTE_PATH_KEY.search(key) and _is_absolute_path(value):
            return _OMIT
        return mask_text(value, settings)
    if isinstance(value, Mapping):
        result: dict[str, Any] = {}
        for child_key, child_value in value.items():
            if not isinstance(child_key, str):
                continue
            sanitized = _safe_value(child_value, settings, key=child_key, depth=depth + 1)
            if sanitized is not _OMIT:
                result[child_key] = sanitized
        return result
    if isinstance(value, Sequence) and not isinstance(value, (str, bytes, bytearray)):
        result = []
        for item in value:
            sanitized = _safe_value(item, settings, depth=depth + 1)
            if sanitized is not _OMIT:
                result.append(sanitized)
        return result
    return _OMIT


def sanitize_metadata(metadata: Mapping[str, Any], settings: ObservabilitySettings) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in metadata.items():
        if key not in ALLOWED_METADATA_KEYS:
            continue
        sanitized = _safe_value(value, settings, key=key)
        if sanitized is not _OMIT:
            result[key] = sanitized
    return result


def sanitize_content(value: Any, *, capture: bool, settings: ObservabilitySettings) -> Any | None:
    if not capture:
        return None
    sanitized = _safe_value(value, settings)
    return None if sanitized is _OMIT else sanitized


def mask_langfuse_data(data: Any, settings: ObservabilitySettings) -> Any:
    """SDK-wide last line of defense for callback-generated observations."""

    if isinstance(data, Mapping) and set(data).issubset(ALLOWED_METADATA_KEYS):
        return sanitize_metadata(data, settings)
    if not settings.capture_content:
        if isinstance(data, Mapping):
            return {
                key: (value if isinstance(value, (bool, int, float)) else REDACTED)
                for key, value in data.items()
                if isinstance(key, str) and not _SENSITIVE_KEY.search(key)
            }
        return data if isinstance(data, (bool, int, float)) else REDACTED
    sanitized = _safe_value(data, settings)
    return REDACTED if sanitized is _OMIT else sanitized


def normalize_usage(usage: Any) -> dict[str, int] | None:
    if not isinstance(usage, Mapping):
        return None
    aliases = {
        "input_tokens": "input",
        "prompt_tokens": "input",
        "output_tokens": "output",
        "completion_tokens": "output",
        "total_tokens": "total",
    }
    result: dict[str, int] = {}
    for key, value in usage.items():
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            continue
        normalized_key = aliases.get(str(key), str(key))
        result[normalized_key] = int(value)
    return result or None
