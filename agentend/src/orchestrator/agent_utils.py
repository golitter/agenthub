"""Helpers for dealing with the server-provided orchestrator agent snapshot."""

from __future__ import annotations

from collections.abc import Mapping


def _field_text(agent: Mapping, field: str) -> str:
    value = agent.get(field, "")
    if value is None:
        return ""
    return str(value).strip()


def _agent_type_text(agent: Mapping) -> str:
    """Normalize config and transport snapshots for internal type checks."""
    return _field_text(agent, "type") or _field_text(agent, "agent_type")


def dispatchable_agent_id(agent: Mapping) -> str:
    """Return the canonical public id for a dispatchable agent, or ``""``."""
    agent_id = _field_text(agent, "id")
    agent_type = _agent_type_text(agent)
    if not agent_id or agent_type.casefold() == "orchestrator":
        return ""
    return agent_id


def dispatchable_agent_ids(agents: list[dict] | None) -> set[str]:
    """Return ids that may be used as ask/dispatch handles."""
    return {
        agent_id
        for agent in agents or []
        if isinstance(agent, Mapping)
        for agent_id in [dispatchable_agent_id(agent)]
        if agent_id
    }


def project_available_agents(agents: list[dict] | None) -> list[dict[str, str]]:
    """Project an internal agent snapshot onto the public discovery schema.

    The returned objects intentionally contain only ``id`` and ``name``.
    Agent type is used internally to filter the Orchestrator, but it never
    crosses the tool boundary. Backend session ids and workspace paths are
    also excluded.
    """
    projected: list[dict[str, str]] = []
    for agent in agents or []:
        if not isinstance(agent, Mapping):
            continue
        agent_id = dispatchable_agent_id(agent)
        if not agent_id:
            continue
        name = _field_text(agent, "name") or agent_id
        projected.append(
            {
                "id": agent_id,
                "name": name,
            }
        )
    return projected
