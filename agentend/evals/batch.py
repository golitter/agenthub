"""Local batch driver: the single supported entry for real-Agent batch runs.

Reuses the official grading chain (EvaluationCoordinator.grade_existing) instead
of duplicating scoring logic. Fail-closed on (a) non-strict sandbox without an
explicit --allow-unsafe acknowledgement and (b) a missing judge API key when the
dataset carries quality-weighted categories.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from dotenv import load_dotenv

from .coordinator import EvaluationCoordinator
from .loader import DatasetValidationError, load_dataset, restore_fixture
from .metrics import aggregate_trials
from .models import ExperimentSnapshot
from .readiness import current_batch_eval_readiness
from .repository import SQLiteEvalRepository
from .sandbox import CommandResult

TERMINAL_STATES = {"completed", "failed", "cancelled"}
FINAL_TEXT_LIMIT = 20_000
TRANSCRIPT_LIMIT = 60_000


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _atomic_json(path: Path, value: object) -> None:
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding="utf-8")
    temporary.replace(path)


class BackendDriver:
    def __init__(self, base_url: str, timeout_buffer: int = 900) -> None:
        self.base_url = base_url.rstrip("/")
        self.timeout_buffer = timeout_buffer

    def request(self, method: str, path: str, payload: dict | None = None, timeout: float = 30) -> dict:
        data = None if payload is None else json.dumps(payload).encode("utf-8")
        req = urllib.request.Request(
            self.base_url + path,
            data=data,
            method=method,
            headers={"Content-Type": "application/json"},
        )
        try:
            with urllib.request.urlopen(req, timeout=timeout) as response:
                parsed = json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            body = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"HTTP {exc.code} {path}: {body[:2000]}") from exc
        if parsed.get("code") != 0:
            raise RuntimeError(f"API error {path}: {parsed}")
        return parsed.get("data") or {}

    def wait_ready(self, deadline_seconds: int = 180) -> None:
        deadline = time.monotonic() + deadline_seconds
        last_error = ""
        while time.monotonic() < deadline:
            try:
                self.request("GET", "/api/tasks?limit=1", timeout=3)
                return
            except Exception as exc:  # noqa: BLE001
                last_error = str(exc)
                time.sleep(2)
        raise RuntimeError(f"backend did not become ready: {last_error}")

    def create_and_run(self, case_id: str, prompt: str, repo: Path, timeout_seconds: int) -> dict:
        task = self.request(
            "POST",
            "/api/tasks",
            {
                "title": f"eval {case_id}",
                "repo_path": str(repo),
                "agents": [
                    {"type": "orchestrator", "name": "Eval Orchestrator"},
                    {"type": "claude-code", "name": "Claude"},
                    {"type": "opencode", "name": "OpenCode"},
                    {"type": "pi", "name": "Pi"},
                ],
            },
        )
        task_id = task["task_id"]
        detail = self.request("GET", f"/api/tasks/{task_id}")
        session = next(item for item in detail["sessions"] if item["agent_type"] == "orchestrator")
        session_id = session["session_id"]
        instruction = (
            "你正在执行一个 AgentHub 评测任务。请检查仓库并完成下面的用户需求。"
            "只可调度 Claude Code、OpenCode、Pi，禁止调度 Codex。"
            "简单任务尽量只派一个实现者；纯对话/问答类任务自己直接回答即可，不要派实现者，也不要修改仓库。"
            "需要复核时再派第二个实现者，避免无关修改。"
            "不得寻找或访问隐藏测试；完成后（编码类）运行仓库中可见的公开检查并提交变更。\n\n"
            f"用户需求：{prompt}"
        )
        run = self.request(
            "POST",
            f"/api/tasks/{task_id}/run",
            {
                "message": instruction,
                "session_id": session_id,
                "agent_type": "orchestrator",
                "cwd": str(repo),
            },
            timeout=60,
        )
        deadline = time.monotonic() + max(1500, timeout_seconds + self.timeout_buffer)
        approvals = 0
        last_state = "queued"
        status: dict = {}
        while time.monotonic() < deadline:
            detail = self.request("GET", f"/api/tasks/{task_id}")
            root_session = next(item for item in detail["sessions"] if item["session_id"] == session_id)
            if root_session["status"] == "awaiting_review":
                try:
                    self.request(
                        "POST",
                        f"/api/tasks/{task_id}/review",
                        {"session_id": session_id, "action": "approve", "content": ""},
                    )
                    approvals += 1
                except RuntimeError as exc:
                    if "not awaiting review" not in str(exc):
                        raise
            try:
                status = self.request("GET", f"/api/tasks/{task_id}/messages/{run['message_id']}/run")
            except RuntimeError as exc:
                # The Backend returns 503 until AgentEnd has persisted the newly
                # accepted Run. This is a normal asynchronous-registration window.
                if "HTTP 503" in str(exc) or "HTTP 404" in str(exc):
                    time.sleep(2)
                    continue
                raise
            last_state = status.get("state", "unknown")
            if last_state in TERMINAL_STATES:
                break
            time.sleep(5)
        else:
            last_state = "failed"
        result = {
            "task_id": task_id,
            "session_id": session_id,
            "run_id": run["run_id"],
            "message_id": run["message_id"],
            "run_state": last_state if last_state in TERMINAL_STATES else "failed",
            "termination_reason": (
                status.get("termination_reason")
                if last_state in TERMINAL_STATES
                else f"batch_driver_timeout:last_state={last_state}"
            ),
            "approvals": approvals,
        }
        result.update(self._assistant_texts(task_id, session_id))
        return result

    def _assistant_texts(self, task_id: str, session_id: str) -> dict[str, str | None]:
        try:
            payload = self.request("GET", f"/api/tasks/{task_id}/messages?session_id={session_id}")
        except RuntimeError:
            return {"final_text": None, "transcript_text": None}
        messages = payload.get("data") or []
        # Backend stores agent-side messages under role="agent".
        assistant = [
            str(item.get("content") or "")
            for item in messages
            if item.get("role") in {"agent", "assistant"} and str(item.get("content") or "").strip()
        ]
        final_text = assistant[-1] if assistant else None
        if final_text is not None and len(final_text) > FINAL_TEXT_LIMIT:
            final_text = final_text[:FINAL_TEXT_LIMIT] + "…[truncated]"
        joined = "\n".join(assistant)
        if len(joined) > TRANSCRIPT_LIMIT:
            marker = f"…[earlier transcript truncated, showing the last {TRANSCRIPT_LIMIT:,} characters]…\n"
            joined = marker + joined[-TRANSCRIPT_LIMIT:]
        return {"final_text": final_text, "transcript_text": joined or None}


class LocalCommandExecutor:
    """CommandExecutor without bwrap: trusted-fixture local subprocess execution."""

    def __init__(self, output_limit: int = 256 * 1024) -> None:
        self.output_limit = output_limit

    def run(
        self,
        argv,
        *,
        workspace: Path,
        hidden_assets: Path | None = None,
        cwd: str | None = None,
        env=None,
        timeout_seconds: int = 180,
    ) -> CommandResult:
        actual = [str(item) for item in argv]
        if hidden_assets is not None and any("/eval-hidden/" in item for item in actual):
            source = (hidden_assets / "check.py").read_text(encoding="utf-8")
            source = source.replace("/workspace/", str(workspace.resolve()) + "/")
            actual = ["python3", "-c", source]
        environment = {**os.environ, "PYTHONDONTWRITEBYTECODE": "1", **(env or {})}
        started = time.monotonic()
        try:
            completed = subprocess.run(
                actual,
                cwd=str(workspace / cwd.strip("/")) if cwd else str(workspace),
                capture_output=True,
                text=True,
                errors="replace",
                timeout=timeout_seconds,
                env=environment,
            )
            return CommandResult(
                argv=tuple(actual),
                exit_code=completed.returncode,
                stdout=completed.stdout[: self.output_limit],
                stderr=completed.stderr[: self.output_limit],
                duration_ms=int((time.monotonic() - started) * 1000),
            )
        except subprocess.TimeoutExpired as exc:
            stdout = exc.stdout if isinstance(exc.stdout, str) else ""
            return CommandResult(
                argv=tuple(actual),
                exit_code=None,
                stdout=stdout[-self.output_limit:],
                stderr="command timed out",
                duration_ms=int((time.monotonic() - started) * 1000),
                timed_out=True,
            )


def _git(repo: Path, *args: str, check: bool = True) -> str:
    completed = subprocess.run(
        ["git", "-C", str(repo), *args],
        capture_output=True, text=True, errors="replace", check=check,
    )
    return completed.stdout.strip()


def _completed_case_ids(results: Path) -> list[str]:
    if not results.exists():
        return []
    ids = []
    for line in results.read_text(encoding="utf-8").splitlines():
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if value.get("case_id"):
            ids.append(value["case_id"])
    return ids


def _experiment_snapshot(dataset, experiment_id: str) -> ExperimentSnapshot:
    return ExperimentSnapshot(
        schema_version=1,
        experiment_id=experiment_id,
        dataset_id=dataset.manifest.dataset_id,
        dataset_version=dataset.manifest.version,
        dataset_digest=dataset.digest,
        system_revision=f"local-{datetime.now(timezone.utc):%Y%m%d%H%M}",
        prompt_revision="agenthub-agent-v2",
        agent_type="orchestrator",
        model="orchestrator+implementers",
        execution_image="local-batch@sha256:" + "0" * 64,
        dependency_cache_digest="sha256:" + "0" * 64,
    )


def _sandbox_gate(args: argparse.Namespace) -> tuple[bool, str]:
    from src.app.config import settings

    readiness = current_batch_eval_readiness(
        sandbox_mode=settings.execution.sandbox.mode,
        sandbox_backend=settings.execution.sandbox.backend,
    )
    if readiness.ready:
        return True, "strict"
    if not args.allow_unsafe:
        return False, (
            "batch evaluation requires the strict sandbox; rerun with --allow-unsafe "
            "to acknowledge trusted-fixture local execution "
            f"(mode={readiness.sandbox_mode}, backend={readiness.sandbox_backend}, "
            f"missing={','.join(readiness.missing_capabilities)})"
        )
    return True, "unsafe"


def _judge_gate(dataset) -> tuple[bool, str]:
    if os.environ.get("DS_API_KEY"):
        return True, ""
    if not dataset.manifest.case_ids:
        return True, ""
    return False, (
        "DS_API_KEY is not set, but every v2 category carries an LLM-quality weight; "
        "a judgeless run would inflate score_percent via dimension normalization "
        f"({len(dataset.manifest.case_ids)} cases). --allow-unsafe does not exempt this gate."
    )


def run_batch(args: argparse.Namespace) -> int:
    load_dotenv(Path(__file__).resolve().parents[1] / ".env")
    try:
        dataset = load_dataset(args.dataset, environment_digest=args.environment_digest)
    except DatasetValidationError as exc:
        print(f"dataset error: {exc}", file=sys.stderr)
        return 2

    strict_ok, strict_message = _sandbox_gate(args)
    if not strict_ok:
        print(f"fail-closed: {strict_message}", file=sys.stderr)
        return 2
    sandbox_mode = strict_message

    judge_ok, judge_message = _judge_gate(dataset)
    if not judge_ok:
        print(f"fail-closed: {judge_message}", file=sys.stderr)
        return 2

    output = args.output
    output.mkdir(parents=True, exist_ok=True)
    results_path = output / "results.jsonl"
    results_path.touch(exist_ok=True)
    summary_path = output / "summary.json"
    state_path = output / "state.json"
    workspaces = output / "workspaces"
    workspaces.mkdir(exist_ok=True)

    case_ids = list(dataset.manifest.case_ids)
    if args.case:
        unknown = [case_id for case_id in args.case if case_id not in dataset.cases]
        if unknown:
            print(f"unknown case ids: {unknown}", file=sys.stderr)
            return 2
        case_ids = list(args.case)
    done = set(_completed_case_ids(results_path))
    pending = [case_id for case_id in case_ids if case_id not in done]
    if args.limit is not None:
        pending = pending[: args.limit]

    experiment_id = f"batch-{datetime.now(timezone.utc):%Y%m%d-%H%M%S}"
    repository = SQLiteEvalRepository(output / "evals.sqlite3")
    driver = BackendDriver(args.backend)
    executor = LocalCommandExecutor()
    coordinator = EvaluationCoordinator(repository, executor)
    try:
        snapshot = _experiment_snapshot(dataset, experiment_id)
        repository.create_experiment(snapshot)
        driver.wait_ready()
        _atomic_json(state_path, {
            "status": "starting",
            "experiment_id": experiment_id,
            "official_strict_sandbox": sandbox_mode == "strict",
            "pending": pending,
            "completed": sorted(done),
            "updated_at": _now(),
        })
        for index, case_id in enumerate(pending, start=1):
            case = dataset.cases[case_id]
            repo = workspaces / case_id
            if repo.exists():
                subprocess.run(["rm", "-rf", str(repo)], check=True)
            started = time.monotonic()
            row: dict[str, Any] = {
                "schema_version": 2,
                "experiment_id": experiment_id,
                "mode": f"trusted_fixture_local_{sandbox_mode}",
                "official_strict_sandbox": sandbox_mode == "strict",
                "case_id": case_id,
                "category": case.category,
                "difficulty": case.difficulty,
                "index": index,
                "started_at": _now(),
            }
            try:
                _, base = restore_fixture(dataset, case_id, repo)
                _git(repo, "config", "user.name", "AgentHub Eval")
                _git(repo, "config", "user.email", "eval@agenthub.local")
                run = driver.create_and_run(case_id, case.prompt, repo, case.execution.timeout_seconds)
                row["run"] = run
                branch_ref = f"refs/heads/task/{run['task_id']}"
                final = (
                    _git(repo, "rev-parse", "--verify", branch_ref, check=False)
                    or _git(repo, "rev-parse", "HEAD")
                )
                row["base_revision"] = base
                row["final_revision"] = final
                # grade_existing executes command graders against the working
                # tree: it must sit at final_revision (the task branch), not at
                # the still-unmerged main the Agent left behind.
                if _git(repo, "rev-parse", "HEAD") != final:
                    _git(repo, "checkout", "-q", "-f", "--detach", final)
                run_facts = {
                    "root_run_id": run["run_id"],
                    "run_state": run["run_state"],
                    "termination_reason": run["termination_reason"],
                    "integration_status": "merged" if final != base else "not_required",
                    "final_text": run.get("final_text"),
                    "transcript_text": run.get("transcript_text"),
                    "duration_seconds": None,
                }
                trial, _results, payload = coordinator.grade_existing(
                    dataset,
                    experiment_id=experiment_id,
                    case_id=case_id,
                    repetition=0,
                    repository_path=repo,
                    base_revision=base,
                    final_revision=final,
                    run_facts=run_facts,
                    execution_image_digest=snapshot.execution_image,
                )
                row["trial_id"] = trial.trial_id
                row["result"] = payload
                row["status"] = "completed"
            except Exception as exc:  # noqa: BLE001 - keep the batch alive
                row["status"] = "infrastructure_error"
                row["error"] = str(exc)[:4000]
            row["duration_seconds"] = round(time.monotonic() - started, 3)
            row["finished_at"] = _now()
            with results_path.open("a", encoding="utf-8") as handle:
                handle.write(json.dumps(row, ensure_ascii=False) + "\n")
                handle.flush()
                os.fsync(handle.fileno())
            _write_summary(results_path, summary_path, dataset, sandbox_mode, experiment_id)
            _atomic_json(state_path, {
                "status": "running",
                "experiment_id": experiment_id,
                "official_strict_sandbox": sandbox_mode == "strict",
                "completed": sorted(set(_completed_case_ids(results_path))),
                "updated_at": _now(),
            })
    finally:
        repository.close()
    _write_summary(results_path, summary_path, dataset, sandbox_mode, experiment_id)
    _atomic_json(state_path, {
        "status": "completed",
        "experiment_id": experiment_id,
        "official_strict_sandbox": sandbox_mode == "strict",
        "completed": sorted(set(_completed_case_ids(results_path))),
        "summary": str(summary_path),
        "updated_at": _now(),
    })
    return 0


def _write_summary(results_path: Path, summary_path: Path, dataset, sandbox_mode: str, experiment_id: str) -> None:
    rows = [json.loads(line) for line in results_path.read_text(encoding="utf-8").splitlines() if line.strip()]
    metric_rows = [
        {"state": "completed" if row.get("status") == "completed" else "invalid",
         "result": row.get("result") or {}}
        for row in rows
    ]
    metrics = aggregate_trials(metric_rows)
    summary = {
        "mode": f"trusted_fixture_local_{sandbox_mode}",
        "official_strict_sandbox": sandbox_mode == "strict",
        "experiment_id": experiment_id,
        "dataset_id": dataset.manifest.dataset_id,
        "dataset_version": dataset.manifest.version,
        "dataset_digest": dataset.digest,
        "completed": len(rows),
        "total": len(dataset.manifest.case_ids),
        "metrics": metrics,
        "updated_at": _now(),
    }
    _atomic_json(summary_path, summary)


def add_parser(subparsers) -> argparse.ArgumentParser:
    parser = subparsers.add_parser("batch", help="run a real-Agent batch through the official grading chain")
    parser.add_argument("dataset", type=Path)
    parser.add_argument("--environment-digest", default="sha256:" + "0" * 64)
    parser.add_argument("--output", type=Path, default=Path(__file__).resolve().parents[1] / "evals" / "tmp" / "batch")
    parser.add_argument(
        "--backend",
        default="http://127.0.0.1:38080",
        help="dedicated eval Backend (SERVER_PORT=38080, AGENTEND_PORT=38081 topology)",
    )
    parser.add_argument("--allow-unsafe", action="store_true",
                        help="acknowledge trusted-fixture local execution without bwrap")
    parser.add_argument("--limit", type=int, default=None, help="run at most N pending cases (pilot)")
    parser.add_argument("--case", nargs="*", default=None, help="run only these case ids")
    parser.set_defaults(func=run_batch)
    return parser
