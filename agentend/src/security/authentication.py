from __future__ import annotations

import hmac
import os
import re

from starlette.responses import JSONResponse
from starlette.types import ASGIApp, Receive, Scope, Send

_ANONYMOUS_PATHS = frozenset({"/health/live"})
_CAPABILITY_EXECUTE_PATH = re.compile(
    r"^/v1/internal/integration-operations/[A-Za-z0-9-]{1,128}/execute$"
)


class ServiceAuthMiddleware:
    """Authenticate service calls without buffering streaming responses."""

    def __init__(self, app: ASGIApp, enabled: bool) -> None:
        self.app = app
        self.enabled = enabled

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        path = scope.get("path", "")
        # The execute endpoint authenticates with a single-use capability
        # bound to one operation/run/workspace.  Requiring the process-wide
        # service token here would force taskctl to receive a broader secret.
        capability_route = bool(_CAPABILITY_EXECUTE_PATH.fullmatch(path))
        if (
            scope["type"] != "http"
            or not self.enabled
            or path in _ANONYMOUS_PATHS
            or capability_route
        ):
            await self.app(scope, receive, send)
            return
        expected = os.environ.get("AGENTEND_SERVICE_TOKEN", "")
        header = next(
            (
                value.decode("latin-1")
                for key, value in scope.get("headers", [])
                if key.lower() == b"authorization"
            ),
            "",
        )
        scheme, separator, credentials = header.partition(" ")
        supplied = credentials.strip() if separator and scheme.lower() == "bearer" else ""
        if not expected or not supplied or not hmac.compare_digest(supplied, expected):
            response = JSONResponse(status_code=401, content={"detail": "service authentication required"})
            await response(scope, receive, send)
            return
        await self.app(scope, receive, send)
