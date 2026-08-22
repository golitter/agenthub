from pathlib import Path
import sys

import pytest
from starlette.responses import JSONResponse

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.security.authentication import ServiceAuthMiddleware


@pytest.mark.asyncio
async def test_service_auth_allows_live_and_rejects_missing_token(monkeypatch):
    monkeypatch.setenv("AGENTEND_SERVICE_TOKEN", "expected-token")

    async def endpoint(scope, receive, send):
        await JSONResponse({"status": "ok"})(scope, receive, send)

    app = ServiceAuthMiddleware(endpoint, enabled=True)

    async def request(path: str, token: str | None = None) -> int:
        messages = []
        headers = []
        if token is not None:
            headers.append((b"authorization", f"Bearer {token}".encode()))
        scope = {"type": "http", "method": "GET", "path": path, "headers": headers}

        async def receive():
            return {"type": "http.request", "body": b"", "more_body": False}

        async def send(message):
            messages.append(message)

        await app(scope, receive, send)
        return next(message["status"] for message in messages if message["type"] == "http.response.start")

    assert await request("/health/live") == 200
    assert (
        await request(
            "/v1/internal/integration-operations/44444444-4444-4444-8444-444444444444/execute"
        )
        == 200
    )
    assert await request("/v1/private") == 401
    assert await request("/v1/private", "wrong") == 401
    assert await request("/v1/private", "expected-token") == 200
