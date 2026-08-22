from __future__ import annotations

import asyncio
import json

import pytest
from pydantic import ValidationError

from src.execution.models import RunSpec
from src.execution.repository import SQLiteRunRepository
from src.generated.agent_run import AgentRunBudget
from src.integration.models import (
    ConflictActionRecord,
    ConflictRecord,
    GitIntegrationRecord,
    ResolutionAttempt,
    ResolutionIntegrationRecord,
)
from src.integration.repository import IntegrationOperationRepository
from src.integration.service import IntegrationService
from src.orchestrator.models import IntegrationResult, IntegrationResultV1, IntegrationResultV2
from src.schemas.request import AgentRequest
from src.workspace.models import MergeResult


def test_internal_request_requires_plan_task_with_operation() -> None:
    with pytest.raises(ValidationError):
        AgentRequest(
            task_id="scope-1",
            session_id="session-1",
            message="merge",
            integration_operation_id="operation-1",
        )


@pytest.mark.asyncio
async def test_operation_is_idempotent_and_capability_is_single_use(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "runs.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        integration_scope_id="scope-1",
    )
    with pytest.raises(Exception, match="operation_binding_mismatch"):
        await service.create_operation(
            plan_task_id="task-001",
            run_id="another-run-id",
            root_run_id="root-1",
            parent_run_id="root-1",
            attempt=0,
            session_id="session-1",
            workspace_id="workspace-1",
            workspace_handle="workspace-1",
            integration_scope_id="scope-1",
        )
    token = await service.issue_capability(operation.integration_operation_id)
    await service.redeem_capability(
        token,
        operation.integration_operation_id,
        run_id="run-1",
        workspace_id="workspace-1",
    )
    with pytest.raises(Exception, match="integration_capability_invalid"):
        await service.redeem_capability(
            token,
            operation.integration_operation_id,
            run_id="run-1",
            workspace_id="workspace-1",
        )
    await repository.close()


@pytest.mark.asyncio
async def test_v1_and_v2_results_are_bound_to_their_distinct_identities(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "integration.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        integration_scope_id="scope-1",
    )
    v1 = IntegrationResultV1(
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        task_id="scope-1",
        session_id="session-1",
        attempt=0,
        status="merged",
    )
    assert await service.validate_result(
        v1,
        expected_run_id="run-1",
        expected_root_run_id="root-1",
        expected_parent_run_id="root-1",
        expected_plan_task_id="task-001",
        expected_session_id="session-1",
        expected_attempt=0,
        expected_integration_scope_id="scope-1",
        expected_workspace_id="workspace-1",
        expected_operation_id=operation.integration_operation_id,
    )
    v2 = IntegrationResultV2(
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        integration_operation_id=operation.integration_operation_id,
        plan_task_id="task-001",
        integration_scope_id="scope-1",
        workspace_id="workspace-1",
        session_id="session-1",
        attempt=0,
        status="merged",
    )
    assert await service.validate_result(
        v2,
        expected_run_id="run-1",
        expected_root_run_id="root-1",
        expected_parent_run_id="root-1",
        expected_plan_task_id="task-001",
        expected_session_id="session-1",
        expected_attempt=0,
        expected_integration_scope_id="scope-1",
        expected_workspace_id="workspace-1",
        expected_operation_id=operation.integration_operation_id,
    )
    bad_v1 = v1.model_copy(update={"task_id": "task-001"})
    with pytest.raises(Exception, match="operation_binding_mismatch"):
        await service.validate_result(
            bad_v1,
            expected_run_id="run-1",
            expected_root_run_id="root-1",
            expected_parent_run_id="root-1",
            expected_plan_task_id="task-001",
            expected_session_id="session-1",
            expected_attempt=0,
            expected_integration_scope_id="scope-1",
            expected_operation_id=operation.integration_operation_id,
        )
    assert (await repository.get(operation.integration_operation_id)).status == "pending"
    await repository.close()


