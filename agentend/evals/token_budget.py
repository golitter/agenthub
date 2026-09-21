"""Conservative, tokenizer-independent budgets for LLM judge requests."""

from __future__ import annotations

import math
import re
from dataclasses import dataclass, field
from typing import Any

ESTIMATOR_VERSION = "weighted_chars_v1"
REQUEST_OVERHEAD_TOKENS = 256
MESSAGE_OVERHEAD_TOKENS = 8
TOKEN_PIECES = re.compile(r"[A-Za-z]+|\d+|[^\sA-Za-z\d]")


def _weighted_chars(text: str) -> int:
    tokens = 0
    for match in TOKEN_PIECES.finditer(text):
        piece = match.group(0)
        if piece[0].isalpha() and piece[0].isascii():
            # Round each word independently. Aggregating all ASCII letters
            # would badly undercount repeated short words such as "a a a".
            tokens += math.ceil(len(piece) / 4)
        elif piece[0].isdigit() and piece[0].isascii():
            tokens += math.ceil(len(piece) / 2)
        else:
            tokens += 1
    return tokens


@dataclass(frozen=True)
class TokenEstimate:
    tokens: int
    raw_tokens: int
    chars: int
    estimator_version: str = ESTIMATOR_VERSION
    by_message: dict[int, int] = field(default_factory=dict)


def estimate_messages(messages: list[dict[str, Any]], *, safety_factor: float = 1.15) -> TokenEstimate:
    """Estimate the serialized request, including roles and JSON overhead."""

    raw_by_message = {
        index: _weighted_chars(str(message.get("role", "")) + str(message.get("content", "")))
        for index, message in enumerate(messages)
    }
    raw = REQUEST_OVERHEAD_TOKENS + MESSAGE_OVERHEAD_TOKENS * len(messages) + sum(raw_by_message.values())
    return TokenEstimate(
        tokens=math.ceil(raw * safety_factor),
        raw_tokens=raw,
        chars=sum(len(str(message.get("content", ""))) for message in messages),
        by_message={index: math.ceil(value * safety_factor) for index, value in raw_by_message.items()},
    )


@dataclass(frozen=True)
class ContextBudget:
    context_window_tokens: int = 65_536
    output_reserve_tokens: int = 2_048
    estimator: str = ESTIMATOR_VERSION
    estimator_safety_factor: float = 1.15
    safety_margin_ratio: float = 0.10
    min_safety_margin_tokens: int = 2_048
    prune_trigger_ratio: float = 0.70
    prune_target_ratio: float = 0.55
    jev_enabled: bool = True
    jev_keep_threshold: float = 0.30
    jev_result_head_chars: int = 300
    overflow_retry_ratio: float = 0.75
    max_overflow_retries: int = 1

    @classmethod
    def from_mapping(cls, values: dict[str, Any] | None) -> ContextBudget:
        """Build a validated budget from deployment/config YAML values."""

        return cls(**(values or {}))

    def __post_init__(self) -> None:
        if self.estimator != ESTIMATOR_VERSION:
            raise ValueError(f"unsupported estimator: {self.estimator}")
        if self.context_window_tokens <= 0:
            raise ValueError("context_window_tokens must be positive")
        if self.output_reserve_tokens < 0 or self.min_safety_margin_tokens < 0:
            raise ValueError("reserve and safety margin must be non-negative")
        if self.estimator_safety_factor < 1:
            raise ValueError("estimator_safety_factor must be at least 1")
        if not 0 <= self.safety_margin_ratio < 1:
            raise ValueError("safety_margin_ratio must be in [0, 1)")
        if self.output_reserve_tokens + self.min_safety_margin_tokens >= self.context_window_tokens:
            raise ValueError("context window must exceed output reserve and safety margin")
        if not 0 < self.prune_target_ratio < self.prune_trigger_ratio <= 1:
            raise ValueError("target must be below trigger and both must be in (0, 1]")
        if not 0 < self.overflow_retry_ratio < 1:
            raise ValueError("overflow_retry_ratio must be in (0, 1)")
        if self.max_overflow_retries not in {0, 1}:
            raise ValueError("max_overflow_retries must be 0 or 1")
        if not 0 <= self.jev_keep_threshold <= 1:
            raise ValueError("jev_keep_threshold must be in [0, 1]")
        if self.jev_result_head_chars < 0:
            raise ValueError("jev_result_head_chars must be non-negative")
        if self.input_budget_tokens <= 0:
            raise ValueError("context window leaves no positive input budget")

    @property
    def safety_margin_tokens(self) -> int:
        return max(math.ceil(self.context_window_tokens * self.safety_margin_ratio), self.min_safety_margin_tokens)

    @property
    def input_budget_tokens(self) -> int:
        return self.context_window_tokens - self.output_reserve_tokens - self.safety_margin_tokens

    @property
    def prune_trigger_tokens(self) -> int:
        return math.floor(self.input_budget_tokens * self.prune_trigger_ratio)

    @property
    def prune_target_tokens(self) -> int:
        return math.floor(self.input_budget_tokens * self.prune_target_ratio)

    def retry_target(self, target: int) -> int:
        return max(1, math.floor(target * self.overflow_retry_ratio))

    def as_dict(self) -> dict[str, Any]:
        return {
            "estimator_version": self.estimator,
            "estimator_safety_factor": self.estimator_safety_factor,
            "context_window_tokens": self.context_window_tokens,
            "output_reserve_tokens": self.output_reserve_tokens,
            "input_budget_tokens": self.input_budget_tokens,
            "prune_trigger_tokens": self.prune_trigger_tokens,
            "prune_target_tokens": self.prune_target_tokens,
            "jev_enabled": self.jev_enabled,
            "jev_threshold": self.jev_keep_threshold,
        }
