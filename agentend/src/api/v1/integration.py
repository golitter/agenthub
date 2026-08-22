from __future__ import annotations

import hmac
import os

from fastapi import APIRouter, Depends, Header, HTTPException
from pydantic import BaseModel, Field

from src.api.dependencies import (
    get_conflict_recovery_coordinator,
    get_integration_service,
    get_workspace_manager,
)
from src.app.config import settings
from src.integration.errors import (
    ERROR_CAPABILITY_INVALID,
    ERROR_INTEGRATION_RESULT_INVALID,
    ERROR_OPERATION_NOT_FOUND,
    IntegrationError,
)
from src.integration.service import IntegrationService
from src.integration.recovery import ConflictActionInput, ConflictRecoveryCoordinator
from src.workspace.manager import WorkspaceManager
from src.security.startup_validation import is_loopback_host

router = APIRouter(prefix="/v1/internal/integration-operations", tags=["integration"])
conflict_router = APIRouter(prefix="/v1/internal/conflicts", tags=["integration-diagnostics"])


class ExecuteIntegrationRequest(BaseModel):
    run_id: str = Field(min_length=1, max_length=128)


class ConflictActionBody(BaseModel):
    action: str = Field(min_length=1, max_length=64)
    task_id: str = Field(min_length=1, max_length=128)
    session_id: str = Field(min_length=1, max_length=128)
    root_run_id: str = Field(min_length=1, max_length=128)
    conflict_id: str = Field(min_length=1, max_length=128)
    expected_attempt: int = Field(ge=0, le=1000)
    confirmation: bool = False
    idempotency_key: str = Field(default="", max_length=128)
    resolver_agent: str = Field(default="", max_length=128)


def _capability_from_header(authorization: str) -> str:
    scheme, separator, token = authorization.partition(" ")
    if not separator or scheme.lower() != "bearer" or not token.strip():
        raise HTTPException(
            status_code=401,
            detail={"code": ERROR_CAPABILITY_INVALID, "message": "capability bearer token required"},
        )
    return token.strip()


def _require_diagnostic_auth(authorization: str = Header(default="")) -> None:
    """Keep Git/audit projections behind the service trust boundary.

    The ordinary operation projection is intentionally safe to expose to the
    Orchestrator/UI.  Git refs, worktree facts and resolver history are not.
    ``ServiceAuthMiddleware`` protects the whole service when enabled; this
    dependency keeps the diagnostic routes protected even in loopback/dev
    mode, where that middleware is commonly disabled.
    """
    # Local loopback development intentionally has no service-token bootstrap;
    # production/non-loopback listeners still require the explicit internal
    # token even when the global middleware is disabled by a legacy config.
    if not settings.security.service_auth_enabled and is_loopback_host(settings.server.host):
        return
    expected = os.environ.get("AGENTEND_SERVICE_TOKEN", "").strip()
    scheme, separator, supplied = authorization.partition(" ")
    if (
        not expected
        or not separator
        or scheme.lower() != "bearer"
        or not hmac.compare_digest(supplied.strip(), expected)
    ):
        raise HTTPException(
            status_code=401,
            detail={"code": ERROR_CAPABILITY_INVALID, "message": "service diagnostic authorization required"},
        )


def _integration_http_error(exc: IntegrationError) -> HTTPException:
    status = 404 if exc.code == ERROR_OPERATION_NOT_FOUND else 409
    if exc.code == ERROR_CAPABILITY_INVALID:
        status = 401
    elif exc.code == ERROR_INTEGRATION_RESULT_INVALID:
        status = 400
    return HTTPException(status_code=status, detail={"code": exc.code, "message": exc.message})


@router.get("/metrics")
async def get_integration_metrics(
    service: IntegrationService = Depends(get_integration_service),
) -> dict:
    return {"metrics": service.metrics_snapshot()}


@router.get("/{operation_id}")
async def get_integration_operation(
    operation_id: str,
    service: IntegrationService = Depends(get_integration_service),
) -> dict:
    try:
        operation = await service.get_operation_or_raise(operation_id)
    except IntegrationError as exc:
        raise _integration_http_error(exc) from exc
    projection = await service.operation_projection(operation_id)
    # Never return the durable binding (workspace handle/scope/session) on the
    # ordinary read path.  Git and workspace authority belongs to diagnostics.
    return {"projection": projection.model_dump(mode="json")}


