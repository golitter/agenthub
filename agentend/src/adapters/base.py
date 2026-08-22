import asyncio
import os
import signal
import time
from abc import ABC, abstractmethod
from collections.abc import AsyncIterator

from src.schemas.events import StreamEvent
from src.schemas.response import AgentResponse

_PROCESS_CONTEXT_KEYS = frozenset(
    {
        "AGENTHUB_ARTIFACT_ENDPOINT",
        "AGENTHUB_ARTIFACT_TOKEN",
        "AGENTHUB_MESSAGE_ID",
        "AGENTHUB_RUN_ID",
        "AGENTHUB_ROOT_RUN_ID",
        "AGENTHUB_PARENT_RUN_ID",
        "AGENTHUB_CURRENT_RUN_ID",
        "AGENTHUB_PLAN_TASK_ID",
        "AGENTHUB_INTEGRATION_OPERATION_ID",
        "AGENTHUB_WORKSPACE_HANDLE",
        "AGENTHUB_INTEGRATION_CAPABILITY",
        "AGENTHUB_INTEGRATION_ATTEMPT",
        "AGENTHUB_INTEGRATION_ENDPOINT",
        "AGENTHUB_INTEGRATION_SERVICE_EXECUTE_ENABLED",
    }
)

# Agent CLI authentication variables remain available, but AgentEnd's own
# database, storage, JWT, and observability credentials must not be inherited
# by a command-capable child process. The allowlisted artifact context is
# applied after this boundary.
_AGENTEND_SECRET_PREFIXES = (
    "ARTIFACT_",
    "ASSET_MINIO_",
    "CORS_",
    "DS_",
    "JWT_",
    "LANGFUSE_",
    "MINIO_",
    "MYSQL_",
    "REDIS_",
    "SKILL_STORAGE_",
)
_AGENTEND_SECRET_KEYS = frozenset(
    {
        "ADMIN_PASSWORD",
        "DATABASE_URL",
        "AGENTEND_SERVICE_TOKEN",
        "BACKEND_SERVICE_TOKEN",
        "CREDENTIAL_BROKER_KEY",
    }
)

_STDERR_CAPTURE_LIMIT = 1024 * 1024


def child_process_env(extra: dict[str, str] | None = None) -> dict[str, str]:
    """Preserve CLI/system environment without forwarding AgentEnd secrets."""
    env = {
        key: value
        for key, value in os.environ.items()
        if key not in _AGENTEND_SECRET_KEYS and not key.startswith(_AGENTEND_SECRET_PREFIXES)
    }
    for key in _PROCESS_CONTEXT_KEYS:
        env.pop(key, None)
    for key in _PROCESS_CONTEXT_KEYS:
        if extra and extra.get(key):
            env[key] = str(extra[key])
    return env


async def drain_stderr(
    stream: asyncio.StreamReader | None,
    capture_limit: int = _STDERR_CAPTURE_LIMIT,
) -> str:
    """Continuously drain stderr while retaining only a bounded diagnostic prefix."""
    if stream is None:
        return ""
    captured = bytearray()
    while chunk := await stream.read(64 * 1024):
        remaining = capture_limit - len(captured)
        if remaining > 0:
            captured.extend(chunk[:remaining])
    return captured.decode(errors="replace")


async def terminate_process_group(process: asyncio.subprocess.Process, timeout: float) -> None:
    """Terminate the CLI and every descendant in its dedicated process group."""
    group_signalled = False
    try:
        os.killpg(process.pid, signal.SIGTERM)
        group_signalled = True
    except (ProcessLookupError, PermissionError, AttributeError):
        if process.returncode is None:
            try:
                process.terminate()
            except ProcessLookupError:
                pass

    deadline = time.monotonic() + max(0.0, timeout)
    while group_signalled and time.monotonic() < deadline:
        try:
            os.killpg(process.pid, 0)
        except ProcessLookupError:
            group_signalled = False
            break
        except PermissionError:
            break
        await asyncio.sleep(0.05)

    if group_signalled:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError, AttributeError):
            pass
    elif process.returncode is None:
        try:
            process.kill()
        except ProcessLookupError:
            pass
    if process.returncode is None:
        await process.wait()


class BaseAgentAdapter(ABC):
    @abstractmethod
    async def create_session(self, session_id: str) -> None: ...

    @abstractmethod
    async def chat(self, session_id: str, message: str, **kwargs) -> AgentResponse: ...

    @abstractmethod
    async def stream_chat(self, session_id: str, message: str, **kwargs) -> AsyncIterator[StreamEvent]: ...

    @abstractmethod
    async def interrupt(self, session_id: str) -> bool: ...

    @abstractmethod
    async def destroy_session(self, session_id: str) -> None: ...
