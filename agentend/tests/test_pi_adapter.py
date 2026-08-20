import asyncio
import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.adapters.pi import PiAdapter
from src.schemas.events import EventType


class _FakeStdout:
    def __init__(self, lines: list[bytes]) -> None:
        self._lines = list(lines)

    def __aiter__(self):
        return self

    async def __anext__(self) -> bytes:
        if not self._lines:
            raise StopAsyncIteration
        return self._lines.pop(0)


class _FakeStderr:
    def __init__(self, payload: bytes = b"") -> None:
        self._payload = payload

    async def read(self, _size: int = -1) -> bytes:
        payload, self._payload = self._payload, b""
        return payload


class _FakeProcess:
    def __init__(self, lines: list[bytes], returncode: int = 0, stderr: bytes = b"") -> None:
        self.stdout = _FakeStdout(lines)
        self.stderr = _FakeStderr(stderr)
        self.returncode = returncode

    async def wait(self) -> int:
        return self.returncode


def _line(payload: dict) -> bytes:
    return json.dumps(payload).encode() + b"\n"


def test_build_command_maps_tools_and_keeps_cwd_out_of_argv() -> None:
    adapter = PiAdapter()

    command = adapter._build_command(
        "inspect the repo",
        cwd="/worktree/session-1",
        system_prompt_append="follow the rules",
        allowed_tools=["Read", "Bash", "CustomTool"],
        model="anthropic/claude-sonnet-4",
    )

    assert command[:7] == [
        command[0],
        "--mode",
        "json",
        "--approve",
        "--no-extensions",
        "--no-prompt-templates",
        "--append-system-prompt",
    ]
    assert "--session" not in command
    assert "/worktree/session-1" not in command
    assert command[command.index("--append-system-prompt") + 1] == "follow the rules"
    assert command[command.index("--tools") + 1] == "read,bash,CustomTool"
    assert command[command.index("--model") + 1] == "anthropic/claude-sonnet-4"
    assert command[-1] == "inspect the repo"


def test_build_command_adds_session_only_for_resume() -> None:
    adapter = PiAdapter()

    fresh = adapter._build_command("hello", cli_session_id="pi-session", is_resume=False)
    resumed = adapter._build_command("hello", cli_session_id="pi-session", is_resume=True)

    assert "--session" not in fresh
    assert resumed[resumed.index("--session") + 1] == "pi-session"


def test_parse_stream_line_maps_pi_events_without_duplicate_text() -> None:
    adapter = PiAdapter()

    init = adapter._parse_stream_line('{"type":"session","id":"pi-123"}')
    text = adapter._parse_stream_line(
        json.dumps(
            {
                "type": "message_update",
                "assistantMessageEvent": {"type": "text_delta", "delta": "Hello"},
            }
        )
    )
    thinking_delta = adapter._parse_stream_line(
        json.dumps(
            {
                "type": "message_update",
                "assistantMessageEvent": {"type": "thinking_delta", "delta": "internal"},
            }
        )
    )
    thinking_end = adapter._parse_stream_line(
        json.dumps(
            {
                "type": "message_update",
                "assistantMessageEvent": {"type": "thinking_end", "content": "checked files"},
            }
        )
    )
    tool_call = adapter._parse_stream_line(
        json.dumps({"type": "tool_execution_start", "toolName": "read", "args": {"path": "a.py"}})
    )
    tool_result = adapter._parse_stream_line(
        json.dumps(
            {
                "type": "tool_execution_end",
                "toolName": "read",
                "result": {"content": [{"type": "text", "text": "source"}]},
                "isError": False,
            }
        )
    )
    done = adapter._parse_stream_line(
        json.dumps(
            {
                "type": "agent_end",
                "messages": [
                    {"role": "user", "usage": {"input": 100}},
                    {"role": "assistant", "usage": {"input": 10, "output": 4}},
                    {"role": "assistant", "usage": {"input": 3, "output": 2}},
                ],
            }
        )
    )

    assert init and init.type == EventType.INIT.value and init.content["cli_session_id"] == "pi-123"
    assert text and text.type == EventType.TEXT.value and text.content["text"] == "Hello"
    assert thinking_delta is None
    assert thinking_end is None
    assert tool_call and tool_call.type == EventType.TOOL_CALL.value
    assert tool_call.content["args"] == {"path": "a.py"}
    assert tool_result and tool_result.type == EventType.TOOL_RESULT.value
    assert tool_result.content["result"]["content"][0]["text"] == "source"
    assert tool_result.content["is_error"] is False
    assert done and done.type == EventType.DONE.value
    assert done.content["usage"] == {"input": 13, "output": 6}
    assert adapter._parse_stream_line("diagnostic output") is None


