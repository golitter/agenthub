from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path

PROFILE_ROOTS: dict[str, tuple[str, ...]] = {
    "local": ("backend", "backend/configs", "agentend"),
    "docker": ("docker/configs/backend", "agentend"),
}
SUPPORTED_KINDS = {".env": "env", ".yaml": "yaml", ".yml": "yaml", ".json": "json"}

@dataclass(frozen=True)
class TemplateFileSpec:
    id: str
    example_path: str
    path: str
    title: str
    section: str
    profile: str
    kind: str

    def resolve_example(self, project_root: Path) -> Path:
        return _safe_resolve(project_root, self.example_path)

    def resolve(self, project_root: Path) -> Path:
        return _safe_resolve(project_root, self.path)

    def public(self) -> dict[str, object]:
        return {
            "id": self.id,
            "path": self.path,
            "examplePath": self.example_path,
            "title": self.title,
            "kind": self.kind,
            "section": self.section,
        }


def _safe_resolve(project_root: Path, relative: str) -> Path:
    root = project_root.resolve()
    resolved = (root / relative).resolve(strict=False)
    if resolved != root and root not in resolved.parents:
        raise ValueError(f"configured path escapes project root: {relative}")
    return resolved


def actual_name_for_example(name: str) -> str | None:
    if name.endswith(".example"):
        return name[: -len(".example")]
    if name.startswith(".example."):
        return "." + name[len(".example.") :]
    if ".example." in name:
        return name.replace(".example.", ".", 1)
    return None


def _file_id(relative: Path) -> str:
    return re.sub(r"[^a-z0-9]+", "_", relative.as_posix().lower()).strip("_")


def _kind_for(path: Path) -> str | None:
    if path.name == ".env":
        return "env"
    return SUPPORTED_KINDS.get(path.suffix.lower())


def discover_template_files(project_root: Path, profile: str) -> tuple[TemplateFileSpec, ...]:
    roots = PROFILE_ROOTS.get(profile)
    if roots is None:
        raise ValueError(f"unknown profile: {profile}")
    project_root = project_root.resolve()
    discovered: list[TemplateFileSpec] = []
    seen_actual: set[str] = set()
    for relative_root in roots:
        root = _safe_resolve(project_root, relative_root)
        if not root.is_dir() or root.is_symlink():
            continue
        for example in sorted(root.glob("*example*")):
            if not example.is_file() or example.is_symlink():
                continue
            actual_name = actual_name_for_example(example.name)
            if not actual_name:
                continue
            actual = example.with_name(actual_name)
            kind = _kind_for(actual)
            if kind is None:
                continue
            relative_example = example.relative_to(project_root)
            relative_actual = actual.relative_to(project_root)
            actual_key = relative_actual.as_posix()
            if actual_key in seen_actual:
                continue
            seen_actual.add(actual_key)
            discovered.append(
                TemplateFileSpec(
                    id=_file_id(relative_actual),
                    example_path=relative_example.as_posix(),
                    path=actual_key,
                    title=relative_actual.as_posix(),
                    section=relative_actual.parent.name or profile,
                    profile=profile,
                    kind=kind,
                )
            )
    return tuple(discovered)


def file_for_profile(project_root: Path, profile: str, file_id: str) -> TemplateFileSpec:
    for spec in discover_template_files(project_root, profile):
        if spec.id == file_id:
            return spec
    raise ValueError(f"file is not available in {profile}: {file_id}")
