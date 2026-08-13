from pydantic import Field

from src.generated.request import AgentRequest as _AgentRequest
from src.generated.request import AgentType


class AgentRequest(_AgentRequest):
    task_id: str = Field(
        min_length=1,
        max_length=128,
        pattern=r"^[A-Za-z0-9](?:[A-Za-z0-9_-]{0,126}[A-Za-z0-9])?$",
    )
    session_id: str = Field(
        min_length=1,
        max_length=128,
        pattern=r"^[A-Za-z0-9](?:[A-Za-z0-9_-]{0,126}[A-Za-z0-9])?$",
    )
    rules: list[str] = Field(default_factory=list)
    config: dict | None = None


__all__ = ["AgentType", "AgentRequest"]
