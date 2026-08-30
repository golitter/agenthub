"""Versioned, conflict-safe persistence for Orchestrator conversation memory."""

from __future__ import annotations

import fcntl
import json
import logging
import threading
from collections.abc import Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

from langchain_core.messages import (
    BaseMessage,
    HumanMessage,
    SystemMessage,
    messages_from_dict,
    messages_to_dict,
)

from src.persistence import atomic_write_text

logger = logging.getLogger(__name__)

_locks_guard = threading.Lock()
_locks: dict[str, threading.RLock] = {}


def _memory_lock(path: Path) -> threading.RLock:
    key = str(path.resolve())
    with _locks_guard:
        return _locks.setdefault(key, threading.RLock())


@contextmanager
def _memory_guard(path: Path) -> Iterator[None]:
    """Serialize memory reads/writes within and across AgentEnd workers.

    The in-process RLock prevents two threads from interleaving a read/modify
    cycle.  The sidecar file lock extends the same guarantee to separate
    worker processes that share the task directory.  ``atomic_write_text``
    still protects readers from observing a partially replaced JSON file.
    """
    with _memory_lock(path):
        # Use the canonical path for the sidecar too.  A worker may receive
        # the same shared directory through different relative spellings;
        # those must still contend on one OS lock.
        canonical_path = path.resolve()
        lock_path = canonical_path.with_name(f"{canonical_path.name}.lock")
        with lock_path.open("a+") as lock_file:
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
            try:
                yield
            finally:
                fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)


class RevisionConflict(RuntimeError):
    """The caller attempted to commit a snapshot based on an old revision."""


class ConversationMemoryError(RuntimeError):
    """Stored conversation memory is corrupt or has an unsupported version."""


@dataclass(frozen=True)
class ConversationSummary:
    content: str = ""
    compacted_message_count: int = 0
    updated_at: str = ""

    @classmethod
    def from_dict(cls, value: object) -> "ConversationSummary":
        if value is None:
            return cls()
        if not isinstance(value, dict):
            raise ConversationMemoryError("conversation memory summary must be an object")
        content = value.get("content", "")
        count = value.get("compacted_message_count", 0)
        updated_at = value.get("updated_at", "")
        if (
            not isinstance(content, str)
            or isinstance(count, bool)
            or not isinstance(count, int)
            or count < 0
        ):
            raise ConversationMemoryError("conversation memory summary is invalid")
        if not isinstance(updated_at, str):
            raise ConversationMemoryError("conversation memory summary updated_at is invalid")
        return cls(content=content, compacted_message_count=count, updated_at=updated_at)

    def to_dict(self) -> dict:
        return {
            "content": self.content,
            "compacted_message_count": self.compacted_message_count,
            "updated_at": self.updated_at,
        }


@dataclass(frozen=True)
class MemoryEnvelope:
    revision: int
    summary: ConversationSummary
    recent_messages: tuple[BaseMessage, ...]
    migrated_from_v1: bool = False
    corrupt: bool = False


