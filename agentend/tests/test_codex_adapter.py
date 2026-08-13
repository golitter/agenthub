import asyncio
import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.adapters.codex import CodexAdapter
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


def test_build_command_resume_without_prompt_omits_prompt_arg() -> None:
    adapter = CodexAdapter()

    cmd = adapter._build_command("", cli_session_id="cli-123", is_resume=True)

    assert cmd[-1] != ""
    assert "resume" in cmd
    assert "cli-123" in cmd


@pytest.mark.asyncio
async def test_stream_chat_emits_done_on_clean_exit_without_turn_completed(monkeypatch) -> None:
    adapter = CodexAdapter()
    init_line = json.dumps({"type": "thread.started", "thread_id": "cli-123"}).encode() + b"\n"

    async def _fake_create_subprocess_exec(*args, **kwargs):
        return _FakeProcess([init_line], returncode=0)

    monkeypatch.setattr("src.adapters.codex._create_subprocess_exec", _fake_create_subprocess_exec)

    events = [event async for event in adapter.stream_chat("session-1", "hello")]

    assert events[0].type == EventType.INIT.value
    assert events[0].content["cli_session_id"] == "cli-123"
    assert events[-1].type == EventType.DONE.value


@pytest.mark.asyncio
async def test_stream_chat_drains_large_stderr_without_deadlocking(monkeypatch) -> None:
    adapter = CodexAdapter()
    stderr_drained = asyncio.Event()
    done_line = json.dumps({"type": "turn.completed", "usage": {}}).encode() + b"\n"

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

    process = _FakeProcess([], returncode=0)
    process.stdout = BlockingStdout()
    process.stderr = SignallingStderr()

    async def fake_create_subprocess_exec(*args, **kwargs):
        return process

    monkeypatch.setattr("src.adapters.codex.asyncio.create_subprocess_exec", fake_create_subprocess_exec)

    async def collect():
        return [event async for event in adapter.stream_chat("session-stderr", "hello")]

    events = await asyncio.wait_for(collect(), timeout=1)
    assert events[-1].type == EventType.DONE.value
