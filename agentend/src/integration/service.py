from __future__ import annotations

import hashlib
import asyncio
import inspect
import json
import uuid
from contextlib import asynccontextmanager
from collections import Counter
from pathlib import Path
from typing import Any

from pydantic import ValidationError

from src.integration.capability import digest_token, expires_at, issue_token
from src.integration.errors import (
    ERROR_CAPABILITY_INVALID,
    ERROR_INTEGRATION_RESULT_INVALID,
    ERROR_INTEGRATION_VERSION_UNSUPPORTED,
    ERROR_OPERATION_BINDING_MISMATCH,
    ERROR_OPERATION_CANCELLED,
    ERROR_OPERATION_NOT_FOUND,
    ERROR_OPERATION_STALE_ATTEMPT,
    ERROR_OPERATION_TERMINAL_MISMATCH,
    ERROR_SOURCE_MISSING,
    ERROR_STATE_UNCERTAIN,
    ERROR_WORKSPACE_MISSING,
    ERROR_MERGE_CONFLICT,
    ERROR_MERGE_FAILED,
    IntegrationError,
    sanitize_error_text,
)
from src.integration.models import (
    ConflictRecord,
    GitIntegrationRecord,
    IntegrationIntent,
    IntegrationOperation,
    IntegrationProjection,
    ResolutionAttempt,
    ResolutionIntegrationRecord,
    utc_now,
)
from src.integration.repository import (
    IntegrationOperationConflictError,
    IntegrationOperationRepository,
    IntegrationTerminalMismatchError,
)
from src.orchestrator.models import IntegrationResult, IntegrationResultV1, IntegrationResultV2


class IntegrationBindingError(IntegrationError):
    pass


_CONFLICT_TERMINAL_STATUSES = {"resolved", "cancelled"}
_CONFLICT_STATUS_TRANSITIONS: dict[str, set[str]] = {
    "detected": {"detected", "preparing", "resolving", "verifying", "resolved", "retryable", "awaiting_user", "cancelled"},
    "preparing": {"preparing", "resolving", "verifying", "resolved", "retryable", "awaiting_user", "cancelled"},
    "resolving": {"resolving", "verifying", "resolved", "retryable", "awaiting_user", "cancelled"},
    "verifying": {"verifying", "resolved", "retryable", "awaiting_user", "cancelled"},
    "retryable": {"retryable", "preparing", "resolving", "verifying", "resolved", "awaiting_user", "cancelled"},
    "awaiting_user": {
        "awaiting_user",
        "retryable",
        "preparing",
        "resolving",
        "verifying",
        "resolved",
        "cancelled",
    },
    "resolved": {"resolved"},
    "cancelled": {"cancelled"},
}

_RESOLUTION_ATTEMPT_STATUS_TRANSITIONS: dict[str, set[str]] = {
    "preparing": {"preparing", "resolving", "verifying", "retryable", "awaiting_user", "failed", "cancelled"},
    "resolving": {"resolving", "verifying", "retryable", "awaiting_user", "failed", "cancelled"},
    "verifying": {"verifying", "completed", "retryable", "awaiting_user", "failed", "cancelled"},
    "retryable": {"retryable", "preparing", "resolving", "verifying", "awaiting_user", "failed", "cancelled"},
    "awaiting_user": {"awaiting_user", "retryable", "preparing", "resolving", "verifying", "completed", "cancelled"},
    "completed": {"completed"},
    "failed": {"failed"},
    "cancelled": {"cancelled"},
}


