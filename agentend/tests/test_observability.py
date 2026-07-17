import asyncio
import sys
from collections.abc import AsyncIterator
from pathlib import Path
from typing import Any

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.observability import client as client_module
from src.observability.cli_trace import trace_stream_events
from src.observability.config import (
    TOKYO_CLOUD_URL,
    ObservabilitySettings,
    get_observability_settings,
    reset_observability_settings,
)
from src.observability.privacy import REDACTED, sanitize_content, sanitize_metadata
from src.schemas.events import EventType, StreamEvent


def settings(**overrides: Any) -> ObservabilitySettings:
    values = {
        "tracing_enabled": True,
        "public_key": "pk-test",
        "secret_key": "sk-test",
        "base_url": TOKYO_CLOUD_URL,
        "environment": "test",
        "release": None,
        "sample_rate": 1.0,
        "capture_content": False,
        "capture_tool_input": False,
        "capture_tool_output": False,
        "max_payload_chars": 256,
        "mask_patterns": (),
    }
    values.update(overrides)
    return ObservabilitySettings(**values)


@pytest.fixture(autouse=True)
def clear_settings_cache():
    reset_observability_settings()
    yield
    reset_observability_settings()


@pytest.mark.parametrize(
    ("environment", "active"),
    [
        ({"LANGFUSE_PUBLIC_KEY": "pk", "LANGFUSE_SECRET_KEY": "sk"}, True),
        ({"LANGFUSE_TRACING_ENABLED": "false", "LANGFUSE_PUBLIC_KEY": "pk", "LANGFUSE_SECRET_KEY": "sk"}, False),
        ({}, False),
        ({"LANGFUSE_PUBLIC_KEY": "pk"}, False),
        ({"LANGFUSE_SECRET_KEY": "sk"}, False),
    ],
)
def test_configuration_activation(monkeypatch, environment, active):
    for key in (
        "LANGFUSE_TRACING_ENABLED",
        "LANGFUSE_PUBLIC_KEY",
        "LANGFUSE_SECRET_KEY",
    ):
        monkeypatch.delenv(key, raising=False)
    for key, value in environment.items():
        monkeypatch.setenv(key, value)
    assert get_observability_settings().active is active


def test_configuration_parses_sampling_and_falls_back(monkeypatch):
    monkeypatch.setenv("LANGFUSE_SAMPLE_RATE", "0")
    monkeypatch.setenv("LANGFUSE_BASE_URL", "")
    assert get_observability_settings().sample_rate == 0
    assert get_observability_settings().base_url == TOKYO_CLOUD_URL

    reset_observability_settings()
    monkeypatch.setenv("LANGFUSE_SAMPLE_RATE", "bad")
    monkeypatch.setenv("LANGFUSE_MAX_PAYLOAD_CHARS", "12")
    parsed = get_observability_settings()
    assert parsed.sample_rate == 1.0
    assert parsed.max_payload_chars == 4000


def test_privacy_defaults_and_independent_opt_in():
    default = settings()
    assert sanitize_content("private prompt", capture=default.capture_content, settings=default) is None
    assert sanitize_content("tool arg", capture=default.capture_tool_input, settings=default) is None

    opted_in = settings(capture_content=True, capture_tool_input=True)
    assert sanitize_content("private prompt", capture=opted_in.capture_content, settings=opted_in) == "private prompt"
    assert sanitize_content("tool arg", capture=opted_in.capture_tool_input, settings=opted_in) == "tool arg"
    assert sanitize_content("tool result", capture=opted_in.capture_tool_output, settings=opted_in) is None


def test_metadata_allowlist_masks_secrets_paths_objects_and_truncates():
    configured = settings(mask_patterns=("customer-[0-9]+",), max_payload_chars=256)
    metadata = sanitize_metadata(
        {
            "task_id": "customer-123",
            "session_id": "Bearer abcdef123",
            "agent_type": "codex",
            "workspace_path": "/home/user/secret",
            "unknown": "drop",
            "usage": {"input_tokens": 10, "token": "secret", "object": object()},
        },
        configured,
    )
    assert metadata["task_id"] == REDACTED
    assert metadata["session_id"] == REDACTED
    assert "workspace_path" not in metadata
    assert "unknown" not in metadata
    assert metadata["usage"]["token"] == REDACTED
    assert "object" not in metadata["usage"]

    short = settings(capture_content=True, max_payload_chars=256)
    result = sanitize_content("x" * 300, capture=True, settings=short)
    assert result.endswith("[truncated]")
    assert len(result) < 320


class Observation:
    def __init__(self, name: str):
        self.name = name
        self.children: list[Observation] = []
        self.updates: list[dict[str, Any]] = []
        self.ended = False

    def start_observation(self, *, name: str, **kwargs: Any):
        child = Observation(name)
        child.updates.append(kwargs)
        self.children.append(child)
        return child

    def update(self, **kwargs: Any):
        self.updates.append(kwargs)

    def end(self, **kwargs: Any):
        self.updates.append(kwargs)
        self.ended = True


class Client:
    def __init__(self, fail_start: bool = False):
        self.fail_start = fail_start
        self.roots: list[Observation] = []

    def start_observation(self, *, name: str, **kwargs: Any):
        if self.fail_start:
            raise RuntimeError("exporter unavailable")
        root = Observation(name)
        root.updates.append(kwargs)
        self.roots.append(root)
        return root


async def stream(*events: StreamEvent, failure: BaseException | None = None) -> AsyncIterator[StreamEvent]:
    for event in events:
        yield event
    if failure is not None:
        raise failure


async def collect(events: AsyncIterator[StreamEvent]) -> list[StreamEvent]:
    return [event async for event in events]


