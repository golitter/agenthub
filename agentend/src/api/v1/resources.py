import os
import platform
import shutil
import subprocess
from dataclasses import dataclass

from fastapi import APIRouter

router = APIRouter(prefix="/v1/resources", tags=["resources"])


@dataclass
class ResourceInfo:
    used: float
    total: float
    unit: str


def _get_disk_usage() -> ResourceInfo:
    usage = shutil.disk_usage("/")
    return ResourceInfo(
        used=usage.used / 1e9,
        total=usage.total / 1e9,
        unit="GB",
    )


def _parse_vm_stat_pages(output: str, prefix: str) -> int:
    for line in output.splitlines():
        if line.startswith(prefix):
            _, _, val = line.partition(":")
            val = val.strip().replace(".", "")
            try:
                return int(val)
            except ValueError:
                return 0
    return 0


def _parse_meminfo_kb(info: dict[str, str], key: str) -> float:
    try:
        return float(info.get(key, "0").split()[0])
    except (ValueError, IndexError):
        return 0


def _linux_used_memory_kb(info: dict[str, str]) -> tuple[float, float]:
    total_kb = _parse_meminfo_kb(info, "MemTotal")
    free_kb = _parse_meminfo_kb(info, "MemFree")
    buffers_kb = _parse_meminfo_kb(info, "Buffers")
    cached_kb = _parse_meminfo_kb(info, "Cached")
    reclaimable_kb = _parse_meminfo_kb(info, "SReclaimable")
    shmem_kb = _parse_meminfo_kb(info, "Shmem")
    used_kb = total_kb - free_kb - buffers_kb - cached_kb - reclaimable_kb + shmem_kb
    return max(used_kb, 0), total_kb


def _parse_free_bytes(output: str) -> tuple[float, float]:
    for line in output.splitlines():
        fields = line.split()
        if len(fields) >= 3 and fields[0] == "Mem:":
            return float(fields[2]), float(fields[1])
    raise ValueError("free output missing Mem row")


def _get_linux_memory_usage() -> ResourceInfo:
    gib = 1024.0**3
    try:
        env = {**os.environ, "LC_ALL": "C"}
        out = subprocess.check_output(["free", "-b"], text=True, env=env)
        used_bytes, total_bytes = _parse_free_bytes(out)
        return ResourceInfo(used=used_bytes / gib, total=total_bytes / gib, unit="GiB")
    except (subprocess.CalledProcessError, FileNotFoundError, ValueError):
        pass

    try:
        with open("/proc/meminfo") as f:
            info = {}
            for line in f:
                key, _, val = line.partition(":")
                info[key.strip()] = val.strip()
        used_kb, total_kb = _linux_used_memory_kb(info)
        return ResourceInfo(used=used_kb / (1024.0**2), total=total_kb / (1024.0**2), unit="GiB")
    except (ValueError, OSError):
        return ResourceInfo(used=0, total=0, unit="GiB")


def _get_memory_usage() -> ResourceInfo:
    system = platform.system()

    if system == "Darwin":
        try:
            out = subprocess.check_output(["sysctl", "-n", "hw.memsize"], text=True)
            total_bytes = float(out.strip())
        except (subprocess.CalledProcessError, ValueError):
            return ResourceInfo(used=0, total=0, unit="GB")

        total_gb = total_bytes / 1e9

        try:
            vm_out = subprocess.check_output("vm_stat", text=True)
        except subprocess.CalledProcessError:
            return ResourceInfo(used=0, total=total_gb, unit="GB")

        free_pages = _parse_vm_stat_pages(vm_out, "Pages free")
        inactive_pages = _parse_vm_stat_pages(vm_out, "Pages inactive")
        page_size = 4096.0
        free_gb = (free_pages + inactive_pages) * page_size / 1e9
        used_gb = total_gb - free_gb

        return ResourceInfo(used=used_gb, total=total_gb, unit="GB")

    if system == "Linux":
        return _get_linux_memory_usage()

    return ResourceInfo(used=0, total=0, unit="GB")


@router.get("")
async def get_resources():
    disk = _get_disk_usage()
    memory = _get_memory_usage()
    return {
        "disk": {"used": disk.used, "total": disk.total, "unit": disk.unit},
        "memory": {"used": memory.used, "total": memory.total, "unit": memory.unit},
    }
