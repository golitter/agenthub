import logging
import socket
from pathlib import Path

from aiohttp import web

logger = logging.getLogger(__name__)


def _find_free_port() -> int:
    """Find a free TCP port on localhost."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class PreviewServer:
    """HTTP server that serves static files from a workspace worktree directory."""

    def __init__(self, worktree_path: str, port: int | None = None):
        self._worktree_path = Path(worktree_path).resolve()
        self._port = port or _find_free_port()
        self._runner: web.AppRunner | None = None

    @property
    def port(self) -> int:
        return self._port

    @property
    def url(self) -> str:
        return f"http://localhost:{self._port}"

    async def start(self) -> None:
        app = web.Application()
        app.router.add_route("*", "/{path:.*}", self._handle)
        self._runner = web.AppRunner(app)
        await self._runner.setup()
        site = web.TCPSite(self._runner, "127.0.0.1", self._port)
        await site.start()
        logger.info("Preview server started at %s for %s", self.url, self._worktree_path)

    async def stop(self) -> None:
        if self._runner:
            await self._runner.cleanup()
            self._runner = None
            logger.info("Preview server stopped for %s", self._worktree_path)

    async def _handle(self, request: web.Request) -> web.Response:
        rel_path = request.match_info.get("path", "index.html") or "index.html"
        # Prevent path traversal
        target = (self._worktree_path / rel_path).resolve()
        if not str(target).startswith(str(self._worktree_path) + "/") and target != self._worktree_path:
            return web.Response(status=403, text="Forbidden")

        if not target.is_file():
            return web.Response(status=404, text="Not Found")

        return web.FileResponse(target)


class PreviewManager:
    """Manages preview servers per workspace."""

    def __init__(self) -> None:
        self._servers: dict[str, PreviewServer] = {}

    async def start(self, workspace_id: str, worktree_path: str, port: int | None = None) -> PreviewServer:
        if workspace_id in self._servers:
            return self._servers[workspace_id]
        srv = PreviewServer(worktree_path, port)
        await srv.start()
        self._servers[workspace_id] = srv
        return srv

    async def stop(self, workspace_id: str) -> None:
        srv = self._servers.pop(workspace_id, None)
        if srv:
            await srv.stop()

    def get(self, workspace_id: str) -> PreviewServer | None:
        return self._servers.get(workspace_id)

    async def stop_all(self) -> None:
        for ws_id in list(self._servers):
            await self.stop(ws_id)
