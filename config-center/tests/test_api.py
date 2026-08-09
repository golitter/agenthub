from __future__ import annotations

import json
import threading
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

import pytest

from server.main import Handler, OPERATION_LOCK


def _request(url: str, method: str = "GET", origin: str | None = None, token: str = "", payload: dict | None = None) -> tuple[int, dict]:
    headers = {"Origin": origin} if origin else {}
    if token:
        headers["X-Config-Center-Token"] = token
    data = json.dumps(payload or {}).encode() if method == "POST" else None
    request = urllib.request.Request(url, method=method, headers=headers, data=data)
    try:
        with urllib.request.urlopen(request, timeout=2) as response:
            return response.status, json.loads(response.read())
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read())


def test_health_session_and_security_boundary() -> None:
    try:
        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    except PermissionError:
        pytest.skip("sandbox does not permit loopback sockets")
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    base = f"http://127.0.0.1:{server.server_port}"
    try:
        status, health = _request(base + "/api/health", origin="http://127.0.0.1:5174")
        assert status == 200
        assert health["status"] == "ok"

        status, session = _request(base + "/api/session", origin="http://127.0.0.1:5174")
        assert status == 200
        assert len(session["token"]) >= 32

        status, denied_config = _request(
            base + "/api/config?profile=local", origin="http://127.0.0.1:5174"
        )
        assert status == 401
        assert denied_config["error"] == "invalid_session"

        status, config = _request(
            base + "/api/config?profile=local", origin="http://127.0.0.1:5174", token=session["token"]
        )
        assert status == 200
        backend_yaml = next(item for item in config["files"] if item["path"] == "backend/configs/config.yaml")
        assert backend_yaml["exampleContent"] != backend_yaml["actualContent"]

        status, denied = _request(base + "/api/config/save", method="POST", origin="http://127.0.0.1:5174")
        assert status == 401
        assert denied["error"] == "invalid_session"

        status, denied_origin = _request(base + "/api/session", origin="https://example.invalid")
        assert status == 403
        assert denied_origin["error"] == "origin_not_allowed"

        status, denied_write_origin = _request(
            base + "/api/config/save",
            method="POST",
            origin="https://example.invalid",
            token=session["token"],
            payload={"profile": "local", "files": {}},
        )
        assert status == 403
        assert denied_write_origin["error"] == "origin_not_allowed"

        status, removed_control = _request(
            base + "/api/control",
            method="POST",
            origin="http://127.0.0.1:5174",
            token=session["token"],
            payload={"profile": "local"},
        )
        assert status == 404
        assert removed_control["error"] == "not_found"

        OPERATION_LOCK.acquire()
        try:
            status, conflict = _request(
                base + "/api/config/save",
                method="POST",
                origin="http://127.0.0.1:5174",
                token=session["token"],
                payload={"profile": "local", "files": {}},
            )
        finally:
            OPERATION_LOCK.release()
        assert status == 409
        assert conflict["error"] == "conflict"
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)
