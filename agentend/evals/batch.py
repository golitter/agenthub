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
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from collections.abc import Mapping
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
# The Backend flattens orchestration events into legacy text markers inside the
# orchestrator message content (stream/writer.go legacyRuntimeBlockLineForEvent):
#   "\ntype: plan\njson: {...}\n" / "\ntype: runtime_status\njson: {...}\n"
# They are the polling-only source for plan/task/conflict facts.
RUNTIME_MARKER_RE = re.compile(r"type: (plan|runtime_status)\njson: (.*)")
STRIP_MARKER_RE = re.compile(r"\ntype: (?:plan|runtime_status|coordination)\njson: .*\n")
# Implementer sessions that count as actively working when sampled by the poller.
ACTIVE_IMPLEMENTER_STATES = {"running", "resolving"}
CONVERSATION_CATEGORIES = {"chat", "knowledge_qa", "no_op"}


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _atomic_json(path: Path, value: object) -> None:
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding="utf-8")
    temporary.replace(path)


def _build_instruction(prompt: str, category: str, arm: str) -> str:
    """Per-category routing guidance wrapped around the case prompt.

    chat / QA / no-op must stay zero-touch; small coding tasks stay minimal;
    orchestrator cases get a neutral decomposition instruction so the measured
    dispatch behaviour is the orchestrator's own choice (serial arm excepted).
    """
    header = (
        "你正在执行一个 AgentHub 评测任务。请检查仓库并完成下面的用户需求。"
        "只可调度 Claude Code、OpenCode、Pi，禁止调度 Codex。"
    )
    if category in CONVERSATION_CATEGORIES:
        routing = "纯对话/问答类任务自己直接回答即可，不要派实现者，也不要修改仓库。"
    elif category == "orchestrator":
        if arm == "serial":
            routing = (
                "本任务要求串行执行：一次只派一个实现者，等它完成并集成后再派下一个，"
                "禁止同时派多个实现者。"
            )
        else:
            routing = "按你认为合理的方式分解并执行；任务结构允许时可以并行派多个实现者。"
    else:
        routing = "简单任务尽量只派一个实现者；需要复核时再派第二个实现者，避免无关修改。"
    tail = "不得寻找或访问隐藏测试；完成后（编码类）运行仓库中可见的公开检查并提交变更。"
    return header + routing + tail + f"\n\n用户需求：{prompt}"


def orchestration_facts(text: str) -> dict[str, Any]:
    """Derive orchestration facts from legacy runtime markers in message content."""

    plan_task_ids: list[str] = []
    dispatched_agents: list[str] = []
    attempts_by_task: dict[str, int] = {}
    last_status_by_task: dict[str, str] = {}
    chains_by_conflict: dict[str, list[str]] = {}
    for match in RUNTIME_MARKER_RE.finditer(text):
        try:
            payload = json.loads(match.group(2))
        except json.JSONDecodeError:
            continue
        if not isinstance(payload, dict):
            continue
        if match.group(1) == "plan":
            for task in payload.get("tasks") or []:
                if not isinstance(task, dict):
                    continue
                task_id = str(task.get("task_id") or "")
                agent = str(task.get("agent") or "")
                if task_id and task_id not in plan_task_ids:
                    plan_task_ids.append(task_id)
                if agent and agent not in dispatched_agents:
                    dispatched_agents.append(agent)
            continue
        task_id = str(payload.get("task_id") or "")
        attempt = payload.get("attempt")
        if task_id:
            if isinstance(attempt, (int, float)) and not isinstance(attempt, bool):
                attempts_by_task[task_id] = max(attempts_by_task.get(task_id, 0), int(attempt))
            status = payload.get("status")
            if isinstance(status, str) and status:
                last_status_by_task[task_id] = status
        conflict_id = payload.get("conflict_id")
        if isinstance(conflict_id, str) and conflict_id:
            status = payload.get("status")
            chains_by_conflict.setdefault(conflict_id, [])
            if isinstance(status, str) and status and status not in chains_by_conflict[conflict_id][-1:]:
                chains_by_conflict[conflict_id].append(status)
    conflict_ids = sorted(chains_by_conflict)
    recovered = [
        conflict_id
        for conflict_id in conflict_ids
        if "resolving" in chains_by_conflict[conflict_id]
    ]
    return {
        "plan_task_ids": plan_task_ids,
        "plan_task_count": len(plan_task_ids),
        "dispatched_implementers": dispatched_agents,
        "retry_count": sum(attempts_by_task.values()),
        "subtask_final_status": last_status_by_task,
        "conflict_ids": conflict_ids,
        "conflict_count": len(conflict_ids),
        "conflict_recovery_count": len(recovered),
        "conflict_chains": chains_by_conflict,
        "integration_conflict_seen": any(
            "conflict" in chain for chain in chains_by_conflict.values()
        ),
        "resolution_completed_seen": any(chain and chain[-1] == "completed" for chain in chains_by_conflict.values()),
    }


