import os
from abc import ABC, abstractmethod
from collections.abc import AsyncIterator

from src.schemas.events import StreamEvent
from src.schemas.response import AgentResponse


_PROCESS_CONTEXT_KEYS = frozenset(
    {
        "AGENTHUB_ARTIFACT_ENDPOINT",
        "AGENTHUB_ARTIFACT_TOKEN",
        "AGENTHUB_MESSAGE_ID",
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
_AGENTEND_SECRET_KEYS = frozenset({"ADMIN_PASSWORD", "DATABASE_URL"})


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
