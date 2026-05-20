import time
from enum import Enum

from pydantic import BaseModel, Field


class EventType(str, Enum):
    INIT = "init"
    TEXT = "text"
    TOOL_CALL = "tool_call"
    TOOL_RESULT = "tool_result"
    ARTIFACT = "artifact"
    DONE = "done"
    ERROR = "error"


class StreamEvent(BaseModel):
    type: str
    content: dict = Field(default_factory=dict)
    timestamp: float = Field(default_factory=time.time)

    @staticmethod
    def create(event_type: EventType, agent_type: str | None = None, **kwargs) -> "StreamEvent":
        if agent_type:
            kwargs["agent_type"] = agent_type
        return StreamEvent(type=event_type.value, content=kwargs)
