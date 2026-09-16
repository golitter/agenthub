from __future__ import annotations

import argparse
import json

from src.app.config import settings

from .readiness import BatchEvalBlocked, current_batch_eval_readiness


def _readiness_payload() -> dict[str, object]:
    readiness = current_batch_eval_readiness(
        sandbox_mode=settings.execution.sandbox.mode,
        sandbox_backend=settings.execution.sandbox.backend,
    )
    return {
        "ready": readiness.ready,
        "sandbox_mode": readiness.sandbox_mode,
        "sandbox_backend": readiness.sandbox_backend,
        "missing_capabilities": readiness.missing_capabilities,
        "capabilities": dict(readiness.capabilities),
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m evals.cli")
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("readiness", help="show whether unattended batch evaluation is safe")
    subparsers.add_parser("experiment", help="start a batch experiment after strict readiness")
    args = parser.parse_args(argv)

    payload = _readiness_payload()
    print(json.dumps(payload, ensure_ascii=False, sort_keys=True))
    if args.command == "readiness":
        return 0 if payload["ready"] else 2

    readiness = current_batch_eval_readiness(
        sandbox_mode=settings.execution.sandbox.mode,
        sandbox_backend=settings.execution.sandbox.backend,
    )
    try:
        readiness.require()
    except BatchEvalBlocked as exc:
        parser.error(str(exc))
    parser.error("batch coordinator is not available until Phase 2")


if __name__ == "__main__":
    raise SystemExit(main())
