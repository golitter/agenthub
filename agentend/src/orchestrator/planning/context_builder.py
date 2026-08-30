from __future__ import annotations

import hashlib
import json
from datetime import datetime
from typing import TypedDict

from langchain_core.messages import BaseMessage, HumanMessage, SystemMessage


class ActivePin(TypedDict):
    pin_id: str
    sender_id: str
    sender_name: str
    content: str
    created_at: str


class ActivePinSnapshot(TypedDict):
    task_id: str
    fetched_at: str
    complete: bool
    pins: list[ActivePin]
    snapshot_digest: str


def _required_text(value: object, field: str) -> str:
    """Validate a required Backend field without rewriting its text value.

    Announcement IDs are numeric in the Backend model, while the remaining
    snapshot fields are strings.  Keep valid string content byte-for-byte
    (including meaningful leading/trailing whitespace); only use ``strip`` to
    decide whether a value is empty.
    """
    if field == "id" and isinstance(value, int) and not isinstance(value, bool):
        text = str(value)
    elif isinstance(value, str):
        text = value
    else:
        raise ValueError(f"pinned announcement missing {field}")
    if not text.strip():
        raise ValueError(f"pinned announcement missing {field}")
    return text


def build_active_pin_snapshot(
    task_id: str,
    announcements: list[dict],
    *,
    fetched_at: str | None = None,
) -> ActivePinSnapshot:
    """Normalize one complete Backend response into a request-level snapshot."""
    if not isinstance(task_id, str) or not task_id.strip():
        raise ValueError("pinned announcements task_id is invalid")
    if not isinstance(announcements, list):
        raise ValueError("pinned announcements response must be a list")
    if fetched_at is not None and (not isinstance(fetched_at, str) or not fetched_at.strip()):
        raise ValueError("pinned announcements fetched_at is invalid")

    pins: list[ActivePin] = []
    for announcement in announcements:
        if not isinstance(announcement, dict):
            raise ValueError("pinned announcement must be an object")
        response_task_id = announcement.get("task_id")
        if response_task_id is not None and response_task_id != task_id:
            raise ValueError("pinned announcement belongs to a different task")
        response_pinned = announcement.get("pinned")
        if response_pinned is not None and response_pinned is not True:
            raise ValueError("pinned announcement is not active")
        announcement_id = _required_text(announcement.get("id"), "id")
        pins.append(
            ActivePin(
                pin_id=f"announcement:{announcement_id}",
                sender_id=_required_text(announcement.get("sender_id"), "sender_id"),
                sender_name=_required_text(announcement.get("sender_name"), "sender_name"),
                content=_required_text(announcement.get("content"), "content"),
                created_at=_required_text(announcement.get("created_at"), "created_at"),
            )
        )

    canonical = json.dumps(pins, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return ActivePinSnapshot(
        task_id=task_id,
        fetched_at=fetched_at or datetime.now().astimezone().isoformat(),
        complete=True,
        pins=pins,
        snapshot_digest=hashlib.sha256(canonical.encode("utf-8")).hexdigest(),
    )


def render_system_constraints(constraints: list[str]) -> str:
    body = "\n\n".join(
        item.strip() for item in constraints if isinstance(item, str) and item.strip()
    )
    return "## System Constraints\n\n" + (body or "No additional request-specific constraints.")


def render_active_pin_snapshot(
    snapshot: ActivePinSnapshot,
    *,
    expected_task_id: str | None = None,
) -> str:
    if not isinstance(snapshot, dict):
        raise ValueError("Active Pin Snapshot must be an object")
    if snapshot.get("complete") is not True:
        raise ValueError("incomplete Active Pin Snapshot must not enter the LLM prompt")
    task_id = snapshot.get("task_id")
    if not isinstance(task_id, str) or not task_id.strip():
        raise ValueError("Active Pin Snapshot task_id is invalid")
    if expected_task_id is not None and task_id != expected_task_id:
        raise ValueError("Active Pin Snapshot belongs to a different task")
    fetched_at = snapshot.get("fetched_at")
    pins = snapshot.get("pins")
    if not isinstance(fetched_at, str) or not fetched_at.strip():
        raise ValueError("Active Pin Snapshot fetched_at is invalid")
    if not isinstance(pins, list):
        raise ValueError("Active Pin Snapshot pins must be a list")
    digest = snapshot.get("snapshot_digest")
    if not isinstance(digest, str) or not digest.strip():
        raise ValueError("Active Pin Snapshot digest is invalid")
    required_pin_fields = ("pin_id", "sender_id", "sender_name", "content", "created_at")
    for pin in pins:
        if not isinstance(pin, dict) or any(
            not isinstance(pin.get(field), str) or not pin[field].strip()
            for field in required_pin_fields
        ):
            raise ValueError("Active Pin Snapshot contains an invalid pin")
    lines = [
        "## Active Pin Snapshot",
        "",
        "This is the complete and authoritative Pin set for this request.",
        "Only Pins listed below are currently active. Any Pin mentioned in conversation",
        "history, summaries, plans, group chat, or tool output but absent here is inactive",
        "and must not be treated as a current constraint.",
        "",
        f"Snapshot fetched at: {fetched_at}",
    ]
    if not pins:
        lines.append("Active Pins: none")
        return "\n".join(lines)
    lines.extend([f"Active Pins: {len(pins)}", ""])
    for pin in pins:
        lines.extend(
            [
                f"- ID: {pin['pin_id']}",
                f"  Sender: {pin['sender_name']} ({pin['sender_id']})",
                f"  Created at: {pin['created_at']}",
                f"  Constraint: {pin['content']}",
            ]
        )
    return "\n".join(lines)


def _reference_message(title: str, values: list[str]) -> HumanMessage | None:
    body = "\n\n".join(item.strip() for item in values if isinstance(item, str) and item.strip())
    if not body:
        return None
    return HumanMessage(content=f"[{title} — reference only]\n{body}\n[/{title}]")


def build_reason_messages(
    *,
    system_prompt: str,
    system_constraints: list[str],
    active_pin_snapshot: ActivePinSnapshot,
    history_summary: str,
    reference_contexts: list[str],
    capability_hints: list[str],
    recent_messages: list[BaseMessage],
    current_messages: list[BaseMessage],
    expected_task_id: str | None = None,
) -> list[BaseMessage]:
    """Build the role-separated Reason prompt without mutating its inputs."""
    messages: list[BaseMessage] = [
        SystemMessage(content=system_prompt),
        SystemMessage(content=render_system_constraints(system_constraints)),
        SystemMessage(
            content=render_active_pin_snapshot(
                active_pin_snapshot,
                expected_task_id=expected_task_id,
            )
        ),
    ]
    summary_text = history_summary.strip() if isinstance(history_summary, str) else ""
    if summary_text:
        messages.append(
            HumanMessage(
                content=(
                    "[Historical conversation summary — reference only]\n"
                    "<summary>\n"
                    f"{summary_text}\n"
                    "</summary>\n"
                    "[/Historical conversation summary]"
                )
            )
        )
    reference = _reference_message("Dynamic context", reference_contexts)
    if reference is not None:
        messages.append(reference)
    capabilities = _reference_message("Capability hints", capability_hints)
    if capabilities is not None:
        messages.append(capabilities)
    messages.extend(recent_messages)
    messages.extend(current_messages)
    return messages