@pytest.mark.asyncio
async def test_operation_attempts_are_monotonic_per_plan_task(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "attempts.sqlite3")
    service = IntegrationService(repository)
    await service.create_operation(
        plan_task_id="task-001",
        run_id="run-0",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        integration_scope_id="scope-1",
    )
    with pytest.raises(Exception, match="operation_stale_attempt"):
        await service.create_operation(
            plan_task_id="task-001",
            run_id="run-2",
            root_run_id="root-1",
            parent_run_id="root-1",
            attempt=2,
            session_id="session-1",
            workspace_id="workspace-1",
            workspace_handle="workspace-1",
            integration_scope_id="scope-1",
        )
    retry = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=1,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        integration_scope_id="scope-1",
    )
    replay = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=1,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        integration_scope_id="scope-1",
    )
    assert replay.integration_operation_id == retry.integration_operation_id
    await repository.close()


@pytest.mark.asyncio
async def test_conflict_and_resolution_attempts_are_durable_and_separate_from_git_record(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "integration.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        integration_scope_id="scope-1",
    )
    conflict = await service.save_conflict_record(
        ConflictRecord(
            conflict_id="conflict-1",
            root_run_id="root-1",
            original_operation_id=operation.integration_operation_id,
            plan_task_id="task-001",
            integration_scope_id="scope-1",
            workspace_id="workspace-1",
            status="resolving",
            conflict_files=["1.md"],
        )
    )
    await service.save_resolution_attempt(
        ResolutionAttempt(
            resolution_attempt_id="conflict-1:0",
            conflict_id=conflict.conflict_id,
            original_operation_id=operation.integration_operation_id,
            resolver_run_id="resolver-run-1",
            resolver_workspace_id="resolver-workspace-1",
            status="completed",
            expected_target_commit="target-1",
            resolver_commit="resolver-1",
        )
    )
    loaded = await service.get_conflict_record("conflict-1")
    attempts = await service.list_resolution_attempts("conflict-1")
    assert loaded is not None
    assert loaded.original_operation_id == operation.integration_operation_id
    assert attempts[0].resolver_commit == "resolver-1"
    await repository.close()


@pytest.mark.asyncio
async def test_resolution_attempt_cannot_regress_after_terminal_status(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "resolution-state.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        integration_scope_id="scope-1",
    )
    conflict = await service.save_conflict_record(
        ConflictRecord(
            conflict_id="resolution-state",
            root_run_id="root-1",
            original_operation_id=operation.integration_operation_id,
            plan_task_id="task-001",
            integration_scope_id="scope-1",
            workspace_id="workspace-1",
            status="resolving",
            conflict_files=["1.md"],
        )
    )
    attempt = ResolutionAttempt(
        resolution_attempt_id="resolution-state:0",
        conflict_id=conflict.conflict_id,
        original_operation_id=operation.integration_operation_id,
        resolver_run_id="resolver-run-1",
        resolver_workspace_id="resolver-workspace-1",
        attempt=0,
        status="preparing",
    )
    await service.save_resolution_attempt(attempt)
    await service.save_resolution_attempt(attempt.model_copy(update={"status": "resolving"}))
    await service.save_resolution_attempt(attempt.model_copy(update={"status": "verifying"}))
    await service.save_resolution_attempt(attempt.model_copy(update={"status": "completed", "resolver_commit": "commit-1"}))

    with pytest.raises(Exception, match="operation_terminal_mismatch"):
        await service.save_resolution_attempt(attempt.model_copy(update={"status": "preparing"}))
    loaded = await repository.list_resolution_attempts(conflict.conflict_id)
    assert loaded[0].status == "completed"
    assert loaded[0].resolver_commit == "commit-1"
    await repository.close()


@pytest.mark.asyncio
async def test_conflict_record_cannot_regress_after_resolution(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "conflict-state.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        integration_scope_id="scope-1",
    )

    def conflict(status: str, attempt: int = 0) -> ConflictRecord:
        return ConflictRecord(
            conflict_id="conflict-state",
            root_run_id="root-1",
            original_operation_id=operation.integration_operation_id,
            plan_task_id="task-001",
            integration_scope_id="scope-1",
            workspace_id="workspace-1",
            status=status,
            attempt=attempt,
            conflict_files=["1.md"],
        )

    await service.save_conflict_record(conflict("detected"))
    await service.save_conflict_record(conflict("preparing"))
    await service.save_conflict_record(conflict("retryable"))
    await service.save_conflict_record(conflict("preparing", attempt=1))
    await service.save_conflict_record(conflict("verifying", attempt=1))
    await service.save_conflict_record(conflict("resolved", attempt=1))

    with pytest.raises(Exception, match="operation_terminal_mismatch"):
        await service.save_conflict_record(conflict("preparing", attempt=1))
    with pytest.raises(Exception, match="operation_stale_attempt"):
        await service.save_conflict_record(conflict("resolved"))
    loaded = await service.get_conflict_record("conflict-state")
    assert loaded is not None and loaded.status == "resolved"
    await repository.close()


