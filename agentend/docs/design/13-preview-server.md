# Preview Server — 工作区预览服务

## 实现了什么

基于 aiohttp 的轻量静态文件服务器，为每个 workspace 启动独立的预览 HTTP 服务。`PreviewManager` 管理多实例的生命周期，每个 workspace 对应一个独立端口。

## 怎么实现的

### PreviewServer (`src/preview/server.py`)

单实例静态文件服务器，绑定 worktree 目录为 document root：

```python
class PreviewServer:
    def __init__(self, worktree_path: str, port: int | None = None):
        self._worktree_path = Path(worktree_path).resolve()
        self._port = port if port is not None else 0
```

端口分配发生在 `start()`：未指定 port 时以 0 绑定，由内核分配空闲端口后回读（`listener.getsockname()[1]`），监听经 `web.SockSite` 挂到该 socket；绑定 `127.0.0.1` 仅本机访问。

```python
async def start(self) -> None:
    listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    listener.bind(("127.0.0.1", self._port))
    listener.listen(128)
    listener.setblocking(False)
    self._port = int(listener.getsockname()[1])
    self._site = web.SockSite(self._runner, listener)
    await self._site.start()
```

请求处理：所有路径 `/{path:.*}` 映射到 worktree 目录下的文件，默认回退到 `index.html`：

```python
async def _handle(self, request: web.Request) -> web.Response:
    rel_path = request.match_info.get("path", "index.html") or "index.html"
    target = (self._worktree_path / rel_path).resolve()
    # 防路径穿越
    if not str(target).startswith(str(self._worktree_path) + "/") and target != self._worktree_path:
        return web.Response(status=403, text="Forbidden")
    if not target.is_file():
        return web.Response(status=404, text="Not Found")
    return web.FileResponse(target)
```

### PreviewManager (`src/preview/server.py`)

管理 `dict[str, PreviewServer]` 映射，每个 workspace_id 最多一个实例：

```python
class PreviewManager:
    async def start(self, workspace_id: str, worktree_path: str, port: int | None = None) -> PreviewServer
    async def stop(self, workspace_id: str) -> None
    def get(self, workspace_id: str) -> PreviewServer | None
    async def stop_all(self) -> None
```

- `start()` 幂等：已存在时直接返回已有实例
- `stop()` 从映射中移除并清理 runner
- `stop_all()` 关闭所有实例（用于 shutdown）

### 与 Workspace API 集成

通过 `src/api/v1/workspace.py` 暴露端点：

```python
@router.post("/{workspace_id}/preview/start")
async def start_preview(workspace_id, mgr, preview_mgr):
    ws = mgr.get(workspace_id)
    srv = await preview_mgr.start(workspace_id, ws.worktree_path)
    return {"url": srv.url, "port": srv.port}

@router.post("/{workspace_id}/preview/stop")
async def stop_preview(workspace_id, preview_mgr):
    await preview_mgr.stop(workspace_id)
    return {"success": True}
```

Delete workspace 时也会自动停止预览服务。

### 依赖注入

`PreviewManager` 通过 `src/api/dependencies.py` 注入到 workspace 路由：

```python
def get_preview_manager(request) -> PreviewManager:
    return request.app.state.preview_manager
```
