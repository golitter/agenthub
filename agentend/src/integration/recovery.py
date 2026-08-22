from __future__ import annotations

import asyncio
import json
import logging
import uuid
from dataclasses import dataclass
from pathlib import Path

from src.clients.backend_client import BackendClient
from src.execution.repository import SQLiteRunRepository
from src.execution.supervisor import RunSupervisor
from src.generated.agent_run import AgentRunState
from src.integration.errors import (
    ERROR_INTEGRATION_RESULT_INVALID,
    ERROR_OPERATION_BINDING_MISMATCH,
    ERROR_OPERATION_NOT_FOUND,
    ERROR_OPERATION_STALE_ATTEMPT,
    ERROR_OPERATION_TERMINAL_MISMATCH,
    IntegrationError,
    sanitize_error_text,
)
from src.integration.models import ConflictActionRecord, ConflictRecord, utc_now
from src.integration.service import IntegrationService
from src.orchestrator.execution.engine import ExecutionEngine
from src.orchestrator.models import DispatchResult, TaskResult
from src.session.manager import SessionManager
from src.session.models import SessionState
from src.workspace.manager import WorkspaceManager

logger = logging.getLogger(__name__)

_ACTIONS = {
    "retry",
    "accept_current",
    "accept_source",
    "accept_target",
    "accept_partial",
    "cancel",
}
_CONFIRMATION_REQUIRED = {
    "accept_current",
    "accept_source",
    "accept_target",
    "accept_partial",
    "cancel",
}
_ACTIVE_CONFLICT_STATUSES = {"detected", "preparing", "resolving", "verifying", "retryable", "awaiting_user"}
_ACTIVE_ACTION_STATUSES = {"accepted", "running"}


@dataclass(frozen=True)
class ConflictActionInput:
    action: str
    task_id: str
    session_id: str
    root_run_id: str
    conflict_id: str
    expected_attempt: int
    confirmation: bool = False
    idempotency_key: str = ""
    resolver_agent: str = ""


