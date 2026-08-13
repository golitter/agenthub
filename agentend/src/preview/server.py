import asyncio
import logging
import socket
from pathlib import Path

from aiohttp import web

logger = logging.getLogger(__name__)

class PreviewServer:
    """HTTP 服务器，从 workspace 的 worktree 目录提供静态文件。"""

    def __init__(self, worktree_path: str, port: int | None = None):
        self._worktree_path = Path(worktree_path).resolve()
        self._port = port if port is not None else 0
        self._runner: web.AppRunner | None = None
        self._site: web.SockSite | None = None

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
        listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            listener.bind(("127.0.0.1", self._port))
            listener.listen(128)
            listener.setblocking(False)
            self._port = int(listener.getsockname()[1])
            self._site = web.SockSite(self._runner, listener)
            await self._site.start()
        except BaseException:
            listener.close()
            await self._runner.cleanup()
            self._runner = None
            raise
        logger.info("Preview server started at %s for %s", self.url, self._worktree_path)

    async def stop(self) -> None:
        if self._runner:
            await self._runner.cleanup()
            self._runner = None
            self._site = None
            logger.info("Preview server stopped for %s", self._worktree_path)

    async def _handle(self, request: web.Request) -> web.Response:
        rel_path = request.match_info.get("path", "index.html") or "index.html"
        # 防止路径穿越
        target = (self._worktree_path / rel_path).resolve()
        if not str(target).startswith(str(self._worktree_path) + "/") and target != self._worktree_path:
            return web.Response(status=403, text="Forbidden")

        if not target.is_file():
            return web.Response(status=404, text="Not Found")

        return web.FileResponse(target)


class PreviewManager:
    """按 workspace 管理预览服务器。"""

    def __init__(self) -> None:
        self._servers: dict[str, PreviewServer] = {}
        self._lock = asyncio.Lock()

    async def start(self, workspace_id: str, worktree_path: str, port: int | None = None) -> PreviewServer:
        async with self._lock:
            if workspace_id in self._servers:
                return self._servers[workspace_id]
            srv = PreviewServer(worktree_path, port)
            await srv.start()
            self._servers[workspace_id] = srv
            return srv

    async def stop(self, workspace_id: str) -> None:
        async with self._lock:
            srv = self._servers.pop(workspace_id, None)
        if srv:
            await srv.stop()

    def get(self, workspace_id: str) -> PreviewServer | None:
        return self._servers.get(workspace_id)

    async def stop_all(self) -> None:
        for ws_id in list(self._servers):
            await self.stop(ws_id)