def test_parse_stream_line_maps_protocol_error() -> None:
    adapter = PiAdapter()

    event = adapter._parse_stream_line(
        json.dumps(
            {
                "type": "message_update",
                "assistantMessageEvent": {"type": "error", "errorMessage": "model unavailable"},
            }
        )
    )

    assert event and event.type == EventType.ERROR.value
    assert event.content["error"] == "model unavailable"


@pytest.mark.asyncio
async def test_stream_chat_uses_cwd_and_adds_done_when_agent_end_is_missing(monkeypatch) -> None:
    adapter = PiAdapter()
    init_line = _line({"type": "session", "id": "pi-123"})
    text_line = _line(
        {
            "type": "message_update",
            "assistantMessageEvent": {"type": "text_delta", "delta": "hello"},
        }
    )
    process = _FakeProcess([init_line, text_line])
    captured: dict = {}

    async def fake_create_subprocess_exec(*args, **kwargs):
        captured["args"] = args
        captured["kwargs"] = kwargs
        return process

    monkeypatch.setattr("src.adapters.pi._create_subprocess_exec", fake_create_subprocess_exec)

    events = [
        event
        async for event in adapter.stream_chat(
            "internal-session",
            "hello",
            cwd="/worktree/session-1",
        )
    ]

    assert [event.type for event in events] == [EventType.INIT.value, EventType.TEXT.value, EventType.DONE.value]
    assert captured["kwargs"]["cwd"] == "/worktree/session-1"
    assert "/worktree/session-1" not in captured["args"]
    assert "--session" not in captured["args"]
    assert not adapter._processes


@pytest.mark.asyncio
async def test_stream_chat_deduplicates_protocol_error_from_nonzero_exit(monkeypatch) -> None:
    adapter = PiAdapter()
    error_line = _line(
        {
            "type": "message_update",
            "assistantMessageEvent": {"type": "error", "errorMessage": "Pi failed"},
        }
    )
    process = _FakeProcess([error_line], returncode=1, stderr=b"Pi failed\n")

    async def fake_create_subprocess_exec(*args, **kwargs):
        return process

    monkeypatch.setattr("src.adapters.pi._create_subprocess_exec", fake_create_subprocess_exec)

    events = [event async for event in adapter.stream_chat("session-1", "hello")]

    errors = [event for event in events if event.type == EventType.ERROR.value]
    assert len(errors) == 1
    assert errors[0].content["error"] == "Pi failed"


@pytest.mark.asyncio
async def test_stream_chat_drains_stderr_while_stdout_waits(monkeypatch) -> None:
    adapter = PiAdapter()
    stderr_drained = asyncio.Event()
    done_line = _line({"type": "agent_end", "messages": []})

    class BlockingStdout:
        def __init__(self) -> None:
            self._emitted = False

        def __aiter__(self):
            return self

        async def __anext__(self):
            if self._emitted:
                raise StopAsyncIteration
            await stderr_drained.wait()
            self._emitted = True
            return done_line

    class SignallingStderr:
        def __init__(self) -> None:
            self._emitted = False

        async def read(self, _size: int = -1) -> bytes:
            if self._emitted:
                return b""
            self._emitted = True
            stderr_drained.set()
            return b"x" * (2 * 1024 * 1024)

    process = _FakeProcess([])
    process.stdout = BlockingStdout()
    process.stderr = SignallingStderr()

    async def fake_create_subprocess_exec(*args, **kwargs):
        return process

    monkeypatch.setattr("src.adapters.pi._create_subprocess_exec", fake_create_subprocess_exec)

    events = await asyncio.wait_for(
        asyncio.create_task(collect_events(adapter)),
        timeout=1,
    )
    assert events[-1].type == EventType.DONE.value


async def collect_events(adapter: PiAdapter):
    return [event async for event in adapter.stream_chat("session-stderr", "hello")]


@pytest.mark.asyncio
async def test_interrupt_cleans_process_registry(monkeypatch) -> None:
    adapter = PiAdapter()
    process = _FakeProcess([])
    adapter._processes["session-1"] = process
    called = False

    async def fake_terminate(process_arg, timeout):
        nonlocal called
        assert process_arg is process
        assert timeout >= 0
        called = True

    monkeypatch.setattr("src.adapters.pi.terminate_process_group", fake_terminate)

    assert await adapter.interrupt("session-1") is True
    assert called
    assert "session-1" not in adapter._processes
    assert await adapter.interrupt("missing") is False
