import asyncio
import json
import logging
from collections.abc import AsyncIterator
from numbers import Number

from src.adapters.base import BaseAgentAdapter, child_process_env, drain_stderr, terminate_process_group
from src.app.agent_config import get_agent_cli_path, get_agent_event_type
from src.app.config import settings
from src.schemas.events import EventType, StreamEvent
from src.schemas.response import AgentResponse

logger = logging.getLogger(__name__)

_AGENT_TYPE = get_agent_event_type("pi")
_CLI_PATH = get_agent_cli_path("pi")

_PI_TOOL_NAMES = {
    "read": "read",
    "bash": "bash",
    "edit": "edit",
    "write": "write",
    "grep": "grep",
    "find": "find",
    "ls": "ls",
}


async def _create_subprocess_exec(*args, **kwargs) -> asyncio.subprocess.Process:
    """Local seam for adapter tests without replacing asyncio's global API."""
    return await asyncio.create_subprocess_exec(*args, **kwargs)


def _normalize_allowed_tools(allowed_tools: object) -> list[str]:
    """Map Claude-style built-ins while preserving explicitly configured custom tools."""
    if isinstance(allowed_tools, str):
        values = allowed_tools.split(",")
    elif isinstance(allowed_tools, (list, tuple, set)):
        values = list(allowed_tools)
    else:
        return []

    normalized: list[str] = []
    for value in values:
        if not isinstance(value, str):
            continue
        name = value.strip()
        if not name:
            continue
        normalized.append(_PI_TOOL_NAMES.get(name.lower(), name))
    return normalized


def _text_value(value: object) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        parts: list[str] = []
        for item in value:
            if isinstance(item, str):
                parts.append(item)
            elif isinstance(item, dict):
                text = item.get("text", item.get("content", ""))
                if isinstance(text, str):
                    parts.append(text)
        return "".join(parts)
    return "" if value is None else str(value)


def _merge_usage(target: dict, source: dict) -> None:
    for key, value in source.items():
        if isinstance(value, Number) and not isinstance(value, bool):
            previous = target.get(key)
            if isinstance(previous, Number) and not isinstance(previous, bool):
                target[key] = previous + value
            else:
                target[key] = value
        elif key not in target:
            target[key] = value


def _assistant_usage(messages: object) -> dict:
    usage: dict = {}
    if not isinstance(messages, list):
        return usage
    for message in messages:
        if not isinstance(message, dict) or message.get("role") != "assistant":
            continue
        message_usage = message.get("usage")
        if isinstance(message_usage, dict):
            _merge_usage(usage, message_usage)
    return usage


