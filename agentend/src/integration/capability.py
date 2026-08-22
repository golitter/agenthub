from __future__ import annotations

import hashlib
import secrets
from datetime import datetime, timedelta, timezone

from src.integration.errors import ERROR_CAPABILITY_INVALID, IntegrationError


class CapabilityError(IntegrationError):
    def __init__(self, message: str = ERROR_CAPABILITY_INVALID) -> None:
        super().__init__(ERROR_CAPABILITY_INVALID, message)


def issue_token() -> tuple[str, str]:
    """Return (raw token, SHA-256 digest); only the digest is persisted."""
    raw = secrets.token_urlsafe(32)
    return raw, digest_token(raw)


def digest_token(token: str) -> str:
    return hashlib.sha256(token.encode("utf-8")).hexdigest()


def expires_at(ttl_seconds: int) -> str:
    return (datetime.now(timezone.utc) + timedelta(seconds=max(1, ttl_seconds))).isoformat()


def is_expired(value: str) -> bool:
    try:
        return datetime.fromisoformat(value) <= datetime.now(timezone.utc)
    except (TypeError, ValueError):
        return True
