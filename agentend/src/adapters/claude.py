import asyncio
import json
from collections.abc import AsyncIterator

from src.adapters.base import BaseAgentAdapter
from src.app.config import settings
from src.schemas.events import EventType, StreamEvent
from src.schemas.response import AgentResponse

# Claude CLI output type -> StreamEvent type
_TYPE_MAP: dict[str, str] = {
    "system": EventType.INIT.value,
    "assistant": EventType.TEXT.value,
    "tool_use": EventType.TOOL_CALL.value,
    "tool_result": EventType.TOOL_RESULT.value,
    "result": EventType.DONE.value,
}

# Dangerous tools to block by default
_BLOCKED_TOOLS = {"dangerouslyDisableSandbox"}


class ClaudeCodeAdapter(BaseAgentAdapter):
    def __init__(self) -> None:
        self._processes: dict[str, asyncio.subprocess.Process] = {}

    def _build_command(
        self,
        message: str,
        *,
        cli_session_id: str | None = None,
        is_resume: bool = False,
        system_prompt_append: str | None = None,
        allowed_tools: list[str] | None = None,
        max_turns: int | None = None,
    ) -> list[str]:
        cmd = [settings.CLAUDE_CLI_PATH, "-p", message, "--output-format", "stream-json", "--verbose"]

        if cli_session_id:
            if is_resume:
                cmd.extend(["--resume", cli_session_id])
            else:
                cmd.extend(["--session-id", cli_session_id])
        if system_prompt_append:
            cmd.extend(["--append-system-prompt", system_prompt_append])
        if allowed_tools:
            cmd.extend(["--allowedTools", ",".join(allowed_tools)])
        if max_turns is not None:
            cmd.extend(["--max-turns", str(max_turns)])

        return cmd

    def _parse_stream_line(self, line: str) -> StreamEvent:
        line = line.strip()
        if not line:
            return StreamEvent.create(EventType.TEXT, text="")

        try:
            data = json.loads(line)
        except json.JSONDecodeError:
            return StreamEvent.create(EventType.TEXT, text=line)

        cli_type = data.get("type", "")
        event_type = _TYPE_MAP.get(cli_type)

        if event_type is None:
            return StreamEvent.create(EventType.TEXT, text="")

        if event_type == EventType.INIT:
            cli_session_id = data.get("session_id", "")
            content = {"cli_session_id": cli_session_id} if cli_session_id else {}
        elif event_type == EventType.TEXT:
            message = data.get("message", data)
            content_text = ""
            for block in message.get("content", []):
                if isinstance(block, dict) and block.get("type") == "text":
                    content_text += block.get("text", "")
            content = {"text": content_text} if content_text else {"raw": data}
        elif event_type == EventType.TOOL_CALL:
            content = {"tool": data.get("name", ""), "args": data.get("input", {})}
        elif event_type == EventType.TOOL_RESULT:
            content = {"tool": data.get("tool_use_id", ""), "result": data.get("content", "")}
        elif event_type == EventType.DONE:
            content = {"text": data.get("result", ""), "usage": data.get("usage", {})}
        else:
            content = {"raw": data}

        return StreamEvent(type=event_type, content=content)

    async def create_session(self, session_id: str) -> None:
        pass  # Sessions managed externally; Claude CLI uses --session flag

    async def chat(self, session_id: str, message: str, **kwargs) -> AgentResponse:
        chunks: list[str] = []
        usage: dict = {}

        async for event in self.stream_chat(session_id, message, **kwargs):
            if event.type == EventType.TEXT.value:
                text = event.content.get("text", "")
                if text:
                    chunks.append(text)
            elif event.type == EventType.DONE.value:
                result_text = event.content.get("text", "")
                if result_text and not chunks:
                    chunks.append(result_text)
                usage = event.content.get("usage", {})
            elif event.type == EventType.ERROR.value:
                break

        return AgentResponse(
            session_id=session_id,
            content="".join(chunks),
            usage=usage,
        )

    async def stream_chat(self, session_id: str, message: str, **kwargs) -> AsyncIterator[StreamEvent]:
        cli_session_id = kwargs.get("cli_session_id")
        is_resume = kwargs.get("is_resume", False)
        cmd = self._build_command(
            message,
            cli_session_id=cli_session_id,
            is_resume=is_resume,
            system_prompt_append=kwargs.get("system_prompt_append"),
            allowed_tools=kwargs.get("allowed_tools"),
            max_turns=kwargs.get("max_turns"),
        )

        process = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        self._processes[session_id] = process

        try:
            assert process.stdout is not None
            async for line in process.stdout:
                event = self._parse_stream_line(line.decode())
                yield event

            await process.wait()
            if process.returncode and process.returncode != 0:
                stderr = ""
                if process.stderr:
                    stderr = (await process.stderr.read()).decode()
                yield StreamEvent.create(EventType.ERROR, error=stderr, returncode=process.returncode)
        finally:
            self._processes.pop(session_id, None)

    async def interrupt(self, session_id: str) -> bool:
        process = self._processes.get(session_id)
        if not process or process.returncode is not None:
            return False

        process.terminate()
        try:
            await asyncio.wait_for(process.wait(), timeout=5.0)
        except asyncio.TimeoutError:
            process.kill()

        self._processes.pop(session_id, None)
        return True

    async def destroy_session(self, session_id: str) -> None:
        await self.interrupt(session_id)
