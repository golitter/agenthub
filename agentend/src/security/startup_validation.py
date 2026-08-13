from __future__ import annotations

import ipaddress
import os
import shutil
from pathlib import Path


def is_loopback_host(host: str) -> bool:
    if host.strip().lower() == "localhost":
        return True
    try:
        return ipaddress.ip_address(host.strip()).is_loopback
    except ValueError:
        return False


def sandbox_capabilities() -> dict[str, bool]:
    cgroup_root = Path("/sys/fs/cgroup")
    userns_enabled = True
    userns_sysctl = Path("/proc/sys/kernel/unprivileged_userns_clone")
    if userns_sysctl.exists():
        try:
            userns_enabled = userns_sysctl.read_text().strip() == "1"
        except OSError:
            userns_enabled = False
    return {
        "platform_linux": os.name == "posix" and Path("/proc").is_dir(),
        "bwrap_available": shutil.which("bwrap") is not None,
        "unprivileged_userns": userns_enabled,
        "cgroup_v2": (cgroup_root / "cgroup.controllers").is_file(),
        # These are not inferred from package presence. They become true only
        # after concrete components are configured and actively probed.
        "controlled_egress": False,
        "credential_broker": False,
        "git_metadata_isolation": False,
        "disk_quota": False,
    }


def strict_sandbox_enforced(capabilities: dict[str, bool]) -> bool:
    return bool(capabilities) and all(capabilities.values())
