import asyncio
import os
from pathlib import Path
import sys

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.adapters.base import terminate_process_group


@pytest.mark.asyncio
@pytest.mark.skipif(os.name != "posix", reason="process groups require POSIX")
async def test_terminate_process_group_kills_background_child():
    process = await asyncio.create_subprocess_exec(
        "/bin/sh",
        "-c",
        "sleep 30 & wait",
        start_new_session=True,
    )
    await terminate_process_group(process, 0.2)
    assert process.returncode is not None


@pytest.mark.asyncio
@pytest.mark.skipif(os.name != "posix", reason="process groups require POSIX")
async def test_terminate_process_group_cleans_child_after_parent_already_exited():
    process = await asyncio.create_subprocess_exec(
        "/bin/sh",
        "-c",
        "sleep 30 >/dev/null 2>&1 & echo $!",
        stdout=asyncio.subprocess.PIPE,
        start_new_session=True,
    )
    assert process.stdout is not None
    child_pid = int((await process.stdout.readline()).decode().strip())
    await process.wait()

    await terminate_process_group(process, 0.2)

    for _ in range(20):
        stat = Path(f"/proc/{child_pid}/stat")
        if not stat.exists() or stat.read_text().split()[2] == "Z":
            break
        await asyncio.sleep(0.05)
    else:
        pytest.fail("background child survived process-group cleanup")