class ConflictRecoveryCoordinator:
    """Durable coordinator for manual and post-restart conflict recovery.

    The coordinator owns only control-plane decisions.  Resolver Git work is
    still performed by ``ExecutionEngine``/``WorkspaceManager`` under the same
    per-task lock as ordinary IntegrationService execution.
    """

    def __init__(
        self,
        integration_service: IntegrationService,
        workspace_mgr: WorkspaceManager,
        backend_client: BackendClient,
        run_repository: SQLiteRunRepository,
        run_supervisor: RunSupervisor,
        session_mgr: SessionManager | None = None,
    ) -> None:
        self.integration_service = integration_service
        self.workspace_mgr = workspace_mgr
        self.backend_client = backend_client
        self.run_repository = run_repository
        self.run_supervisor = run_supervisor
        self.session_mgr = session_mgr

    def _set_session_state(self, session_id: str, state: SessionState, agent_type: str = "orchestrator") -> None:
        if self.session_mgr is None or not session_id:
            return
        session = self.session_mgr.get(session_id)
        if session is None:
            try:
                session = self.session_mgr.create(agent_type=agent_type, session_id=session_id)
            except ValueError:
                session = self.session_mgr.get(session_id)
        if session is None or session.state == state:
            return
        try:
            if state == SessionState.RESOLVING and session.state == SessionState.AWAITING_RESOLUTION:
                self.session_mgr.update_state(session_id, SessionState.RESOLVING)
            elif state == SessionState.AWAITING_RESOLUTION and session.state in {
                SessionState.RUNNING,
                SessionState.RESOLVING,
            }:
                self.session_mgr.update_state(session_id, state)
            elif state == SessionState.COMPLETED and session.state in {
                SessionState.AWAITING_RESOLUTION,
                SessionState.RESOLVING,
            }:
                self.session_mgr.update_state(session_id, state)
            elif state == SessionState.INTERRUPTED and session.state in {
                SessionState.AWAITING_RESOLUTION,
                SessionState.RESOLVING,
            }:
                self.session_mgr.update_state(session_id, state)
            elif state == SessionState.RESOLVING and session.state == SessionState.IDLE:
                self.session_mgr.update_state(session_id, SessionState.RUNNING)
                self.session_mgr.update_state(session_id, state)
            elif state == SessionState.COMPLETED and session.state == SessionState.IDLE:
                self.session_mgr.update_state(session_id, SessionState.RUNNING)
                self.session_mgr.update_state(session_id, state)
        except ValueError:
            logger.debug("session state projection skipped session=%s state=%s", session_id, state.value)

    @staticmethod
    def _action_text(action: object) -> str:
        return str(getattr(action, "value", action) or "").strip()

    @staticmethod
    def _response_from_record(record: ConflictActionRecord) -> dict[str, object] | None:
        if not record.result_json:
            return None
        try:
            value = json.loads(record.result_json)
        except (TypeError, ValueError):
            return None
        return value if isinstance(value, dict) else None

    async def _operation_and_root(self, conflict: ConflictRecord):
        operation = await self.integration_service.repository.get(conflict.original_operation_id)
        if operation is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "integration operation not found")
        root = await self.run_repository.get(conflict.root_run_id or operation.root_run_id)
        if root is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "root Run not found")
        return operation, root

    async def _validate_request(self, request: ConflictActionInput) -> tuple[ConflictRecord, object, object]:
        action = self._action_text(request.action)
        if action not in _ACTIONS:
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "unsupported conflict action")
        if not request.task_id or not request.session_id or not request.root_run_id or not request.conflict_id:
            raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "conflict action identity is incomplete")
        if request.expected_attempt < 0:
            raise IntegrationError(ERROR_OPERATION_STALE_ATTEMPT, "expected_attempt must be non-negative")
        if action in _CONFIRMATION_REQUIRED and not request.confirmation:
            raise IntegrationError(
                ERROR_INTEGRATION_RESULT_INVALID,
                "explicit confirmation is required for this conflict action",
            )
        conflict = await self.integration_service.get_conflict_record(request.conflict_id)
        if conflict is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "conflict record not found")
        operation, root = await self._operation_and_root(conflict)
        if request.task_id != conflict.integration_scope_id:
            raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "task_id does not match conflict scope")
        if request.root_run_id != (conflict.root_run_id or operation.root_run_id):
            raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "root_run_id does not match conflict")
        if request.session_id not in {root.spec.session_id, operation.session_id}:
            raise IntegrationError(ERROR_OPERATION_BINDING_MISMATCH, "session_id does not own the paused Run")
        if request.expected_attempt != conflict.attempt:
            raise IntegrationError(
                ERROR_OPERATION_STALE_ATTEMPT,
                f"expected_attempt must be {conflict.attempt}",
            )
        if conflict.status not in _ACTIVE_CONFLICT_STATUSES and action != "cancel":
            raise IntegrationError(
                ERROR_OPERATION_TERMINAL_MISMATCH,
                f"conflict is already {conflict.status}",
            )
        if operation.status == "cancelled" and action != "cancel":
            raise IntegrationError(ERROR_OPERATION_TERMINAL_MISMATCH, "integration operation is already cancelled")
        if root.state != AgentRunState.AWAITING_RESOLUTION:
            raise IntegrationError(
                ERROR_OPERATION_TERMINAL_MISMATCH,
                f"root Run is {root.state.value}; it is not awaiting conflict resolution",
            )
        return conflict, operation, root

    async def conflict_projection(self, conflict_id: str) -> dict[str, object] | None:
        return await self.integration_service.conflict_projection(conflict_id)

    async def handle_action(self, request: ConflictActionInput) -> dict[str, object]:
        action = self._action_text(request.action)
        conflict = await self.integration_service.get_conflict_record(request.conflict_id)
        if conflict is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "conflict record not found")

        idempotency_key = (request.idempotency_key or "").strip() or str(uuid.uuid4())
        if len(idempotency_key) > 128 or "\x00" in idempotency_key:
            raise IntegrationError(ERROR_INTEGRATION_RESULT_INVALID, "invalid idempotency_key")
        existing = await self.integration_service.repository.get_conflict_action_by_key(
            request.conflict_id,
            idempotency_key,
        )
        if existing is not None:
            if action in _CONFIRMATION_REQUIRED and not request.confirmation:
                raise IntegrationError(
                    ERROR_INTEGRATION_RESULT_INVALID,
                    "explicit confirmation is required for this conflict action",
                )
            if (
                existing.action != action
                or existing.task_id != request.task_id
                or existing.session_id != request.session_id
                or existing.root_run_id != request.root_run_id
                or existing.expected_attempt != request.expected_attempt
            ):
                raise IntegrationError(
                    ERROR_OPERATION_BINDING_MISMATCH,
                    "idempotency key is bound to a different conflict action identity",
                )
            if existing.status in _ACTIVE_ACTION_STATUSES:
                root_for_recovery = await self.run_repository.get(existing.root_run_id)
                if root_for_recovery is not None and root_for_recovery.state == AgentRunState.AWAITING_RESOLUTION:
                    await self._start_existing_action(existing, self._request_for_action(existing))
                    refreshed = await self.integration_service.repository.get_conflict_action_by_key(
                        request.conflict_id,
                        idempotency_key,
                    )
                    if refreshed is not None:
                        existing = refreshed
            stored = self._response_from_record(existing)
            if stored is not None:
                return stored
            projection = await self.integration_service.conflict_projection(request.conflict_id)
            if existing.status in _ACTIVE_ACTION_STATUSES:
                return self._accepted_response(existing, projection, "action is already in progress")
            return self._terminal_action_response(existing, projection)

        conflict, operation, root = await self._validate_request(
            request.__class__(
                action=action,
                task_id=request.task_id,
                session_id=request.session_id,
                root_run_id=request.root_run_id,
                conflict_id=request.conflict_id,
                expected_attempt=request.expected_attempt,
                confirmation=request.confirmation,
                idempotency_key=idempotency_key,
                resolver_agent=request.resolver_agent,
            )
        )
        action_record = ConflictActionRecord(
            action_id=str(uuid.uuid4()),
            conflict_id=conflict.conflict_id,
            action=action,
            task_id=request.task_id,
            session_id=request.session_id,
            root_run_id=root.spec.root_run_id,
            expected_attempt=request.expected_attempt,
            idempotency_key=idempotency_key,
            status="running",
        )
        action_record, created = await self.integration_service.repository.create_conflict_action(action_record)
        if not created:
            stored = self._response_from_record(action_record)
            if stored is not None:
                return stored
            projection = await self.integration_service.conflict_projection(request.conflict_id)
            if action_record.status in _ACTIVE_ACTION_STATUSES:
                return self._accepted_response(action_record, projection, "action is already in progress")
            return self._terminal_action_response(action_record, projection)

        if action == "cancel":
            try:
                result = await self._execute_cancel(action_record, conflict, operation, root)
            except IntegrationError as exc:
                await self._finish_action(action_record, "failed", error_code=exc.code, error_message=exc.message)
                raise
            await self._finish_action(action_record, "completed", result=result)
            return result

        projection = await self.integration_service.conflict_projection(conflict.conflict_id)
        accepted = self._accepted_response(action_record, projection, "conflict recovery accepted")
        await self.integration_service.repository.finish_conflict_action(
            action_record.action_id,
            status="running",
            result_json=json.dumps(accepted, separators=(",", ":")),
        )
        resumed, started = await self.run_supervisor.resume(
            root.spec.run_id,
            lambda emit: self._execute_resumed_action(action_record, request, emit),
        )
        if not started:
            message = "root Run could not be resumed; it may already have another recovery action"
            await self._finish_action(
                action_record,
                "failed",
                error_code=ERROR_OPERATION_TERMINAL_MISMATCH,
                error_message=message,
            )
            raise IntegrationError(ERROR_OPERATION_TERMINAL_MISMATCH, message)
        self._set_session_state(root.spec.session_id, SessionState.RESOLVING, root.spec.agent_type)
        accepted["run_id"] = root.spec.run_id
        accepted["root_run_id"] = root.spec.root_run_id
        return accepted

    @staticmethod
    def _accepted_response(
        action: ConflictActionRecord,
        projection: dict[str, object] | None,
        message: str,
    ) -> dict[str, object]:
        projection = projection or {}
        return {
            "action_id": action.action_id,
            "conflict_id": action.conflict_id,
            "action": action.action,
            "accepted": True,
            "status": "accepted",
            "conflict_status": projection.get("status", ""),
            "operation_status": "conflict",
            "run_id": action.root_run_id,
            "root_run_id": action.root_run_id,
            "attempt": action.expected_attempt,
            "message": message,
        }

    @staticmethod
    def _terminal_action_response(
        action: ConflictActionRecord,
        projection: dict[str, object] | None,
    ) -> dict[str, object]:
        projection = projection or {}
        return {
            "action_id": action.action_id,
            "conflict_id": action.conflict_id,
            "action": action.action,
            "accepted": False,
            "status": action.status,
            "conflict_status": projection.get("status", ""),
            "operation_status": "conflict",
            "run_id": action.root_run_id,
            "root_run_id": action.root_run_id,
            "attempt": action.expected_attempt,
            "message": action.error_message or f"conflict action is {action.status}",
        }

    @staticmethod
    def _request_for_action(action: ConflictActionRecord) -> ConflictActionInput:
        """Reconstruct the authenticated action envelope from its audit row."""
        return ConflictActionInput(
            action=action.action,
            task_id=action.task_id,
            session_id=action.session_id,
            root_run_id=action.root_run_id,
            conflict_id=action.conflict_id,
            expected_attempt=action.expected_attempt,
            idempotency_key=action.idempotency_key,
        )

    async def _finish_action(
        self,
        action: ConflictActionRecord,
        status: str,
        *,
        result: dict[str, object] | None = None,
        error_code: str = "",
        error_message: str = "",
    ) -> None:
        await self.integration_service.repository.finish_conflict_action(
            action.action_id,
            status=status,
            result_json=json.dumps(result, separators=(",", ":")) if result is not None else "",
            error_code=sanitize_error_text(error_code, limit=128),
            error_message=sanitize_error_text(error_message),
        )

    async def _execute_cancel(self, action: ConflictActionRecord, conflict, operation, root) -> dict[str, object]:
        if operation.status == "cancelled":
            result = {
                "conflict_status": "cancelled",
                "operation_status": "cancelled",
            }
        else:
            result = await self.integration_service.apply_manual_action(
                conflict.conflict_id,
                "cancel",
                action.action_id,
                target_status="cancelled",
            )
        await self.run_supervisor.cancel(root.spec.run_id)
        self._set_session_state(root.spec.session_id, SessionState.INTERRUPTED, root.spec.agent_type)
        return {
            "action_id": action.action_id,
            "conflict_id": conflict.conflict_id,
            "action": "cancel",
            "accepted": True,
            "status": "completed",
            "conflict_status": result["conflict_status"],
            "operation_status": result["operation_status"],
            "run_id": root.spec.run_id,
            "root_run_id": root.spec.root_run_id,
            "attempt": conflict.attempt,
            "message": "conflict operation cancelled",
        }

    async def _execute_resumed_action(self, action: ConflictActionRecord, request: ConflictActionInput, emit) -> None:
        conflict = await self.integration_service.get_conflict_record(action.conflict_id)
        if conflict is None:
            await self._pause_after_failure(action, "conflict record disappeared", emit)
            return
        try:
            if action.action == "retry":
                await self._execute_retry(action, request, conflict, emit)
            else:
                await self._execute_accept(action, conflict, emit)
        except asyncio.CancelledError:
            await self._finish_action(
                action,
                "cancelled",
                error_code="operation_cancelled",
                error_message="conflict action cancelled with its root Run",
            )
            raise
        except Exception as exc:
            await self._pause_after_failure(action, str(exc) or exc.__class__.__name__, emit)

    async def _execute_accept(self, action: ConflictActionRecord, conflict: ConflictRecord, emit) -> None:
        operation = await self.integration_service.repository.get(conflict.original_operation_id)
        resolution = await self.integration_service.repository.get_resolution_record(conflict.conflict_id)
        event_identity = {
            "task_id": conflict.plan_task_id or conflict.integration_scope_id,
            "plan_task_id": conflict.plan_task_id,
            "integration_operation_id": operation.integration_operation_id if operation is not None else conflict.original_operation_id,
            "run_id": operation.run_id if operation is not None else action.root_run_id,
            "root_run_id": operation.root_run_id if operation is not None else action.root_run_id,
        }
        if (
            operation is not None
            and operation.status == "conflict"
            and resolution is not None
            and resolution.resolver_run_id == action.action_id
            and resolution.status.startswith("accepted_")
        ):
            result = await self.integration_service.finalize_manual_resolution(
                conflict.conflict_id,
                action.action_id,
            )
            target_status = str(result["operation_status"])
            result_conflict = result.get("conflict")
            result_attempt = (
                result_conflict.get("attempt", conflict.attempt)
                if isinstance(result_conflict, dict)
                else conflict.attempt
            )
            await emit(
                {
                    "type": "resolution_completed",
                    "content": {
                        **event_identity,
                        "conflict_id": conflict.conflict_id,
                        "attempt": result_attempt,
                        "status": target_status,
                        "action": action.action,
                    },
                }
            )
            await emit(
                {
                    "type": "runtime_completed",
                    "content": {
                        **event_identity,
                        "conflict_id": conflict.conflict_id,
                        "status": target_status,
                        "integration_status": target_status,
                        "success": target_status == "merged",
                    },
                }
            )
            await emit(
                {
                    "type": "done",
                    "content": {
                        **event_identity,
                        "status": "completed",
                        "integration_status": target_status,
                        "conflict_id": conflict.conflict_id,
                    },
                }
            )
            await self._finish_action(
                action,
                "completed",
                result=self._final_response(action, result, "manual conflict decision replayed"),
            )
            root = await self.run_repository.get(action.root_run_id)
            self._set_session_state(
                root.spec.session_id if root is not None else action.session_id,
                SessionState.COMPLETED,
                operation.session_id,
            )
            return
        if operation is not None and operation.status in {"merged", "partial"}:
            if conflict.status not in {"resolved", "cancelled"}:
                conflict = await self.integration_service.save_conflict_record(
                    conflict.model_copy(update={"status": "resolved", "updated_at": utc_now()})
                )
                result = {
                "conflict_status": conflict.status,
                "operation_status": operation.status,
            }
            await emit(
                {
                    "type": "runtime_completed",
                    "content": {
                        **event_identity,
                        "conflict_id": conflict.conflict_id,
                        "status": operation.status,
                        "integration_status": operation.status,
                        "success": operation.status == "merged",
                    },
                }
            )
            await emit(
                {
                    "type": "done",
                    "content": {
                        **event_identity,
                        "status": "completed",
                        "integration_status": operation.status,
                        "conflict_id": conflict.conflict_id,
                    },
                }
            )
            await self._finish_action(action, "completed", result=self._final_response(action, result, "conflict decision replayed"))
            root = await self.run_repository.get(action.root_run_id)
            self._set_session_state(
                root.spec.session_id if root is not None else action.session_id,
                SessionState.COMPLETED,
                operation.session_id,
            )
            return
        facts: dict[str, object] = {}
        if action.action == "accept_source":
            merge = await self.workspace_mgr.adopt_source(
                conflict.workspace_id,
                conflict.source_branch,
                expected_source_commit=conflict.source_commit,
                expected_target_commit=conflict.target_commit,
            )
            if not merge.success:
                raise IntegrationError(merge.error_code or "accept_source_failed", merge.error or "source was not adopted")
            facts = {
                "source_branch": merge.source_branch,
                "source_commit": merge.source_commit,
                "target_branch": merge.target_branch,
                "target_commit_before": merge.target_commit,
                "target_commit_after": merge.target_commit_after,
                "merge_base": merge.merge_base,
            }
            target_status = "merged"
        else:
            current_target = await self.workspace_mgr.current_task_commit(conflict.workspace_id)
            if conflict.target_commit and current_target and current_target != conflict.target_commit:
                raise IntegrationError("target_moved", "task branch moved after the conflict was recorded")
            facts = {
                "target_commit_after": current_target or conflict.target_commit,
            }
            target_status = "partial"

        result = await self.integration_service.apply_manual_action(
            conflict.conflict_id,
            action.action,
            action.action_id,
            target_status=target_status,
            git_facts=facts,
        )
        result_conflict = result.get("conflict")
        result_attempt = (
            result_conflict.get("attempt", conflict.attempt)
            if isinstance(result_conflict, dict)
            else conflict.attempt
        )
        await emit(
            {
                "type": "resolution_completed",
                "content": {
                    **event_identity,
                    "conflict_id": conflict.conflict_id,
                    "attempt": result_attempt,
                    "status": target_status,
                    "action": action.action,
                },
            }
        )
        await emit(
            {
                "type": "runtime_completed",
                "content": {
                    **event_identity,
                    "conflict_id": conflict.conflict_id,
                    "status": target_status,
                    "integration_status": target_status,
                    "success": target_status == "merged",
                },
            }
        )
        await emit(
            {
                "type": "done",
                "content": {
                    **event_identity,
                    "status": "completed",
                    "integration_status": target_status,
                    "conflict_id": conflict.conflict_id,
                },
            }
        )
        root = await self.run_repository.get(action.root_run_id)
        if root is not None:
            self._set_session_state(root.spec.session_id, SessionState.COMPLETED, root.spec.agent_type)
        response = self._final_response(action, result, "conflict decision applied")
        await self._finish_action(action, "completed", result=response)

    async def _execute_retry(self, action: ConflictActionRecord, request: ConflictActionInput, conflict: ConflictRecord, emit) -> None:
        operation = await self.integration_service.repository.get(conflict.original_operation_id)
        root = await self.run_repository.get(conflict.root_run_id)
        workspace = self.workspace_mgr.get(conflict.workspace_id)
        if operation is None or root is None or workspace is None:
            raise IntegrationError(ERROR_OPERATION_NOT_FOUND, "resolver recovery binding is unavailable")
        event_identity = {
            "task_id": conflict.plan_task_id or conflict.integration_scope_id,
            "plan_task_id": conflict.plan_task_id,
            "integration_operation_id": operation.integration_operation_id,
            "run_id": operation.run_id,
            "root_run_id": operation.root_run_id,
        }
        resolver = self._select_resolver(conflict, request.resolver_agent)
        if resolver is None:
            raise IntegrationError("resolver_agent_unavailable", "no active resolver Agent is available")
        resolver_id, resolver_session_id, resolver_type = resolver
        dispatch = DispatchResult(
            task_id=conflict.plan_task_id or conflict.integration_scope_id,
            attempt=conflict.attempt + 1,
            agent=resolver_id,
            agent_type=resolver_type,
            real_session_id=resolver_session_id,
            mention=f"@{resolver_id}",
            content=(
                "Continue the persisted conflict recovery for the task artifact. "
                f"Conflict {conflict.conflict_id}: {conflict.last_error_message or 'manual retry'}"
            ),
            plan_task_id=conflict.plan_task_id or conflict.integration_scope_id,
            integration_operation_id=operation.integration_operation_id,
            workspace_handle=conflict.workspace_id,
            integration_scope_id=conflict.integration_scope_id,
        )
        task_result = TaskResult(
            task_id=dispatch.task_id,
            root_task_id=dispatch.task_id,
            agent=resolver_id,
            attempt=conflict.attempt + 1,
            execution_status="completed",
            integration_status="conflict",
            content=dispatch.content,
            run_id=operation.run_id,
            plan_task_id=dispatch.plan_task_id,
            integration_operation_id=operation.integration_operation_id,
            integration_scope_id=operation.integration_scope_id,
            workspace_id=operation.workspace_id,
            conflict_files=list(conflict.conflict_files),
            source_branch=conflict.source_branch,
            source_commit=conflict.source_commit,
            target_branch=conflict.target_branch,
            target_commit=conflict.target_commit,
            merge_base=conflict.merge_base,
            success=False,
        )
        repo_root = Path(workspace.repo_path).resolve()
        shared_dir = repo_root.parent / "worktrees" / conflict.integration_scope_id / "shared" / ".agent"
        agents = self._resolver_agent_configs(conflict, resolver)
        engine = ExecutionEngine(
            self.backend_client,
            workspace_mgr=self.workspace_mgr,
            repo_path=str(repo_root),
            task_id=conflict.integration_scope_id,
            shared_dir=str(shared_dir),
            cwd=str(repo_root.parent / "worktrees" / conflict.integration_scope_id / "task-base"),
            root_run_id=root.spec.root_run_id,
            parent_run_id=root.spec.parent_run_id or root.spec.root_run_id,
            current_run_id=root.spec.run_id,
            budget=root.spec.budget.model_dump(mode="json"),
            agents=agents,
            integration_service=self.integration_service,
        )
        recovered, events = await engine._attempt_conflict_recovery(
            dispatch,
            task_result,
            start_attempt=conflict.attempt + 1,
            force_manual=True,
        )
        for event in events:
            await emit(event.model_dump(mode="json"))
        if recovered.integration_status == "merged":
            result = await self.integration_service.finalize_resolver_recovery(
                conflict.conflict_id,
                action.action_id,
            )
            await emit(
                {
                    "type": "runtime_completed",
                    "content": {
                        **event_identity,
                        "conflict_id": conflict.conflict_id,
                        "status": "merged",
                        "integration_status": "merged",
                        "success": True,
                    },
                }
            )
            await emit(
                {
                    "type": "done",
                    "content": {
                        **event_identity,
                        "status": "completed",
                        "integration_status": "merged",
                        "conflict_id": conflict.conflict_id,
                    },
                }
            )
            self._set_session_state(root.spec.session_id, SessionState.COMPLETED, root.spec.agent_type)
            await self._finish_action(action, "completed", result=self._final_response(action, result, "Resolver recovery completed"))
            return
        await self._pause_after_failure(
            action,
            recovered.error_message or "Resolver recovery requires another manual decision",
            emit,
            error_code=recovered.error_code or "conflict_resolver_exhausted",
        )

    async def _pause_after_failure(
        self,
        action: ConflictActionRecord,
        message: str,
        emit,
        *,
        error_code: str = "resolver_failed",
    ) -> None:
        conflict = await self.integration_service.get_conflict_record(action.conflict_id)
        operation = (
            await self.integration_service.repository.get(conflict.original_operation_id)
            if conflict is not None
            else None
        )
        event_identity = {
            "task_id": conflict.plan_task_id or conflict.integration_scope_id if conflict is not None else "",
            "plan_task_id": conflict.plan_task_id if conflict is not None else "",
            "integration_operation_id": operation.integration_operation_id if operation is not None else "",
            "run_id": operation.run_id if operation is not None else action.root_run_id,
            "root_run_id": action.root_run_id,
        }
        if conflict is not None and conflict.status not in {"awaiting_user", "resolved", "cancelled"}:
            try:
                conflict = await self.integration_service.save_conflict_record(
                    conflict.model_copy(
                        update={
                            "status": "awaiting_user",
                            "last_error_code": error_code,
                            "last_error_message": sanitize_error_text(message),
                            "updated_at": utc_now(),
                        }
                    )
                )
            except Exception:
                logger.warning("failed to persist manual conflict pause %s", action.conflict_id, exc_info=True)
        await emit(
            {
                "type": "resolution_failed",
                "content": {
                    **event_identity,
                    "conflict_id": action.conflict_id,
                    "status": "awaiting_user",
                    "retrying": False,
                    "error_code": error_code,
                    "error_message": sanitize_error_text(message),
                },
            }
        )
        await emit(
            {
                "type": "orchestrator_paused",
                "content": {
                    **event_identity,
                    "root_run_id": action.root_run_id,
                    "run_id": action.root_run_id,
                    "conflict_id": action.conflict_id,
                    "status": "awaiting_user",
                    "reason": sanitize_error_text(message),
                },
            }
        )
        root = await self.run_repository.get(action.root_run_id)
        if root is not None:
            self._set_session_state(root.spec.session_id, SessionState.AWAITING_RESOLUTION, root.spec.agent_type)
        await self._finish_action(
            action,
            "awaiting_user",
            error_code=error_code,
            error_message=message,
        )

    @staticmethod
    def _final_response(action: ConflictActionRecord, result: dict[str, object], message: str) -> dict[str, object]:
        conflict = result.get("conflict")
        attempt = conflict.get("attempt", action.expected_attempt) if isinstance(conflict, dict) else action.expected_attempt
        return {
            "action_id": action.action_id,
            "conflict_id": action.conflict_id,
            "action": action.action,
            "accepted": True,
            "status": "completed",
            "conflict_status": result.get("conflict_status", "resolved"),
            "operation_status": result.get("operation_status", "merged"),
            "run_id": action.root_run_id,
            "root_run_id": action.root_run_id,
            "attempt": attempt,
            "message": message,
        }

    def _select_resolver(self, conflict: ConflictRecord, requested_agent: str) -> tuple[str, str, str] | None:
        candidates = []
        for workspace in self.workspace_mgr.list():
            if workspace.status.value != "active" or workspace.task_id != conflict.integration_scope_id:
                continue
            if not workspace.session_id or workspace.agent_type is None:
                continue
            agent_type = getattr(workspace.agent_type, "value", workspace.agent_type)
            if agent_type == "orchestrator":
                continue
            candidates.append((workspace.agent_name or workspace.session_id, workspace.session_id, str(agent_type)))
        if requested_agent:
            for candidate in candidates:
                if candidate[0] == requested_agent or candidate[1] == requested_agent:
                    return candidate
        if conflict.resolver_session_id:
            for candidate in candidates:
                if candidate[1] == conflict.resolver_session_id:
                    return candidate
        if candidates:
            return candidates[0]
        return None

    def _resolver_agent_configs(self, conflict: ConflictRecord, selected: tuple[str, str, str]) -> list[dict]:
        configs: list[dict] = []
        seen: set[str] = set()
        for workspace in self.workspace_mgr.list():
            if workspace.status.value != "active" or workspace.task_id != conflict.integration_scope_id:
                continue
            if not workspace.session_id or workspace.agent_type is None:
                continue
            agent_type = getattr(workspace.agent_type, "value", workspace.agent_type)
            if agent_type == "orchestrator":
                continue
            item = {
                "id": workspace.agent_name or workspace.session_id,
                "session_id": workspace.session_id,
                "type": str(agent_type),
            }
            key = item["id"] + "\x00" + item["session_id"]
            if key not in seen:
                configs.append(item)
                seen.add(key)
        if selected[1] not in {item["session_id"] for item in configs}:
            configs.insert(0, {"id": selected[0], "session_id": selected[1], "type": selected[2]})
        return configs

    async def recover(self) -> None:
        """Continue resolver attempts that were interrupted by process death."""
        # Read all records because an action can be durable while its conflict
        # has already reached a terminal state: a process may die after the
        # operation promotion but before the action audit row is finished.
        # ``recoverable_root_run_ids`` deliberately preserves that root, so the
        # action must be replayed here as well.
        records = await self.integration_service.list_conflict_records()
        for conflict in records:
            try:
                was_inflight = conflict.status in {"preparing", "resolving", "verifying"}
                await self._mark_interrupted(conflict)
                root = await self.run_repository.get(conflict.root_run_id)
                if root is None or root.state != AgentRunState.AWAITING_RESOLUTION:
                    continue
                active_actions = [
                    item
                    for item in await self.integration_service.list_conflict_actions(conflict.conflict_id)
                    if item.status in _ACTIVE_ACTION_STATUSES
                ]
                if active_actions:
                    action = active_actions[-1]
                    await self._start_existing_action(action, self._request_for_action(action))
                    continue
                # ``retryable`` and ``awaiting_user`` are deliberate pauses.
                # They must remain visible for a human decision; only an
                # attempt that was actually in flight at process death gets
                # the automatic continuation below.
                if not was_inflight and conflict.status not in _ACTIVE_CONFLICT_STATUSES:
                    continue
                operation = await self.integration_service.repository.get(conflict.original_operation_id)
                if operation is None:
                    continue
                request = ConflictActionInput(
                    action="retry",
                    task_id=conflict.integration_scope_id,
                    session_id=root.spec.session_id,
                    root_run_id=root.spec.root_run_id,
                    conflict_id=conflict.conflict_id,
                    expected_attempt=conflict.attempt,
                    idempotency_key=f"recovery:{conflict.conflict_id}:{conflict.attempt}",
                    resolver_agent=conflict.resolver_agent,
                )
                await self.handle_action(request)
            except Exception:
                logger.warning("failed to resume conflict %s after restart", conflict.conflict_id, exc_info=True)

    async def _start_existing_action(self, action: ConflictActionRecord, request: ConflictActionInput) -> None:
        root = await self.run_repository.get(action.root_run_id)
        if root is None:
            return
        if action.action == "cancel":
            conflict = await self.integration_service.get_conflict_record(action.conflict_id)
            if conflict is None:
                await self._finish_action(
                    action,
                    "failed",
                    error_code=ERROR_OPERATION_NOT_FOUND,
                    error_message="conflict record not found during recovery",
                )
                return
            try:
                operation, bound_root = await self._operation_and_root(conflict)
                result = await self._execute_cancel(action, conflict, operation, bound_root)
            except IntegrationError as exc:
                await self._finish_action(
                    action,
                    "failed",
                    error_code=exc.code,
                    error_message=exc.message,
                )
                return
            except Exception as exc:
                await self._finish_action(
                    action,
                    "failed",
                    error_code=ERROR_OPERATION_TERMINAL_MISMATCH,
                    error_message=str(exc) or exc.__class__.__name__,
                )
                return
            await self._finish_action(action, "completed", result=result)
            return
        self._set_session_state(root.spec.session_id, SessionState.RESOLVING, root.spec.agent_type)
        await self.run_supervisor.resume(
            root.spec.run_id,
            lambda emit: self._execute_resumed_action(action, request, emit),
        )

    async def _mark_interrupted(self, conflict: ConflictRecord) -> None:
        now = utc_now()
        attempts = await self.integration_service.list_resolution_attempts(conflict.conflict_id)
        latest = max(attempts, key=lambda item: item.attempt, default=None)
        if latest is not None and latest.status in {"preparing", "resolving", "verifying"}:
            await self.integration_service.save_resolution_attempt(
                latest.model_copy(
                    update={
                        "status": "retryable",
                        "error_code": "agentend_restarted",
                        "error_message": "AgentEnd restarted during Resolver execution",
                        "finished_at": now,
                    }
                )
            )
        current = await self.integration_service.get_conflict_record(conflict.conflict_id)
        if current is not None and current.status in {"preparing", "resolving", "verifying"}:
            await self.integration_service.save_conflict_record(
                current.model_copy(
                    update={
                        "status": "retryable",
                        "last_error_code": "agentend_restarted",
                        "last_error_message": "AgentEnd restarted during Resolver execution",
                        "updated_at": now,
                    }
                )
            )


__all__ = ["ConflictActionInput", "ConflictRecoveryCoordinator"]
