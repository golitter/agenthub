from __future__ import annotations

import hashlib
import os
import secrets
import shutil
import tempfile
import threading
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from server.config_io.formats import validate_content
from server.schema import TemplateFileSpec, discover_template_files, file_for_profile


class ConfigError(Exception):
    pass


class ConfigConflict(ConfigError):
    pass


class ValidationError(ConfigError):
    def __init__(self, issues: list[dict[str, Any]]):
        super().__init__("configuration validation failed")
        self.issues = issues


@dataclass
class PreparedFile:
    spec: TemplateFileSpec
    path: Path
    original: bytes | None
    content: bytes


def revision_of(data: bytes | None) -> str:
    if data is None:
        return "missing"
    return "sha256:" + hashlib.sha256(data).hexdigest()


class ConfigService:
    def __init__(self, project_root: Path, backups_root: Path | None = None):
        self.project_root = project_root.resolve()
        requested_backups = backups_root or self.project_root / "config-center" / ".backups"
        current = requested_backups if requested_backups.is_absolute() else self.project_root / requested_backups
        probe = Path(current.anchor)
        for part in current.parts[1:]:
            probe = probe / part
            if probe.exists() and probe.is_symlink():
                raise ConfigError("backup directory cannot contain symbolic links")
        self.backups_root = current.resolve(strict=False)
        if self.backups_root != self.project_root and self.project_root not in self.backups_root.parents:
            raise ConfigError("backup directory must stay inside the project root")
        self._write_lock = threading.Lock()

    def file_specs(self, profile: str) -> tuple[TemplateFileSpec, ...]:
        return discover_template_files(self.project_root, profile)

    def _path(self, spec: TemplateFileSpec) -> Path:
        current = self.project_root
        for part in Path(spec.path).parts:
            current = current / part
            if current.exists() and current.is_symlink():
                raise ConfigError(f"symbolic links are not allowed: {spec.path}")
        return spec.resolve(self.project_root)

    def _example_path(self, spec: TemplateFileSpec) -> Path:
        path = spec.resolve_example(self.project_root)
        if path.is_symlink() or not path.is_file():
            raise ConfigError(f"example file is unavailable: {spec.example_path}")
        return path

    def _read_original(self, spec: TemplateFileSpec) -> bytes | None:
        path = self._path(spec)
        return path.read_bytes() if path.exists() else None

    @staticmethod
    def _issue(severity: str, code: str, message: str, file_id: str = "") -> dict[str, Any]:
        return {"severity": severity, "code": code, "message": message, "fileId": file_id}

    def get_config(self, profile: str) -> dict[str, Any]:
        files: list[dict[str, Any]] = []
        for spec in self.file_specs(profile):
            original = self._read_original(spec)
            try:
                example_content = self._example_path(spec).read_text(encoding="utf-8")
                actual_content = original.decode("utf-8") if original is not None else ""
            except UnicodeDecodeError as exc:
                raise ConfigError(f"configuration file is not UTF-8: {spec.path}") from exc
            files.append(
                {
                    **spec.public(),
                    "exists": original is not None,
                    "revision": revision_of(original),
                    "exampleContent": example_content,
                    "actualContent": actual_content,
                    "sameContent": original is not None and actual_content == example_content,
                }
            )
        return {"profile": profile, "files": files}

    def _prepare(self, profile: str, payload: dict[str, Any]) -> tuple[list[PreparedFile], list[dict[str, Any]]]:
        requested = payload.get("files", {})
        if not isinstance(requested, dict):
            raise ConfigError("files must be an object")
        by_id = {spec.id: spec for spec in self.file_specs(profile)}
        issues: list[dict[str, Any]] = []
        prepared: list[PreparedFile] = []
        for file_id in requested:
            if file_id not in by_id:
                issues.append(self._issue("error", "unknown_file", "文件不在当前 profile 的 example 白名单中", str(file_id)))
        for file_id, request in requested.items():
            spec = by_id.get(file_id)
            if spec is None:
                continue
            if not isinstance(request, dict):
                issues.append(self._issue("error", "invalid_request", "文件变更必须是对象", file_id))
                continue
            original = self._read_original(spec)
            if request.get("revision") != revision_of(original):
                raise ConfigConflict(f"revision changed for {file_id}")
            content = request.get("content")
            if not isinstance(content, str):
                issues.append(self._issue("error", "invalid_content", "actual 文件内容必须是字符串", file_id))
                continue
            try:
                validate_content(content, spec.kind)
            except Exception as exc:
                issues.append(self._issue("error", "invalid_syntax", f"{spec.kind.upper()} 格式错误：{exc}", file_id))
                continue
            encoded = content.encode("utf-8")
            if original != encoded:
                prepared.append(PreparedFile(spec, self._path(spec), original, encoded))
        return prepared, issues

    def validate(self, profile: str, payload: dict[str, Any]) -> dict[str, Any]:
        prepared, issues = self._prepare(profile, payload)
        return {
            "ok": not any(issue["severity"] == "error" for issue in issues),
            "issues": issues,
            "changes": [{"fileId": item.spec.id, "path": item.spec.path, "changes": []} for item in prepared],
        }

    def save(self, profile: str, payload: dict[str, Any]) -> dict[str, Any]:
        if not self._write_lock.acquire(blocking=False):
            raise ConfigConflict("another write transaction is running")
        try:
            prepared, issues = self._prepare(profile, payload)
            if any(issue["severity"] == "error" for issue in issues):
                raise ValidationError(issues)
            backups: list[str] = []
            committed: list[PreparedFile] = []
            try:
                for item in prepared:
                    self._assert_unchanged(item.spec, item.original, "before backup")
                    if item.original is not None:
                        backups.append(self._backup(item.spec, item.path))
                for item in prepared:
                    self._assert_unchanged(item.spec, item.original, "before commit")
                    try:
                        self._atomic_write(item.path, item.content)
                    except Exception:
                        if self._read_original(item.spec) == item.content:
                            committed.append(item)
                        raise
                    else:
                        committed.append(item)
                for item in prepared:
                    if self._read_original(item.spec) != item.content:
                        raise ConfigConflict(f"file changed during verification for {item.spec.id}")
            except Exception:
                self._rollback_files(committed)
                raise
            return {
                "saved": [item.spec.id for item in prepared],
                "backups": backups,
                "warnings": [],
                "ignored": [],
                "revisions": {item.spec.id: revision_of(item.content) for item in prepared},
            }
        finally:
            self._write_lock.release()

    def _assert_unchanged(self, spec: TemplateFileSpec, expected: bytes | None, phase: str) -> None:
        if self._read_original(spec) != expected:
            raise ConfigConflict(f"file changed {phase} for {spec.id}")

    def _rollback_files(self, committed: list[PreparedFile]) -> None:
        failures: list[str] = []
        for item in reversed(committed):
            try:
                if self._read_original(item.spec) != item.content:
                    failures.append(f"{item.spec.id}: current file changed externally")
                    continue
                if item.original is None:
                    item.path.unlink(missing_ok=True)
                else:
                    self._atomic_write(item.path, item.original)
            except Exception as exc:
                failures.append(f"{item.spec.id}: {exc}")
        if failures:
            raise ConfigConflict("rollback incomplete: " + "; ".join(failures))

    def _atomic_write(self, path: Path, content: bytes) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        mode = path.stat().st_mode & 0o777 if path.exists() else 0o600
        descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", suffix=".tmp", dir=path.parent)
        try:
            with os.fdopen(descriptor, "wb") as handle:
                handle.write(content)
                handle.flush()
                os.fsync(handle.fileno())
            os.chmod(temporary, mode)
            os.replace(temporary, path)
            directory_fd = os.open(path.parent, os.O_DIRECTORY)
            try:
                os.fsync(directory_fd)
            finally:
                os.close(directory_fd)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)

    def _backup(self, spec: TemplateFileSpec, source: Path) -> str:
        timestamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S.%fZ")
        destination_dir = self.backups_root / Path(spec.path).parent
        destination_dir.mkdir(parents=True, exist_ok=True)
        destination = destination_dir / f"{source.name}.bak.{timestamp}.{secrets.token_hex(3)}"
        shutil.copy2(source, destination)
        self._prune_backups(spec)
        return destination.relative_to(self.backups_root).as_posix()

    def _backup_candidates(self, spec: TemplateFileSpec) -> list[Path]:
        directory = self.backups_root / Path(spec.path).parent
        if not directory.exists():
            return []
        return sorted(directory.glob(f"{Path(spec.path).name}.bak.*"), key=lambda path: path.stat().st_mtime, reverse=True)

    def _prune_backups(self, spec: TemplateFileSpec) -> None:
        for stale in self._backup_candidates(spec)[10:]:
            stale.unlink(missing_ok=True)

    def list_backups(self, profile: str) -> list[dict[str, Any]]:
        result: list[dict[str, Any]] = []
        for spec in self.file_specs(profile):
            current_revision = revision_of(self._read_original(spec))
            for path in self._backup_candidates(spec):
                stat = path.stat()
                result.append(
                    {
                        "id": path.relative_to(self.backups_root).as_posix(),
                        "fileId": spec.id,
                        "path": spec.path,
                        "size": stat.st_size,
                        "createdAt": datetime.fromtimestamp(stat.st_mtime, timezone.utc).isoformat(),
                        "currentRevision": current_revision,
                    }
                )
        return sorted(result, key=lambda item: item["createdAt"], reverse=True)

    def restore(self, profile: str, file_id: str, backup_id: str, revision: str) -> dict[str, Any]:
        if not self._write_lock.acquire(blocking=False):
            raise ConfigConflict("another write transaction is running")
        try:
            spec = file_for_profile(self.project_root, profile, file_id)
            candidates = {candidate.relative_to(self.backups_root).as_posix(): candidate for candidate in self._backup_candidates(spec)}
            backup = candidates.get(backup_id)
            if backup is None or backup.is_symlink():
                raise ConfigError("backup does not belong to file")
            original = self._read_original(spec)
            if revision_of(original) != revision:
                raise ConfigConflict("file changed before restore")
            content = backup.read_bytes()
            try:
                validate_content(content.decode("utf-8"), spec.kind)
            except Exception as exc:
                raise ValidationError([self._issue("error", "invalid_backup", "备份不是有效的 UTF-8 配置文件", spec.id)]) from exc
            attempted = PreparedFile(spec, self._path(spec), original, content)
            committed: list[PreparedFile] = []
            try:
                self._assert_unchanged(spec, original, "before restore backup")
                previous = self._backup(spec, attempted.path) if original is not None else None
                self._assert_unchanged(spec, original, "before restore commit")
                try:
                    self._atomic_write(attempted.path, content)
                except Exception:
                    if self._read_original(spec) == content:
                        committed.append(attempted)
                    raise
                else:
                    committed.append(attempted)
                if self._read_original(spec) != content:
                    raise ConfigConflict(f"file changed during restore verification for {spec.id}")
            except Exception:
                self._rollback_files(committed)
                raise
            return {"restored": file_id, "revision": revision_of(content), "previousBackup": previous}
        finally:
            self._write_lock.release()