@pytest.mark.asyncio
async def test_operation_terminal_result_is_idempotent(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "integration.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        integration_scope_id="scope-1",
    )
    result = IntegrationResult(
        version=2,
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        plan_task_id="task-001",
        integration_operation_id=operation.integration_operation_id,
        integration_scope_id="scope-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        session_id="session-1",
        status="merged",
        source_branch="agent/session-1/scope-1",
        source_commit="source-1",
        target_branch="task/scope-1",
        target_commit="target-1",
        target_commit_after="target-merged-1",
        merge_base="base-1",
    )
    first = await service.finalize_result(result, operation)
    second = await service.finalize_result(result, operation)
    assert first is not None and second is not None
    assert first.status == second.status == "merged"
    assert len((await repository.get_git_record(operation.integration_operation_id)).model_dump()) > 0
    await repository.close()


@pytest.mark.asyncio
async def test_recovery_reconciles_git_record_written_before_operation_terminal(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "record-before-terminal.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )
    await repository.save_git_record(
        GitIntegrationRecord(
            record_id="record-1",
            integration_operation_id=operation.integration_operation_id,
            workspace_id="workspace-1",
            integration_scope_id="scope-1",
            status="merged",
            source_branch="agent/session-1/scope-1",
            source_commit="source-1",
            target_branch="task/scope-1",
            target_commit_before="target-1",
            target_commit_after="target-merged-1",
            merge_base="base-1",
        )
    )
    recovered = await service.recover_incomplete()
    assert recovered[0].status == "merged"
    assert (await repository.get(operation.integration_operation_id)).result_digest
    await repository.close()


@pytest.mark.asyncio
async def test_recovery_reconciles_phase1_result_file_before_probe(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "phase1-recovery.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )
    worktree = tmp_path / "worktrees" / "scope-1" / "session-1"
    result_path = worktree.parent / "shared" / ".agent" / "integration-results" / "run-1.json"
    result_path.parent.mkdir(parents=True)
    result_path.write_text(
        json.dumps(
            {
                "version": 2,
                "run_id": "run-1",
                "root_run_id": "root-1",
                "parent_run_id": "root-1",
                "integration_operation_id": operation.integration_operation_id,
                "plan_task_id": "task-001",
                "integration_scope_id": "scope-1",
                "workspace_handle": "opaque-handle",
                "session_id": "session-1",
                "attempt": 0,
                "status": "merged",
                "source_branch": "agent/session-1/scope-1",
                "source_commit": "source-1",
                "target_branch": "task/scope-1",
                "target_commit": "target-1",
                "target_commit_after": "target-merged-1",
                "merge_base": "base-1",
            }
        )
    )

    class WorkspaceManager:
        def get(self, workspace_id: str):
            return type(
                "Workspace",
                (),
                {
                    "status": "active",
                    "task_id": "scope-1",
                    "session_id": "session-1",
                    "branch_name": "agent/session-1/scope-1",
                    "worktree_path": str(worktree),
                },
            )()

        async def probe_integration(self, workspace_id: str) -> MergeResult:
            return MergeResult(
                success=True,
                source_branch="agent/session-1/scope-1",
                source_commit="source-1",
                target_branch="task/scope-1",
                target_commit="target-1",
                target_commit_after="target-merged-1",
                merge_base="base-1",
            )

    recovered = await service.recover_incomplete(workspace_mgr=WorkspaceManager())
    assert recovered[0].status == "merged"
    assert (await repository.get_git_record(operation.integration_operation_id)) is not None
    await repository.close()


@pytest.mark.asyncio
async def test_partial_git_record_cannot_be_overwritten_by_conflicting_result(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "partial-record.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )
    await repository.save_git_record(
        GitIntegrationRecord(
            record_id="record-1",
            integration_operation_id=operation.integration_operation_id,
            workspace_id="workspace-1",
            integration_scope_id="scope-1",
            status="merged",
            source_commit="authoritative-source",
            target_commit_before="target-1",
            target_commit_after="target-merged-1",
        )
    )
    conflicting = IntegrationResult(
        version=2,
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        plan_task_id="task-001",
        integration_operation_id=operation.integration_operation_id,
        integration_scope_id="scope-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        session_id="session-1",
        status="merged",
        source_branch="agent/session-1/scope-1",
        source_commit="attacker-source",
        target_branch="task/scope-1",
        target_commit="target-1",
        target_commit_after="target-merged-1",
        merge_base="base-1",
    )
    with pytest.raises(Exception, match="operation_terminal_mismatch"):
        await service.finalize_result(conflicting, operation)
    assert (await repository.get(operation.integration_operation_id)).status == "pending"
    await repository.close()


@pytest.mark.asyncio
async def test_resolved_conflict_is_a_replay_marker(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "resolved-conflict.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )
    await service.save_conflict_record(
        ConflictRecord(
            conflict_id="conflict-1",
            root_run_id="root-1",
            original_operation_id=operation.integration_operation_id,
            plan_task_id="task-001",
            integration_scope_id="scope-1",
            workspace_id="workspace-1",
            status="resolved",
        )
    )
    await service.save_resolution_integration_record(
        ResolutionIntegrationRecord(
            resolution_record_id="conflict-1:0",
            conflict_id="conflict-1",
            original_operation_id=operation.integration_operation_id,
            root_run_id="root-1",
            plan_task_id="task-001",
            integration_scope_id="scope-1",
            resolver_run_id="resolver-run-1",
            resolver_workspace_id="resolver-workspace-1",
            attempt=0,
            status="merged",
            source_branch="resolve/scope-1/conflict-1/0",
            source_commit="resolver-source-1",
            target_branch="task/scope-1",
            target_commit_before="target-1",
            target_commit_after="target-2",
            merge_base="base-1",
        )
    )
    resolved = await service.get_resolved_conflict(operation.integration_operation_id)
    assert resolved is not None and resolved.status == "resolved"
    await repository.close()


@pytest.mark.asyncio
async def test_manual_partial_action_finalizes_conflict_and_is_replayable(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "manual-action.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )
    result = IntegrationResult(
        version=2,
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        plan_task_id="task-001",
        integration_operation_id=operation.integration_operation_id,
        integration_scope_id="scope-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        session_id="session-1",
        status="conflict",
        source_branch="agent/session-1/scope-1",
        source_commit="source-1",
        target_branch="task/scope-1",
        target_commit="target-1",
        target_commit_after="target-1",
        merge_base="base-1",
        conflict_files=["1.md"],
        aborted=True,
    )
    await service.finalize_result(result, operation)
    conflict_id = service.conflict_id_for(
        operation.integration_operation_id,
        "source-1",
        "target-1",
        ["1.md"],
    )
    applied = await service.apply_manual_action(
        conflict_id,
        "accept_partial",
        "action-1",
        target_status="partial",
        git_facts={"target_commit_after": "target-1"},
    )
    assert applied["operation_status"] == "partial"
    assert (await repository.get(operation.integration_operation_id)).status == "partial"
    resolution = await repository.get_resolution_record(conflict_id)
    assert resolution is not None and resolution.status == "accepted_partial"
    action, created = await repository.create_conflict_action(
        ConflictActionRecord(
            action_id="action-1",
            conflict_id=conflict_id,
            action="accept_partial",
            task_id="scope-1",
            session_id="session-1",
            root_run_id="root-1",
            expected_attempt=0,
            idempotency_key="idempotency-1",
        )
    )
    assert created and (await repository.get_conflict_action_by_key(conflict_id, "idempotency-1")).action_id == action.action_id
    await repository.close()


@pytest.mark.asyncio
async def test_manual_action_advances_past_an_immutable_failed_resolution_attempt(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "manual-after-failure.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )
    result = IntegrationResult(
        version=2,
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        plan_task_id="task-001",
        integration_operation_id=operation.integration_operation_id,
        integration_scope_id="scope-1",
        workspace_id="workspace-1",
        session_id="session-1",
        status="conflict",
        source_branch="agent/session-1/scope-1",
        source_commit="source-1",
        target_branch="task/scope-1",
        target_commit="target-1",
        target_commit_after="target-1",
        merge_base="base-1",
        conflict_files=["1.md"],
        aborted=True,
    )
    await service.finalize_result(result, operation)
    conflict_id = service.conflict_id_for(operation.integration_operation_id, "source-1", "target-1", ["1.md"])
    conflict = await repository.get_conflict_record(conflict_id)
    assert conflict is not None
    await service.save_resolution_integration_record(
        ResolutionIntegrationRecord(
            resolution_record_id="failed-resolution-0",
            conflict_id=conflict_id,
            original_operation_id=operation.integration_operation_id,
            root_run_id="root-1",
            plan_task_id="task-001",
            integration_scope_id="scope-1",
            resolver_run_id="resolver-run-0",
            resolver_workspace_id="resolver-workspace-0",
            attempt=0,
            status="failed",
            conflict_files=["1.md"],
            error_code="resolver_failed",
            error_message="validation failed",
        )
    )

    applied = await service.apply_manual_action(
        conflict_id,
        "accept_partial",
        "action-after-failure",
        target_status="partial",
        git_facts={"target_commit_after": "target-1"},
    )
    assert applied["operation_status"] == "partial"
    updated_conflict = await repository.get_conflict_record(conflict_id)
    assert updated_conflict is not None and updated_conflict.attempt == 1
    assert (await repository.get_resolution_record(conflict_id, 0)).status == "failed"
    assert (await repository.get_resolution_record(conflict_id, 1)).status == "accepted_partial"
    await repository.close()


@pytest.mark.asyncio
async def test_cancelling_claimed_but_not_started_operation_fences_capability(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "cancel-integrating.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )
    token = await service.issue_capability(operation.integration_operation_id)
    await repository.claim(operation.integration_operation_id)
    assert await service.cancel(operation.integration_operation_id, "user_cancelled")
    assert (await repository.get(operation.integration_operation_id)).status == "cancelled"
    with pytest.raises(Exception, match="integration_capability_invalid"):
        await service.redeem_capability(
            token,
            operation.integration_operation_id,
            run_id="run-1",
            workspace_id="workspace-1",
        )
    await repository.close()


@pytest.mark.asyncio
async def test_result_digest_is_control_plane_derived(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "digest.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )
    result = IntegrationResult(
        version=2,
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        plan_task_id="task-001",
        integration_operation_id=operation.integration_operation_id,
        integration_scope_id="scope-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        session_id="session-1",
        status="merged",
        source_branch="agent/session-1/scope-1",
        source_commit="source-1",
        target_branch="task/scope-1",
        target_commit="target-1",
        target_commit_after="target-merged-1",
        merge_base="base-1",
        result_digest="attacker-chosen-digest",
    )
    with pytest.raises(Exception, match="integration_result_invalid"):
        await service.finalize_result(result, operation)
    assert (await repository.get(operation.integration_operation_id)).status == "pending"
    await repository.close()


@pytest.mark.asyncio
async def test_phase2_execution_uses_authoritative_workspace_and_never_reissues_terminal_capability(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "integration.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )

    class WorkspaceManager:
        def __init__(self) -> None:
            self.merge_calls = 0

        def get(self, workspace_id: str):
            assert workspace_id == "workspace-1"
            return type("Workspace", (), {"status": "active", "task_id": "scope-1", "session_id": "session-1"})()

        async def merge(self, workspace_id: str):
            self.merge_calls += 1
            return MergeResult(
                success=True,
                source_branch="agent/session-1/scope-1",
                source_commit="source-1",
                target_branch="task/scope-1",
                target_commit="target-1",
                merge_base="base-1",
            )

        async def current_commit(self, workspace_id: str) -> str:
            return "target-merged-1"

    workspace_mgr = WorkspaceManager()
    token = await service.issue_capability(operation.integration_operation_id)
    projection = await service.execute_operation(
        operation.integration_operation_id,
        capability=token,
        run_id="run-1",
        workspace_mgr=workspace_mgr,
    )
    assert projection.status == "merged"
    assert workspace_mgr.merge_calls == 1
    assert (await repository.get(operation.integration_operation_id)).status == "merged"
    replay = await service.execute_operation(
        operation.integration_operation_id,
        capability=token,
        run_id="run-1",
        workspace_mgr=workspace_mgr,
    )
    assert replay.status == "merged"
    assert workspace_mgr.merge_calls == 1
    with pytest.raises(Exception, match="operation_terminal_mismatch"):
        await service.issue_capability(operation.integration_operation_id)
    await repository.close()


@pytest.mark.asyncio
async def test_phase2_concurrent_capabilities_execute_git_once(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "concurrent.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )

    class WorkspaceManager:
        def __init__(self) -> None:
            self.merge_calls = 0
            self.merge_entered = asyncio.Event()
            self.release_merge = asyncio.Event()

        def get(self, workspace_id: str):
            return type("Workspace", (), {"status": "active", "task_id": "scope-1", "session_id": "session-1"})()

        async def merge(self, workspace_id: str):
            self.merge_calls += 1
            self.merge_entered.set()
            await self.release_merge.wait()
            return MergeResult(
                success=True,
                source_branch="agent/session-1/scope-1",
                source_commit="source-1",
                target_branch="task/scope-1",
                target_commit="target-1",
                merge_base="base-1",
            )

        async def current_commit(self, workspace_id: str) -> str:
            return "target-merged-1"

    workspace_mgr = WorkspaceManager()
    token_one = await service.issue_capability(operation.integration_operation_id)
    token_two = await service.issue_capability(operation.integration_operation_id)
    first = asyncio.create_task(
        service.execute_operation(
            operation.integration_operation_id,
            capability=token_one,
            run_id="run-1",
            workspace_mgr=workspace_mgr,
        )
    )
    await workspace_mgr.merge_entered.wait()
    second = asyncio.create_task(
        service.execute_operation(
            operation.integration_operation_id,
            capability=token_two,
            run_id="run-1",
            workspace_mgr=workspace_mgr,
        )
    )
    await asyncio.sleep(0)
    workspace_mgr.release_merge.set()
    first_projection, second_projection = await asyncio.gather(first, second)
    assert first_projection.status == second_projection.status == "merged"
    assert workspace_mgr.merge_calls == 1
    await repository.close()


@pytest.mark.asyncio
async def test_recovery_probes_integrating_operation_before_resuming_git(tmp_path) -> None:
    repository = IntegrationOperationRepository(tmp_path / "recovery.sqlite3")
    service = IntegrationService(repository)
    operation = await service.create_operation(
        plan_task_id="task-001",
        run_id="run-1",
        root_run_id="root-1",
        parent_run_id="root-1",
        attempt=0,
        session_id="session-1",
        workspace_id="workspace-1",
        workspace_handle="opaque-handle",
        integration_scope_id="scope-1",
    )
    await repository.claim(operation.integration_operation_id)

    class WorkspaceManager:
        async def probe_integration(self, workspace_id: str) -> MergeResult:
            return MergeResult(
                success=True,
                source_branch="agent/session-1/scope-1",
                source_commit="source-1",
                target_branch="task/scope-1",
                target_commit="target-1",
                target_commit_after="target-merged-1",
                merge_base="base-1",
            )

        async def merge(self, workspace_id: str):
            raise AssertionError("recovery must not rerun Git after probe proves the merge")

    recovered = await service.recover_incomplete(workspace_mgr=WorkspaceManager())
    assert len(recovered) == 1
    assert recovered[0].status == "merged"
    assert (await repository.get_git_record(operation.integration_operation_id)) is not None
    await repository.close()


@pytest.mark.asyncio
async def test_run_repository_round_trips_internal_identity(tmp_path) -> None:
    repository = SQLiteRunRepository(tmp_path / "runs.sqlite3")
    spec = RunSpec(
        run_id="run-1",
        root_run_id="run-1",
        parent_run_id=None,
        task_id="scope-1",
        plan_task_id="task-001",
        integration_operation_id="operation-1",
        workspace_id="workspace-1",
        workspace_handle="workspace-1",
        integration_attempt=2,
        session_id="session-1",
        agent_type="orchestrator",
        budget=AgentRunBudget(),
    )
    record, created = await repository.create(spec)
    assert created
    loaded = await repository.get(record.spec.run_id)
    assert loaded is not None
    assert loaded.spec.plan_task_id == "task-001"
    assert loaded.spec.integration_operation_id == "operation-1"
    assert loaded.spec.workspace_handle == "workspace-1"
    assert loaded.spec.integration_attempt == 2
    await repository.close()
