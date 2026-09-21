"""Optional Jev relevance adapter. It is deliberately fail-open."""

from __future__ import annotations

import json
import math
import os
import urllib.error
import urllib.request
from typing import Protocol

from .evidence import EvidenceChunk


class JevClient(Protocol):
    def rank(self, chunks: list[EvidenceChunk], *, prompt: str) -> dict[str, float] | None: ...


class TypeSafeJevClient:
    DEFAULT_URL = "https://api.typesafe.ai/v1/systemone"
    DEFAULT_MODEL = "jev-latest"

    def __init__(
        self,
        *,
        api_key: str | None = None,
        base_url: str | None = None,
        model: str | None = None,
        timeout_seconds: int = 30,
    ) -> None:
        self.api_key = api_key if api_key is not None else os.environ.get("TYPESAFE_API_KEY", "")
        self.base_url = base_url or os.environ.get("TYPESAFE_BASE_URL", self.DEFAULT_URL)
        self.model = model or os.environ.get("TYPESAFE_MODEL", self.DEFAULT_MODEL)
        self.timeout_seconds = timeout_seconds

    def rank(self, chunks: list[EvidenceChunk], *, prompt: str) -> dict[str, float] | None:
        if not self.api_key or not chunks:
            return None
        question_to_chunk = {f"keep_c{index}": chunk.id for index, chunk in enumerate(chunks)}
        payload = {
            "model": self.model,
            "state": {
                "context": (
                    "Agent evaluation evidence is being compacted. Each history entry is a candidate "
                    "that may be removed; protected evidence and hidden judge material are not included."
                ),
                "goal": prompt,
                "history": [
                    {"id": chunk.id, "kind": chunk.kind.value, "text": chunk.text}
                    for chunk in chunks
                ],
            },
            "questions": {
                question: {
                    "type": "noul",
                    "instructions": (
                        f"Evidence {chunk_id} should remain available to the evaluation judge because "
                        "it can change at least one scoring dimension or contains independent evidence "
                        "not recoverable from the final answer, tests, or diff"
                    ),
                }
                for question, chunk_id in question_to_chunk.items()
            },
        }
        request = urllib.request.Request(
            self.base_url,
            data=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
            method="POST",
            headers={"Content-Type": "application/json", "Authorization": f"Bearer {self.api_key}"},
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
                value = json.loads(response.read().decode("utf-8"))
            answers = value.get("answers")
            if not isinstance(answers, dict):
                return None
            scores: dict[str, float] = {}
            for question, chunk_id in question_to_chunk.items():
                answer = answers.get(question)
                if not isinstance(answer, dict):
                    return None
                score = answer.get("noul")
                if (
                    not isinstance(score, (int, float))
                    or isinstance(score, bool)
                    or not math.isfinite(score)
                ):
                    return None
                scores[chunk_id] = max(0.0, min(1.0, float(score)))
            return scores
        except (urllib.error.URLError, urllib.error.HTTPError, TimeoutError, ValueError, TypeError, KeyError):
            return None
