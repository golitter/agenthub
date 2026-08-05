"""具备 fail-open 行为的 Langfuse 客户端生命周期管理。"""

from __future__ import annotations

import asyncio
import logging
import threading
from functools import lru_cache
from typing import Any

from langfuse import Langfuse

from src.observability.config import get_observability_settings
from src.observability.privacy import mask_langfuse_data

logger = logging.getLogger(__name__)

LANGFUSE_SHUTDOWN_TIMEOUT_SECONDS = 5.0


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
    failure: BaseException | None = None

    def run_shutdown() -> None:
        nonlocal failure
        try:
            client.shutdown()
        except BaseException as exc:
            failure = exc

    thread = threading.Thread(target=run_shutdown, daemon=True)
    thread.start()
    loop = asyncio.get_running_loop()
    deadline = loop.time() + LANGFUSE_SHUTDOWN_TIMEOUT_SECONDS
    while thread.is_alive() and loop.time() < deadline:
        await asyncio.sleep(0.05)
    if thread.is_alive():
        logger.warning("Langfuse shutdown timed out after %.0fs", LANGFUSE_SHUTDOWN_TIMEOUT_SECONDS)
        return
    if failure is not None:
        logger.error(
            "Failed to shut down Langfuse cleanly",
            exc_info=(type(failure), failure, failure.__traceback__),
        )
        return


def safe_observation_call(target: Any, method: str, **kwargs: Any) -> Any | None:
    if target is None:
        return None
    try:
        return getattr(target, method)(**kwargs)
    except Exception:
        logger.exception("Langfuse observation operation failed: %s", method)
        return None
