import asyncio
import json
import logging
from collections.abc import AsyncIterator

from src.adapters.base import BaseAgentAdapter
from src.app.config import settings
from src.schemas.events import EventType, StreamEvent
from src.schemas.response import AgentResponse

logger = logging.getLogger(__name__)


class OpenCodeAdapter(BaseAgentAdapter):
    def __init__(self) -> None:
        self._processes: dict[str, asyncio.subprocess.Process] = {}

    def _build_command(
        self,
        message: str,
        *,
        cwd: str | None = None,
        system_prompt_append: str | None = None,
    ) -> list[str]:
        prompt = message
        if system_prompt_append:
            prompt = f"[系统约束: {system_prompt_append}]\n\n{message}"
        cmd = [settings.OPENCODE_CLI_PATH, "-p", prompt, "-f", "json", "-q"]
        if cwd:
            cmd.extend(["-c", cwd])
        return cmd

    def _parse_json_output(self, raw: str) -> list[StreamEvent]:
        events: list[StreamEvent] = []
        try:
            data = json.loads(raw)
        except json.JSONDecodeError:
            events.append(StreamEvent.create(EventType.TEXT, text=raw, agent_type="opencode"))
            events.append(StreamEvent.create(EventType.DONE, agent_type="opencode"))
            return events

        content_text = data.get("content", "")
        if content_text:
            events.append(StreamEvent.create(EventType.TEXT, text=content_text, agent_type="opencode"))

        tool_uses = data.get("toolUses", [])
        for tu in tool_uses:
            events.append(
                StreamEvent.create(
                    EventType.TOOL_CALL,
                    tool=tu.get("name", ""),
                    args=tu.get("input", {}),
                    agent_type="opencode",
                )
            )

        usage = data.get("usage", {})
        events.append(StreamEvent.create(EventType.DONE, usage=usage, agent_type="opencode"))
        return events

    async def create_session(self, session_id: str) -> None:
        pass

    async def chat(self, session_id: str, message: str, **kwargs) -> AgentResponse:
        events = []
        async for event in self.stream_chat(session_id, message, **kwargs):
            events.append(event)

        text_parts = []
        usage = {}
        for e in events:
            if e.type == EventType.TEXT.value:
                t = e.content.get("text", "")
                if t:
                    text_parts.append(t)
            elif e.type == EventType.DONE.value:
                usage = e.content.get("usage", {})

        return AgentResponse(
            session_id=session_id,
            content="".join(text_parts),
            usage=usage,
        )

    async def stream_chat(self, session_id: str, message: str, **kwargs) -> AsyncIterator[StreamEvent]:
        cwd = kwargs.get("cwd")
        system_prompt_append = kwargs.get("system_prompt_append")
        cmd = self._build_command(message, cwd=cwd, system_prompt_append=system_prompt_append)

        process = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            cwd=cwd,
        )
        self._processes[session_id] = process

        try:
            stdout, stderr = await process.communicate()
            raw = stdout.decode()

            if process.returncode and process.returncode != 0:
                err = stderr.decode().strip()
                yield StreamEvent.create(EventType.ERROR, error=err or "OpenCode process failed", agent_type="opencode")
                return

            for event in self._parse_json_output(raw):
                yield event
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
