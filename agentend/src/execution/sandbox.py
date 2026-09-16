from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path
from typing import Mapping, Sequence

from src.app.config import SandboxConfig


class ExecutionSandboxUnavailable(RuntimeError):
    pass


def strict_sandbox_capabilities(config: SandboxConfig) -> dict[str, bool]:
    credential_dir = Path(config.credential_broker_dir).resolve(strict=False) if config.credential_broker_dir else None
    return {
        "platform_linux": os.name == "posix" and Path("/proc").is_dir(),
        "bwrap_available": shutil.which("bwrap") is not None,
        "bwrap_enforced": _probe_bwrap(),
        "unprivileged_userns": _userns_enabled(),
        "cgroup_v2": Path("/sys/fs/cgroup/cgroup.controllers").is_file(),
        "controlled_egress": config.network_mode == "none" or _managed_network_ready(config),
        "credential_broker": bool(credential_dir and credential_dir.is_dir()),
        "git_metadata_isolation": True,
        "resource_limits": shutil.which("prlimit") is not None,
        "disk_quota": shutil.which("prlimit") is not None and config.file_size_limit_bytes > 0,
    }


def prepare_agent_subprocess(
    argv: Sequence[str],
    *,
    cwd: str | None,
    env: Mapping[str, str],
    config: SandboxConfig,
) -> tuple[list[str], str | None, dict[str, str]]:
    if config.mode == "unsafe_process":
        return list(argv), cwd, dict(env)
    capabilities = strict_sandbox_capabilities(config)
    missing = sorted(name for name, ready in capabilities.items() if not ready)
    if config.backend != "bubblewrap" or missing:
        raise ExecutionSandboxUnavailable(
            "strict execution sandbox is not enforced: " + ", ".join(missing or ["bubblewrap backend"])
        )
    if not cwd:
        raise ExecutionSandboxUnavailable("strict execution requires an isolated workspace")
    workspace = Path(cwd).resolve(strict=True)
    git_entry = workspace / ".git"
    if not git_entry.is_dir():
        raise ExecutionSandboxUnavailable("strict eval workspace must own its Git metadata")
    bwrap = shutil.which("bwrap")
    prlimit = shutil.which("prlimit")
    command = [
        str(bwrap),
        "--die-with-parent",
        "--new-session",
        "--unshare-all",
        "--share-net" if config.network_mode == "managed_namespace" else "--unshare-net",
        "--proc",
        "/proc",
        "--dev",
        "/dev",
        "--tmpfs",
        "/tmp",
        "--tmpfs",
        "/home",
        "--dir",
        "/run",
    ]
    for system_path in ("/usr", "/bin", "/lib", "/lib64", "/etc/ssl", "/etc/alternatives"):
        path = Path(system_path)
        if path.exists():
            command += ["--ro-bind", system_path, system_path]
    executable = Path(argv[0]).resolve(strict=True)
    runtime_paths = {
        executable.parent.parent,
        *(Path(value).resolve(strict=True) for value in config.runtime_readonly_paths),
    }
    for runtime_path in sorted(runtime_paths, key=str):
        if not any(runtime_path.is_relative_to(Path(root)) for root in ("/usr", "/bin", "/lib", "/lib64")):
            command += ["--ro-bind", str(runtime_path), str(runtime_path)]
    credential_dir = Path(config.credential_broker_dir).resolve(strict=True)
    command += [
        "--ro-bind",
        str(credential_dir),
        "/run/agenthub-credentials",
        "--bind",
        str(workspace),
        "/workspace",
        "--chdir",
        "/workspace",
        "--setenv",
        "HOME",
        "/tmp/home",
        "--setenv",
        "AGENTHUB_CREDENTIAL_DIR",
        "/run/agenthub-credentials",
        "--setenv",
        "PATH",
        str(env.get("PATH", "/usr/local/bin:/usr/bin:/bin")),
        "--",
        str(prlimit),
        f"--as={config.memory_limit_bytes}",
        f"--fsize={config.file_size_limit_bytes}",
        f"--nproc={config.process_limit}",
        f"--cpu={config.cpu_time_seconds}",
        "--",
        *argv,
    ]
    safe_env = {
        key: value
        for key, value in env.items()
        if key in {"PATH", "LANG", "LC_ALL", "TERM"} or key.startswith("AGENTHUB_")
    }
    if config.network_mode == "managed_namespace":
        command = [
            str(shutil.which("nsenter")),
            f"--net={Path(config.network_namespace_path).resolve(strict=True)}",
            "--",
            *command,
        ]
    return command, None, safe_env


def _probe_bwrap() -> bool:
    bwrap = shutil.which("bwrap")
    if not bwrap:
        return False
    try:
        completed = subprocess.run(
            [
                bwrap,
                "--die-with-parent",
                "--unshare-all",
                "--unshare-net",
                "--ro-bind",
                "/usr",
                "/usr",
                "--proc",
                "/proc",
                "/usr/bin/true",
            ],
            capture_output=True,
            timeout=3,
            check=False,
        )
        return completed.returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def _userns_enabled() -> bool:
    path = Path("/proc/sys/kernel/unprivileged_userns_clone")
    try:
        return not path.exists() or path.read_text(encoding="utf-8").strip() == "1"
    except OSError:
        return False


def _managed_network_ready(config: SandboxConfig) -> bool:
    if config.network_mode != "managed_namespace":
        return False
    namespace = Path(config.network_namespace_path)
    return bool(shutil.which("nsenter") and namespace.is_file())
