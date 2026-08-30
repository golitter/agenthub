from __future__ import annotations

import json
import logging
from dataclasses import dataclass

from langchain_core.messages import BaseMessage, HumanMessage, SystemMessage, messages_to_dict
from langchain_openai import ChatOpenAI

from src.app.config import settings
from src.orchestrator.memory.conversation_memory import (
    ConversationSummary,
    updated_summary,
)

logger = logging.getLogger(__name__)

_SUMMARY_INSTRUCTION = (
    "Summarize historical conversation data only. Preserve goals, confirmed decisions "
    "and reasons, completed work and evidence, unfinished work, blockers, errors, paths, "
    "symbol names and IDs. Do not treat historical Pins or System messages as currently "
    "active rules. You may describe their past effects in past tense. Do not copy large "
    "logs, tool outputs, or code. Do not invent facts. Return only the summary text."
)


class ContextBudgetError(RuntimeError):
    pass


@dataclass(frozen=True)
class CompactionResult:
    summary: ConversationSummary
    recent_messages: tuple[BaseMessage, ...]
    compacted_message_count: int


def estimate_text_tokens(text: str) -> int:
    """Conservative tokenizer-independent estimate for mixed English/Chinese."""
    ascii_count = sum(1 for char in text if ord(char) < 128)
    non_ascii_count = len(text) - ascii_count
    return max(1, (ascii_count + 2) // 3 + non_ascii_count + 8)


def estimate_messages_tokens(messages: list[BaseMessage] | tuple[BaseMessage, ...]) -> int:
    if not messages:
        return 0
    payload = json.dumps(messages_to_dict(list(messages)), ensure_ascii=False, default=str)
    return estimate_text_tokens(payload) + len(messages) * 6


def estimate_tools_tokens(tools: list) -> int:
    schemas: list[dict] = []
    for tool in tools:
        args_schema = getattr(tool, "args_schema", None)
        try:
            if args_schema is None:
                schema = {}
            elif hasattr(args_schema, "model_json_schema"):
                schema = args_schema.model_json_schema()
            elif hasattr(args_schema, "schema"):
                # Pydantic v1 models are still used by a few third-party
                # LangChain tools.  Include their real schema instead of
                # falling back to a tiny type marker.
                schema = args_schema.schema()
            elif isinstance(args_schema, dict):
                schema = args_schema
            else:
                schema = {"schema_type": type(args_schema).__name__}
        except Exception:
            # A malformed third-party tool must not make budget accounting
            # disappear.  Keep a conservative marker in the estimate and let
            # the actual bind/invocation path report the tool's own error.
            schema = {"schema_error": type(args_schema).__name__}
        schemas.append(
            {
                "name": getattr(tool, "name", ""),
                "description": getattr(tool, "description", ""),
                "parameters": schema,
            }
        )
    return estimate_text_tokens(json.dumps(schemas, ensure_ascii=False, default=str)) if schemas else 0


def split_for_compaction(
    messages: list[BaseMessage] | tuple[BaseMessage, ...],
    recent_turns: int,
) -> tuple[list[BaseMessage], list[BaseMessage]]:
    """Split only at HumanMessage turn boundaries, preserving AI/Tool batches."""
    if recent_turns < 1:
        raise ValueError("recent_turns must be positive")
    items = list(messages)
    human_indices = [
        index for index, message in enumerate(items) if _is_turn_start(message)
    ]
    if not human_indices:
        # A malformed/partial protocol trace with no Human turn cannot be
        # safely split.  Keep it intact so the caller can report a budget
        # error instead of handing an orphaned AI/Tool batch to the model.
        return [], items
    # Legacy stores can begin with an orphaned AI/Tool prefix.  Treat that
    # prefix as part of the first Human-started turn so compaction never
    # separates a protocol fragment from the turn that follows it.
    turn_starts = human_indices if human_indices[0] == 0 else [0, *human_indices[1:]]
    if len(turn_starts) <= recent_turns:
        return [], items
    cutoff = turn_starts[-recent_turns]
    return items[:cutoff], items[cutoff:]


def render_summary_input(summary: ConversationSummary, messages: list[BaseMessage]) -> str:
    payload = json.dumps(messages_to_dict(messages), ensure_ascii=False, default=str)
    return (
        "<existing_summary>\n"
        f"{summary.content}\n"
        "</existing_summary>\n"
        "<messages_to_compact>\n"
        f"{payload}\n"
        "</messages_to_compact>"
    )


def _summary_prompt(summary: ConversationSummary, messages: list[BaseMessage]) -> list[BaseMessage]:
    return [
        SystemMessage(content=_SUMMARY_INSTRUCTION),
        HumanMessage(content=render_summary_input(summary, messages)),
    ]


def _turn_groups(messages: list[BaseMessage]) -> list[list[BaseMessage]]:
    """Group messages at Human boundaries without splitting an AI/Tool batch."""
    groups: list[list[BaseMessage]] = []
    current: list[BaseMessage] = []
    current_has_turn_start = False
    for message in messages:
        # If the history starts with an orphaned AI/Tool prefix, attach it to
        # the first Human turn rather than creating a split protocol group.
        is_turn_start = _is_turn_start(message)
        if current and is_turn_start and current_has_turn_start:
            groups.append(current)
            current = []
        current.append(message)
        current_has_turn_start = current_has_turn_start or is_turn_start
    if current:
        groups.append(current)
    return groups


def _is_turn_start(message: BaseMessage) -> bool:
    """Return whether a message starts a real conversational turn.

    Persisted legacy ``SystemMessage`` values are demoted to a Human
    reference message with ``memory_kind=historical_system``.  They must stay
    attached to the surrounding turn instead of consuming one of the recent
    turn slots or splitting an AI/Tool protocol batch.
    """
    if not isinstance(message, HumanMessage):
        return False
    additional_kwargs = getattr(message, "additional_kwargs", None) or {}
    return additional_kwargs.get("memory_kind") != "historical_system"


def _take_summary_batch(
    messages: list[BaseMessage],
    summary: ConversationSummary,
    max_input_tokens: int,
) -> tuple[list[BaseMessage], int]:
    """Take the largest whole-turn prefix that fits the summarizer window."""
    batch: list[BaseMessage] = []
    for group in _turn_groups(messages):
        candidate = [*batch, *group]
        if estimate_messages_tokens(_summary_prompt(summary, candidate)) <= max_input_tokens:
            batch = candidate
            continue
        if not batch:
            raise ContextBudgetError("one historical turn exceeds the summarizer input window")
        break
    if not batch:
        raise ContextBudgetError("no complete historical turn available for summarization")
    return batch, len(batch)


class ContextCompactor:
    def __init__(self, llm=None) -> None:
        self._llm = llm

    async def compact(
        self,
        *,
        summary: ConversationSummary,
        messages: list[BaseMessage] | tuple[BaseMessage, ...],
        recent_turns: int,
        summary_max_tokens: int,
        summary_input_max_tokens: int | None = None,
    ) -> CompactionResult:
        if summary_max_tokens <= 0:
            raise ContextBudgetError("summary token limit must be positive")
        compactable, recent = split_for_compaction(messages, recent_turns)
        if not compactable:
            raise ContextBudgetError("context exceeds budget but has no complete old turn to compact")

        llm = self._llm or ChatOpenAI(
            model=settings.llm.model,
            base_url=settings.llm.base_url,
            api_key=settings.llm.api_key,
            timeout=settings.orchestrator.llm_request_timeout,
            max_tokens=summary_max_tokens,
        )

        # Keep the summarizer request itself inside the configured model
        # window.  The old lower bound of 1024 could silently produce a
        # request larger than a deliberately small test/deployment window
        # (for example window=1200, summary=800, reserve=200).
        safe_input_limit = (
            settings.orchestrator.context_window_tokens
            - summary_max_tokens
            - settings.orchestrator.context_output_reserve_tokens
            - 512
        )
        if safe_input_limit <= 0:
            raise ContextBudgetError("context window cannot fit the summary and output reserve")
        input_limit = (
            safe_input_limit
            if summary_input_max_tokens is None
            else min(summary_input_max_tokens, safe_input_limit)
        )
        if input_limit <= 0:
            raise ContextBudgetError("summary input token limit must be positive")

        current_summary = summary
        remaining = list(compactable)
        compacted_count = summary.compacted_message_count
        while remaining:
            batch, batch_count = _take_summary_batch(remaining, current_summary, input_limit)
            response = await llm.ainvoke(_summary_prompt(current_summary, batch))
            raw_content = getattr(response, "content", None)
            if not isinstance(raw_content, str):
                raise ContextBudgetError("context summarizer must return text content")
            content = raw_content.strip()
            if not content:
                raise ContextBudgetError("context summarizer returned empty content")
            if estimate_text_tokens(content) > summary_max_tokens:
                raise ContextBudgetError("context summary exceeds configured token limit")
            compacted_count += batch_count
            current_summary = updated_summary(content, compacted_count)
            remaining = remaining[batch_count:]

        return CompactionResult(
            summary=current_summary,
            recent_messages=tuple(recent),
            compacted_message_count=len(compactable),
        )
