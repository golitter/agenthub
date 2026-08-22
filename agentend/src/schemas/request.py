from uuid import UUID

from pydantic import Field, field_validator, model_validator

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

    current_run_id: str | None = Field(default=None, max_length=128)
    workspace_id: str | None = Field(default=None, max_length=128)
    plan_task_id: str | None = Field(
        default=None,
        max_length=128,
        pattern=r"^[A-Za-z0-9](?:[A-Za-z0-9_-]{0,126}[A-Za-z0-9])?$",
    )
    integration_operation_id: str | None = Field(default=None, max_length=128)
    workspace_handle: str | None = Field(default=None, max_length=128)
    integration_capability: str | None = Field(default=None, max_length=4096)
    integration_attempt: int = Field(default=0, ge=0, le=100)

    @model_validator(mode="after")
    def validate_internal_lineage(self) -> "AgentRequest":
        if self.integration_operation_id:
            required = {
                "run_id": self.run_id,
                "root_run_id": self.root_run_id,
                "parent_run_id": self.parent_run_id,
                "current_run_id": self.current_run_id,
                "plan_task_id": self.plan_task_id,
                "workspace_id": self.workspace_id,
                "workspace_handle": self.workspace_handle,
            }
            missing = [name for name, value in required.items() if not isinstance(value, str) or not value.strip()]
            if missing:
                raise ValueError(
                    "internal child-run identity is incomplete: " + ", ".join(sorted(missing))
                )
            for name in ("run_id", "root_run_id", "parent_run_id", "integration_operation_id"):
                try:
                    UUID(getattr(self, name) or "")
                except (ValueError, TypeError, AttributeError) as exc:
                    raise ValueError(f"{name} must be a UUID for an internal child run") from exc
        if self.integration_attempt and not self.integration_operation_id and not self.plan_task_id:
            raise ValueError("integration_attempt requires an internal plan task")
        return self

    @field_validator("current_run_id", "integration_operation_id")
    @classmethod
    def validate_internal_uuid(cls, value: str | None) -> str | None:
        if value is not None:
            try:
                UUID(value)
            except (ValueError, TypeError, AttributeError) as exc:
                raise ValueError("internal run/operation identity must be a UUID") from exc
        return value


__all__ = ["AgentType", "AgentRequest"]
