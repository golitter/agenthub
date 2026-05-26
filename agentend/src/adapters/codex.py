import asyncio
import json
from collections.abc import AsyncIterator

from src.adapters.base import BaseAgentAdapter
from src.app.agent_config import get_agent_cli_path, get_agent_event_type
from src.app.config import settings
from src.schemas.events import EventType, StreamEvent
from src.schemas.response import AgentResponse

_AGENT_TYPE = get_agent_event_type("codex")
_CLI_PATH = get_agent_cli_path("codex")


class CodexAdapter(BaseAgentAdapter):
    def __init__(self) -> None:
        self._processes: dict[str, asyncio.subprocess.Process] = {}

    def _build_command(
        self,
        message: str,
        *,
        cwd: str | None = None,
        cli_session_id: str | None = None,
        is_resume: bool = False,
        model: str | None = None,
    ) -> list[str]:
        if cli_session_id and is_resume:
            cmd = [
                _CLI_PATH,
                "exec",
                "resume",
                cli_session_id,
                "--json",
                "--dangerously-bypass-approvals-and-sandbox",
            ]
        else:
            cmd = [
                _CLI_PATH,
                "exec",
                "--json",
                "--dangerously-bypass-approvals-and-sandbox",
                "-s",
                "workspace-write",
            ]
            if cwd:
                cmd.extend(["-C", cwd])
        if model:
            cmd.extend(["-m", model])
        cmd.append(message)
        return cmd

    def _parse_stream_line(self, line: str) -> StreamEvent | None:
        line = line.strip()
        if not line:
            return None

        try:
            data = json.loads(line)
        except json.JSONDecodeError:
            return None

        event_type = data.get("type", "")

        # thread.started → INIT
        if event_type == "thread.started":
            thread_id = data.get("thread_id", "")
            return StreamEvent.create(EventType.INIT, cli_session_id=thread_id, agent_type=_AGENT_TYPE)

        # turn.started → ignore
        if event_type == "turn.started":
            return None

        # item.started → TOOL_CALL (command_execution only)
        if event_type == "item.started":
            item = data.get("item", {})
            if item.get("type") == "command_execution":
                return StreamEvent.create(
                    EventType.TOOL_CALL,
                    tool="command_execution",
                    args={"command": item.get("command", "")},
                    agent_type=_AGENT_TYPE,
                )
            return None

        # item.completed → depends on item.type
        if event_type == "item.completed":
            item = data.get("item", {})
            item_type = item.get("type", "")

            if item_type == "reasoning":
                text = item.get("text", "")
                if text:
                    return StreamEvent.create(EventType.TEXT, text=f"[thinking] {text}", agent_type=_AGENT_TYPE)
                return None

            if item_type == "agent_message":
                text = item.get("text", "")
                if text:
                    return StreamEvent.create(EventType.TEXT, text=text, agent_type=_AGENT_TYPE)
                return None

            if item_type == "command_execution":
                return StreamEvent.create(
                    EventType.TOOL_RESULT,
                    tool="command_execution",
                    result=item.get("aggregated_output", ""),
                    exit_code=item.get("exit_code"),
                    agent_type=_AGENT_TYPE,
                )

            return None

        # turn.completed → DONE
        if event_type == "turn.completed":
            usage = data.get("usage", {})
            return StreamEvent.create(EventType.DONE, usage=usage, agent_type=_AGENT_TYPE)

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
            cli_session_id=kwargs.get("cli_session_id"),
            is_resume=kwargs.get("is_resume", False),
            model=kwargs.get("model"),
        )

        process = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            cwd=cwd,
        )
        self._processes[session_id] = process

        try:
            assert process.stdout is not None
            async for line in process.stdout:
                event = self._parse_stream_line(line.decode())
                if event:
                    yield event

            await process.wait()
            if process.returncode and process.returncode != 0:
                stderr = ""
                if process.stderr:
                    stderr = (await process.stderr.read()).decode()
                yield StreamEvent.create(
                    EventType.ERROR, error=stderr or "Codex process failed", agent_type=_AGENT_TYPE
                )
        finally:
            self._processes.pop(session_id, None)

    async def interrupt(self, session_id: str) -> bool:
        process = self._processes.get(session_id)
        if not process or process.returncode is not None:
            return False
        process.terminate()
        try:
            await asyncio.wait_for(process.wait(), timeout=settings.execution.process_terminate_timeout)
        except asyncio.TimeoutError:
            process.kill()
        self._processes.pop(session_id, None)
        return True

    async def destroy_session(self, session_id: str) -> None:
        await self.interrupt(session_id)
