from __future__ import annotations

import os
import shutil
import subprocess
import time
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol

from .digests import canonical_digest


@dataclass(frozen=True)
class CommandResult:
    argv: tuple[str, ...]
    exit_code: int | None
    stdout: str
    stderr: str
    duration_ms: int
    timed_out: bool = False
    truncated: bool = False

    @property
    def evidence_digest(self) -> str:
        return canonical_digest(
            {
                "argv": self.argv,
                "exit_code": self.exit_code,
                "stdout": self.stdout,
                "stderr": self.stderr,
                "timed_out": self.timed_out,
                "truncated": self.truncated,
            }
        )


class CommandExecutor(Protocol):
    def run(
        self,
        argv: Sequence[str],
        *,
        workspace: Path,
        hidden_assets: Path | None = None,
        cwd: str | None = None,
        env: Mapping[str, str] | None = None,
        timeout_seconds: int = 180,
    ) -> CommandResult: ...


class GraderSandboxUnavailable(RuntimeError):
    pass


class BubblewrapGraderSandbox:
    """Execute hostile repository code without network, credentials, or host writes."""

    def __init__(self, *, output_limit_bytes: int = 256 * 1024) -> None:
        self.bwrap = shutil.which("bwrap")
        self.output_limit_bytes = output_limit_bytes

    def readiness(self) -> dict[str, bool]:
        return {
            "bwrap_available": self.bwrap is not None,
            "linux_proc": Path("/proc").is_dir(),
            "user_namespace": _unprivileged_userns_enabled(),
        }

    def require_ready(self) -> None:
        missing = [key for key, value in self.readiness().items() if not value]
        if missing:
            raise GraderSandboxUnavailable("grader sandbox unavailable: " + ", ".join(missing))

    def run(
        self,
        argv: Sequence[str],
        *,
        workspace: Path,
        hidden_assets: Path | None = None,
        cwd: str | None = None,
        env: Mapping[str, str] | None = None,
        timeout_seconds: int = 180,
    ) -> CommandResult:
        self.require_ready()
        if not argv or any("\x00" in value for value in argv):
            raise ValueError("argv must be a non-empty NUL-free argument array")
        workspace = workspace.resolve(strict=True)
        command = [
            str(self.bwrap),
            "--die-with-parent",
            "--new-session",
            "--unshare-all",
            "--unshare-net",
            "--proc",
            "/proc",
            "--dev",
            "/dev",
            "--tmpfs",
            "/tmp",
            "--dir",
            "/run",
            "--ro-bind",
            "/usr",
            "/usr",
            "--ro-bind",
            "/bin",
            "/bin",
            "--ro-bind",
            "/lib",
            "/lib",
        ]
        if Path("/lib64").exists():
            command += ["--ro-bind", "/lib64", "/lib64"]
        if Path("/etc/alternatives").exists():
            command += ["--ro-bind", "/etc/alternatives", "/etc/alternatives"]
        command += ["--bind", str(workspace), "/workspace"]
        if hidden_assets is not None:
            command += ["--ro-bind", str(hidden_assets.resolve(strict=True)), "/eval-hidden"]
        clean_env = {
            "HOME": "/tmp/home",
            "PATH": "/usr/local/bin:/usr/bin:/bin",
            "LANG": "C.UTF-8",
            "LC_ALL": "C.UTF-8",
            "NO_PROXY": "*",
        }
        for key, value in (env or {}).items():
            if not key.replace("_", "").isalnum() or any(
                sensitive in key.upper() for sensitive in ("TOKEN", "SECRET", "PASSWORD", "KEY", "CREDENTIAL")
            ):
                raise ValueError(f"unsafe grader environment key: {key}")
            clean_env[key] = value
        for key, value in clean_env.items():
            command += ["--setenv", key, value]
        working_directory = "/workspace" if not cwd else f"/workspace/{cwd.strip('/')}"
        command += ["--chdir", working_directory, "--", *argv]
        started = time.monotonic()
        try:
            completed = subprocess.run(
                command,
                capture_output=True,
                check=False,
                timeout=timeout_seconds,
                env={"PATH": os.environ.get("PATH", "/usr/bin:/bin")},
            )
            stdout, stdout_truncated = _bounded_text(completed.stdout, self.output_limit_bytes)
            stderr, stderr_truncated = _bounded_text(completed.stderr, self.output_limit_bytes)
            return CommandResult(
                argv=tuple(argv),
                exit_code=completed.returncode,
                stdout=stdout,
                stderr=stderr,
                duration_ms=int((time.monotonic() - started) * 1000),
                truncated=stdout_truncated or stderr_truncated,
            )
        except subprocess.TimeoutExpired as exc:
            stdout, stdout_truncated = _bounded_text(exc.stdout or b"", self.output_limit_bytes)
            stderr, stderr_truncated = _bounded_text(exc.stderr or b"", self.output_limit_bytes)
            return CommandResult(
                argv=tuple(argv),
                exit_code=None,
                stdout=stdout,
                stderr=stderr,
                duration_ms=int((time.monotonic() - started) * 1000),
                timed_out=True,
                truncated=stdout_truncated or stderr_truncated,
            )


def _bounded_text(value: bytes, limit: int) -> tuple[str, bool]:
    truncated = len(value) > limit
    return value[:limit].decode("utf-8", errors="replace"), truncated


def _unprivileged_userns_enabled() -> bool:
    path = Path("/proc/sys/kernel/unprivileged_userns_clone")
    try:
        return not path.exists() or path.read_text(encoding="utf-8").strip() == "1"
    except OSError:
        return False
