import asyncio
import json
import logging
from collections.abc import AsyncIterator

from src.adapters.base import BaseAgentAdapter
from src.app.agent_config import get_agent_cli_path, get_agent_event_type
from src.app.config import settings
from src.schemas.events import EventType, StreamEvent
from src.schemas.response import AgentResponse

logger = logging.getLogger(__name__)

_AGENT_TYPE = get_agent_event_type("opencode")
_CLI_PATH = get_agent_cli_path("opencode")


class OpenCodeAdapter(BaseAgentAdapter):
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
        model: str | None = None,
        agent: str | None = None,
    ) -> list[str]:
        prompt = message
        if system_prompt_append:
            prompt = f"[系统约束: {system_prompt_append}]\n\n{message}"
        # CLI 可执行文件路径来自 config.yaml 的 cli.opencode_path
        cmd = [_CLI_PATH, "run", prompt, "--format", "json"]
        if cwd:
            cmd.extend(["--dir", cwd])
        if cli_session_id:
            cmd.extend(["--session", cli_session_id])
            if is_resume:
                cmd.append("--fork")
        if model:
            cmd.extend(["--model", model])
        if agent:
            cmd.extend(["--agent", agent])
        return cmd

    def _parse_ndjson_line(self, line: str) -> StreamEvent | None:
        line = line.strip()
        if not line:
            return None

        try:
            data = json.loads(line)
        except json.JSONDecodeError:
            return StreamEvent.create(EventType.TEXT, text=line, agent_type=_AGENT_TYPE)

        event_type = data.get("type", "")

        if event_type == "error":
            error_obj = data.get("error", {})
            msg = ""
            if isinstance(error_obj, dict):
                err_data = error_obj.get("data", {})
                msg = err_data.get("message", error_obj.get("name", str(error_obj)))
            else:
                msg = str(error_obj)
            return StreamEvent.create(EventType.ERROR, error=msg, agent_type=_AGENT_TYPE)

        if event_type == "step_start":
            sid = data.get("sessionID", "")
            return StreamEvent.create(EventType.INIT, cli_session_id=sid, agent_type=_AGENT_TYPE)

        if event_type == "text":
            part = data.get("part", {})
            text = part.get("text", "")
            if text:
                return StreamEvent.create(EventType.TEXT, text=text, agent_type=_AGENT_TYPE)
            return None

        if event_type == "reasoning":
            part = data.get("part", {})
            text = part.get("text", "")
            if text:
                return StreamEvent.create(EventType.TEXT, text=f"[thinking] {text}", agent_type=_AGENT_TYPE)
            return None

        if event_type == "tool_use":
            part = data.get("part", {})
            tool_name = part.get("tool", "")
            state = part.get("state", {})
            args = state.get("input", {})
            status = state.get("status", "")
            output = state.get("output", "")
            if status == "error":
                err = state.get("error", "tool error")
                return StreamEvent.create(EventType.TOOL_RESULT, tool=tool_name, result=err, agent_type=_AGENT_TYPE)
            return StreamEvent.create(
                EventType.TOOL_CALL, tool=tool_name, args=args, result=output, agent_type=_AGENT_TYPE
            )

        if event_type == "step_finish":
            return None

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
            model=kwargs.get("model"),
            agent=kwargs.get("agent"),
        )

        process = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            cwd=cwd,
            limit=10 * 1024 * 1024,  # 10 MB — CLI may emit very long lines
        )
        self._processes[session_id] = process

        try:
            assert process.stdout is not None
            async for line in process.stdout:
                event = self._parse_ndjson_line(line.decode())
                if event:
                    yield event

            await process.wait()
            if process.returncode and process.returncode != 0:
                stderr = ""
                if process.stderr:
                    stderr = (await process.stderr.read()).decode()
                yield StreamEvent.create(
                    EventType.ERROR, error=stderr or "OpenCode process failed", agent_type=_AGENT_TYPE
                )
            else:
                yield StreamEvent.create(EventType.DONE, agent_type=_AGENT_TYPE)
        finally:
            self._processes.pop(session_id, None)

    async def interrupt(self, session_id: str) -> bool:
        process = self._processes.get(session_id)
        if not process or process.returncode is not None:
            return False
        process.terminate()
        # 超时来自 config.yaml 的 execution.process_terminate_timeout
        try:
            await asyncio.wait_for(process.wait(), timeout=settings.execution.process_terminate_timeout)
        except asyncio.TimeoutError:
            process.kill()
        self._processes.pop(session_id, None)
        return True

    async def destroy_session(self, session_id: str) -> None:
        await self.interrupt(session_id)
