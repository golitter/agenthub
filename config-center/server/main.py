from __future__ import annotations

import json
import os
import secrets
import sys
import threading
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Callable
from urllib.parse import parse_qs, urlparse

from server import __version__
from server.config_io import ConfigConflict, ConfigError, ConfigService, ValidationError
from server.profiles import ProfileService
from server.runner import ApplyService

HOST = "127.0.0.1"
PORT = 9100
MAX_BODY = 1024 * 1024
ALLOWED_ORIGINS = {"http://127.0.0.1:5174", "http://localhost:5174"}
PROJECT_ROOT = Path(__file__).resolve().parents[2]
SESSION_TOKEN = secrets.token_urlsafe(32)
CONFIG = ConfigService(PROJECT_ROOT)
PROFILES = ProfileService(PROJECT_ROOT, CONFIG)
APPLY = ApplyService(PROJECT_ROOT)
OPERATION_LOCK = threading.Lock()


class Handler(BaseHTTPRequestHandler):
    server_version = "AgentHubConfigCenter/" + __version__

    def log_message(self, format: str, *args: Any) -> None:
        sys.stderr.write(f"[config-center] {self.address_string()} {format % args}\n")

    def _origin_allowed(self) -> bool:
        origin = self.headers.get("Origin")
        return origin is None or origin in ALLOWED_ORIGINS

    def _authorized(self) -> bool:
        return secrets.compare_digest(self.headers.get("X-Config-Center-Token", ""), SESSION_TOKEN)

    def _headers(self, status: int, content_type: str = "application/json; charset=utf-8") -> None:
        self.send_response(status)
        origin = self.headers.get("Origin")
        if origin in ALLOWED_ORIGINS:
            self.send_header("Access-Control-Allow-Origin", origin)
            self.send_header("Vary", "Origin")
        self.send_header("Content-Type", content_type)
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
        self.end_headers()

    def _json(self, status: int, payload: Any) -> None:
        body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        origin = self.headers.get("Origin")
        if origin in ALLOWED_ORIGINS:
            self.send_header("Access-Control-Allow-Origin", origin)
            self.send_header("Vary", "Origin")
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
        self.end_headers()
        self.wfile.write(body)

    def _read_json(self) -> dict[str, Any]:
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError as exc:
            raise ConfigError("invalid content length") from exc
        if length <= 0 or length > MAX_BODY:
            raise ConfigError("request body must be between 1 byte and 1 MiB")
        try:
            payload = json.loads(self.rfile.read(length))
        except (json.JSONDecodeError, UnicodeDecodeError) as exc:
            raise ConfigError("invalid JSON body") from exc
        if not isinstance(payload, dict):
            raise ConfigError("JSON body must be an object")
        return payload

    def _query_profile(self, query: dict[str, list[str]]) -> str:
        profile = query.get("profile", ["local"])[0]
        if profile not in {"local", "docker"}:
            raise ConfigError("profile must be local or docker")
        return profile

    def _exclusive_operation(self, operation: Callable[[], dict[str, Any]]) -> dict[str, Any]:
        if not OPERATION_LOCK.acquire(blocking=False):
            raise ConfigConflict("another save, restore, or apply operation is running")
        try:
            return operation()
        finally:
            OPERATION_LOCK.release()

    def do_OPTIONS(self) -> None:
        if not self._origin_allowed():
            self._json(HTTPStatus.FORBIDDEN, {"error": "origin_not_allowed"})
            return
        self.send_response(HTTPStatus.NO_CONTENT)
        origin = self.headers.get("Origin")
        if origin in ALLOWED_ORIGINS:
            self.send_header("Access-Control-Allow-Origin", origin)
            self.send_header("Vary", "Origin")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, X-Config-Center-Token")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Max-Age", "600")
        self.end_headers()

    def do_GET(self) -> None:
        if not self._origin_allowed():
            self._json(HTTPStatus.FORBIDDEN, {"error": "origin_not_allowed"})
            return
        parsed = urlparse(self.path)
        query = parse_qs(parsed.query)
        try:
            if parsed.path == "/api/health":
                self._json(HTTPStatus.OK, {"status": "ok", "version": __version__})
            elif parsed.path == "/api/session":
                self._json(HTTPStatus.OK, {"token": SESSION_TOKEN})
            elif parsed.path == "/api/profiles":
                self._json(HTTPStatus.OK, {"profiles": PROFILES.profiles()})
            elif parsed.path == "/api/config":
                if not self._authorized():
                    self._json(HTTPStatus.UNAUTHORIZED, {"error": "invalid_session"})
                else:
                    self._json(HTTPStatus.OK, CONFIG.get_config(self._query_profile(query)))
            elif parsed.path == "/api/backups":
                self._json(HTTPStatus.OK, {"backups": CONFIG.list_backups(self._query_profile(query))})
            else:
                self._json(HTTPStatus.NOT_FOUND, {"error": "not_found"})
        except Exception as exc:
            self._handle_error(exc)

    def do_POST(self) -> None:
        if not self._origin_allowed():
            self._json(HTTPStatus.FORBIDDEN, {"error": "origin_not_allowed"})
            return
        if not self._authorized():
            self._json(HTTPStatus.UNAUTHORIZED, {"error": "invalid_session"})
            return
        parsed = urlparse(self.path)
        try:
            payload = self._read_json()
            profile = payload.get("profile", "local")
            if profile not in {"local", "docker"}:
                raise ConfigError("profile must be local or docker")
            if parsed.path == "/api/config/validate":
                self._json(HTTPStatus.OK, CONFIG.validate(profile, payload))
            elif parsed.path == "/api/config/save":
                self._json(HTTPStatus.OK, self._exclusive_operation(lambda: CONFIG.save(profile, payload)))
            elif parsed.path == "/api/backups/restore":
                self._json(
                    HTTPStatus.OK,
                    self._exclusive_operation(
                        lambda: CONFIG.restore(
                            profile,
                            str(payload.get("fileId", "")),
                            str(payload.get("backupId", "")),
                            str(payload.get("revision", "")),
                        )
                    ),
                )
            elif parsed.path == "/api/apply":
                result = self._exclusive_operation(lambda: APPLY.apply(profile))
                self._json(HTTPStatus.OK if result["ok"] else HTTPStatus.UNPROCESSABLE_ENTITY, result)
            else:
                self._json(HTTPStatus.NOT_FOUND, {"error": "not_found"})
        except Exception as exc:
            self._handle_error(exc)

    def _handle_error(self, exc: Exception) -> None:
        if isinstance(exc, ConfigConflict):
            self._json(HTTPStatus.CONFLICT, {"error": "conflict", "message": str(exc)})
        elif isinstance(exc, ValidationError):
            self._json(HTTPStatus.UNPROCESSABLE_ENTITY, {"error": "validation_failed", "issues": exc.issues})
        elif isinstance(exc, (ConfigError, ValueError, KeyError)):
            self._json(HTTPStatus.BAD_REQUEST, {"error": "bad_request", "message": str(exc)})
        else:
            self.log_error("unhandled error: %r", exc)
            self._json(HTTPStatus.INTERNAL_SERVER_ERROR, {"error": "internal_error", "message": "operation failed"})


def main() -> None:
    server = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"AgentHub config center API listening on http://{HOST}:{PORT}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