@router.get("/{operation_id}/git-record")
async def get_git_integration_record(
    operation_id: str,
    service: IntegrationService = Depends(get_integration_service),
    _auth: None = Depends(_require_diagnostic_auth),
) -> dict:
    try:
        await service.get_operation_or_raise(operation_id)
    except IntegrationError as exc:
        raise _integration_http_error(exc) from exc
    record = await service.repository.get_git_record(operation_id)
    if record is None:
        raise HTTPException(status_code=404, detail={"code": ERROR_OPERATION_NOT_FOUND, "message": "git record not found"})
    return {"git_record": record.model_dump(mode="json")}


@router.get("/{operation_id}/resolution-attempts")
async def get_resolution_attempts(
    operation_id: str,
    service: IntegrationService = Depends(get_integration_service),
    _auth: None = Depends(_require_diagnostic_auth),
) -> dict:
    try:
        await service.get_operation_or_raise(operation_id)
    except IntegrationError as exc:
        raise _integration_http_error(exc) from exc
    conflicts = await service.repository.list_resolution_attempts_for_operation(operation_id)
    resolution_records = await service.repository.list_resolution_integration_records_for_operation(operation_id)
    return {
        "resolution_attempts": [item.model_dump(mode="json") for item in conflicts],
        "resolution_integration_records": [item.model_dump(mode="json") for item in resolution_records],
    }


@router.post("/{operation_id}/execute")
async def execute_integration_operation(
    operation_id: str,
    request: ExecuteIntegrationRequest,
    authorization: str = Header(default=""),
    service: IntegrationService = Depends(get_integration_service),
    workspace_mgr: WorkspaceManager = Depends(get_workspace_manager),
) -> dict:
    if not settings.orchestrator.integration_service_execute_enabled:
        raise HTTPException(
            status_code=503,
            detail={"code": "integration_service_disabled", "message": "IntegrationService execute is disabled"},
        )
    capability = _capability_from_header(authorization)
    try:
        projection = await service.execute_operation(
            operation_id,
            capability=capability,
            run_id=request.run_id,
            workspace_mgr=workspace_mgr,
        )
    except IntegrationError as exc:
        raise _integration_http_error(exc) from exc
    return projection.model_dump(mode="json")


@conflict_router.get("/{conflict_id}")
async def get_conflict_record(
    conflict_id: str,
    service: IntegrationService = Depends(get_integration_service),
    _auth: None = Depends(_require_diagnostic_auth),
) -> dict:
    record = await service.get_conflict_record(conflict_id)
    if record is None:
        raise HTTPException(
            status_code=404,
            detail={"code": ERROR_OPERATION_NOT_FOUND, "message": "conflict not found"},
        )
    attempts = await service.list_resolution_attempts(conflict_id)
    return {
        "conflict": record.model_dump(mode="json"),
        "resolution_attempts": [item.model_dump(mode="json") for item in attempts],
    }


@conflict_router.get("/{conflict_id}/projection")
async def get_conflict_projection(
    conflict_id: str,
    service: IntegrationService = Depends(get_integration_service),
    _auth: None = Depends(_require_diagnostic_auth),
) -> dict:
    projection = await service.conflict_projection(conflict_id)
    if projection is None:
        raise HTTPException(
            status_code=404,
            detail={"code": ERROR_OPERATION_NOT_FOUND, "message": "conflict not found"},
        )
    return projection


@conflict_router.post("/{conflict_id}/actions")
async def apply_conflict_action(
    conflict_id: str,
    request: ConflictActionBody,
    coordinator: ConflictRecoveryCoordinator = Depends(get_conflict_recovery_coordinator),
    _auth: None = Depends(_require_diagnostic_auth),
) -> dict:
    if request.conflict_id != conflict_id:
        raise HTTPException(
            status_code=409,
            detail={"code": "operation_binding_mismatch", "message": "conflict_id does not match path"},
        )
    try:
        return await coordinator.handle_action(
            ConflictActionInput(
                action=request.action,
                task_id=request.task_id,
                session_id=request.session_id,
                root_run_id=request.root_run_id,
                conflict_id=request.conflict_id,
                expected_attempt=request.expected_attempt,
                confirmation=request.confirmation,
                idempotency_key=request.idempotency_key,
                resolver_agent=request.resolver_agent,
            )
        )
    except IntegrationError as exc:
        raise _integration_http_error(exc) from exc
