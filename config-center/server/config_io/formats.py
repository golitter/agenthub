from __future__ import annotations

import json
from typing import Any

from ruamel.yaml import YAML


def validate_content(text: str, kind: str) -> Any:
    """Parse structured formats for syntax validation; dotenv stays verbatim."""
    if kind == "env":
        return None
    if kind == "json":
        return json.loads(text)
    if kind == "yaml":
        return YAML(typ="safe").load(text)
    raise ValueError(f"unsupported configuration format: {kind}")
