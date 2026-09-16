from __future__ import annotations

import subprocess
from dataclasses import dataclass
from pathlib import Path

import yaml
from pydantic import ValidationError

from .digests import canonical_digest, dataset_digest_payload, file_digest, tree_digest
from .models import CaseManifest, DatasetManifest


class DatasetValidationError(ValueError):
    pass


@dataclass(frozen=True)
class LoadedDataset:
    root: Path
    manifest: DatasetManifest
    cases: dict[str, CaseManifest]
    fixture_digests: dict[str, str]
    hidden_asset_digests: dict[str, str]
    grader_set_digest: str
    environment_digest: str
    digest: str


def load_dataset(root: Path, *, environment_digest: str) -> LoadedDataset:
    root = root.resolve(strict=True)
    manifest = _load_model(root / "dataset.yaml", DatasetManifest)
    cases: dict[str, CaseManifest] = {}
    fixtures: dict[str, str] = {}
    hidden_assets: dict[str, str] = {}
    for case_id in manifest.case_ids:
        case = _load_model(root / "cases" / f"{case_id}.yaml", CaseManifest)
        if case.case_id != case_id:
            raise DatasetValidationError(f"case id mismatch for {case_id}")
        fixture = _inside(root, root / case.fixture.source)
        if not fixture.is_file():
            raise DatasetValidationError(f"fixture missing: {case.fixture.source}")
        actual_fixture_digest = file_digest(fixture)
        if actual_fixture_digest != case.fixture.sha256:
            raise DatasetValidationError(f"fixture digest mismatch: {case_id}")
        _validate_bundle(fixture)
        fixtures[case_id] = actual_fixture_digest
        for grader in case.graders + case.baseline:
            if grader.asset_id:
                asset = _inside(root, root / "hidden" / grader.asset_id)
                if not asset.is_dir():
                    raise DatasetValidationError(f"hidden asset missing: {grader.asset_id}")
                hidden_assets[grader.asset_id] = tree_digest(asset)
        cases[case_id] = case
    if set(cases) != set(manifest.case_ids):
        raise DatasetValidationError("dataset case list is incomplete")
    grader_set_digest = canonical_digest(
        {case_id: [grader.model_dump(mode="json") for grader in cases[case_id].graders] for case_id in sorted(cases)}
    )
    payload = dataset_digest_payload(
        manifest,
        cases,
        fixture_digests=fixtures,
        hidden_asset_digests=hidden_assets,
        grader_set_digest=grader_set_digest,
        environment_digest=environment_digest,
    )
    return LoadedDataset(
        root=root,
        manifest=manifest,
        cases=cases,
        fixture_digests=fixtures,
        hidden_asset_digests=hidden_assets,
        grader_set_digest=grader_set_digest,
        environment_digest=environment_digest,
        digest=canonical_digest(payload),
    )


def restore_fixture(dataset: LoadedDataset, case_id: str, destination: Path) -> tuple[Path, str]:
    case = dataset.cases[case_id]
    bundle = _inside(dataset.root, dataset.root / case.fixture.source)
    if destination.exists():
        raise DatasetValidationError("fixture destination already exists")
    subprocess.run(
        ["git", "clone", "--quiet", "--no-checkout", str(bundle), str(destination)],
        capture_output=True,
        check=True,
    )
    subprocess.run(
        ["git", "-C", str(destination), "checkout", "--quiet", case.fixture.base_ref],
        capture_output=True,
        check=True,
    )
    _reject_external_git_dependencies(destination)
    commit = subprocess.run(
        ["git", "-C", str(destination), "rev-parse", "HEAD"],
        capture_output=True,
        check=True,
        text=True,
    ).stdout.strip()
    return destination, commit


def _load_model(path: Path, model_type: type[DatasetManifest] | type[CaseManifest]):
    try:
        raw = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as exc:
        raise DatasetValidationError(f"cannot load {path.name}: {exc}") from exc
    try:
        return model_type.model_validate(raw)
    except ValidationError as exc:
        raise DatasetValidationError(f"invalid {path.name}: {exc}") from exc


def _inside(root: Path, candidate: Path) -> Path:
    resolved = candidate.resolve(strict=False)
    if not resolved.is_relative_to(root):
        raise DatasetValidationError("dataset path escapes its root")
    return resolved


def _validate_bundle(bundle: Path) -> None:
    # `git bundle list-heads` is repository-independent and proves the bundle
    # has a readable header and a self-contained advertised ref.
    completed = subprocess.run(["git", "bundle", "list-heads", str(bundle)], capture_output=True, text=True)
    if completed.returncode != 0 or not completed.stdout.strip():
        raise DatasetValidationError(f"invalid git bundle: {bundle.name}")


def _reject_external_git_dependencies(repository: Path) -> None:
    if (repository / ".gitmodules").exists():
        raise DatasetValidationError("fixtures with submodules are not self-contained")
    attributes = repository / ".gitattributes"
    if attributes.exists() and "filter=lfs" in attributes.read_text(encoding="utf-8", errors="replace"):
        raise DatasetValidationError("fixtures using Git LFS are not self-contained")
