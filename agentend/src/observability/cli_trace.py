"""Langfuse observations for opaque CLI Agent StreamEvent lifecycles."""

from __future__ import annotations

import asyncio
import logging
from collections.abc import AsyncIterator
from typing import Any

from src.observability.client import get_langfuse_client, safe_observation_call
from src.observability.config import ObservabilitySettings, get_observability_settings
from src.observability.orchestrator import observation_attributes
from src.observability.privacy import mask_text, normalize_usage, sanitize_content, sanitize_metadata
from src.schemas.events import EventType, StreamEvent

logger = logging.getLogger(__name__)


async def _passthrough(events: AsyncIterator[StreamEvent]) -> AsyncIterator[StreamEvent]:
    async for event in events:
        yield event


async def trace_stream_events(
    events: AsyncIterator[StreamEvent],
    *,
    run_name: str,
    inputs: dict[str, Any],
    client: Any | None = None,
    settings: ObservabilitySettings | None = None,
) -> AsyncIterator[StreamEvent]:
    settings = settings or get_observability_settings()
    client = client if client is not None else get_langfuse_client()
    if client is None or not settings.active:
        async for event in _passthrough(events):
            yield event
        return

    metadata = sanitize_metadata(inputs, settings)
    session_id = str(metadata.get("session_id", ""))
    message = sanitize_content(inputs.get("message"), capture=settings.capture_content, settings=settings)
    tags = [str(metadata.get("agent_type", "cli-agent"))]

    with observation_attributes(trace_name=run_name, session_id=session_id, metadata=metadata, tags=tags):
        root = safe_observation_call(
            client,
            "start_observation",
            name=run_name,
            as_type="agent",
            input=message,
            metadata=metadata,
        )
        if root is None:
            async for event in _passthrough(events):
                yield event
            return

        generation = safe_observation_call(
            root,
            "start_observation",
            name="cli-response",
            as_type="generation",
            input=message,
            metadata={"opaque_cli": True},
        )
        text_parts: list[str] = []
        pending_tools: list[tuple[str, Any]] = []
        usage: dict[str, int] | None = None
        status = "interrupted"
        status_message: str | None = None

        try:
            async for event in events:
                content = event.content
                if event.type == EventType.INIT.value:
                    init_metadata = sanitize_metadata(content, settings)
                    init_span = safe_observation_call(
                        root,
                        "start_observation",
                        name="init",
                        as_type="span",
                        output=init_metadata,
                    )
                    safe_observation_call(init_span, "end")
                elif event.type == EventType.TEXT.value:
                    text = content.get("text", "")
                    if isinstance(text, str):
                        text_parts.append(text)
                elif event.type == EventType.TOOL_CALL.value:
                    tool_name = str(content.get("tool") or "unknown")
                    tool_input = sanitize_content(
                        content.get("args", {}),
                        capture=settings.capture_tool_input,
                        settings=settings,
                    )
                    tool_span = safe_observation_call(
                        root,
                        "start_observation",
                        name=f"tool:{tool_name}",
                        as_type="tool",
                        input=tool_input,
                    )
                    if tool_span is not None:
                        pending_tools.append((tool_name, tool_span))
                elif event.type == EventType.TOOL_RESULT.value:
                    tool_name = str(content.get("tool") or "unknown")
                    matched_index = next(
                        (
                            index
                            for index in range(len(pending_tools) - 1, -1, -1)
                            if pending_tools[index][0] == tool_name
                        ),
                        None,
                    )
                    if matched_index is None:
                        logger.warning("TOOL_RESULT for unmatched tool: %s", tool_name)
                    else:
                        _, tool_span = pending_tools.pop(matched_index)
                        tool_output = sanitize_content(
                            content.get("result", ""),
                            capture=settings.capture_tool_output,
                            settings=settings,
                        )
                        safe_observation_call(tool_span, "update", output=tool_output)
                        safe_observation_call(tool_span, "end")
                elif event.type == EventType.DONE.value:
                    usage = normalize_usage(content.get("usage"))
                    status = "completed"
                elif event.type == EventType.ERROR.value:
                    status = "error"
                    status_message = mask_text(str(content.get("error", "unknown error")), settings)
                yield event
        except asyncio.CancelledError:
            status = "interrupted"
            status_message = "stream cancelled"
            raise
        except Exception as exc:
            status = "error"
            status_message = mask_text(str(exc), settings)
            raise
        finally:
            for tool_name, tool_span in pending_tools:
                safe_observation_call(
                    tool_span,
                    "update",
                    level="ERROR",
                    status_message=f"stream ended before result: {tool_name}",
                )
                safe_observation_call(tool_span, "end")

            output = sanitize_content(
                "".join(text_parts),
                capture=settings.capture_content,
                settings=settings,
            )
            generation_kwargs: dict[str, Any] = {"output": output}
            if usage:
                generation_kwargs["usage_details"] = usage
            if status == "error":
                generation_kwargs.update(level="ERROR", status_message=status_message)
            elif status == "interrupted":
                generation_kwargs.update(level="WARNING", status_message=status_message or "stream interrupted")
            safe_observation_call(generation, "update", **generation_kwargs)
            safe_observation_call(generation, "end")

            root_kwargs: dict[str, Any] = {"output": {"status": status}}
            if status == "error":
                root_kwargs.update(level="ERROR", status_message=status_message)
            elif status == "interrupted":
                root_kwargs.update(level="WARNING", status_message=status_message or "stream interrupted")
            safe_observation_call(root, "update", **root_kwargs)
            safe_observation_call(root, "end")
