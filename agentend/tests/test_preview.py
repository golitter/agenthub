import asyncio
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.preview.server import PreviewManager, PreviewServer


@pytest.mark.asyncio
async def test_preview_manager_concurrent_start_reuses_one_server(tmp_path: Path, monkeypatch):
    (tmp_path / "index.html").write_text("<main>preview</main>")
    manager = PreviewManager()
    start_count = 0

    async def fake_start(server: PreviewServer) -> None:
        nonlocal start_count
        start_count += 1
        await asyncio.sleep(0)
        server._port = 3928

    async def fake_stop(_server: PreviewServer) -> None:
        return None

    monkeypatch.setattr(PreviewServer, "start", fake_start)
    monkeypatch.setattr(PreviewServer, "stop", fake_stop)

    first, second = await asyncio.gather(
        manager.start("workspace-1", str(tmp_path)),
        manager.start("workspace-1", str(tmp_path)),
    )
    try:
        assert first is second
        assert start_count == 1
        assert first.port > 0
        assert first.url.startswith("http://localhost:")
    finally:
        await manager.stop_all()