def _strip_runtime_markers(text: str) -> str:
    return STRIP_MARKER_RE.sub("\n", text)


def _parse_timestamp(value: Any) -> float | None:
    if not isinstance(value, str) or not value.strip():
        return None
    text = value.strip()
    if text.endswith(("Z", "z")):
        text = text[:-1] + "+00:00"
    # RFC3339Nano fractional seconds can exceed the 6 digits Python accepts.
    match = re.fullmatch(r"(.*\.\d{6})\d+(.*)", text)
    if match:
        text = match.group(1) + match.group(2)
    try:
        return datetime.fromisoformat(text).timestamp()
    except ValueError:
        return None


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

    def create_and_run(
        self,
        case_id: str,
        prompt: str,
        repo: Path,
        timeout_seconds: int,
        *,
        category: str,
        arm: str = "parallel",
    ) -> dict:
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
        instruction = _build_instruction(prompt, category, arm)
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
        max_concurrent_implementers = 0
        engaged_implementers: set[str] = set()
        while time.monotonic() < deadline:
            detail = self.request("GET", f"/api/tasks/{task_id}")
            root_session = next(item for item in detail["sessions"] if item["session_id"] == session_id)
            active = 0
            for item in detail["sessions"]:
                if item["agent_type"] == "orchestrator":
                    continue
                if item["status"] in ACTIVE_IMPLEMENTER_STATES:
                    active += 1
                    engaged_implementers.add(item["session_id"])
            max_concurrent_implementers = max(max_concurrent_implementers, active)
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
        started = _parse_timestamp(status.get("started_at"))
        finished = _parse_timestamp(status.get("finished_at"))
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
            "duration_seconds": (
                round(finished - started, 3)
                if started is not None and finished is not None and finished >= started
                else None
            ),
            "max_concurrent_implementers": max_concurrent_implementers,
            "implementer_sessions_engaged": sorted(engaged_implementers),
        }
        result.update(self._collect_evidence(task_id, session_id))
        return result

    def _collect_evidence(self, task_id: str, session_id: str) -> dict[str, Any]:
        try:
            payload = self.request("GET", f"/api/tasks/{task_id}/messages?session_id={session_id}")
        except RuntimeError:
            return {
                "final_text": None,
                "transcript_text": None,
                "orchestration": orchestration_facts(""),
            }
        messages = payload.get("data") or []
        # Backend stores agent-side messages under role="agent".
        assistant = [
            str(item.get("content") or "")
            for item in messages
            if item.get("role") in {"agent", "assistant"} and str(item.get("content") or "").strip()
        ]
        orchestration = orchestration_facts("\n".join(assistant))
        readable = [_strip_runtime_markers(text) for text in assistant]
        readable = [text for text in readable if text.strip()]
        final_text = readable[-1] if readable else None
        if final_text is not None and len(final_text) > FINAL_TEXT_LIMIT:
            final_text = final_text[:FINAL_TEXT_LIMIT] + "…[truncated]"
        joined = "\n".join(readable)
        if len(joined) > TRANSCRIPT_LIMIT:
            marker = f"…[earlier transcript truncated, showing the last {TRANSCRIPT_LIMIT:,} characters]…\n"
            joined = marker + joined[-TRANSCRIPT_LIMIT:]
        return {
            "final_text": final_text,
            "transcript_text": joined or None,
            "orchestration": orchestration,
        }