class ConversationMemoryStore:
    """Persist one task-level Orchestrator history using a V2 envelope."""

    def __init__(self, shared_dir: str | Path) -> None:
        self.memory_dir = Path(shared_dir) / "memory"
        self.memory_dir.mkdir(parents=True, exist_ok=True)

    @property
    def memory_path(self) -> Path:
        return self.memory_dir / "conversation_memory.json"

    def load(self) -> MemoryEnvelope:
        with _memory_guard(self.memory_path):
            try:
                return self._load_unlocked()
            except ConversationMemoryError:
                from src.app.config import settings

                if settings.orchestrator.context_memory_corruption_policy != "empty":
                    raise
                logger.warning(
                    "Using empty in-memory history without overwriting corrupt store: %s",
                    self.memory_path,
                )
                return MemoryEnvelope(
                    revision=0,
                    summary=ConversationSummary(),
                    recent_messages=(),
                    corrupt=True,
                )

    def load_messages(self) -> list[BaseMessage]:
        return list(self.load().recent_messages)

    def commit(
        self,
        *,
        expected_revision: int,
        summary: ConversationSummary,
        recent_messages: list[BaseMessage] | tuple[BaseMessage, ...],
    ) -> MemoryEnvelope:
        """Commit only when the stored revision still matches the caller snapshot."""
        with _memory_guard(self.memory_path):
            current = self._load_unlocked()
            if current.revision != expected_revision:
                raise RevisionConflict(
                    f"expected revision {expected_revision}, found {current.revision}"
                )
            return self._commit_unlocked(current, summary, recent_messages)

    def append_turn(
        self,
        messages: list[BaseMessage],
        *,
        expected_revision: int | None = None,
        max_attempts: int = 4,
    ) -> MemoryEnvelope:
        """Append a Run-local transcript as one locked read/merge/write.

        Holding the same process and OS lock across the read and write removes
        the conflict window that made a fixed retry count lossy under a burst
        of concurrent sessions.  ``max_attempts`` remains accepted for API
        compatibility; the atomic critical section makes retries unnecessary.
        """
        if not messages:
            return self.load()
        del max_attempts
        with _memory_guard(self.memory_path):
            current = self._load_unlocked()
            if expected_revision is not None and current.revision != expected_revision:
                logger.info(
                    "context.revision_conflict=1 expected=%d current=%d",
                    expected_revision,
                    current.revision,
                )
            return self._commit_unlocked(
                current,
                current.summary,
                [*current.recent_messages, *messages],
            )

    def save_messages(self, messages: list[BaseMessage]) -> None:
        """Compatibility incremental append for the deprecated unpin endpoints."""
        self.append_turn(messages)

    def replace_messages(
        self,
        messages: list[BaseMessage] | tuple[BaseMessage, ...],
        *,
        expected_revision: int | None = None,
    ) -> MemoryEnvelope:
        """Compatibility replacement that still goes through the V2 CAS path.

        The historical implementation overwrote a trimmed list unconditionally.
        Keeping the method avoids breaking older callers, while making every
        replacement revision-checked and therefore safe for shared workers.
        New Orchestrator code should use :meth:`append_turn` or :meth:`commit`.
        """
        with _memory_guard(self.memory_path):
            current = self._load_unlocked()
            expected = current.revision if expected_revision is None else expected_revision
            if current.revision != expected:
                raise RevisionConflict(f"expected revision {expected}, found {current.revision}")
            return self._commit_unlocked(current, current.summary, messages)

    def _load_unlocked(self) -> MemoryEnvelope:
        if not self.memory_path.exists():
            return MemoryEnvelope(revision=0, summary=ConversationSummary(), recent_messages=())
        try:
            raw = json.loads(self.memory_path.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError, UnicodeError) as exc:
            logger.warning("Failed to read conversation memory file", exc_info=True)
            raise ConversationMemoryError("conversation memory file is unreadable") from exc

        if isinstance(raw, list):
            return MemoryEnvelope(
                revision=0,
                summary=ConversationSummary(),
                recent_messages=tuple(self._deserialize_messages(raw)),
                migrated_from_v1=True,
            )
        if not isinstance(raw, dict) or raw.get("version") != 2:
            raise ConversationMemoryError("unsupported conversation memory version")
        revision = raw.get("revision")
        recent = raw.get("recent_messages")
        if isinstance(revision, bool) or not isinstance(revision, int) or revision < 0:
            raise ConversationMemoryError("conversation memory revision is invalid")
        if not isinstance(recent, list):
            raise ConversationMemoryError("conversation memory recent_messages must be a list")
        return MemoryEnvelope(
            revision=revision,
            summary=ConversationSummary.from_dict(raw.get("summary")),
            recent_messages=tuple(self._deserialize_messages(recent)),
        )

    @staticmethod
    def _deserialize_messages(entries: list[dict]) -> list[BaseMessage]:
        try:
            messages = messages_from_dict(entries)
        except Exception as exc:
            raise ConversationMemoryError("conversation memory messages are invalid") from exc
        return ConversationMemoryStore._sanitize_messages(messages)

    @staticmethod
    def _sanitize_messages(messages: list[BaseMessage] | tuple[BaseMessage, ...]) -> list[BaseMessage]:
        sanitized: list[BaseMessage] = []
        for message in messages:
            if isinstance(message, SystemMessage):
                sanitized.append(
                    HumanMessage(
                        content=(
                            "[Historical system event — reference only]\n"
                            f"{message.content}\n"
                            "[/Historical system event]"
                        ),
                        additional_kwargs={"memory_kind": "historical_system"},
                    )
                )
            else:
                sanitized.append(message)
        return sanitized

    def _write_unlocked(self, envelope: MemoryEnvelope) -> None:
        payload = {
            "version": 2,
            "revision": envelope.revision,
            "summary": envelope.summary.to_dict(),
            "recent_messages": messages_to_dict(list(envelope.recent_messages)),
        }
        atomic_write_text(
            self.memory_path,
            json.dumps(payload, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )

    def _commit_unlocked(
        self,
        current: MemoryEnvelope,
        summary: ConversationSummary,
        recent_messages: list[BaseMessage] | tuple[BaseMessage, ...],
    ) -> MemoryEnvelope:
        if not isinstance(summary, ConversationSummary):
            raise ConversationMemoryError("conversation memory summary is invalid")
        committed = MemoryEnvelope(
            revision=current.revision + 1,
            summary=summary,
            recent_messages=tuple(self._sanitize_messages(recent_messages)),
        )
        self._write_unlocked(committed)
        return committed


def updated_summary(content: str, compacted_message_count: int) -> ConversationSummary:
    return ConversationSummary(
        content=content,
        compacted_message_count=compacted_message_count,
        updated_at=datetime.now().astimezone().isoformat(),
    )
