from __future__ import annotations

import os
from pathlib import Path


class PathPolicyError(ValueError):
    pass


class PathPolicy:
    def __init__(self, allowed_repo_roots: list[str]) -> None:
        roots: list[Path] = []
        home = Path.home().resolve()
        for raw in allowed_repo_roots:
            if not raw or "\x00" in raw or not Path(raw).is_absolute():
                raise PathPolicyError("allowed repo roots must be absolute paths")
            root = Path(raw).resolve(strict=True)
            if root in {Path("/"), home} or root in home.parents:
                raise PathPolicyError(f"allowed repo root is too broad: {root}")
            if not root.is_dir():
                raise PathPolicyError(f"allowed repo root is not a directory: {root}")
            roots.append(root)
        self._roots = tuple(roots)

    @property
    def configured(self) -> bool:
        return bool(self._roots)

    def resolve_repo(self, raw: str, *, must_exist: bool = True) -> Path:
        if not raw or len(raw) > 4096 or "\x00" in raw:
            raise PathPolicyError("invalid repository path")
        candidate = Path(raw)
        if not candidate.is_absolute():
            raise PathPolicyError("repository path must be absolute")
        try:
            resolved = candidate.resolve(strict=must_exist)
        except OSError as exc:
            raise PathPolicyError("repository path cannot be resolved") from exc
        if not any(resolved == root or root in resolved.parents for root in self._roots):
            raise PathPolicyError("repository path is outside configured roots")
        if must_exist and not resolved.is_dir():
            raise PathPolicyError("repository path is not a directory")
        return resolved

    def validate_managed_path(self, raw: str, expected: str) -> Path:
        resolved = self.resolve_repo(raw)
        if expected == "git_repo":
            git_entry = resolved / ".git"
            if not git_entry.exists():
                raise PathPolicyError("path is not a git repository")
        return resolved

    @staticmethod
    def safe_open_parent(path: Path, boundary: Path) -> None:
        """Best-effort final-parent validation before a managed file write."""
        parent = path.parent.resolve(strict=True)
        boundary = boundary.resolve(strict=True)
        if parent != boundary and boundary not in parent.parents:
            raise PathPolicyError("file parent escaped managed boundary")
        if os.path.islink(path):
            raise PathPolicyError("managed file target must not be a symlink")
