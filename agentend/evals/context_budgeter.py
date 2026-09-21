"""Deterministic final packing of judge evidence."""

from __future__ import annotations

from dataclasses import dataclass, replace
from typing import Any

from .evidence import EvidenceChunk, EvidenceKind
from .jev_client import JevClient
from .token_budget import ContextBudget, TokenEstimate, estimate_messages


class JudgeEvidenceOverflow(RuntimeError):  # noqa: N818 - public plan/API name
    code = "protected_evidence_overflow"


@dataclass(frozen=True)
class BudgetDecision:
    chunks: tuple[EvidenceChunk, ...]
    estimate: TokenEstimate
    raw_estimate: int
    invoked_jev: bool
    target_tokens: int
    dropped: tuple[str, ...] = ()
    truncated: tuple[str, ...] = ()

    @property
    def stats(self) -> dict[str, Any]:
        return {
            "chunks_total": len(self.chunks) + len(self.dropped),
            "chunks_protected": sum(c.protected for c in self.chunks),
            "chunks_kept": len(self.chunks),
            "chunks_truncated": len(self.truncated),
            "chunks_dropped": len(self.dropped),
            "jev_invoked": self.invoked_jev,
        }


def build_budgeted_messages(
    system: str,
    chunks: list[EvidenceChunk],
    budget: ContextBudget,
    *,
    jev: JevClient | None = None,
    jev_prompt: str = "判断证据是否可能改变任一评分维度；结果只作为排序信号。",
    target_tokens: int | None = None,
) -> tuple[list[dict[str, str]], BudgetDecision]:
    original = [{"role": "system", "content": system}, {"role": "user", "content": _render(chunks)}]
    raw_estimate = estimate_messages(original, safety_factor=budget.estimator_safety_factor)
    target = target_tokens or budget.prune_target_tokens
    if target_tokens is None and raw_estimate.tokens <= budget.prune_trigger_tokens:
        return original, BudgetDecision(tuple(chunks), raw_estimate, raw_estimate.tokens, False, target)

    scored = list(chunks)
    invoked = False
    if budget.jev_enabled and jev is not None:
        candidates = [chunk for chunk in scored if not chunk.protected]
        scores = jev.rank(candidates, prompt=jev_prompt)
        if scores:
            invoked = True
            scored = [
                chunk if chunk.id not in scores else _with_score(chunk, scores[chunk.id])
                for chunk in scored
            ]
    protected = [chunk for chunk in scored if chunk.protected]
    if _estimate_chunks(system, protected, budget).tokens > target:
        raise JudgeEvidenceOverflow("protected evidence exceeds judge input budget")
    selected: list[EvidenceChunk] = list(protected)
    groups: dict[str, list[EvidenceChunk]] = {}
    for chunk in scored:
        if not chunk.protected:
            groups.setdefault(chunk.atomic_group, []).append(chunk)
    ordered = sorted(
        groups.values(),
        key=lambda group: max(_rank(chunk, budget.jev_keep_threshold) for chunk in group),
        reverse=True,
    )
    dropped: list[str] = []
    truncated: list[str] = []
    for group in ordered:
        candidate = selected + group
        if _estimate_chunks(system, candidate, budget).tokens <= target:
            selected.extend(group)
            continue
        # A low-relevance result can retain a stable head only when its paired call
        # is kept; this never creates an orphan result.
        result = next((chunk for chunk in group if chunk.kind == EvidenceKind.AGENT_RESULT), None)
        call = next((chunk for chunk in group if chunk.kind == EvidenceKind.AGENT_DISPATCH), None)
        if result and call:
            short = result.text[: budget.jev_result_head_chars] + "\n…[result truncated by judge budget]"
            replacement = [call, replace(result, text=short)]
            if _estimate_chunks(system, selected + replacement, budget).tokens <= target:
                selected.extend(replacement)
                truncated.append(result.id)
                continue
        dropped.extend(chunk.id for chunk in group)
    order = {chunk.id: index for index, chunk in enumerate(chunks)}
    selected.sort(key=lambda chunk: order[chunk.id])
    messages = [{"role": "system", "content": system}, {"role": "user", "content": _render(selected)}]
    estimate = estimate_messages(messages, safety_factor=budget.estimator_safety_factor)
    while estimate.tokens > target:
        removable = [chunk for chunk in selected if not chunk.protected]
        if not removable:
            raise JudgeEvidenceOverflow("protected evidence exceeds judge input budget")
        victim = min(removable, key=lambda chunk: _rank(chunk, budget.jev_keep_threshold))
        selected.remove(victim)
        dropped.append(victim.id)
        selected.sort(key=lambda chunk: order[chunk.id])
        messages[1]["content"] = _render(selected)
        estimate = estimate_messages(messages, safety_factor=budget.estimator_safety_factor)
    return messages, BudgetDecision(
        tuple(selected), estimate, raw_estimate.tokens, invoked, target, tuple(dropped), tuple(truncated)
    )


def _estimate_chunks(system: str, chunks: list[EvidenceChunk], budget: ContextBudget) -> TokenEstimate:
    return estimate_messages(
        [{"role": "system", "content": system}, {"role": "user", "content": _render(chunks)}],
        safety_factor=budget.estimator_safety_factor,
    )


def _render(chunks: list[EvidenceChunk] | tuple[EvidenceChunk, ...]) -> str:
    return "\n\n".join(chunk.text for chunk in chunks)


def _with_score(chunk: EvidenceChunk, score: float) -> EvidenceChunk:
    return replace(
        chunk,
        jev_keep_probability=score,
        jev_drop_probability=1 - score,
    )


def _rank(chunk: EvidenceChunk, threshold: float) -> tuple[int, bool, float, float, str]:
    score = chunk.jev_keep_probability if chunk.jev_keep_probability is not None else 0.5
    return chunk.priority, score >= threshold, score, 1 / max(1, len(chunk.text)), chunk.id