def event(event_type: EventType, **content: Any) -> StreamEvent:
    return StreamEvent.create(event_type, **content)


@pytest.mark.asyncio
async def test_cli_success_aggregates_text_tracks_tools_usage_and_identity():
    source = [
        event(EventType.INIT, cli_session_id="cli-1", agent_type="codex"),
        event(EventType.TEXT, text="hello "),
        event(EventType.TOOL_CALL, tool="Read", args={"path": "/tmp/private"}),
        event(EventType.TOOL_CALL, tool="Read", args={"path": "/tmp/other"}),
        event(EventType.TOOL_RESULT, tool="Read", result="second"),
        event(EventType.TOOL_RESULT, tool="Read", result="first"),
        event(EventType.TEXT, text="world"),
        event(EventType.DONE, usage={"prompt_tokens": 3, "completion_tokens": 4}),
    ]
    client = Client()
    traced = trace_stream_events(
        stream(*source),
        run_name="codex session",
        inputs={"message": "secret", "session_id": "s1", "task_id": "t1", "agent_type": "codex"},
        client=client,
        settings=settings(),
    )
    output = await collect(traced)

    assert output == source
    assert all(actual is expected for actual, expected in zip(output, source, strict=True))
    root = client.roots[0]
    generation = next(child for child in root.children if child.name == "cli-response")
    tools = [child for child in root.children if child.name == "tool:Read"]
    assert all(tool.ended for tool in tools)
    assert generation.updates[-2]["usage_details"] == {"input": 3, "output": 4}
    assert generation.updates[-2]["output"] is None
    assert root.updates[-2]["output"] == {"status": "completed"}


@pytest.mark.asyncio
async def test_cli_missing_tool_result_and_error_event_are_terminal_errors():
    client = Client()
    source = [
        event(EventType.TOOL_CALL, tool="Write", args={"token": "secret"}),
        event(EventType.ERROR, error="Bearer abcdef123"),
    ]
    output = await collect(
        trace_stream_events(
            stream(*source),
            run_name="cli",
            inputs={"session_id": "s", "agent_type": "codex"},
            client=client,
            settings=settings(),
        )
    )
    assert output == source
    root = client.roots[0]
    tool = next(child for child in root.children if child.name == "tool:Write")
    assert tool.ended
    assert tool.updates[-2]["level"] == "ERROR"
    assert root.updates[-2]["level"] == "ERROR"
    assert root.updates[-2]["status_message"] == REDACTED


@pytest.mark.asyncio
async def test_cli_iterator_exception_and_cancellation_are_reraised():
    for failure, expected_level in ((ValueError("boom"), "ERROR"), (asyncio.CancelledError(), "WARNING")):
        client = Client()
        traced = trace_stream_events(
            stream(event(EventType.TEXT, text="x"), failure=failure),
            run_name="cli",
            inputs={"session_id": "s", "agent_type": "codex"},
            client=client,
            settings=settings(),
        )
        with pytest.raises(type(failure)):
            await collect(traced)
        assert client.roots[0].updates[-2]["level"] == expected_level


def test_no_project_owned_langsmith_runtime_or_active_configuration():
    project_root = Path(__file__).resolve().parents[1]
    forbidden = ("from langsmith", "import langsmith", "RunTree", "LANGSMITH_")
    violations: list[str] = []
    for path in (project_root / "src").rglob("*.py"):
        text = path.read_text(encoding="utf-8")
        for marker in forbidden:
            if marker in text:
                violations.append(f"{path.relative_to(project_root)}: {marker}")
    env_template = (project_root / ".env.example").read_text(encoding="utf-8")
    if "LANGSMITH_" in env_template:
        violations.append(".env.example: LANGSMITH_")
    assert violations == []


class ShutdownClient:
    def __init__(self, failure: Exception | None = None):
        self.failure = failure
        self.called = False

    def shutdown(self):
        self.called = True
        if self.failure:
            raise self.failure


@pytest.mark.asyncio
async def test_shutdown_success_and_failure_are_contained(monkeypatch):
    successful = ShutdownClient()
    monkeypatch.setattr(client_module, "get_langfuse_client", lambda: successful)
    await client_module.shutdown_langfuse()
    assert successful.called

    failing = ShutdownClient(RuntimeError("exporter shutdown failed"))
    monkeypatch.setattr(client_module, "get_langfuse_client", lambda: failing)
    await client_module.shutdown_langfuse()
    assert failing.called


def test_client_initialization_failure_is_contained(monkeypatch):
    monkeypatch.setenv("LANGFUSE_PUBLIC_KEY", "pk")
    monkeypatch.setenv("LANGFUSE_SECRET_KEY", "sk")
    reset_observability_settings()
    client_module.reset_langfuse_client()

    def fail(**kwargs):
        raise RuntimeError("unreachable endpoint")

    monkeypatch.setattr(client_module, "Langfuse", fail)
    assert client_module.get_langfuse_client() is None
    client_module.reset_langfuse_client()


@pytest.mark.asyncio
async def test_cli_disabled_and_exporter_failure_are_transparent():
    source = [event(EventType.TEXT, text="unchanged"), event(EventType.DONE)]
    disabled = await collect(
        trace_stream_events(
            stream(*source),
            run_name="cli",
            inputs={},
            client=Client(),
            settings=settings(tracing_enabled=False),
        )
    )
    unavailable = await collect(
        trace_stream_events(
            stream(*source),
            run_name="cli",
            inputs={},
            client=Client(fail_start=True),
            settings=settings(),
        )
    )
    assert disabled == source
    assert unavailable == source
