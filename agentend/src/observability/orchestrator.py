"""LangGraph callback and attribute helpers for Orchestrator traces."""

from __future__ import annotations

import logging
from contextlib import contextmanager
from typing import Any

from langfuse import propagate_attributes
from langfuse.langchain import CallbackHandler

from src.observability.client import get_langfuse_client
from src.observability.config import ObservabilitySettings, get_observability_settings
from src.observability.privacy import REDACTED, mask_text, sanitize_content, sanitize_metadata

logger = logging.getLogger(__name__)


class PrivacySafeCallbackHandler(CallbackHandler):
    def __init__(self, settings: ObservabilitySettings, client: Any) -> None:
        super().__init__(public_key=settings.public_key)
        self._langfuse_client = client
        self._settings = settings

    def on_tool_start(self, serialized, input_str, **kwargs):
        safe_input = sanitize_content(
            input_str,
            capture=self._settings.capture_tool_input,
            settings=self._settings,
        )
        return super().on_tool_start(serialized, safe_input or REDACTED, **kwargs)

    def on_tool_end(self, output, **kwargs):
        safe_output = sanitize_content(
            output,
            capture=self._settings.capture_tool_output,
            settings=self._settings,
        )
        return super().on_tool_end(safe_output if safe_output is not None else REDACTED, **kwargs)

    def on_tool_error(self, error: BaseException, **kwargs):
        safe_error = RuntimeError(mask_text(str(error), self._settings))
        return super().on_tool_error(safe_error, **kwargs)


def create_orchestrator_callback() -> CallbackHandler | None:
    settings = get_observability_settings()
    client = get_langfuse_client()
    if client is None:
        return None
    try:
        return PrivacySafeCallbackHandler(settings, client)
    except Exception:
        logger.exception("Failed to create Langfuse callback; Orchestrator tracing is disabled")
        return None


@contextmanager
def observation_attributes(*, trace_name: str, session_id: str, metadata: dict[str, Any], tags: list[str]):
    settings = get_observability_settings()
    if not settings.active:
        yield
        return
    safe_metadata = sanitize_metadata(metadata, settings)
    manager = propagate_attributes(
        trace_name=trace_name,
        session_id=session_id,
        metadata=safe_metadata,
        tags=tags,
    )
    entered = False
    try:
        manager.__enter__()
        entered = True
    except Exception:
        logger.exception("Failed to establish Langfuse attribute context")
    try:
        yield
    finally:
        if entered:
            try:
                manager.__exit__(None, None, None)
            except Exception:
                logger.exception("Failed to close Langfuse attribute context")
