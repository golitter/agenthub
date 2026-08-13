from fastapi import APIRouter, Response, status

from src.app.config import settings
from src.security.startup_validation import sandbox_capabilities, strict_sandbox_enforced

router = APIRouter(tags=["health"])


@router.get("/health")
async def health_check() -> dict:
    return {"status": "ok", "version": settings.app.version}


@router.get("/health/live")
async def health_live() -> dict:
    return {"status": "ok", "version": settings.app.version}


@router.get("/health/ready")
async def health_ready(response: Response) -> dict:
    strict = settings.execution.sandbox.mode == "strict"
    # A strict backend is not marked ready until the real capability probe is
    # implemented and succeeds. This avoids advertising a policy-only sandbox.
    capabilities = sandbox_capabilities()
    sandbox_enforced = strict_sandbox_enforced(capabilities)
    auth_ready = settings.security.service_auth_enabled
    path_ready = bool(settings.security.allowed_repo_roots)
    ready = auth_ready and path_ready and (sandbox_enforced if strict else True)
    if not ready:
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
    return {
        "status": "ready" if ready else "not_ready",
        "service_auth_enabled": auth_ready,
        "sandbox_mode": settings.execution.sandbox.mode,
        "sandbox_enforced": sandbox_enforced,
        "path_policy_configured": path_ready,
        "capabilities": capabilities,
    }