class LocalCommandExecutor:
    """CommandExecutor without bwrap: trusted-fixture local subprocess execution."""

    # Same sensitive-substring list BubblewrapGraderSandbox rejects: grader
    # commands execute agent-modified code (public_check.py), so operator
    # secrets must never leak into that environment even in trusted mode.
    _SENSITIVE_MARKERS = ("TOKEN", "SECRET", "PASSWORD", "KEY", "CREDENTIAL")

    def __init__(self, output_limit: int = 256 * 1024) -> None:
        self.output_limit = output_limit

    def _scrubbed_environment(self, env: Mapping[str, str] | None) -> dict[str, str]:
        environment = {
            key: value
            for key, value in os.environ.items()
            if not any(marker in key.upper() for marker in self._SENSITIVE_MARKERS)
        }
        environment["PYTHONDONTWRITEBYTECODE"] = "1"
        environment.update(env or {})
        return environment

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
        environment = self._scrubbed_environment(env)
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
            truncated = len(completed.stdout) > self.output_limit or len(completed.stderr) > self.output_limit
            return CommandResult(
                argv=tuple(actual),
                exit_code=completed.returncode,
                stdout=completed.stdout[: self.output_limit],
                stderr=completed.stderr[: self.output_limit],
                duration_ms=int((time.monotonic() - started) * 1000),
                truncated=truncated,
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


def _completed_keys(results: Path) -> set[tuple[str, int]]:
    """Resume set of (case_id, repetition) pairs already written to JSONL."""

    if not results.exists():
        return set()
    keys = set()
    for line in results.read_text(encoding="utf-8").splitlines():
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if value.get("case_id"):
            keys.add((value["case_id"], int(value.get("repetition") or 0)))
    return keys


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


def _experiment_snapshot(dataset, experiment_id: str, *, arm: str, repetitions: int) -> ExperimentSnapshot:
    return ExperimentSnapshot(
        schema_version=1,
        experiment_id=experiment_id,
        dataset_id=dataset.manifest.dataset_id,
        dataset_version=dataset.manifest.version,
        dataset_digest=dataset.digest,
        system_revision=f"local-{datetime.now(timezone.utc):%Y%m%d%H%M}",
        prompt_revision=f"agenthub-agent-v2-{arm}",
        agent_type="orchestrator",
        model="orchestrator+implementers",
        execution_image="local-batch@sha256:" + "0" * 64,
        dependency_cache_digest="sha256:" + "0" * 64,
        max_parallelism=1 if arm == "serial" else 4,
        repetitions=repetitions,
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

    # 绝对化输出目录：repo_path 会随任务创建 payload 发给 Backend，
    # 相对路径会被后端进程按其 CWD 解析（曾在 backend/ 下长出残留目录树）。
    output = args.output.resolve()
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
    repetitions = max(1, args.repetitions)
    done = _completed_keys(results_path)
    pending = [
        (case_id, repetition)
        for case_id in case_ids
        for repetition in range(repetitions)
        if (case_id, repetition) not in done
    ]
    if args.limit is not None:
        pending = pending[: args.limit]

    experiment_id = f"batch-{args.arm}-{datetime.now(timezone.utc):%Y%m%d-%H%M%S}"
    repository = SQLiteEvalRepository(output / "evals.sqlite3")
    driver = BackendDriver(args.backend)
    executor = LocalCommandExecutor()
    coordinator = EvaluationCoordinator(repository, executor)
    try:
        snapshot = _experiment_snapshot(dataset, experiment_id, arm=args.arm, repetitions=repetitions)
        repository.create_experiment(snapshot)
        driver.wait_ready()
        _atomic_json(state_path, {
            "status": "starting",
            "experiment_id": experiment_id,
            "arm": args.arm,
            "repetitions": repetitions,
            "official_strict_sandbox": sandbox_mode == "strict",
            "pending": [f"{case_id}#{repetition}" for case_id, repetition in pending],
            "completed": sorted(f"{case_id}#{repetition}" for case_id, repetition in done),
            "updated_at": _now(),
        })
        for index, (case_id, repetition) in enumerate(pending, start=1):
            case = dataset.cases[case_id]
            repo = workspaces / f"{case_id}--rep{repetition}"
            if repo.exists():
                subprocess.run(["rm", "-rf", str(repo)], check=True)
            started = time.monotonic()
            row: dict[str, Any] = {
                "schema_version": 3,
                "experiment_id": experiment_id,
                "arm": args.arm,
                "repetition": repetition,
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
                run = driver.create_and_run(
                    case_id,
                    case.prompt,
                    repo,
                    case.execution.timeout_seconds,
                    category=case.category,
                    arm=args.arm,
                )
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
                orchestration = run.get("orchestration") or {}
                run_facts = {
                    "root_run_id": run["run_id"],
                    "run_state": run["run_state"],
                    "termination_reason": run["termination_reason"],
                    "integration_status": "merged" if final != base else "not_required",
                    "final_text": run.get("final_text"),
                    "transcript_text": run.get("transcript_text"),
                    "duration_seconds": run.get("duration_seconds"),
                    "plan_task_count": orchestration.get("plan_task_count", 0),
                    "implementer_count": len(run.get("implementer_sessions_engaged") or []),
                    "max_concurrent_implementers": run.get("max_concurrent_implementers", 0),
                    "retry_count": orchestration.get("retry_count", 0),
                    "conflict_count": orchestration.get("conflict_count", 0),
                    "conflict_recovery_count": orchestration.get("conflict_recovery_count", 0),
                    "integration_conflict_seen": orchestration.get("integration_conflict_seen", False),
                    "resolution_completed_seen": orchestration.get("resolution_completed_seen", False),
                    "conflict_chains": orchestration.get("conflict_chains") or {},
                    "subtask_final_status": orchestration.get("subtask_final_status") or {},
                }
                trial, _results, payload = coordinator.grade_existing(
                    dataset,
                    experiment_id=experiment_id,
                    case_id=case_id,
                    repetition=repetition,
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
            row["wall_seconds"] = round(time.monotonic() - started, 3)
            row["finished_at"] = _now()
            with results_path.open("a", encoding="utf-8") as handle:
                handle.write(json.dumps(row, ensure_ascii=False) + "\n")
                handle.flush()
                os.fsync(handle.fileno())
            _write_summary(results_path, summary_path, dataset, sandbox_mode, experiment_id, args.arm, repetitions)
            _atomic_json(state_path, {
                "status": "running",
                "experiment_id": experiment_id,
                "arm": args.arm,
                "repetitions": repetitions,
                "official_strict_sandbox": sandbox_mode == "strict",
                "completed": sorted(f"{case_id}#{repetition}" for case_id, repetition in _completed_keys(results_path)),
                "updated_at": _now(),
            })
    finally:
        repository.close()
    _write_summary(results_path, summary_path, dataset, sandbox_mode, experiment_id, args.arm, repetitions)
    _atomic_json(state_path, {
        "status": "completed",
        "experiment_id": experiment_id,
        "arm": args.arm,
        "repetitions": repetitions,
        "official_strict_sandbox": sandbox_mode == "strict",
        "completed": sorted(f"{case_id}#{repetition}" for case_id, repetition in _completed_keys(results_path)),
        "summary": str(summary_path),
        "updated_at": _now(),
    })
    return 0


def _write_summary(
    results_path: Path,
    summary_path: Path,
    dataset,
    sandbox_mode: str,
    experiment_id: str,
    arm: str,
    repetitions: int,
) -> None:
    rows = [json.loads(line) for line in results_path.read_text(encoding="utf-8").splitlines() if line.strip()]
    metric_rows = [
        {"state": "completed" if row.get("status") == "completed" else "invalid",
         "case_id": row.get("case_id"),
         "repetition": row.get("repetition", 0),
         "result": row.get("result") or {}}
        for row in rows
    ]
    metrics = aggregate_trials(metric_rows)
    summary = {
        "mode": f"trusted_fixture_local_{sandbox_mode}",
        "official_strict_sandbox": sandbox_mode == "strict",
        "experiment_id": experiment_id,
        "arm": arm,
        "repetitions": repetitions,
        "dataset_id": dataset.manifest.dataset_id,
        "dataset_version": dataset.manifest.version,
        "dataset_digest": dataset.digest,
        "completed": len(rows),
        "total": len(dataset.manifest.case_ids) * repetitions,
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
    parser.add_argument("--repetitions", type=int, default=1,
                        help="run every selected case N times (resume dedupes by case_id+repetition)")
    parser.add_argument("--arm", choices=("parallel", "serial"), default="parallel",
                        help="experiment arm: parallel = neutral dispatch instruction; "
                             "serial = one implementer at a time (speedup control group)")
    parser.set_defaults(func=run_batch)
    return parser