class IntegrationService:
    """Trusted control-plane binding for child integration attempts."""

    def __init__(self, repository: IntegrationOperationRepository) -> None:
        self.repository = repository
        self._metrics: Counter[str] = Counter()
        # The repository CAS protects one operation. This second lock protects
        # the Git target shared by several operations in one scope while
        # allowing unrelated task branches to integrate concurrently.
        self._scope_locks: dict[str, asyncio.Lock] = {}

    def _metric(self, name: str, **labels: object) -> None:
        suffix = ""
        if labels:
            suffix = "{" + ",".join(f"{key}={labels[key]}" for key in sorted(labels)) + "}"
        self._metrics[name + suffix] += 1

    def metrics_snapshot(self) -> dict[str, int]:
        return dict(sorted(self._metrics.items()))

    @staticmethod
    def _supports_keyword(method: Any, name: str) -> bool:
        try:
            return name in inspect.signature(method).parameters
        except (TypeError, ValueError):
            return False

    async def _probe_workspace(
        self,
        workspace_mgr: Any,
        operation: IntegrationOperation,
        intent: IntegrationIntent | None = None,
        *,
        expected_source_commit: str = "",
        expected_target_commit_before: str = "",
        expected_merge_base: str = "",
    ) -> Any | None:
        """Probe Git with the durable pre-merge snapshot when available."""
        probe_method = getattr(workspace_mgr, "probe_integration", None)
        if probe_method is None:
            return None
        kwargs: dict[str, str] = {}
        if intent is not None:
            expected_source_commit = intent.source_commit
            expected_target_commit_before = intent.target_commit_before
            expected_merge_base = intent.merge_base
        if expected_source_commit:
            if self._supports_keyword(probe_method, "expected_source_commit"):
                kwargs["expected_source_commit"] = expected_source_commit
        if expected_target_commit_before:
            if self._supports_keyword(probe_method, "expected_target_commit_before"):
                kwargs["expected_target_commit_before"] = expected_target_commit_before
        if expected_merge_base:
            if self._supports_keyword(probe_method, "expected_merge_base"):
                kwargs["expected_merge_base"] = expected_merge_base
        probe = await probe_method(operation.workspace_id, **kwargs)
        if probe is None:
            return None
        if intent is not None:
            if probe.source_branch and probe.source_branch != intent.source_branch:
                return probe.model_copy(
                    update={
                        "success": False,
                        "error": "source branch does not match persisted integration intent",
                        "error_code": "integration_state_uncertain",
                    }
                )
            if probe.target_branch and probe.target_branch != intent.target_branch:
                return probe.model_copy(
                    update={
                        "success": False,
                        "error": "target branch does not match persisted integration intent",
                        "error_code": "integration_state_uncertain",
                    }
                )
        return probe

    async def _merge_workspace(
        self,
        workspace_mgr: Any,
        operation: IntegrationOperation,
    ) -> Any:
        """Run a merge while persisting its exact pre-Git snapshot first."""

        async def persist_intent(snapshot: Any) -> None:
            if not all(
                getattr(snapshot, field, "")
                for field in ("source_branch", "source_commit", "target_branch", "target_commit", "merge_base")
            ):
                raise IntegrationError(
                    ERROR_INTEGRATION_RESULT_INVALID,
                    "Git merge snapshot is incomplete",
                )
            await self.repository.save_integration_intent(
                IntegrationIntent(
                    integration_operation_id=operation.integration_operation_id,
                    workspace_id=operation.workspace_id,
                    integration_scope_id=operation.integration_scope_id,
                    source_branch=snapshot.source_branch,
                    source_commit=snapshot.source_commit,
                    target_branch=snapshot.target_branch,
                    target_commit_before=snapshot.target_commit,
                    merge_base=snapshot.merge_base,
                )
            )

        merge_method = workspace_mgr.merge
        if self._supports_keyword(merge_method, "before_merge"):
            return await merge_method(operation.workspace_id, before_merge=persist_intent)
        # Test doubles and Phase 1 adapters may expose only the legacy call;
        # keep them usable while the production WorkspaceManager uses the
        # callback-enabled path above.
        return await merge_method(operation.workspace_id)

    def record_result_rejected(self, reason: str, version: int | str) -> None:
        self._metric("integration_result_rejected_total", reason=reason, version=version)

    def _scope_lock(self, scope_id: str) -> asyncio.Lock:
        lock = self._scope_locks.get(scope_id)
        if lock is None:
            lock = asyncio.Lock()
            self._scope_locks[scope_id] = lock
        return lock

    @asynccontextmanager
    async def integration_scope_lock(self, scope_id: str):
        """Serialize resolver Git mutations with ordinary integrations."""
        async with self._scope_lock(scope_id):
            yield

    async def create_operation(
        self,
        *,
        plan_task_id: str,
        run_id: str,
        root_run_id: str,
        parent_run_id: str,
        attempt: int,
        session_id: str,
        workspace_id: str,
        workspace_handle: str,
        integration_scope_id: str,
    ) -> IntegrationOperation:
        if not all(
            isinstance(value, str) and value.strip()
            for value in (
                plan_task_id,
                run_id,
                root_run_id,
                session_id,
                workspace_id,
                workspace_handle,
                integration_scope_id,
                parent_run_id,
            )
        ):
            raise IntegrationBindingError(
                ERROR_OPERATION_BINDING_MISMATCH,
                "operation binding fields must be non-empty",
            )
        operation = IntegrationOperation(
            integration_operation_id=str(uuid.uuid4()),
            plan_task_id=plan_task_id,
            run_id=run_id,
            root_run_id=root_run_id,
            parent_run_id=parent_run_id,
            attempt=attempt,
            session_id=session_id,
            workspace_id=workspace_id,
            workspace_handle=workspace_handle,
            integration_scope_id=integration_scope_id,
        )
        existing = await self.repository.get_by_binding(root_run_id, plan_task_id, attempt)
        if existing is not None:
            for field in (
                "run_id",
                "session_id",
                "workspace_id",
                "workspace_handle",
                "integration_scope_id",
                "parent_run_id",
            ):
                if getattr(existing, field) != getattr(operation, field):
                    raise IntegrationError(
                        ERROR_OPERATION_BINDING_MISMATCH,
                        f"existing operation {field} does not match",
                    )
            self._metric("integration_idempotent_replay_total")
            return existing
        latest = await self.repository.get_latest_for_plan(root_run_id, plan_task_id)
        expected_attempt = latest.attempt + 1 if latest is not None else 0
        if attempt != expected_attempt:
            raise IntegrationError(
                ERROR_OPERATION_STALE_ATTEMPT,
                f"attempt must be {expected_attempt} for plan task {plan_task_id}",
            )
        try:
            created = await self.repository.create_operation(operation)
            self._metric("integration_operation_total", status="pending", version="v2")
            return created
        except IntegrationOperationConflictError as exc:
            existing = await self.repository.get_by_binding(root_run_id, plan_task_id, attempt)
            if not existing:
                raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, str(exc)) from exc
            for field in (
                "run_id",
                "session_id",
                "workspace_id",
                "workspace_handle",
                "integration_scope_id",
                "parent_run_id",
            ):
                if getattr(existing, field) != getattr(operation, field):
                    raise IntegrationError(
                        ERROR_OPERATION_BINDING_MISMATCH,
                        f"existing operation {field} does not match",
                    )
            self._metric("integration_idempotent_replay_total")
            return existing

    async def issue_capability(
        self,
        operation_id: str,
        *,
        ttl_seconds: int = 60,
        action: str = "execute",
    ) -> str:
        operation = await self.repository.get(operation_id)
        if not operation:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
        if operation.terminal:
            raise IntegrationError(ERROR_OPERATION_TERMINAL_MISMATCH)
        ttl_seconds = max(1, min(int(ttl_seconds), 300))
        raw, token_digest = issue_token()
        await self.repository.issue_capability(
            token_digest,
            operation.integration_operation_id,
            operation.run_id,
            operation.workspace_id,
            action,
            expires_at(ttl_seconds),
        )
        return raw

    async def redeem_capability(
        self,
        token: str,
        operation_id: str,
        *,
        run_id: str,
        workspace_id: str,
        action: str = "execute",
    ) -> None:
        if not token or not await self.repository.redeem_capability(
            digest_token(token), operation_id, run_id, workspace_id, action
        ):
            raise IntegrationError(ERROR_CAPABILITY_INVALID)

    async def get_operation_or_raise(self, operation_id: str) -> IntegrationOperation:
        operation = await self.repository.get(operation_id)
        if not operation:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
        return operation

    async def operation_projection(self, operation_id: str) -> IntegrationProjection:
        operation = await self.get_operation_or_raise(operation_id)
        projection = await self._projection_for_terminal(operation)
        if projection is not None:
            return projection
        return IntegrationProjection(
            integration_operation_id=operation.integration_operation_id,
            plan_task_id=operation.plan_task_id,
            run_id=operation.run_id,
            attempt=operation.attempt,
            status=operation.status,
            error_code=operation.error_code,
            error_message=operation.error_message,
            sequence=operation.row_version,
            finished_at=operation.finished_at,
        )

    async def begin_operation(self, operation_id: str) -> IntegrationOperation:
        operation = await self.repository.begin(operation_id)
        if operation is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
        if operation.status == "cancelled":
            raise IntegrationError(ERROR_OPERATION_CANCELLED)
        return operation

    async def _claim_operation(self, operation_id: str) -> tuple[IntegrationOperation, bool]:
        operation, claimed = await self.repository.claim(operation_id)
        if operation is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
        if operation.status == "cancelled":
            raise IntegrationError(ERROR_OPERATION_CANCELLED)
        return operation, claimed

    async def fail_operation(self, operation_id: str, error_code: str, error_message: str) -> IntegrationOperation:
        safe_error_code = sanitize_error_text(error_code, limit=128)
        safe_error_message = sanitize_error_text(error_message)
        operation = await self.repository.get(operation_id)
        if operation is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, operation_id)
        finished_at = utc_now()
        record = GitIntegrationRecord(
            record_id=str(uuid.uuid4()),
            integration_operation_id=operation.integration_operation_id,
            workspace_id=operation.workspace_id,
            integration_scope_id=operation.integration_scope_id,
            status="failed",
            error_code=safe_error_code,
            error_message=safe_error_message,
            started_at=operation.started_at or finished_at,
            finished_at=finished_at,
        )
        try:
            operation, _ = await self.repository.finalize_with_git_record(
                operation_id,
                "failed",
                hashlib.sha256(f"{safe_error_code}:{safe_error_message}".encode("utf-8")).hexdigest(),
                record,
                safe_error_code,
                safe_error_message,
                finished_at,
            )
        except KeyError as exc:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, str(exc)) from exc
        except IntegrationTerminalMismatchError as exc:
            raise IntegrationError(ERROR_OPERATION_TERMINAL_MISMATCH, str(exc)) from exc
        return operation

    @staticmethod
    def _result_from_git_record(
        operation: IntegrationOperation,
        record: GitIntegrationRecord,
    ) -> IntegrationResult:
        return IntegrationResult(
            version=2,
            run_id=operation.run_id,
            root_run_id=operation.root_run_id,
            parent_run_id=operation.parent_run_id,
            plan_task_id=operation.plan_task_id,
            integration_operation_id=operation.integration_operation_id,
            integration_scope_id=operation.integration_scope_id,
            workspace_id=operation.workspace_id,
            workspace_handle=operation.workspace_handle,
            session_id=operation.session_id,
            attempt=operation.attempt,
            status=record.status,
            source_branch=record.source_branch,
            source_commit=record.source_commit,
            target_branch=record.target_branch,
            target_commit=record.target_commit_before,
            target_commit_after=record.target_commit_after,
            merge_base=record.merge_base,
            conflict_files=list(record.conflict_files),
            aborted=record.aborted,
            error_code=record.error_code,
            error_message=record.error_message,
            started_at=record.started_at,
            finished_at=record.finished_at,
        )

    async def _finalize_existing_git_record(
        self,
        operation: IntegrationOperation,
        record: GitIntegrationRecord,
    ) -> IntegrationOperation:
        """Converge a record written before a process crash without rerunning Git."""
        if record.status in {"merged", "conflict", "partial", "failed"}:
            result = self._result_from_git_record(operation, record)
            await self.finalize_result(result, operation)
            return await self.repository.get(operation.integration_operation_id) or operation

        if record.status in {"cancelled", ERROR_STATE_UNCERTAIN}:
            error_code = sanitize_error_text(
                record.error_code or (
                    ERROR_STATE_UNCERTAIN if record.status == ERROR_STATE_UNCERTAIN else "operation_cancelled"
                ),
                limit=128,
            )
            error_message = sanitize_error_text(record.error_message or error_code)
            digest = operation.result_digest or hashlib.sha256(
                f"{record.status}:{error_code}:{error_message}".encode("utf-8")
            ).hexdigest()
            try:
                updated, _ = await self.repository.finalize_with_git_record(
                    operation.integration_operation_id,
                    record.status,
                    digest,
                    record,
                    error_code,
                    error_message,
                    record.finished_at or utc_now(),
                )
            except KeyError as exc:
                raise IntegrationError(ERROR_OPERATION_NOT_FOUND, str(exc)) from exc
            except IntegrationTerminalMismatchError as exc:
                raise IntegrationError(ERROR_OPERATION_TERMINAL_MISMATCH, str(exc)) from exc
            return updated

        raise IntegrationError(
            ERROR_STATE_UNCERTAIN,
            "persisted Git record has an unsupported terminal status",
        )

    @staticmethod
    def _result_from_probe(
        operation: IntegrationOperation,
        probe: Any,
        intent: IntegrationIntent | None = None,
    ) -> IntegrationResult | None:
        if probe is None:
            return None
        if probe.success:
            return IntegrationResult(
                version=2,
                run_id=operation.run_id,
                root_run_id=operation.root_run_id,
                parent_run_id=operation.parent_run_id,
                plan_task_id=operation.plan_task_id,
                integration_operation_id=operation.integration_operation_id,
                integration_scope_id=operation.integration_scope_id,
                workspace_id=operation.workspace_id,
                workspace_handle=operation.workspace_handle,
                session_id=operation.session_id,
                attempt=operation.attempt,
                status="merged",
                source_branch=probe.source_branch,
                source_commit=probe.source_commit,
                target_branch=probe.target_branch,
                target_commit=probe.target_commit,
                target_commit_after=probe.target_commit_after,
                merge_base=intent.merge_base if intent is not None else probe.merge_base,
            )
        if probe.error_code in {ERROR_SOURCE_MISSING, ERROR_WORKSPACE_MISSING}:
            return IntegrationResult(
                **IntegrationService._failed_result(
                    operation,
                    probe.error_code,
                    probe.error or probe.error_code,
                ).model_dump()
            )
        return None

    async def _reconcile_result_file(
        self,
        operation: IntegrationOperation,
        workspace_mgr: Any,
    ) -> IntegrationOperation | None:
        """Consume a Phase 1 result file during restart recovery.

        The file is only a migration transport. It is accepted after the
        operation binding and the registered workspace's Git facts have been
        checked, then converted into the same durable record used by Phase 2.
        """
        if workspace_mgr is None or not hasattr(workspace_mgr, "get"):
            return None
        workspace = workspace_mgr.get(operation.workspace_id)
        if workspace is None:
            return None
        workspace_status = getattr(getattr(workspace, "status", None), "value", getattr(workspace, "status", ""))
        if workspace_status != "active":
            return None
        run_id = operation.run_id
        run_path = Path(run_id)
        if not run_id or run_path.name != run_id or run_path.is_absolute():
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "operation run_id is not a safe result filename")
        result_path = (
            Path(workspace.worktree_path).resolve().parent
            / "shared"
            / ".agent"
            / "integration-results"
            / f"{run_id}.json"
        )
        try:
            if result_path.stat().st_size > 256 * 1024:
                raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "integration result is too large")
            raw = json.loads(result_path.read_text(encoding="utf-8"))
        except FileNotFoundError:
            return None
        except IntegrationError:
            raise
        except (OSError, json.JSONDecodeError, UnicodeError) as exc:
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "integration result file is unreadable") from exc
        if not isinstance(raw, dict) or isinstance(raw.get("version"), bool) or not isinstance(raw.get("version"), int):
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "integration result version is invalid")
        try:
            if raw["version"] == 1:
                result = IntegrationResult.model_validate(IntegrationResultV1.model_validate(raw).model_dump())
            elif raw["version"] == 2:
                result = IntegrationResult.model_validate(IntegrationResultV2.model_validate(raw).model_dump())
            else:
                raise IntegrationError(
                    ERROR_INTEGRATION_VERSION_UNSUPPORTED,
                    "integration result version is unsupported",
                )
        except ValidationError as exc:
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "integration result schema is invalid") from exc

        await self.validate_result(
            result,
            expected_run_id=operation.run_id,
            expected_root_run_id=operation.root_run_id,
            expected_parent_run_id=operation.parent_run_id,
            expected_plan_task_id=operation.plan_task_id,
            expected_session_id=operation.session_id,
            expected_attempt=operation.attempt,
            expected_integration_scope_id=operation.integration_scope_id,
            expected_workspace_id=operation.workspace_id,
            expected_operation_id=operation.integration_operation_id,
        )
        await self._validate_result_git_facts(result, operation, workspace_mgr)
        current = await self.repository.get(operation.integration_operation_id)
        if current is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
        if current.status == "pending":
            current = await self.begin_operation(operation.integration_operation_id)
        await self.finalize_result(result, current)
        return await self.repository.get(operation.integration_operation_id)

    async def _validate_result_git_facts(
        self,
        result: IntegrationResult,
        operation: IntegrationOperation,
        workspace_mgr: Any,
    ) -> None:
        """Cross-check migration result facts against the registered workspace."""
        workspace = workspace_mgr.get(operation.workspace_id) if hasattr(workspace_mgr, "get") else None
        if workspace is None:
            raise IntegrationError(ERROR_WORKSPACE_MISSING, "registered workspace is unavailable")
        expected_source = getattr(workspace, "branch_name", "")
        expected_target = f"task/{operation.integration_scope_id}"
        if result.source_branch and expected_source and result.source_branch != expected_source:
            raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "source branch does not match workspace")
        if result.target_branch and result.target_branch != expected_target:
            raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "target branch does not match operation scope")
        if not hasattr(workspace_mgr, "probe_integration"):
            return
        intent = await self.repository.get_integration_intent(operation.integration_operation_id)
        probe = await self._probe_workspace(
            workspace_mgr,
            operation,
            intent,
            expected_source_commit=result.source_commit,
            expected_target_commit_before=result.target_commit,
            expected_merge_base=result.merge_base,
        )
        if result.status == "merged":
            if any(
                not value
                for value in (
                    result.source_branch,
                    result.source_commit,
                    result.target_branch,
                    result.target_commit,
                    result.target_commit_after,
                    result.merge_base,
                )
            ):
                raise IntegrationError(
                    ERROR_INTEGRATION_RESULT_INVALID,
                    "merged result is missing authoritative Git facts",
                )
            if not probe.success:
                raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "Git probe does not prove merged state")
            if result.source_commit and result.source_commit != probe.source_commit:
                raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "source commit is not authoritative")
            if result.target_commit != probe.target_commit:
                raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "target commit before is not authoritative")
            if result.target_commit_after != probe.target_commit_after:
                raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "target commit is not authoritative")
            if result.merge_base != probe.merge_base:
                raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "merge base is not authoritative")
        elif result.status == "conflict" and result.aborted and result.target_commit:
            if probe.target_commit != result.target_commit:
                raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "aborted conflict moved target commit")

    async def recover_incomplete(
        self,
        run_repository: Any | None = None,
        workspace_mgr: Any | None = None,
        *,
        execute_pending: bool = False,
    ) -> list[IntegrationOperation]:
        """Return durable in-flight operations for startup recovery.

        Phase 1 keeps Git execution in taskctl, so recovery must not guess a
        path or rerun a merge.  The caller can resume a pending operation from
        its persisted child Run; an ``integrating`` operation is probed first
        and only becomes uncertain when the Git facts cannot prove a safe
        terminal outcome.
        """
        pending = await self.repository.list_incomplete()
        recovered: list[IntegrationOperation] = []
        for operation in pending:
            existing_record = await self.repository.get_git_record(operation.integration_operation_id)
            if existing_record is not None:
                if (
                    existing_record.workspace_id != operation.workspace_id
                    or existing_record.integration_scope_id != operation.integration_scope_id
                ):
                    operation = await self.mark_state_uncertain(
                        operation,
                        message="persisted Git record does not match operation workspace/scope",
                    )
                else:
                    try:
                        operation = await self._finalize_existing_git_record(operation, existing_record)
                    except IntegrationError:
                        self._metric("integration_recovery_record_finalize_failed_total")
                        operation = await self.mark_state_uncertain(
                            operation,
                            message="persisted Git record could not be reconciled safely",
                        )
                recovered.append(operation)
                continue

            intent = await self.repository.get_integration_intent(operation.integration_operation_id)
            result_file_error: IntegrationError | None = None
            if workspace_mgr is not None:
                try:
                    reconciled = await self._reconcile_result_file(operation, workspace_mgr)
                    if reconciled is not None:
                        recovered.append(reconciled)
                        continue
                except IntegrationError as exc:
                    result_file_error = exc
                    self._metric("integration_recovery_result_file_rejected_total", code=exc.code)

            if run_repository is not None:
                run = await run_repository.get(operation.run_id)
                run_state = getattr(run.state, "value", run.state) if run is not None else ""
                if run_state == "cancelled" and operation.status == "pending":
                    await self.repository.cancel(operation.integration_operation_id, "operation_cancelled")
                    operation = await self.repository.get(operation.integration_operation_id) or operation
                    recovered.append(operation)
                    continue
                if run_state == "failed" and operation.status == "pending":
                    operation = await self.fail_operation(
                        operation.integration_operation_id,
                        "execution_failed",
                        "child Run failed during AgentEnd recovery",
                    )
                    recovered.append(operation)
                    continue
                if run_state == "completed" and operation.status == "pending" and workspace_mgr:
                    probe = None
                    if hasattr(workspace_mgr, "probe_integration"):
                        try:
                            registered_workspace = (
                                workspace_mgr.get(operation.workspace_id)
                                if hasattr(workspace_mgr, "get")
                                else None
                            )
                            if registered_workspace is None or (
                                getattr(registered_workspace, "task_id", "") == operation.integration_scope_id
                                and getattr(registered_workspace, "session_id", "") == operation.session_id
                            ):
                                probe = await self._probe_workspace(workspace_mgr, operation, intent)
                        except Exception:
                            self._metric("integration_recovery_probe_failed_total")
                    probe_result = self._result_from_probe(operation, probe, intent)
                    if probe_result is not None:
                        try:
                            await self.finalize_result(probe_result, operation)
                            operation = await self.repository.get(operation.integration_operation_id) or operation
                            recovered.append(operation)
                            continue
                        except IntegrationError:
                            self._metric("integration_recovery_finalize_failed_total")
                    if execute_pending and probe is not None and probe.error_code == "not_merged":
                        try:
                            capability = await self.issue_capability(operation.integration_operation_id)
                            await self.execute_operation(
                                operation.integration_operation_id,
                                capability=capability,
                                run_id=operation.run_id,
                                workspace_mgr=workspace_mgr,
                            )
                            operation = await self.repository.get(operation.integration_operation_id) or operation
                            recovered.append(operation)
                            continue
                        except IntegrationError:
                            # A restart must never blindly repeat an uncertain
                            # Git action. Preserve pending for an explicit retry
                            # when the control-plane path cannot prove safety.
                            continue
                    if probe is not None and probe.error_code == "not_merged":
                        operation = await self.fail_operation(
                            operation.integration_operation_id,
                            result_file_error.code if result_file_error else "integration_missing",
                            (
                                result_file_error.message
                                if result_file_error
                                else "child Run completed without a durable integration result"
                            ),
                        )
                        recovered.append(operation)
                        continue
            if operation.status == "integrating":
                probe = None
                recovery_uncertain_message = "AgentEnd restarted while integration was in progress"
                if workspace_mgr is not None and hasattr(workspace_mgr, "probe_integration"):
                    try:
                        registered_workspace = (
                            workspace_mgr.get(operation.workspace_id)
                            if hasattr(workspace_mgr, "get")
                            else None
                        )
                        if registered_workspace is not None and (
                            getattr(registered_workspace, "task_id", "") != operation.integration_scope_id
                            or getattr(registered_workspace, "session_id", "") != operation.session_id
                        ):
                            recovery_uncertain_message = (
                                "persisted workspace no longer belongs to the operation scope/session"
                            )
                        else:
                            probe = await self._probe_workspace(workspace_mgr, operation, intent)
                    except Exception:
                        self._metric("integration_recovery_probe_failed_total")
                if probe is not None and probe.success:
                    # Git already contains the source commit.  Reconstruct the
                    # terminal fact without invoking merge a second time.
                    result = IntegrationResult(
                        version=2,
                        run_id=operation.run_id,
                        root_run_id=operation.root_run_id,
                        parent_run_id=operation.parent_run_id,
                        plan_task_id=operation.plan_task_id,
                        integration_operation_id=operation.integration_operation_id,
                        integration_scope_id=operation.integration_scope_id,
                        workspace_id=operation.workspace_id,
                        workspace_handle=operation.workspace_handle,
                        session_id=operation.session_id,
                        attempt=operation.attempt,
                        status="merged",
                        source_branch=probe.source_branch,
                        source_commit=probe.source_commit,
                        target_branch=probe.target_branch,
                        target_commit=probe.target_commit,
                        target_commit_after=probe.target_commit_after,
                        merge_base=intent.merge_base if intent is not None else probe.merge_base,
                    )
                    try:
                        operation = await self.repository.get(operation.integration_operation_id) or operation
                        await self.finalize_result(result, operation)
                        operation = await self.repository.get(operation.integration_operation_id) or operation
                        recovered.append(operation)
                        continue
                    except IntegrationError:
                        self._metric("integration_recovery_finalize_failed_total")
                elif probe is not None and probe.error_code in {ERROR_SOURCE_MISSING, ERROR_WORKSPACE_MISSING}:
                    result = self._failed_result(
                        operation,
                        probe.error_code,
                        probe.error or probe.error_code,
                    )
                    try:
                        await self.finalize_result(result, operation)
                        operation = await self.repository.get(operation.integration_operation_id) or operation
                        recovered.append(operation)
                        continue
                    except IntegrationError:
                        self._metric("integration_recovery_finalize_failed_total")
                elif (
                    execute_pending
                    and probe is not None
                    and probe.error_code == "not_merged"
                    and workspace_mgr is not None
                ):
                    # The prior process claimed the operation but left no Git
                    # side effect.  Resume exactly once after the probe proved
                    # the target worktree is clean.
                    try:
                        capability = await self.issue_capability(operation.integration_operation_id)
                        await self.execute_operation(
                            operation.integration_operation_id,
                            capability=capability,
                            run_id=operation.run_id,
                            workspace_mgr=workspace_mgr,
                            resume_integrating=True,
                        )
                        operation = await self.repository.get(operation.integration_operation_id) or operation
                        recovered.append(operation)
                        continue
                    except IntegrationError:
                        self._metric("integration_recovery_resume_failed_total")
                try:
                    operation = await self.mark_state_uncertain(
                        operation,
                        probe=probe,
                        message=recovery_uncertain_message,
                    )
                except IntegrationError:
                    operation = await self.repository.get(operation.integration_operation_id) or operation
                recovered.append(operation)
        return recovered

    async def validate_result(
        self,
        result: IntegrationResult,
        *,
        expected_run_id: str,
        expected_root_run_id: str,
        expected_parent_run_id: str,
        expected_plan_task_id: str,
        expected_session_id: str,
        expected_attempt: int,
        expected_integration_scope_id: str,
        expected_workspace_id: str = "",
        expected_operation_id: str = "",
    ) -> IntegrationOperation | None:
        """Validate a V1/V2 result against the control-plane operation.

        V1 has no operation ID and is accepted only when its legacy task_id
        equals the Git integration scope. V2 must resolve an operation and
        compare every immutable binding before any result is finalized.
        """
        operation_id = result.integration_operation_id
        if not operation_id:
            if result.version != 1:
                raise IntegrationBindingError(
                    ERROR_OPERATION_BINDING_MISMATCH,
                    "V2 result is missing integration_operation_id",
                )
            self._require(result.run_id, expected_run_id, "run_id")
            self._require(result.task_id, expected_integration_scope_id, "integration_scope_id")
            self._require(result.session_id, expected_session_id, "session_id")
            if result.attempt != expected_attempt:
                raise IntegrationBindingError(
                    ERROR_OPERATION_STALE_ATTEMPT,
                    f"attempt mismatch: expected {expected_attempt}, actual {result.attempt}",
                )
            self._require_optional(result.root_run_id, expected_root_run_id, "root_run_id")
            self._require_optional(result.parent_run_id, expected_parent_run_id, "parent_run_id")
            if not expected_operation_id:
                self._metric("legacy_v1_result_consumed_total", version="v1")
                return None
            operation = await self.repository.get(expected_operation_id)
            if not operation:
                raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
            if operation.status == "cancelled":
                raise IntegrationError(ERROR_OPERATION_CANCELLED)
            self._require(operation.run_id, expected_run_id, "run_id")
            self._require(operation.root_run_id, expected_root_run_id, "root_run_id")
            self._require_optional(operation.parent_run_id, expected_parent_run_id, "parent_run_id")
            self._require(operation.plan_task_id, expected_plan_task_id, "plan_task_id")
            self._require(operation.session_id, expected_session_id, "session_id")
            if operation.attempt != expected_attempt:
                raise IntegrationBindingError(ERROR_OPERATION_STALE_ATTEMPT, "operation attempt mismatch")
            self._require(operation.integration_scope_id, expected_integration_scope_id, "integration_scope_id")
            if expected_workspace_id:
                self._require(operation.workspace_id, expected_workspace_id, "workspace_id")
            self._metric("legacy_v1_result_consumed_total", version="v1")
            return operation

        operation = await self.repository.get(operation_id)
        if not operation:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
        if result.version != 2:
            raise IntegrationBindingError(
                ERROR_INTEGRATION_RESULT_INVALID,
                "operation-addressed result must use version 2",
            )
        if expected_operation_id and operation_id != expected_operation_id:
            raise IntegrationBindingError(
                ERROR_OPERATION_BINDING_MISMATCH,
                "integration_operation_id mismatch",
            )
        if operation.status == "cancelled":
            raise IntegrationError(ERROR_OPERATION_CANCELLED)
        self._require(result.run_id, operation.run_id, "run_id")
        self._require(result.run_id, expected_run_id, "run_id")
        self._require(operation.root_run_id, expected_root_run_id, "root_run_id")
        self._require_optional(operation.parent_run_id, expected_parent_run_id, "parent_run_id")
        self._require(result.session_id, operation.session_id, "session_id")
        self._require(result.session_id, expected_session_id, "session_id")
        self._require(operation.plan_task_id, expected_plan_task_id, "plan_task_id")
        self._require(operation.integration_scope_id, expected_integration_scope_id, "integration_scope_id")
        if result.attempt != operation.attempt or result.attempt != expected_attempt:
            raise IntegrationBindingError(ERROR_OPERATION_STALE_ATTEMPT, "attempt does not match operation")
        if result.version == 2:
            self._require(result.root_run_id, operation.root_run_id, "root_run_id")
            self._require(result.parent_run_id, operation.parent_run_id, "parent_run_id")
            self._require(result.plan_task_id, operation.plan_task_id, "plan_task_id")
            self._require(result.integration_scope_id, operation.integration_scope_id, "integration_scope_id")
            if not result.workspace_id and not result.workspace_handle:
                raise IntegrationBindingError(
                    ERROR_OPERATION_BINDING_MISMATCH,
                    "V2 result is missing workspace identity",
                )
        else:
            self._require_optional(result.root_run_id, operation.root_run_id, "root_run_id")
            self._require_optional(result.parent_run_id, operation.parent_run_id, "parent_run_id")
            self._require_optional(result.plan_task_id, operation.plan_task_id, "plan_task_id")
            self._require_optional(result.integration_scope_id, operation.integration_scope_id, "integration_scope_id")
        self._require_optional(result.workspace_id, operation.workspace_id, "workspace_id")
        self._require_optional(result.workspace_handle, operation.workspace_handle, "workspace_handle")
        if expected_workspace_id:
            self._require(operation.workspace_id, expected_workspace_id, "workspace_id")
        self._metric("integration_result_accepted_total", version="v2")
        return operation

    async def finalize_result(
        self,
        result: IntegrationResult,
        operation: IntegrationOperation | None,
    ) -> IntegrationProjection | None:
        if operation is None:
            return None
        if any(not self._is_normalized_conflict_path(name) for name in result.conflict_files):
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "invalid conflict file path")
        if result.status == "merged" and not result.target_commit_after:
            raise IntegrationError(
                ERROR_INTEGRATION_RESULT_INVALID,
                "merged result is missing target_commit_after",
            )
        if result.status == "merged" and any(
            not value
            for value in (
                result.source_branch,
                result.source_commit,
                result.target_branch,
                result.target_commit,
                result.merge_base,
            )
        ):
            raise IntegrationError(
                ERROR_INTEGRATION_RESULT_INVALID,
                "merged result is missing authoritative Git facts",
            )
        if result.status == "conflict" and result.aborted:
            if not result.target_commit:
                raise IntegrationError(
                    ERROR_INTEGRATION_RESULT_INVALID,
                    "aborted conflict is missing target_commit_before",
                )
            if result.target_commit_after and result.target_commit_after != result.target_commit:
                raise IntegrationError(
                    ERROR_INTEGRATION_RESULT_INVALID,
                    "aborted conflict target_commit_after must equal target_commit_before",
                )
        status = result.status
        safe_error_code = sanitize_error_text(result.error_code, limit=128)
        safe_error_message = sanitize_error_text(result.error_message)
        normalized_result = result.model_copy(
            update={"error_code": safe_error_code, "error_message": safe_error_message}
        )
        computed_digest = self.result_digest(normalized_result)
        if result.result_digest and result.result_digest != computed_digest:
            raise IntegrationError(
                ERROR_INTEGRATION_RESULT_INVALID,
                "result_digest does not match the normalized result",
            )
        digest = computed_digest
        # ConflictRecord is a separate durable projection, but it must exist
        # before the operation becomes terminal. Otherwise a crash or a
        # repository error between ``finalize_with_git_record`` and the
        # conflict insert would leave a terminal conflict with no recovery
        # handle. For a replay, only the exact same terminal digest may pass
        # this preflight.
        conflict_id = ""
        operation_for_finalize = await self.repository.get(operation.integration_operation_id)
        if operation_for_finalize is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
        if operation_for_finalize.terminal and (
            operation_for_finalize.status != status
            or operation_for_finalize.result_digest != digest
        ):
            raise IntegrationError(
                ERROR_OPERATION_TERMINAL_MISMATCH,
                f"operation {operation.integration_operation_id} already finalized as {operation_for_finalize.status}",
            )
        if status == "conflict":
            conflict_id = self.conflict_id_for(
                operation_for_finalize.integration_operation_id,
                normalized_result.source_commit,
                normalized_result.target_commit,
                normalized_result.conflict_files,
            )
            existing_conflict = await self.repository.get_conflict_record(conflict_id)
            if existing_conflict is not None:
                if existing_conflict.original_operation_id != operation_for_finalize.integration_operation_id:
                    raise IntegrationError(
                        ERROR_OPERATION_BINDING_MISMATCH,
                        "conflict record identity cannot be rebound",
                    )
            else:
                await self._save_conflict_from_result(operation_for_finalize, normalized_result)
        existing_record = await self.repository.get_git_record(operation.integration_operation_id)
        if existing_record is not None:
            # Replaying a record written by an older writer may legitimately
            # have empty timestamps; preserve those immutable facts exactly.
            record_started_at = result.started_at or existing_record.started_at
            record_finished_at = result.finished_at or existing_record.finished_at
        else:
            record_started_at = result.started_at or operation.started_at or utc_now()
            record_finished_at = result.finished_at or utc_now()
        record = GitIntegrationRecord(
            record_id=str(uuid.uuid4()),
            integration_operation_id=operation.integration_operation_id,
            workspace_id=operation.workspace_id,
            integration_scope_id=operation.integration_scope_id,
            status=status,
            source_branch=result.source_branch,
            source_commit=result.source_commit,
            target_branch=result.target_branch,
            target_commit_before=result.target_commit,
            target_commit_after=(
                result.target_commit_after or result.target_commit
                if status == "conflict" and result.aborted
                else result.target_commit_after
            ),
            merge_base=result.merge_base,
            conflict_files=list(result.conflict_files),
            aborted=result.aborted,
            error_code=safe_error_code,
            error_message=safe_error_message,
            started_at=record_started_at,
            finished_at=record_finished_at,
        )
        try:
            updated, idempotent = await self.repository.finalize_with_git_record(
                operation_for_finalize.integration_operation_id,
                status,
                digest,
                record,
                safe_error_code,
                safe_error_message,
                record_finished_at,
            )
        except KeyError as exc:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, str(exc)) from exc
        except IntegrationTerminalMismatchError as exc:
            raise IntegrationError(ERROR_OPERATION_TERMINAL_MISMATCH, str(exc)) from exc
        if not idempotent:
            self._metric("integration_operation_total", status=status, version=f"v{result.version}")
        else:
            self._metric("integration_idempotent_replay_total")
        return IntegrationProjection(
            integration_operation_id=updated.integration_operation_id,
            plan_task_id=updated.plan_task_id,
            run_id=updated.run_id,
            attempt=updated.attempt,
            status=updated.status,
            conflict_id=conflict_id,
            conflict_files=list(result.conflict_files),
            error_code=updated.error_code,
            error_message=updated.error_message,
            sequence=updated.row_version,
            finished_at=updated.finished_at,
        )

    async def mark_state_uncertain(
        self,
        operation: IntegrationOperation,
        *,
        probe: Any | None = None,
        message: str = "AgentEnd restarted while integration was in progress",
    ) -> IntegrationOperation:
        """Close an interrupted claim with an auditable non-Git terminal fact."""
        error_code = ERROR_STATE_UNCERTAIN
        safe_message = sanitize_error_text(message)
        digest = hashlib.sha256(f"{error_code}:{safe_message}".encode("utf-8")).hexdigest()
        record = GitIntegrationRecord(
            record_id=str(uuid.uuid4()),
            integration_operation_id=operation.integration_operation_id,
            workspace_id=operation.workspace_id,
            integration_scope_id=operation.integration_scope_id,
            status=ERROR_STATE_UNCERTAIN,
            source_branch=getattr(probe, "source_branch", "") if probe else "",
            source_commit=getattr(probe, "source_commit", "") if probe else "",
            target_branch=getattr(probe, "target_branch", "") if probe else "",
            target_commit_before=getattr(probe, "target_commit", "") if probe else "",
            merge_base=getattr(probe, "merge_base", "") if probe else "",
            error_code=error_code,
            error_message=safe_message,
            started_at=operation.started_at,
            finished_at=utc_now(),
        )
        try:
            updated, _ = await self.repository.finalize_with_git_record(
                operation.integration_operation_id,
                ERROR_STATE_UNCERTAIN,
                digest,
                record,
                error_code,
                safe_message,
                record.finished_at,
            )
        except KeyError as exc:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, str(exc)) from exc
        except IntegrationTerminalMismatchError as exc:
            raise IntegrationError(ERROR_OPERATION_TERMINAL_MISMATCH, str(exc)) from exc
        self._metric("integration_state_uncertain_total")
        return updated

    async def cancel(self, operation_id: str, reason: str = "operation_cancelled") -> bool:
        operation = await self.repository.get(operation_id)
        if operation is None or operation.terminal:
            return False
        if operation.status == "integrating":
            # Claiming an operation and entering the scope lock are separate
            # moments. Holding the same lock here lets cancellation win only
            # before Git starts; once merge owns the lock, cancellation waits
            # for its durable terminal fact instead of lying about the result.
            async with self._scope_lock(operation.integration_scope_id):
                current = await self.repository.get(operation_id)
                if current is None or current.terminal:
                    return False
                return await self.repository.cancel(
                    operation_id,
                    reason,
                    allow_integrating=True,
                )
        return await self.repository.cancel(operation_id, reason)

    async def save_conflict_record(self, record: ConflictRecord) -> ConflictRecord:
        operation = await self.repository.get(record.original_operation_id)
        if operation is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
        for field in ("root_run_id", "plan_task_id", "integration_scope_id", "workspace_id"):
            value = getattr(record, field)
            if not value or value != getattr(operation, field):
                raise IntegrationError(
                    ERROR_OPERATION_BINDING_MISMATCH,
                    f"conflict record {field} does not match operation",
                )
        if record.status not in {
            "detected",
            "preparing",
            "resolving",
            "verifying",
            "resolved",
            "retryable",
            "awaiting_user",
            "cancelled",
        }:
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "invalid conflict record status")
        if any(not self._is_normalized_conflict_path(name) for name in record.conflict_files):
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "invalid conflict file path")
        existing = await self.repository.get_conflict_record(record.conflict_id)
        if existing is not None:
            if existing.original_operation_id != record.original_operation_id:
                raise IntegrationError(
                    ERROR_OPERATION_BINDING_MISMATCH,
                    "conflict record identity cannot be rebound",
                )
            if record.attempt < existing.attempt:
                raise IntegrationError(
                    ERROR_OPERATION_STALE_ATTEMPT,
                    f"conflict attempt must be at least {existing.attempt}",
                )
            if record.attempt > existing.attempt + 1:
                raise IntegrationError(
                    ERROR_OPERATION_STALE_ATTEMPT,
                    f"conflict attempt must be at most {existing.attempt + 1}",
                )
            allowed = _CONFLICT_STATUS_TRANSITIONS.get(existing.status, set())
            if record.status not in allowed:
                code = (
                    ERROR_OPERATION_TERMINAL_MISMATCH
                    if existing.status in _CONFLICT_TERMINAL_STATUSES
                    else ERROR_INTEGRATION_RESULT_INVALID
                )
                raise IntegrationError(
                    code,
                    f"invalid conflict transition {existing.status} -> {record.status}",
                )
            for field in (
                "root_run_id",
                "plan_task_id",
                "integration_scope_id",
                "workspace_id",
                "source_branch",
                "source_commit",
                "target_branch",
                "target_commit",
                "merge_base",
            ):
                previous = getattr(existing, field)
                current = getattr(record, field)
                if previous and current and previous != current:
                    raise IntegrationError(
                        ERROR_OPERATION_TERMINAL_MISMATCH,
                        f"conflict record {field} cannot be rewritten",
                    )
            if existing.conflict_files and record.conflict_files:
                if existing.conflict_files != record.conflict_files:
                    raise IntegrationError(
                        ERROR_OPERATION_TERMINAL_MISMATCH,
                        "conflict files cannot be rewritten",
                    )
            record = record.model_copy(
                update={
                    "created_at": existing.created_at,
                    **{
                        field: getattr(existing, field) or getattr(record, field)
                        for field in (
                            "source_branch",
                            "source_commit",
                            "target_branch",
                            "target_commit",
                            "merge_base",
                            "resolver_agent",
                            "resolver_session_id",
                            "resolver_branch",
                            "resolver_run_id",
                        )
                    },
                    "conflict_files": list(existing.conflict_files or record.conflict_files),
                }
            )
        record = record.model_copy(
            update={
                "last_error_code": sanitize_error_text(record.last_error_code, limit=128),
                "last_error_message": sanitize_error_text(record.last_error_message),
            }
        )
        return await self.repository.save_conflict_record(record)

    async def get_conflict_record(self, conflict_id: str) -> ConflictRecord | None:
        return await self.repository.get_conflict_record(conflict_id)

    async def list_conflict_records(self, statuses: set[str] | None = None) -> list[ConflictRecord]:
        return await self.repository.list_conflict_records(statuses)

    async def recoverable_root_run_ids(self) -> set[str]:
        """Roots that must survive startup long enough for recovery to run.

        Besides an in-flight Resolver, an accepted/running manual action is a
        durable continuation too.  If the process dies after the action row is
        inserted but before its final projection is written, cancelling that
        root during generic Run recovery would strand the action forever.
        """
        records = await self.repository.list_conflict_records()
        preserved: set[str] = set()
        for record in records:
            if not record.root_run_id:
                continue
            if record.status in {"preparing", "resolving", "verifying"}:
                preserved.add(record.root_run_id)
                continue
            actions = await self.repository.list_conflict_actions(record.conflict_id)
            if any(action.status in {"accepted", "running"} for action in actions):
                preserved.add(record.root_run_id)
        return preserved

    async def list_conflict_actions(self, conflict_id: str):
        return await self.repository.list_conflict_actions(conflict_id)

    async def conflict_projection(self, conflict_id: str) -> dict[str, object] | None:
        record = await self.repository.get_conflict_record(conflict_id)
        if record is None:
            return None
        return {
            "conflict_id": record.conflict_id,
            "task_id": record.integration_scope_id,
            "root_run_id": record.root_run_id,
            "original_operation_id": record.original_operation_id,
            "plan_task_id": record.plan_task_id,
            "status": record.status,
            "attempt": record.attempt,
            "conflict_files": list(record.conflict_files),
            "last_error_code": record.last_error_code,
            "last_error_message": record.last_error_message,
            "updated_at": record.updated_at,
        }

    async def apply_manual_action(
        self,
        conflict_id: str,
        action: str,
        action_id: str,
        *,
        target_status: str,
        git_facts: dict[str, object] | None = None,
    ) -> dict[str, object]:
        """Persist the database half of an already-serialized Git decision."""
        facts = git_facts or {}
        conflict = await self.repository.get_conflict_record(conflict_id)
        if conflict is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "conflict record not found")
        operation = await self.repository.get(conflict.original_operation_id)
        if operation is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "integration operation not found")
        if conflict.status in _CONFLICT_TERMINAL_STATUSES or operation.status in {
            "merged",
            "partial",
            "cancelled",
        }:
            if operation.status == target_status and conflict.status in _CONFLICT_TERMINAL_STATUSES:
                return {
                    "conflict": await self.conflict_projection(conflict_id),
                    "operation_status": operation.status,
                    "conflict_status": conflict.status,
                    "target_commit_after": str((git_facts or {}).get("target_commit_after") or conflict.target_commit),
                    "result_digest": operation.result_digest,
                }
            raise IntegrationError(
                ERROR_OPERATION_TERMINAL_MISMATCH,
                f"conflict {conflict_id} is already {conflict.status}",
            )
        if target_status not in {"merged", "partial", "cancelled"}:
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "invalid manual action result")

        now = utc_now()
        source_branch = str(facts.get("source_branch") or conflict.source_branch)
        source_commit = str(facts.get("source_commit") or conflict.source_commit)
        target_branch = str(facts.get("target_branch") or conflict.target_branch)
        target_before = str(facts.get("target_commit_before") or conflict.target_commit)
        target_after = str(facts.get("target_commit_after") or target_before)
        merge_base = str(facts.get("merge_base") or conflict.merge_base)
        resolution_attempt = conflict.attempt
        existing_resolution = (
            await self.repository.get_resolution_record(conflict_id, resolution_attempt)
            if target_status != "cancelled"
            else None
        )
        if existing_resolution is not None:
            # A failed Resolver attempt is immutable.  A later human decision
            # therefore gets its own monotonically increasing resolution
            # attempt instead of colliding with the failed Git fact row.
            if existing_resolution.status not in {"failed", "uncertain", "cancelled"}:
                raise IntegrationError(
                    ERROR_OPERATION_TERMINAL_MISMATCH,
                    f"conflict {conflict_id} already has a terminal resolution fact",
                )
            resolution_attempt += 1
        resolution_status = {
            "retry": "merged",
            "accept_source": "accepted_source",
            "accept_target": "accepted_target",
            "accept_current": "accepted_current",
            "accept_partial": "accepted_partial",
        }.get(action, action)

        if target_status != "cancelled":
            await self.save_resolution_integration_record(
                ResolutionIntegrationRecord(
                    resolution_record_id=str(uuid.uuid4()),
                    conflict_id=conflict.conflict_id,
                    original_operation_id=operation.integration_operation_id,
                    root_run_id=operation.root_run_id,
                    plan_task_id=operation.plan_task_id,
                    integration_scope_id=operation.integration_scope_id,
                    resolver_run_id=action_id,
                    resolver_workspace_id=str(facts.get("resolver_workspace_id") or "manual"),
                    attempt=resolution_attempt,
                    status=resolution_status,
                    source_branch=source_branch,
                    source_commit=source_commit,
                    target_branch=target_branch,
                    target_commit_before=target_before,
                    target_commit_after=target_after,
                    merge_base=merge_base,
                    conflict_files=list(conflict.conflict_files),
                    started_at=conflict.updated_at,
                    finished_at=now,
                    created_at=now,
                )
            )

        digest_payload = {
            "action": action,
            "action_id": action_id,
            "conflict_id": conflict_id,
            "target_status": target_status,
            "source_commit": source_commit,
            "target_before": target_before,
            "target_after": target_after,
        }
        digest = hashlib.sha256(
            json.dumps(digest_payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
        ).hexdigest()
        error_code = "" if target_status == "merged" else (
            "operation_cancelled" if target_status == "cancelled" else "manual_partial_accept"
        )
        error_message = "" if target_status == "merged" else (
            "cancelled by user"
            if target_status == "cancelled"
            else "accepted without integrating the source artifact"
        )
        updated_operation, _ = await self.repository.finalize_conflict_operation(
            operation.integration_operation_id,
            target_status,
            digest,
            error_code,
            error_message,
            now,
        )
        updated_conflict = await self.save_conflict_record(
            conflict.model_copy(
                update={
                    "status": "cancelled" if target_status == "cancelled" else "resolved",
                    "attempt": resolution_attempt,
                    "last_error_code": error_code,
                    "last_error_message": error_message,
                    "updated_at": now,
                }
            )
        )
        return {
            "conflict": await self.conflict_projection(conflict_id),
            "operation_status": updated_operation.status,
            "conflict_status": updated_conflict.status,
            "target_commit_after": target_after,
            "result_digest": digest,
        }

    async def finalize_manual_resolution(
        self,
        conflict_id: str,
        action_id: str,
    ) -> dict[str, object]:
        """Finish the database half of a manual action after a crash.

        ``apply_manual_action`` writes the immutable resolution fact before it
        promotes the original operation.  This method is the replay point for
        the process-death window between those two writes; it never performs
        Git work.
        """
        conflict = await self.repository.get_conflict_record(conflict_id)
        if conflict is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "conflict record not found")
        operation = await self.repository.get(conflict.original_operation_id)
        if operation is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "integration operation not found")
        resolution = await self.repository.get_resolution_record(conflict_id)
        if resolution is None or not resolution.status.startswith("accepted_"):
            raise IntegrationError(
                ERROR_INTEGRATION_RESULT_INVALID,
                "manual resolution facts are not durable",
            )
        if resolution.resolver_run_id != action_id:
            raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "manual resolution action does not match audit record")
        target_status = "merged" if resolution.status == "accepted_source" else "partial"
        digest_payload = {
            "action": resolution.status.removeprefix("accepted_"),
            "action_id": action_id,
            "conflict_id": conflict_id,
            "target_status": target_status,
            "source_commit": resolution.source_commit,
            "target_before": resolution.target_commit_before,
            "target_after": resolution.target_commit_after,
        }
        digest = hashlib.sha256(
            json.dumps(digest_payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
        ).hexdigest()
        if operation.status == "conflict":
            updated_operation, _ = await self.repository.finalize_conflict_operation(
                operation.integration_operation_id,
                target_status,
                digest,
                "" if target_status == "merged" else "manual_partial_accept",
                "" if target_status == "merged" else "accepted without integrating the source artifact",
                resolution.finished_at or utc_now(),
            )
        elif operation.status == target_status:
            updated_operation = operation
        else:
            raise IntegrationError(
                ERROR_OPERATION_TERMINAL_MISMATCH,
                f"operation {operation.integration_operation_id} is already {operation.status}",
            )
        if (
            conflict.status != "cancelled"
            and (
                conflict.status not in _CONFLICT_TERMINAL_STATUSES
                or conflict.attempt != resolution.attempt
            )
        ):
            conflict = await self.save_conflict_record(
                conflict.model_copy(
                    update={
                        "status": "resolved",
                        "attempt": resolution.attempt,
                        "updated_at": resolution.finished_at or utc_now(),
                        "last_error_code": "" if target_status == "merged" else "manual_partial_accept",
                        "last_error_message": "" if target_status == "merged" else "accepted without integrating the source artifact",
                    }
                )
            )
        return {
            "conflict": await self.conflict_projection(conflict_id),
            "operation_status": updated_operation.status,
            "conflict_status": conflict.status,
            "target_commit_after": resolution.target_commit_after,
            "result_digest": updated_operation.result_digest,
        }

    async def finalize_resolver_recovery(
        self,
        conflict_id: str,
        action_id: str,
    ) -> dict[str, object]:
        """Promote an immutable successful Resolver fact to the original op.

        Resolver Git facts are written before this promotion.  Keeping the
        promotion separate makes a crash between the two writes replayable:
        the next process can observe the already-completed resolution record
        and perform only this database transition, never Git again.
        """
        conflict = await self.repository.get_conflict_record(conflict_id)
        if conflict is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "conflict record not found")
        operation = await self.repository.get(conflict.original_operation_id)
        if operation is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "integration operation not found")
        resolution = await self.repository.get_resolution_record(conflict_id)
        if resolution is None or resolution.status != "merged":
            raise IntegrationError(
                ERROR_INTEGRATION_RESULT_INVALID,
                "successful Resolver Git facts are not durable",
            )

        digest_payload = {
            "action": "retry",
            "action_id": action_id,
            "conflict_id": conflict_id,
            "target_status": "merged",
            "source_commit": resolution.source_commit,
            "target_before": resolution.target_commit_before,
            "target_after": resolution.target_commit_after,
        }
        digest = hashlib.sha256(
            json.dumps(digest_payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
        ).hexdigest()
        if operation.status == "conflict":
            updated_operation, _ = await self.repository.finalize_conflict_operation(
                operation.integration_operation_id,
                "merged",
                digest,
                finished_at=resolution.finished_at or utc_now(),
            )
        elif operation.status == "merged":
            updated_operation = operation
        else:
            raise IntegrationError(
                ERROR_OPERATION_TERMINAL_MISMATCH,
                f"operation {operation.integration_operation_id} is already {operation.status}",
            )

        if conflict.status != "cancelled" and (
            conflict.status != "resolved" or conflict.attempt != resolution.attempt
        ):
            conflict = await self.save_conflict_record(
                conflict.model_copy(
                    update={
                        "status": "resolved",
                        "attempt": resolution.attempt,
                        "resolver_run_id": resolution.resolver_run_id,
                        "resolver_branch": resolution.source_branch,
                        "last_error_code": "",
                        "last_error_message": "",
                        "updated_at": resolution.finished_at or utc_now(),
                    }
                )
            )
        return {
            "conflict": await self.conflict_projection(conflict_id),
            "operation_status": updated_operation.status,
            "conflict_status": conflict.status,
            "target_commit_after": resolution.target_commit_after,
            "result_digest": updated_operation.result_digest,
        }

    async def get_resolved_conflict(self, operation_id: str) -> ConflictRecord | None:
        """Return a durable resolution marker for replay without rerunning a resolver."""
        conflict = await self.repository.get_conflict_for_operation(operation_id)
        resolution = await self.repository.get_merged_resolution_for_operation(operation_id)
        if conflict is None or resolution is None:
            return None
        if conflict.status != "resolved":
            # The record is written before the conflict projection. If a
            # process dies between those writes, converge the projection on
            # the next replay instead of starting another resolver attempt.
            conflict = await self.save_conflict_record(
                conflict.model_copy(
                    update={
                        "status": "resolved",
                        "resolver_run_id": resolution.resolver_run_id,
                        "resolver_branch": resolution.source_branch,
                        "resolver_session_id": conflict.resolver_session_id,
                        "last_error_code": "",
                        "last_error_message": "",
                    }
                )
            )
        return conflict

    async def list_resolution_attempts(self, conflict_id: str) -> list[ResolutionAttempt]:
        return await self.repository.list_resolution_attempts(conflict_id)

    async def save_resolution_attempt(self, attempt: ResolutionAttempt) -> ResolutionAttempt:
        conflict = await self.repository.get_conflict_record(attempt.conflict_id)
        if conflict is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "conflict record not found")
        if conflict.original_operation_id != attempt.original_operation_id:
            raise IntegrationError(
                ERROR_OPERATION_BINDING_MISMATCH,
                "resolution attempt does not belong to the conflict operation",
            )
        attempts = await self.repository.list_resolution_attempts(attempt.conflict_id)
        existing = next((item for item in attempts if item.attempt == attempt.attempt), None)
        if existing is not None:
            if (
                existing.original_operation_id != attempt.original_operation_id
                or (
                    existing.resolver_run_id
                    and attempt.resolver_run_id
                    and existing.resolver_run_id != attempt.resolver_run_id
                )
            ):
                raise IntegrationError(
                    ERROR_OPERATION_BINDING_MISMATCH,
                    "resolution attempt identity cannot be rebound",
                )
            allowed = _RESOLUTION_ATTEMPT_STATUS_TRANSITIONS.get(existing.status, set())
            if attempt.status not in allowed:
                code = (
                    ERROR_OPERATION_TERMINAL_MISMATCH
                    if existing.status in {"completed", "failed", "cancelled"}
                    else ERROR_INTEGRATION_RESULT_INVALID
                )
                raise IntegrationError(
                    code,
                    f"invalid resolution attempt transition {existing.status} -> {attempt.status}",
                )
            attempt = attempt.model_copy(
                update={
                    "created_at": existing.created_at,
                    "resolver_run_id": existing.resolver_run_id or attempt.resolver_run_id,
                    "resolver_workspace_id": existing.resolver_workspace_id or attempt.resolver_workspace_id,
                    "expected_target_commit": existing.expected_target_commit or attempt.expected_target_commit,
                    "resolver_commit": existing.resolver_commit or attempt.resolver_commit,
                    "error_code": existing.error_code or attempt.error_code,
                    "error_message": existing.error_message or attempt.error_message,
                    "finished_at": existing.finished_at or attempt.finished_at,
                }
            )
        else:
            expected_attempt = max((item.attempt for item in attempts), default=-1) + 1
            if attempt.attempt != expected_attempt:
                raise IntegrationError(
                    ERROR_OPERATION_STALE_ATTEMPT,
                    f"resolution attempt must be {expected_attempt}",
                )
        attempt = attempt.model_copy(
            update={
                "error_code": sanitize_error_text(attempt.error_code, limit=128),
                "error_message": sanitize_error_text(attempt.error_message),
            }
        )
        self._metric("resolver_attempt_total", status=attempt.status)
        return await self.repository.save_resolution_attempt(attempt)

    async def save_resolution_integration_record(
        self,
        record: ResolutionIntegrationRecord,
    ) -> ResolutionIntegrationRecord:
        operation = await self.repository.get(record.original_operation_id)
        if operation is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
        expected = {
            "root_run_id": operation.root_run_id,
            "plan_task_id": operation.plan_task_id,
            "integration_scope_id": operation.integration_scope_id,
        }
        for field, value in expected.items():
            if getattr(record, field) != value:
                raise IntegrationError(
                    ERROR_OPERATION_BINDING_MISMATCH,
                    f"resolution record {field} does not match operation",
                )
        manual_record = record.status.startswith("accepted_")
        if not record.conflict_id or (
            not manual_record and (not record.resolver_run_id or not record.resolver_workspace_id)
        ):
            raise IntegrationError(
                ERROR_OPERATION_BINDING_MISMATCH,
                "resolution record is missing resolver identity",
            )
        if record.status not in {
            "merged",
            "partial",
            "failed",
            ERROR_STATE_UNCERTAIN,
            "cancelled",
            "accepted_source",
            "accepted_target",
            "accepted_current",
            "accepted_partial",
        }:
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "invalid resolution record status")
        if any(not self._is_normalized_conflict_path(name) for name in record.conflict_files):
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "invalid conflict file path")
        if record.status in {"merged", "accepted_source"} and any(
            not value
            for value in (
                record.source_branch,
                record.source_commit,
                record.target_branch,
                record.target_commit_before,
                record.target_commit_after,
                record.merge_base,
            )
        ):
            raise IntegrationError(
                ERROR_INTEGRATION_RESULT_INVALID,
                "merged resolution record is missing authoritative Git facts",
            )
        if (
            record.status == "failed"
            and record.aborted
            and record.target_commit_before
            and record.target_commit_after
            and record.target_commit_before != record.target_commit_after
        ):
            raise IntegrationError(
                ERROR_OPERATION_BINDING_MISMATCH,
                "aborted resolution moved the target commit",
            )
        normalized = record.model_copy(
            update={
                "error_code": sanitize_error_text(record.error_code, limit=128),
                "error_message": sanitize_error_text(record.error_message),
            }
        )
        try:
            saved = await self.repository.save_resolution_integration_record(normalized)
        except KeyError as exc:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, str(exc)) from exc
        except IntegrationTerminalMismatchError as exc:
            raise IntegrationError(ERROR_OPERATION_TERMINAL_MISMATCH, str(exc)) from exc
        self._metric("resolution_integration_record_total", status=saved.status)
        return saved

    async def result_for_operation(self, operation_id: str) -> IntegrationResult | None:
        operation = await self.repository.get(operation_id)
        if not operation:
            return None
        record = await self.repository.get_git_record(operation_id)
        result_record = record
        if (
            operation.terminal
            and (
                not result_record
                or result_record.status != operation.status
                or result_record.status not in {"merged", "conflict", "failed", "partial"}
            )
            and operation.status in {"merged", "partial"}
        ):
            conflict = await self.repository.get_conflict_for_operation(operation_id)
            resolution = (
                await self.repository.get_resolution_record(conflict.conflict_id)
                if conflict is not None
                else None
            )
            if resolution is not None:
                result_record = GitIntegrationRecord(
                    record_id=resolution.resolution_record_id,
                    integration_operation_id=operation.integration_operation_id,
                    workspace_id=operation.workspace_id,
                    integration_scope_id=operation.integration_scope_id,
                    status=operation.status,
                    source_branch=resolution.source_branch,
                    source_commit=resolution.source_commit,
                    target_branch=resolution.target_branch,
                    target_commit_before=resolution.target_commit_before,
                    target_commit_after=resolution.target_commit_after,
                    merge_base=resolution.merge_base,
                    conflict_files=list(resolution.conflict_files),
                    aborted=resolution.aborted,
                    error_code=resolution.error_code,
                    error_message=resolution.error_message,
                    started_at=resolution.started_at,
                    finished_at=resolution.finished_at,
                    created_at=resolution.created_at,
                )
        if not operation.terminal or not result_record or result_record.status != operation.status:
            return None
        return IntegrationResult(
            version=2,
            run_id=operation.run_id,
            root_run_id=operation.root_run_id,
            parent_run_id=operation.parent_run_id,
            plan_task_id=operation.plan_task_id,
            integration_operation_id=operation.integration_operation_id,
            integration_scope_id=operation.integration_scope_id,
            workspace_id=operation.workspace_id,
            workspace_handle=operation.workspace_handle,
            session_id=operation.session_id,
            attempt=operation.attempt,
            status=operation.status,
            source_branch=result_record.source_branch,
            source_commit=result_record.source_commit,
            target_branch=result_record.target_branch,
            target_commit=result_record.target_commit_before,
            target_commit_after=result_record.target_commit_after,
            merge_base=result_record.merge_base,
            conflict_files=list(result_record.conflict_files),
            aborted=result_record.aborted,
            error_code=operation.error_code or result_record.error_code,
            error_message=operation.error_message or result_record.error_message,
            started_at=result_record.started_at,
            finished_at=result_record.finished_at,
            result_digest=operation.result_digest,
        )

    async def execute_operation(
        self,
        operation_id: str,
        *,
        capability: str,
        run_id: str,
        workspace_mgr: Any,
        resume_integrating: bool = False,
    ) -> IntegrationProjection:
        """Execute one capability-authorized merge against the task branch.

        This is the Phase 2 control-plane path.  It deliberately resolves the
        workspace from the durable operation and never accepts a caller path or
        branch name.
        """
        operation = await self.repository.get(operation_id)
        # Execute is capability-protected and must not reveal whether an
        # arbitrary operation ID exists to a caller holding a bad token.
        if operation is None:
            raise IntegrationError(ERROR_CAPABILITY_INVALID)
        if operation.terminal:
            return await self._replay_terminal_operation(operation, capability, run_id)
        try:
            await self.redeem_capability(
                capability,
                operation.integration_operation_id,
                run_id=run_id,
                workspace_id=operation.workspace_id,
            )
        except IntegrationError as exc:
            # A concurrent caller may have completed the Git side effect and
            # invalidated the capability between the initial read and the
            # single-use redemption. Re-read the operation and allow only the
            # already-authorized terminal projection; never retry Git.
            if exc.code != ERROR_CAPABILITY_INVALID:
                raise
            current = await self.repository.get(operation.integration_operation_id)
            if current is not None and current.terminal:
                return await self._replay_terminal_operation(current, capability, run_id)
            raise
        workspace = workspace_mgr.get(operation.workspace_id)
        workspace_status = getattr(getattr(workspace, "status", None), "value", getattr(workspace, "status", ""))
        if not workspace or workspace_status != "active":
            result = self._failed_result(operation, ERROR_WORKSPACE_MISSING, "workspace is missing or inactive")
            return await self.finalize_result(result, operation)  # type: ignore[return-value]
        if workspace.task_id != operation.integration_scope_id or workspace.session_id != operation.session_id:
            result = self._failed_result(operation, ERROR_OPERATION_BINDING_MISMATCH, "workspace scope/session mismatch")
            return await self.finalize_result(result, operation)  # type: ignore[return-value]
        operation, claimed = await self._claim_operation(operation_id)
        if operation.terminal:
            projection = await self._projection_for_terminal(operation)
            if projection:
                return projection
            raise IntegrationError(
                ERROR_OPERATION_TERMINAL_MISMATCH,
                "terminal operation has no Git record",
            )
        if not claimed and operation.status == "integrating" and not resume_integrating:
            # Another caller owns the Git side effect.  Wait for its durable
            # terminal result instead of invoking merge a second time.
            deadline = asyncio.get_running_loop().time() + 60.0
            while asyncio.get_running_loop().time() < deadline:
                current = await self.repository.get(operation_id)
                if current is None:
                    raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
                if current.terminal:
                    projection = await self._projection_for_terminal(current)
                    if projection:
                        return projection
                    raise IntegrationError(
                        ERROR_OPERATION_TERMINAL_MISMATCH,
                        "terminal operation has no Git record",
                    )
                await asyncio.sleep(0.05)
            raise IntegrationError(
                ERROR_STATE_UNCERTAIN,
                "another integration attempt did not reach a durable terminal state",
            )

        scope_lock = self._scope_lock(operation.integration_scope_id)
        wait_started = asyncio.get_running_loop().time()
        try:
            async with scope_lock:
                waited = asyncio.get_running_loop().time() - wait_started
                if waited > 0:
                    self._metric("integration_scope_lock_wait_seconds")
                current = await self.repository.get(operation_id)
                if current is None:
                    raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
                if current.terminal:
                    projection = await self._projection_for_terminal(current)
                    if projection:
                        return projection
                    raise IntegrationError(
                        ERROR_OPERATION_TERMINAL_MISMATCH,
                        "terminal operation has no Git record",
                    )
                merge = await self._merge_workspace(workspace_mgr, current)
                status = (
                    "merged"
                    if merge.success
                    else ("conflict" if merge.error_code == ERROR_MERGE_CONFLICT else "failed")
                )
                error_code = merge.error_code
                if not merge.success and error_code == ERROR_SOURCE_MISSING:
                    error_code = ERROR_SOURCE_MISSING
                elif not merge.success and status == "failed":
                    error_code = ERROR_MERGE_FAILED
                target_after = ""
                if merge.success:
                    # GitOps captures this while the integration file lock is
                    # still held. Reading it after releasing that lock could
                    # attribute a later same-scope merge to this operation.
                    target_after = merge.target_commit_after or await workspace_mgr.current_commit(
                        current.workspace_id
                    )
                elif merge.aborted:
                    target_after = merge.target_commit_after or merge.target_commit
                result = IntegrationResult(
                    version=2,
                    run_id=current.run_id,
                    root_run_id=current.root_run_id,
                    parent_run_id=current.parent_run_id,
                    plan_task_id=current.plan_task_id,
                    integration_operation_id=current.integration_operation_id,
                    integration_scope_id=current.integration_scope_id,
                    workspace_id=current.workspace_id,
                    workspace_handle=current.workspace_handle,
                    session_id=current.session_id,
                    attempt=current.attempt,
                    status=status,
                    source_branch=merge.source_branch,
                    source_commit=merge.source_commit,
                    target_branch=merge.target_branch,
                    target_commit=merge.target_commit,
                    target_commit_after=target_after,
                    merge_base=merge.merge_base,
                    conflict_files=list(merge.conflict_files),
                    aborted=merge.aborted,
                    error_code=error_code,
                    error_message=merge.error,
                )
                projection = await self.finalize_result(result, current)
        except asyncio.CancelledError:
            try:
                await self.mark_state_uncertain(
                    operation,
                    message="integration request was cancelled after Git execution was claimed",
                )
            except IntegrationError:
                pass
            raise
        except IntegrationError:
            # A successful Git command followed by a read/persistence failure
            # cannot be safely retried as a fresh merge. Mark the operation
            # uncertain so recovery probes Git instead of duplicating it.
            current = await self.repository.get(operation_id)
            if current is not None and current.status == "integrating":
                try:
                    await self.mark_state_uncertain(
                        current,
                        message="integration result could not be durably finalized",
                    )
                except IntegrationError:
                    pass
            raise
        except Exception as exc:
            current = await self.repository.get(operation_id)
            if current is not None and current.status == "integrating":
                try:
                    await self.mark_state_uncertain(
                        current,
                        message=f"Git execution outcome is uncertain: {sanitize_error_text(exc)}",
                    )
                except IntegrationError:
                    pass
            raise IntegrationError(
                ERROR_STATE_UNCERTAIN,
                "Git integration outcome is uncertain; recovery is required",
            ) from exc
        if projection is None:
            raise IntegrationError(ERROR_OPERATION_TERMINAL_MISMATCH, "operation was not finalized")
        # The in-process adapter exposes a count-shaped snapshot. Production
        # exporters can replace this observation with a real histogram.
        self._metric("integration_operation_duration_seconds", status=status)
        return projection

    async def _replay_terminal_operation(
        self,
        operation: IntegrationOperation,
        capability: str,
        run_id: str,
    ) -> IntegrationProjection:
        """Return a terminal projection without ever re-entering Git."""
        replay_authorized = bool(capability) and await self.repository.authorize_terminal_capability_replay(
            digest_token(capability),
            operation.integration_operation_id,
            run_id=run_id,
            workspace_id=operation.workspace_id,
            action="execute",
        )
        if not replay_authorized:
            raise IntegrationError(ERROR_CAPABILITY_INVALID)
        if operation.status == "cancelled":
            raise IntegrationError(ERROR_OPERATION_CANCELLED)
        projection = await self._projection_for_terminal(operation)
        if projection:
            return projection
        raise IntegrationError(ERROR_OPERATION_TERMINAL_MISMATCH, "terminal operation has no Git record")

    async def _projection_for_terminal(self, operation: IntegrationOperation) -> IntegrationProjection | None:
        record = await self.repository.get_git_record(operation.integration_operation_id)
        conflict = await self.repository.get_conflict_for_operation(operation.integration_operation_id)
        return IntegrationProjection(
            integration_operation_id=operation.integration_operation_id,
            plan_task_id=operation.plan_task_id,
            run_id=operation.run_id,
            attempt=operation.attempt,
            status=operation.status,
            conflict_id=conflict.conflict_id if conflict else "",
            conflict_files=(
                list(record.conflict_files)
                if record
                else (list(conflict.conflict_files) if conflict else [])
            ),
            error_code=operation.error_code,
            error_message=operation.error_message,
            sequence=operation.row_version,
            finished_at=operation.finished_at,
        )

    async def _save_conflict_from_result(
        self,
        operation: IntegrationOperation,
        result: IntegrationResult,
    ) -> ConflictRecord:
        conflict_id = self.conflict_id_for(
            operation.integration_operation_id,
            result.source_commit,
            result.target_commit,
            result.conflict_files,
        )
        return await self.repository.save_conflict_record(
            ConflictRecord(
                conflict_id=conflict_id,
                root_run_id=operation.root_run_id,
                original_operation_id=operation.integration_operation_id,
                plan_task_id=operation.plan_task_id,
                integration_scope_id=operation.integration_scope_id,
                workspace_id=operation.workspace_id,
                status="detected",
                attempt=operation.attempt,
                source_branch=result.source_branch,
                source_commit=result.source_commit,
                target_branch=result.target_branch,
                target_commit=result.target_commit,
                merge_base=result.merge_base,
                conflict_files=list(result.conflict_files),
                last_error_code=sanitize_error_text(result.error_code, limit=128),
                last_error_message=sanitize_error_text(result.error_message),
            )
        )

    @staticmethod
    def conflict_id_for(
        operation_id: str,
        source_commit: str,
        target_commit: str,
        conflict_files: list[str],
    ) -> str:
        return hashlib.sha256(
            "|".join(
                (
                    operation_id,
                    source_commit,
                    target_commit,
                    ",".join(sorted(conflict_files)),
                )
            ).encode("utf-8")
        ).hexdigest()[:20]

    @staticmethod
    def _failed_result(operation: IntegrationOperation, code: str, message: str) -> IntegrationResult:
        return IntegrationResult(
            version=2,
            run_id=operation.run_id,
            root_run_id=operation.root_run_id,
            parent_run_id=operation.parent_run_id,
            plan_task_id=operation.plan_task_id,
            integration_operation_id=operation.integration_operation_id,
            integration_scope_id=operation.integration_scope_id,
            workspace_id=operation.workspace_id,
            workspace_handle=operation.workspace_handle,
            session_id=operation.session_id,
            attempt=operation.attempt,
            status="failed",
            error_code=code,
            error_message=message,
        )

    @staticmethod
    def result_digest(result: IntegrationResult) -> str:
        # Timestamps and protocol version describe the transport/audit event,
        # not the immutable integration outcome. Excluding them lets a V1/V2
        # replay converge on the same terminal digest after a restart.
        payload = result.model_dump(
            mode="json",
            exclude={"result_digest", "version", "started_at", "finished_at"},
        )
        canonical = json.dumps(payload, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
        return hashlib.sha256(canonical.encode("utf-8")).hexdigest()

    @staticmethod
    def _is_normalized_conflict_path(name: object) -> bool:
        if not isinstance(name, str) or not name or len(name) > 1024:
            return False
        if "\x00" in name or "\\" in name or any(ord(char) < 32 for char in name):
            return False
        path = Path(name)
        if path.is_absolute() or ".." in path.parts or "." in path.parts:
            return False
        return path.as_posix() == name and name not in {".", ".."}

    @staticmethod
    def _require(actual: str, expected: str, field: str) -> None:
        if actual != expected:
            raise IntegrationBindingError(
                ERROR_OPERATION_BINDING_MISMATCH,
                f"{field} mismatch: expected {expected!r}, actual {actual!r}",
            )

    @staticmethod
    def _require_optional(actual: str, expected: str, field: str) -> None:
        if actual and actual != expected:
            raise IntegrationBindingError(
                ERROR_OPERATION_BINDING_MISMATCH,
                f"{field} mismatch: expected {expected!r}, actual {actual!r}",
            )
