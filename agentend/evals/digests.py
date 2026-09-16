from __future__ import annotations

import hashlib
import json
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any

from pydantic import BaseModel


def _json_value(value: Any) -> Any:
    if isinstance(value, BaseModel):
        return value.model_dump(mode="json", exclude_none=True)
    if isinstance(value, Mapping):
        return {str(key): _json_value(item) for key, item in value.items()}
    if isinstance(value, Sequence) and not isinstance(value, (str, bytes, bytearray)):
        return [_json_value(item) for item in value]
    if isinstance(value, Path):
        return value.as_posix()
    return value


def canonical_json(value: Any) -> bytes:
    """Serialize a digest payload independently of YAML whitespace or key order."""

    return json.dumps(
        _json_value(value),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    ).encode("utf-8")


def canonical_digest(value: Any) -> str:
    return f"sha256:{hashlib.sha256(canonical_json(value)).hexdigest()}"


def file_digest(path: Path, *, chunk_size: int = 1024 * 1024) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(chunk_size):
            digest.update(chunk)
    return f"sha256:{digest.hexdigest()}"


def tree_digest(root: Path) -> str:
    """Hash regular files by canonical relative path and content.

    Symlinks and special files are rejected so a hidden-asset digest cannot be
    redirected outside its declared root between validation and grading.
    """

    root = root.resolve(strict=True)
    entries: list[dict[str, str]] = []
    for path in sorted(root.rglob("*"), key=lambda item: item.relative_to(root).as_posix()):
        relative = path.relative_to(root).as_posix()
        if path.is_symlink():
            raise ValueError(f"asset tree contains a symlink: {relative}")
        if path.is_dir():
            continue
        if not path.is_file():
            raise ValueError(f"asset tree contains a special file: {relative}")
        entries.append({"path": relative, "digest": file_digest(path)})
    return canonical_digest(entries)


def dataset_digest_payload(
    dataset: BaseModel,
    cases: Mapping[str, BaseModel],
    *,
    fixture_digests: Mapping[str, str],
    hidden_asset_digests: Mapping[str, str],
    grader_set_digest: str,
    environment_digest: str,
) -> dict[str, Any]:
    """Build the complete, immutable Dataset digest input."""

    return {
        "dataset": dataset,
        "cases": {case_id: cases[case_id] for case_id in sorted(cases)},
        "fixtures": dict(sorted(fixture_digests.items())),
        "hidden_assets": dict(sorted(hidden_asset_digests.items())),
        "grader_set_digest": grader_set_digest,
        "environment_digest": environment_digest,
    }
