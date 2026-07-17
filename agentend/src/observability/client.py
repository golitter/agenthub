"""Langfuse client lifecycle with fail-open behavior."""

from __future__ import annotations

import asyncio
import logging
from functools import lru_cache
from typing import Any

from langfuse import Langfuse

from src.observability.config import get_observability_settings
from src.observability.privacy import mask_langfuse_data

logger = logging.getLogger(__name__)


@lru_cache(maxsize=1)
def get_langfuse_client() -> Langfuse | None:
    settings = get_observability_settings()
    if not settings.active:
        if settings.tracing_enabled and bool(settings.public_key) != bool(settings.secret_key):
            logger.warning("Langfuse tracing disabled because project credentials are incomplete")
        return None
    try:
        return Langfuse(
            public_key=settings.public_key,
            secret_key=settings.secret_key,
            base_url=settings.base_url,
            environment=settings.environment,
            release=settings.release,
            sample_rate=settings.sample_rate,
            tracing_enabled=True,
            mask=lambda *, data: mask_langfuse_data(data, settings),
        )
    except Exception:
        logger.exception("Failed to initialize Langfuse; observability is disabled")
        return None


def reset_langfuse_client() -> None:
    get_langfuse_client.cache_clear()


async def shutdown_langfuse() -> None:
    client = get_langfuse_client()
    if client is None:
        return
    try:
        await asyncio.to_thread(client.shutdown)
    except Exception:
        logger.exception("Failed to shut down Langfuse cleanly")


def safe_observation_call(target: Any, method: str, **kwargs: Any) -> Any | None:
    if target is None:
        return None
    try:
        return getattr(target, method)(**kwargs)
    except Exception:
        logger.exception("Langfuse observation operation failed: %s", method)
        return None
