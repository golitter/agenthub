"""Offline-first evaluation harness for coding agents."""

from .digests import canonical_digest, file_digest, tree_digest
from .models import (
    CaseManifest,
    DatasetManifest,
    ExperimentSnapshot,
    GraderResult,
    TrialRecord,
    trial_id_for,
)

__all__ = [
    "CaseManifest",
    "DatasetManifest",
    "ExperimentSnapshot",
    "GraderResult",
    "TrialRecord",
    "canonical_digest",
    "file_digest",
    "tree_digest",
    "trial_id_for",
]