class PiAdapter(BaseAgentAdapter):
    def __init__(self) -> None:
        self._processes: dict[str, asyncio.subprocess.Process] = {}

    def _build_command(
        self,
        message: str,
        *,
        cwd: str | None = None,
        system_prompt_append: str | None = None,
        cli_session_id: str | None = None,
        is_resume: bool = False,
        allowed_tools: list[str] | None = None,
        model: str | None = None,
    ) -> list[str]:
        # cwd deliberately stays a subprocess parameter.  Passing it through
        # the prompt or a CLI flag would make worktree isolation dependent on
        # user-controlled text instead of the process boundary.
        del cwd
        cmd = [
            _CLI_PATH,
            "--mode",
            "json",
            "--approve",
            "--no-extensions",
            "--no-prompt-templates",
        ]

        if cli_session_id and is_resume:
            cmd.extend(["--session", cli_session_id])
        if system_prompt_append:
            cmd.extend(["--append-system-prompt", system_prompt_append])

        normalized_tools = _normalize_allowed_tools(allowed_tools)
        if normalized_tools:
            cmd.extend(["--tools", ",".join(normalized_tools)])
        if model:
            cmd.extend(["--model", str(model)])

        cmd.append(message)
        return cmd

    def _parse_stream_line(self, line: str) -> StreamEvent | None:
        line = line.strip()
        if not line:
            return None

        try:
            data = json.loads(line)
        except json.JSONDecodeError:
            logger.debug("Ignoring non-JSON Pi stdout line: %s", line[:500])
            return None
        if not isinstance(data, dict):
            logger.debug("Ignoring non-object Pi JSON event: %r", data)
            return None

        event_type = data.get("type", "")

        if event_type == "session":
            session_id = data.get("id", "")
            return StreamEvent.create(EventType.INIT, cli_session_id=session_id, agent_type=_AGENT_TYPE)

        if event_type == "message_update":
            assistant_event = data.get("assistantMessageEvent")
            if not isinstance(assistant_event, dict):
                return None
            assistant_event_type = assistant_event.get("type", "")
            if assistant_event_type == "text_delta":
                text = _text_value(assistant_event.get("delta"))
                if text:
                    return StreamEvent.create(EventType.TEXT, text=text, agent_type=_AGENT_TYPE)
                return None
            if assistant_event_type == "thinking_end":
                content = _text_value(assistant_event.get("content"))
                if content:
                    return StreamEvent.create(
                        EventType.TEXT,
                        text=f"[thinking] {content}",
                        agent_type=_AGENT_TYPE,
                    )
                return None
            if assistant_event_type == "error":
                error_message = assistant_event.get("errorMessage") or data.get("errorMessage")
                if not error_message:
                    error_message = assistant_event.get("message") or data.get("message")
                return StreamEvent.create(
                    EventType.ERROR,
                    error=_text_value(error_message) or "Pi agent error",
                    agent_type=_AGENT_TYPE,
                )
            # thinking_delta and all other message update variants are
            # intentionally ignored; text_delta is the only true text delta.
            return None

        if event_type == "tool_execution_start":
            return StreamEvent.create(
                EventType.TOOL_CALL,
                tool=data.get("toolName", ""),
                args=data.get("args", {}),
                agent_type=_AGENT_TYPE,
            )

        if event_type == "tool_execution_end":
            return StreamEvent.create(
                EventType.TOOL_RESULT,
                tool=data.get("toolName", ""),
                result=data.get("result"),
                is_error=bool(data.get("isError")),
                agent_type=_AGENT_TYPE,
            )

        if event_type == "agent_end":
            usage = _assistant_usage(data.get("messages"))
            if not usage and isinstance(data.get("usage"), dict):
                usage = data["usage"]
            return StreamEvent.create(EventType.DONE, usage=usage, agent_type=_AGENT_TYPE)

        # agent_start, turn_start, message_start, thinking_delta,
        # tool_execution_update, queue/compaction/retry events and unknown
        # protocol events are lifecycle/debug details, not SSE content.
        return None

    async def create_session(self, session_id: str) -> None:
        pass

    async def chat(self, session_id: str, message: str, **kwargs) -> AgentResponse:
        chunks: list[str] = []
        usage: dict = {}

        async for event in self.stream_chat(session_id, message, **kwargs):
            if event.type == EventType.TEXT.value:
                text = event.content.get("text", "")
                if text:
                    chunks.append(text)
            elif event.type == EventType.DONE.value:
                usage = event.content.get("usage", {})

        return AgentResponse(
            session_id=session_id,
            content="".join(chunks),
            usage=usage,
        )

    async def stream_chat(self, session_id: str, message: str, **kwargs) -> AsyncIterator[StreamEvent]:
        cwd = kwargs.get("cwd")
        cmd = self._build_command(
            message,
            cwd=cwd,
            system_prompt_append=kwargs.get("system_prompt_append"),
            cli_session_id=kwargs.get("cli_session_id"),
            is_resume=kwargs.get("is_resume", False),
            allowed_tools=kwargs.get("allowed_tools"),
            model=kwargs.get("model"),
        )

        process = await _create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            cwd=cwd,
            env=child_process_env(kwargs.get("process_env")),
            start_new_session=True,
            limit=10 * 1024 * 1024,
        )
        self._processes[session_id] = process
        stderr_task = asyncio.create_task(drain_stderr(process.stderr))
        saw_done = False
        protocol_errors: list[str] = []

        try:
            assert process.stdout is not None
            async for line in process.stdout:
                if isinstance(line, bytes):
                    line = line.decode(errors="replace")
                event = self._parse_stream_line(line)
                if event:
                    if event.type == EventType.DONE.value:
                        saw_done = True
                    elif event.type == EventType.ERROR.value:
                        error = event.content.get("error", "")
                        if isinstance(error, str):
                            protocol_errors.append(error)
                    yield event

            stderr = await stderr_task
            if process.returncode is None:
                await process.wait()
            if process.returncode and process.returncode != 0:
                diagnostic = stderr.strip() or "Pi process failed"
                if diagnostic not in protocol_errors:
                    yield StreamEvent.create(
                        EventType.ERROR,
                        error=diagnostic,
                        returncode=process.returncode,
                        agent_type=_AGENT_TYPE,
                    )
            elif not saw_done:
                yield StreamEvent.create(EventType.DONE, agent_type=_AGENT_TYPE)
        finally:
            await terminate_process_group(process, settings.execution.process_terminate_timeout)
            if not stderr_task.done():
                await stderr_task
            self._processes.pop(session_id, None)

    async def interrupt(self, session_id: str) -> bool:
        process = self._processes.get(session_id)
        if not process:
            return False
        await terminate_process_group(process, settings.execution.process_terminate_timeout)
        self._processes.pop(session_id, None)
        return True

    async def destroy_session(self, session_id: str) -> None:
        await self.interrupt(session_id)
