from __future__ import annotations

import asyncio
import hashlib
import json
import sqlite3
from pathlib import Path

from src.integration.capability import is_expired
from src.integration.errors import sanitize_error_text
from src.integration.models import (
    ConflictActionRecord,
    ConflictRecord,
    GitIntegrationRecord,
    IntegrationIntent,
    IntegrationOperation,
    ResolutionAttempt,
    ResolutionIntegrationRecord,
    utc_now,
)


class IntegrationOperationConflictError(ValueError):
    pass


class IntegrationTerminalMismatchError(ValueError):
    pass


class IntegrationOperationRepository:
    """Durable IntegrationOperation and Git fact repository.

    The repository intentionally shares the AgentEnd SQLite file with Run
    metadata, but keeps its own serialized connection and schema. WAL allows
    Run and integration writes to coexist while each transaction remains
    short and conditionally updateable.
    """

    def __init__(self, path: str | Path) -> None:
        self.path = Path(path)
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self._db = sqlite3.connect(self.path, check_same_thread=False)
        self._db.row_factory = sqlite3.Row
        self._lock = asyncio.Lock()
        self._init_schema()

    def _init_schema(self) -> None:
        self._db.executescript(
            """
            PRAGMA journal_mode=WAL;
            PRAGMA foreign_keys=ON;
            CREATE TABLE IF NOT EXISTS integration_operations (
                integration_operation_id TEXT PRIMARY KEY,
                root_run_id TEXT NOT NULL,
                parent_run_id TEXT NOT NULL DEFAULT '',
                run_id TEXT NOT NULL UNIQUE,
                plan_task_id TEXT NOT NULL,
                attempt INTEGER NOT NULL CHECK(attempt >= 0),
                session_id TEXT NOT NULL,
                workspace_id TEXT NOT NULL,
                workspace_handle TEXT NOT NULL DEFAULT '',
                integration_scope_id TEXT NOT NULL,
                status TEXT NOT NULL,
                result_digest TEXT NOT NULL DEFAULT '',
                error_code TEXT NOT NULL DEFAULT '',
                error_message TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL,
                started_at TEXT NOT NULL DEFAULT '',
                finished_at TEXT NOT NULL DEFAULT '',
                row_version INTEGER NOT NULL DEFAULT 0,
                UNIQUE(root_run_id, plan_task_id, attempt)
            );
            CREATE INDEX IF NOT EXISTS idx_integration_operations_root
                ON integration_operations(root_run_id, status);
            CREATE INDEX IF NOT EXISTS idx_integration_operations_scope
                ON integration_operations(integration_scope_id, status);
            CREATE TABLE IF NOT EXISTS integration_intents (
                integration_operation_id TEXT PRIMARY KEY,
                workspace_id TEXT NOT NULL,
                integration_scope_id TEXT NOT NULL,
                source_branch TEXT NOT NULL,
                source_commit TEXT NOT NULL,
                target_branch TEXT NOT NULL,
                target_commit_before TEXT NOT NULL,
                merge_base TEXT NOT NULL,
                created_at TEXT NOT NULL,
                FOREIGN KEY(integration_operation_id)
                    REFERENCES integration_operations(integration_operation_id)
            );
            CREATE INDEX IF NOT EXISTS idx_integration_intents_scope
                ON integration_intents(integration_scope_id, created_at);
            CREATE TABLE IF NOT EXISTS git_integration_records (
                record_id TEXT PRIMARY KEY,
                integration_operation_id TEXT NOT NULL,
                workspace_id TEXT NOT NULL,
                integration_scope_id TEXT NOT NULL,
                status TEXT NOT NULL,
                source_branch TEXT NOT NULL DEFAULT '',
                source_commit TEXT NOT NULL DEFAULT '',
                target_branch TEXT NOT NULL DEFAULT '',
                target_commit_before TEXT NOT NULL DEFAULT '',
                target_commit_after TEXT NOT NULL DEFAULT '',
                merge_base TEXT NOT NULL DEFAULT '',
                conflict_files_json TEXT NOT NULL DEFAULT '[]',
                aborted INTEGER NOT NULL DEFAULT 0,
                git_exit_code INTEGER,
                error_code TEXT NOT NULL DEFAULT '',
                error_message TEXT NOT NULL DEFAULT '',
                started_at TEXT NOT NULL DEFAULT '',
                finished_at TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL,
                UNIQUE(record_id),
                UNIQUE(integration_operation_id),
                FOREIGN KEY(integration_operation_id)
                    REFERENCES integration_operations(integration_operation_id)
            );
            CREATE INDEX IF NOT EXISTS idx_git_records_operation
                ON git_integration_records(integration_operation_id, created_at);
            CREATE TABLE IF NOT EXISTS conflict_records (
                conflict_id TEXT PRIMARY KEY,
                root_run_id TEXT NOT NULL DEFAULT '',
                original_operation_id TEXT NOT NULL,
                plan_task_id TEXT NOT NULL,
                integration_scope_id TEXT NOT NULL,
                workspace_id TEXT NOT NULL,
                status TEXT NOT NULL,
                attempt INTEGER NOT NULL CHECK(attempt >= 0),
                source_branch TEXT NOT NULL DEFAULT '',
                source_commit TEXT NOT NULL DEFAULT '',
                target_branch TEXT NOT NULL DEFAULT '',
                target_commit TEXT NOT NULL DEFAULT '',
                merge_base TEXT NOT NULL DEFAULT '',
                conflict_files_json TEXT NOT NULL DEFAULT '[]',
                resolver_agent TEXT NOT NULL DEFAULT '',
                resolver_session_id TEXT NOT NULL DEFAULT '',
                resolver_branch TEXT NOT NULL DEFAULT '',
                resolver_run_id TEXT NOT NULL DEFAULT '',
                last_error_code TEXT NOT NULL DEFAULT '',
                last_error_message TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL,
                row_version INTEGER NOT NULL DEFAULT 0,
                FOREIGN KEY(original_operation_id)
                    REFERENCES integration_operations(integration_operation_id)
            );
            CREATE INDEX IF NOT EXISTS idx_conflict_records_operation
                ON conflict_records(original_operation_id, updated_at);
            CREATE TABLE IF NOT EXISTS resolution_attempts (
                resolution_attempt_id TEXT PRIMARY KEY,
                conflict_id TEXT NOT NULL,
                original_operation_id TEXT NOT NULL,
                resolver_run_id TEXT NOT NULL DEFAULT '',
                resolver_workspace_id TEXT NOT NULL DEFAULT '',
                attempt INTEGER NOT NULL CHECK(attempt >= 0),
                status TEXT NOT NULL,
                expected_target_commit TEXT NOT NULL DEFAULT '',
                resolver_commit TEXT NOT NULL DEFAULT '',
                error_code TEXT NOT NULL DEFAULT '',
                error_message TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL,
                finished_at TEXT NOT NULL DEFAULT '',
                UNIQUE(conflict_id, attempt),
                FOREIGN KEY(conflict_id)
                    REFERENCES conflict_records(conflict_id),
                FOREIGN KEY(original_operation_id)
                    REFERENCES integration_operations(integration_operation_id)
            );
            CREATE INDEX IF NOT EXISTS idx_resolution_conflict
                ON resolution_attempts(conflict_id, attempt);
            CREATE TABLE IF NOT EXISTS resolution_integration_records (
                resolution_record_id TEXT PRIMARY KEY,
                conflict_id TEXT NOT NULL,
                original_operation_id TEXT NOT NULL,
                root_run_id TEXT NOT NULL,
                plan_task_id TEXT NOT NULL,
                integration_scope_id TEXT NOT NULL,
                resolver_run_id TEXT NOT NULL,
                resolver_workspace_id TEXT NOT NULL,
                attempt INTEGER NOT NULL CHECK(attempt >= 0),
                status TEXT NOT NULL,
                source_branch TEXT NOT NULL DEFAULT '',
                source_commit TEXT NOT NULL DEFAULT '',
                target_branch TEXT NOT NULL DEFAULT '',
                target_commit_before TEXT NOT NULL DEFAULT '',
                target_commit_after TEXT NOT NULL DEFAULT '',
                merge_base TEXT NOT NULL DEFAULT '',
                conflict_files_json TEXT NOT NULL DEFAULT '[]',
                aborted INTEGER NOT NULL DEFAULT 0,
                error_code TEXT NOT NULL DEFAULT '',
                error_message TEXT NOT NULL DEFAULT '',
                started_at TEXT NOT NULL DEFAULT '',
                finished_at TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL,
                UNIQUE(conflict_id, attempt),
                UNIQUE(resolver_run_id),
                FOREIGN KEY(conflict_id) REFERENCES conflict_records(conflict_id),
                FOREIGN KEY(original_operation_id)
                    REFERENCES integration_operations(integration_operation_id)
            );
            CREATE INDEX IF NOT EXISTS idx_resolution_records_operation
                ON resolution_integration_records(original_operation_id, attempt);
            CREATE TABLE IF NOT EXISTS conflict_actions (
                action_id TEXT PRIMARY KEY,
                conflict_id TEXT NOT NULL,
                action TEXT NOT NULL,
                task_id TEXT NOT NULL,
                session_id TEXT NOT NULL,
                root_run_id TEXT NOT NULL,
                expected_attempt INTEGER NOT NULL CHECK(expected_attempt >= 0),
                idempotency_key TEXT NOT NULL,
                status TEXT NOT NULL,
                result_json TEXT NOT NULL DEFAULT '',
                error_code TEXT NOT NULL DEFAULT '',
                error_message TEXT NOT NULL DEFAULT '',
                created_at TEXT NOT NULL,
                finished_at TEXT NOT NULL DEFAULT '',
                UNIQUE(conflict_id, idempotency_key),
                FOREIGN KEY(conflict_id) REFERENCES conflict_records(conflict_id)
            );
            CREATE INDEX IF NOT EXISTS idx_conflict_actions_conflict
                ON conflict_actions(conflict_id, created_at);
            CREATE TABLE IF NOT EXISTS integration_capabilities (
                token_digest TEXT PRIMARY KEY,
                integration_operation_id TEXT NOT NULL,
                run_id TEXT NOT NULL,
                workspace_id TEXT NOT NULL,
                allowed_action TEXT NOT NULL,
                expires_at TEXT NOT NULL,
                redeemed_at TEXT NOT NULL DEFAULT '',
                FOREIGN KEY(integration_operation_id)
                    REFERENCES integration_operations(integration_operation_id)
            );
            CREATE INDEX IF NOT EXISTS idx_integration_capability_operation
                ON integration_capabilities(integration_operation_id);
            """
        )
        self._db.commit()

    async def close(self) -> None:
        async with self._lock:
            self._db.close()

    async def create_operation(self, operation: IntegrationOperation) -> IntegrationOperation:
        async with self._lock:
            try:
                self._db.execute(
                    """
                    INSERT INTO integration_operations (
                        integration_operation_id, root_run_id, parent_run_id, run_id,
                        plan_task_id, attempt, session_id, workspace_id, workspace_handle,
                        integration_scope_id, status, result_digest, error_code, error_message,
                        created_at, started_at, finished_at, row_version
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        operation.integration_operation_id,
                        operation.root_run_id,
                        operation.parent_run_id,
                        operation.run_id,
                        operation.plan_task_id,
                        operation.attempt,
                        operation.session_id,
                        operation.workspace_id,
                        operation.workspace_handle,
                        operation.integration_scope_id,
                        operation.status,
                        operation.result_digest,
                        operation.error_code,
                        operation.error_message,
                        operation.created_at,
                        operation.started_at,
                        operation.finished_at,
                        operation.row_version,
                    ),
                )
                self._db.commit()
            except sqlite3.IntegrityError as exc:
                self._db.rollback()
                raise IntegrationOperationConflictError(str(exc)) from exc
            return operation

    async def get(self, operation_id: str) -> IntegrationOperation | None:
        async with self._lock:
            row = self._db.execute(
                "SELECT * FROM integration_operations WHERE integration_operation_id = ?",
                (operation_id,),
            ).fetchone()
            return self._row_to_operation(row) if row else None

    async def get_by_run(self, run_id: str) -> IntegrationOperation | None:
        async with self._lock:
            row = self._db.execute(
                "SELECT * FROM integration_operations WHERE run_id = ?", (run_id,)
            ).fetchone()
            return self._row_to_operation(row) if row else None

    async def save_integration_intent(self, intent: IntegrationIntent) -> IntegrationIntent:
        """Persist the exact Git snapshot immediately before merge execution."""
        async with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                operation_row = self._db.execute(
                    "SELECT workspace_id, integration_scope_id FROM integration_operations "
                    "WHERE integration_operation_id = ?",
                    (intent.integration_operation_id,),
                ).fetchone()
                if not operation_row:
                    raise KeyError(intent.integration_operation_id)
                if (
                    intent.workspace_id != operation_row["workspace_id"]
                    or intent.integration_scope_id != operation_row["integration_scope_id"]
                ):
                    raise IntegrationTerminalMismatchError(
                        f"integration intent for {intent.integration_operation_id} does not match operation binding"
                    )
                existing_row = self._db.execute(
                    "SELECT * FROM integration_intents WHERE integration_operation_id = ?",
                    (intent.integration_operation_id,),
                ).fetchone()
                if existing_row:
                    existing = self._row_to_integration_intent(existing_row)
                    if not self._integration_intent_matches(existing, intent):
                        raise IntegrationTerminalMismatchError(
                            f"integration intent for {intent.integration_operation_id} has conflicting Git facts"
                        )
                    self._db.commit()
                    return existing
                self._db.execute(
                    """
                    INSERT INTO integration_intents (
                        integration_operation_id, workspace_id, integration_scope_id,
                        source_branch, source_commit, target_branch, target_commit_before,
                        merge_base, created_at
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        intent.integration_operation_id,
                        intent.workspace_id,
                        intent.integration_scope_id,
                        intent.source_branch,
                        intent.source_commit,
                        intent.target_branch,
                        intent.target_commit_before,
                        intent.merge_base,
                        intent.created_at,
                    ),
                )
                self._db.commit()
                return intent
            except Exception:
                self._db.rollback()
                raise

    async def get_integration_intent(self, operation_id: str) -> IntegrationIntent | None:
        async with self._lock:
            row = self._db.execute(
                "SELECT * FROM integration_intents WHERE integration_operation_id = ?",
                (operation_id,),
            ).fetchone()
            return self._row_to_integration_intent(row) if row else None

    async def get_by_binding(
        self,
        root_run_id: str,
        plan_task_id: str,
        attempt: int,
    ) -> IntegrationOperation | None:
        async with self._lock:
            row = self._db.execute(
                """SELECT * FROM integration_operations
                   WHERE root_run_id = ? AND plan_task_id = ? AND attempt = ?""",
                (root_run_id, plan_task_id, attempt),
            ).fetchone()
            return self._row_to_operation(row) if row else None

    async def get_latest_for_plan(
        self,
        root_run_id: str,
        plan_task_id: str,
    ) -> IntegrationOperation | None:
        async with self._lock:
            row = self._db.execute(
                """SELECT * FROM integration_operations
                   WHERE root_run_id = ? AND plan_task_id = ?
                   ORDER BY attempt DESC LIMIT 1""",
                (root_run_id, plan_task_id),
            ).fetchone()
            return self._row_to_operation(row) if row else None

    async def list_incomplete(self, root_run_id: str | None = None) -> list[IntegrationOperation]:
        active = ("pending", "integrating")
        async with self._lock:
            if root_run_id:
                rows = self._db.execute(
                    """SELECT * FROM integration_operations
                       WHERE root_run_id = ? AND status IN (?, ?)
                       ORDER BY created_at""",
                    (root_run_id, *active),
                ).fetchall()
            else:
                rows = self._db.execute(
                    """SELECT * FROM integration_operations
                       WHERE status IN (?, ?) ORDER BY created_at""",
                    active,
                ).fetchall()
            return [self._row_to_operation(row) for row in rows]

    async def claim(self, operation_id: str) -> tuple[IntegrationOperation | None, bool]:
        """Claim a pending operation exactly once.

        The boolean distinguishes the caller that won the transition from a
        duplicate caller that observed an already integrating operation.  A
        caller must never execute Git merely because ``begin`` returned an
        integrating row.
        """
        async with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                row = self._db.execute(
                    "SELECT * FROM integration_operations WHERE integration_operation_id = ?",
                    (operation_id,),
                ).fetchone()
                if not row:
                    self._db.commit()
                    return None, False
                current = self._row_to_operation(row)
                if current.status == "pending":
                    self._db.execute(
                        """UPDATE integration_operations
                           SET status = 'integrating', started_at = ?, row_version = row_version + 1
                           WHERE integration_operation_id = ? AND status = 'pending'""",
                        (current.started_at or utc_now(), operation_id),
                    )
                    self._db.commit()
                    row = self._db.execute(
                        "SELECT * FROM integration_operations WHERE integration_operation_id = ?",
                        (operation_id,),
                    ).fetchone()
                    return self._row_to_operation(row), True
                self._db.commit()
                return current, False
            except Exception:
                self._db.rollback()
                raise

    async def begin(self, operation_id: str) -> IntegrationOperation | None:
        """Backward-compatible operation lookup plus claim."""
        operation, _ = await self.claim(operation_id)
        return operation

    async def finalize(
        self,
        operation_id: str,
        status: str,
        result_digest: str,
        error_code: str = "",
        error_message: str = "",
        finished_at: str = "",
    ) -> tuple[IntegrationOperation, bool]:
        """Conditionally finalize an operation; return (record, idempotent)."""
        async with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                row = self._db.execute(
                    "SELECT * FROM integration_operations WHERE integration_operation_id = ?",
                    (operation_id,),
                ).fetchone()
                if not row:
                    raise KeyError(operation_id)
                current = self._row_to_operation(row)
                if current.terminal:
                    if current.status == status and current.result_digest == result_digest:
                        self._invalidate_capabilities_locked(operation_id, utc_now())
                        self._db.commit()
                        return current, True
                    raise IntegrationTerminalMismatchError(
                        f"operation {operation_id} already finalized as {current.status}"
                    )
                if status not in {"merged", "conflict", "partial", "failed", "cancelled", "integration_state_uncertain"}:
                    raise ValueError(f"invalid terminal status: {status}")
                now = finished_at or current.finished_at
                if not now:
                    from src.integration.models import utc_now

                    now = utc_now()
                cursor = self._db.execute(
                    """
                    UPDATE integration_operations
                    SET status = ?, result_digest = ?, error_code = ?, error_message = ?,
                        started_at = CASE WHEN started_at = '' THEN ? ELSE started_at END,
                        finished_at = ?, row_version = row_version + 1
                    WHERE integration_operation_id = ? AND status IN ('pending', 'integrating')
                    """,
                    (
                        status,
                        result_digest,
                        error_code,
                        error_message,
                        now,
                        now,
                        operation_id,
                    ),
                )
                if cursor.rowcount != 1:
                    raise IntegrationTerminalMismatchError(
                        f"operation {operation_id} changed while finalizing"
                    )
                self._invalidate_capabilities_locked(operation_id, now)
                self._db.commit()
                updated = self._db.execute(
                    "SELECT * FROM integration_operations WHERE integration_operation_id = ?",
                    (operation_id,),
                ).fetchone()
                return self._row_to_operation(updated), False
            except Exception:
                self._db.rollback()
                raise

    async def cancel(
        self,
        operation_id: str,
        reason: str = "operation_cancelled",
        *,
        allow_integrating: bool = False,
    ) -> bool:
        async with self._lock:
            safe_reason = sanitize_error_text(reason, limit=4096)
            digest = hashlib.sha256(f"cancelled:{safe_reason}".encode("utf-8")).hexdigest()
            finished_at = utc_now()
            allowed_states = ("pending", "integrating") if allow_integrating else ("pending",)
            state_placeholders = ", ".join("?" for _ in allowed_states)
            cursor = self._db.execute(
                f"""
                UPDATE integration_operations
                SET status = 'cancelled', error_code = ?, error_message = ?,
                    result_digest = ?, finished_at = ?, row_version = row_version + 1
                WHERE integration_operation_id = ? AND status IN ({state_placeholders})
                """,
                ("operation_cancelled", safe_reason, digest, finished_at, operation_id, *allowed_states),
            )
            if cursor.rowcount == 1:
                self._invalidate_capabilities_locked(operation_id, finished_at)
            self._db.commit()
            return cursor.rowcount == 1

    def _invalidate_capabilities_locked(self, operation_id: str, timestamp: str) -> None:
        """Make every outstanding capability unusable once an operation ends."""
        self._db.execute(
            """UPDATE integration_capabilities
               SET redeemed_at = CASE WHEN redeemed_at = '' THEN ? ELSE redeemed_at END
               WHERE integration_operation_id = ?""",
            (timestamp, operation_id),
        )

    def _insert_git_record_locked(self, record: GitIntegrationRecord) -> None:
        self._db.execute(
            """
            INSERT OR IGNORE INTO git_integration_records (
                record_id, integration_operation_id, workspace_id, integration_scope_id,
                status, source_branch, source_commit, target_branch,
                target_commit_before, target_commit_after, merge_base,
                conflict_files_json, aborted, git_exit_code, error_code, error_message,
                started_at, finished_at, created_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                record.record_id,
                record.integration_operation_id,
                record.workspace_id,
                record.integration_scope_id,
                record.status,
                record.source_branch,
                record.source_commit,
                record.target_branch,
                record.target_commit_before,
                record.target_commit_after,
                record.merge_base,
                json.dumps(record.conflict_files, separators=(",", ":")),
                int(record.aborted),
                record.git_exit_code,
                record.error_code,
                record.error_message,
                record.started_at,
                record.finished_at,
                record.created_at,
            ),
        )

    async def save_git_record(self, record: GitIntegrationRecord) -> GitIntegrationRecord:
        async with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                operation_row = self._db.execute(
                    "SELECT workspace_id, integration_scope_id FROM integration_operations "
                    "WHERE integration_operation_id = ?",
                    (record.integration_operation_id,),
                ).fetchone()
                if not operation_row:
                    raise KeyError(record.integration_operation_id)
                if (
                    record.workspace_id != operation_row["workspace_id"]
                    or record.integration_scope_id != operation_row["integration_scope_id"]
                ):
                    raise IntegrationTerminalMismatchError(
                        f"Git record for {record.integration_operation_id} does not match its operation binding"
                    )
                existing_row = self._db.execute(
                    "SELECT * FROM git_integration_records WHERE integration_operation_id = ? LIMIT 1",
                    (record.integration_operation_id,),
                ).fetchone()
                if existing_row:
                    existing = self._row_to_git_record(existing_row)
                    if not self._git_record_matches(existing, record):
                        raise IntegrationTerminalMismatchError(
                            f"operation {record.integration_operation_id} already has conflicting Git facts"
                        )
                    self._db.commit()
                    return existing
                self._insert_git_record_locked(record)
                self._db.commit()
                return record
            except Exception:
                self._db.rollback()
                raise

    async def finalize_with_git_record(
        self,
        operation_id: str,
        status: str,
        result_digest: str,
        record: GitIntegrationRecord,
        error_code: str = "",
        error_message: str = "",
        finished_at: str = "",
    ) -> tuple[IntegrationOperation, bool]:
        """Atomically persist Git facts and the operation terminal state."""
        async with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                row = self._db.execute(
                    "SELECT * FROM integration_operations WHERE integration_operation_id = ?",
                    (operation_id,),
                ).fetchone()
                if not row:
                    raise KeyError(operation_id)
                current = self._row_to_operation(row)
                existing_row = self._db.execute(
                    "SELECT * FROM git_integration_records WHERE integration_operation_id = ? LIMIT 1",
                    (operation_id,),
                ).fetchone()
                existing_record = self._row_to_git_record(existing_row) if existing_row else None
                if existing_record and not self._git_record_matches(existing_record, record):
                    raise IntegrationTerminalMismatchError(
                        f"operation {operation_id} already has conflicting Git facts"
                    )
                if current.terminal:
                    if current.status == status and current.result_digest == result_digest:
                        if not existing_record:
                            self._insert_git_record_locked(record)
                        self._invalidate_capabilities_locked(operation_id, utc_now())
                        self._db.commit()
                        return current, True
                    raise IntegrationTerminalMismatchError(
                        f"operation {operation_id} already finalized as {current.status}"
                    )
                if status not in {"merged", "conflict", "partial", "failed", "cancelled", "integration_state_uncertain"}:
                    raise ValueError(f"invalid terminal status: {status}")
                now = finished_at or current.finished_at or utc_now()
                cursor = self._db.execute(
                    """
                    UPDATE integration_operations
                    SET status = ?, result_digest = ?, error_code = ?, error_message = ?,
                        started_at = CASE WHEN started_at = '' THEN ? ELSE started_at END,
                        finished_at = ?, row_version = row_version + 1
                    WHERE integration_operation_id = ? AND status IN ('pending', 'integrating')
                    """,
                    (
                        status,
                        result_digest,
                        error_code,
                        error_message,
                        now,
                        now,
                        operation_id,
                    ),
                )
                if cursor.rowcount != 1:
                    raise IntegrationTerminalMismatchError(
                        f"operation {operation_id} changed while finalizing"
                    )
                if not existing_record:
                    self._insert_git_record_locked(record)
                self._invalidate_capabilities_locked(operation_id, now)
                self._db.commit()
                updated = self._db.execute(
                    "SELECT * FROM integration_operations WHERE integration_operation_id = ?",
                    (operation_id,),
                ).fetchone()
                return self._row_to_operation(updated), False
            except Exception:
                self._db.rollback()
                raise

    async def get_git_record(self, operation_id: str) -> GitIntegrationRecord | None:
        async with self._lock:
            row = self._db.execute(
                """SELECT * FROM git_integration_records
                   WHERE integration_operation_id = ?
                   ORDER BY created_at DESC LIMIT 1""",
                (operation_id,),
            ).fetchone()
            return self._row_to_git_record(row) if row else None

    async def save_conflict_record(self, record: ConflictRecord) -> ConflictRecord:
        async with self._lock:
            self._db.execute(
                """
                INSERT INTO conflict_records (
                    conflict_id, root_run_id, original_operation_id, plan_task_id,
                    integration_scope_id, workspace_id, status, attempt,
                    source_branch, source_commit, target_branch, target_commit,
                    merge_base, conflict_files_json, resolver_agent,
                    resolver_session_id, resolver_branch, resolver_run_id,
                    last_error_code, last_error_message, created_at, updated_at,
                    row_version
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(conflict_id) DO UPDATE SET
                    root_run_id = excluded.root_run_id,
                    original_operation_id = excluded.original_operation_id,
                    plan_task_id = excluded.plan_task_id,
                    integration_scope_id = excluded.integration_scope_id,
                    workspace_id = excluded.workspace_id,
                    status = excluded.status,
                    attempt = excluded.attempt,
                    source_branch = excluded.source_branch,
                    source_commit = excluded.source_commit,
                    target_branch = excluded.target_branch,
                    target_commit = excluded.target_commit,
                    merge_base = excluded.merge_base,
                    conflict_files_json = excluded.conflict_files_json,
                    resolver_agent = excluded.resolver_agent,
                    resolver_session_id = excluded.resolver_session_id,
                    resolver_branch = excluded.resolver_branch,
                    resolver_run_id = excluded.resolver_run_id,
                    last_error_code = excluded.last_error_code,
                    last_error_message = excluded.last_error_message,
                    updated_at = excluded.updated_at,
                    row_version = conflict_records.row_version + 1
                """,
                (
                    record.conflict_id,
                    record.root_run_id,
                    record.original_operation_id,
                    record.plan_task_id,
                    record.integration_scope_id,
                    record.workspace_id,
                    record.status,
                    record.attempt,
                    record.source_branch,
                    record.source_commit,
                    record.target_branch,
                    record.target_commit,
                    record.merge_base,
                    json.dumps(record.conflict_files, separators=(",", ":")),
                    record.resolver_agent,
                    record.resolver_session_id,
                    record.resolver_branch,
                    record.resolver_run_id,
                    record.last_error_code,
                    record.last_error_message,
                    record.created_at,
                    record.updated_at,
                    record.row_version,
                ),
            )
            self._db.commit()
            row = self._db.execute(
                "SELECT * FROM conflict_records WHERE conflict_id = ?", (record.conflict_id,)
            ).fetchone()
            return self._row_to_conflict_record(row)

    async def get_conflict_record(self, conflict_id: str) -> ConflictRecord | None:
        async with self._lock:
            row = self._db.execute(
                "SELECT * FROM conflict_records WHERE conflict_id = ?", (conflict_id,)
            ).fetchone()
            return self._row_to_conflict_record(row) if row else None

    async def get_conflict_for_operation(self, operation_id: str) -> ConflictRecord | None:
        async with self._lock:
            row = self._db.execute(
                """SELECT * FROM conflict_records
                   WHERE original_operation_id = ? ORDER BY updated_at DESC LIMIT 1""",
                (operation_id,),
            ).fetchone()
            return self._row_to_conflict_record(row) if row else None

    async def list_resolution_attempts(self, conflict_id: str) -> list[ResolutionAttempt]:
        async with self._lock:
            rows = self._db.execute(
                """SELECT * FROM resolution_attempts
                   WHERE conflict_id = ? ORDER BY attempt, created_at""",
                (conflict_id,),
            ).fetchall()
            return [self._row_to_resolution_attempt(row) for row in rows]

    async def list_resolution_attempts_for_operation(self, operation_id: str) -> list[ResolutionAttempt]:
        async with self._lock:
            rows = self._db.execute(
                """SELECT * FROM resolution_attempts
                   WHERE original_operation_id = ? ORDER BY created_at, attempt""",
                (operation_id,),
            ).fetchall()
            return [self._row_to_resolution_attempt(row) for row in rows]

    async def list_conflict_records(self, statuses: set[str] | None = None) -> list[ConflictRecord]:
        async with self._lock:
            if statuses:
                placeholders = ",".join("?" for _ in statuses)
                rows = self._db.execute(
                    f"SELECT * FROM conflict_records WHERE status IN ({placeholders}) ORDER BY updated_at",
                    tuple(sorted(statuses)),
                ).fetchall()
            else:
                rows = self._db.execute(
                    "SELECT * FROM conflict_records ORDER BY updated_at"
                ).fetchall()
            return [self._row_to_conflict_record(row) for row in rows]

    async def get_conflict_action_by_key(
        self,
        conflict_id: str,
        idempotency_key: str,
    ) -> ConflictActionRecord | None:
        async with self._lock:
            row = self._db.execute(
                "SELECT * FROM conflict_actions WHERE conflict_id = ? AND idempotency_key = ?",
                (conflict_id, idempotency_key),
            ).fetchone()
            return self._row_to_conflict_action(row) if row else None

    async def list_conflict_actions(self, conflict_id: str) -> list[ConflictActionRecord]:
        async with self._lock:
            rows = self._db.execute(
                "SELECT * FROM conflict_actions WHERE conflict_id = ? ORDER BY created_at",
                (conflict_id,),
            ).fetchall()
            return [self._row_to_conflict_action(row) for row in rows]

    async def create_conflict_action(
        self,
        record: ConflictActionRecord,
    ) -> tuple[ConflictActionRecord, bool]:
        async with self._lock:
            try:
                self._db.execute(
                    """
                    INSERT INTO conflict_actions (
                        action_id, conflict_id, action, task_id, session_id, root_run_id,
                        expected_attempt, idempotency_key, status, result_json,
                        error_code, error_message, created_at, finished_at
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        record.action_id,
                        record.conflict_id,
                        record.action,
                        record.task_id,
                        record.session_id,
                        record.root_run_id,
                        record.expected_attempt,
                        record.idempotency_key,
                        record.status,
                        record.result_json,
                        record.error_code,
                        record.error_message,
                        record.created_at,
                        record.finished_at,
                    ),
                )
                self._db.commit()
                return record, True
            except sqlite3.IntegrityError:
                self._db.rollback()
                existing = self._db.execute(
                    "SELECT * FROM conflict_actions WHERE conflict_id = ? AND idempotency_key = ?",
                    (record.conflict_id, record.idempotency_key),
                ).fetchone()
                if not existing:
                    raise
                return self._row_to_conflict_action(existing), False

    async def finish_conflict_action(
        self,
        action_id: str,
        *,
        status: str,
        result_json: str = "",
        error_code: str = "",
        error_message: str = "",
    ) -> ConflictActionRecord | None:
        async with self._lock:
            finished_at = utc_now()
            self._db.execute(
                """
                UPDATE conflict_actions
                SET status = ?, result_json = ?, error_code = ?, error_message = ?, finished_at = ?
                WHERE action_id = ?
                """,
                (status, result_json, error_code, error_message, finished_at, action_id),
            )
            self._db.commit()
            row = self._db.execute(
                "SELECT * FROM conflict_actions WHERE action_id = ?", (action_id,)
            ).fetchone()
            return self._row_to_conflict_action(row) if row else None

    async def finalize_conflict_operation(
        self,
        operation_id: str,
        status: str,
        result_digest: str,
        error_code: str = "",
        error_message: str = "",
        finished_at: str = "",
    ) -> tuple[IntegrationOperation, bool]:
        """Apply an explicit human decision to a terminal conflict.

        Normal taskctl results remain immutable.  This narrow transition is
        the only path that can move ``conflict`` to ``merged``, ``partial`` or
        ``cancelled`` after a user action has been durably audited.
        """
        if status not in {"merged", "partial", "cancelled"}:
            raise ValueError(f"invalid manual conflict status: {status}")
        async with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                row = self._db.execute(
                    "SELECT * FROM integration_operations WHERE integration_operation_id = ?",
                    (operation_id,),
                ).fetchone()
                if not row:
                    raise KeyError(operation_id)
                current = self._row_to_operation(row)
                if current.status == status and current.result_digest == result_digest:
                    self._db.commit()
                    return current, True
                if current.status != "conflict":
                    raise IntegrationTerminalMismatchError(
                        f"operation {operation_id} cannot receive a manual action from {current.status}"
                    )
                now = finished_at or utc_now()
                cursor = self._db.execute(
                    """
                    UPDATE integration_operations
                    SET status = ?, result_digest = ?, error_code = ?, error_message = ?,
                        finished_at = ?, row_version = row_version + 1
                    WHERE integration_operation_id = ? AND status = 'conflict'
                    """,
                    (status, result_digest, error_code, error_message, now, operation_id),
                )
                if cursor.rowcount != 1:
                    raise IntegrationTerminalMismatchError(
                        f"operation {operation_id} changed while applying manual action"
                    )
                self._invalidate_capabilities_locked(operation_id, now)
                self._db.commit()
                updated = self._db.execute(
                    "SELECT * FROM integration_operations WHERE integration_operation_id = ?",
                    (operation_id,),
                ).fetchone()
                return self._row_to_operation(updated), False
            except Exception:
                self._db.rollback()
                raise

    async def save_resolution_integration_record(
        self,
        record: ResolutionIntegrationRecord,
    ) -> ResolutionIntegrationRecord:
        """Persist one immutable resolver Git outcome for a conflict attempt."""
        async with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                operation_row = self._db.execute(
                    """SELECT root_run_id, plan_task_id, workspace_id, integration_scope_id
                       FROM integration_operations
                       WHERE integration_operation_id = ?""",
                    (record.original_operation_id,),
                ).fetchone()
                if not operation_row:
                    raise KeyError(record.original_operation_id)
                if (
                    record.root_run_id != operation_row["root_run_id"]
                    or record.plan_task_id != operation_row["plan_task_id"]
                    or record.integration_scope_id != operation_row["integration_scope_id"]
                ):
                    raise IntegrationTerminalMismatchError(
                        f"resolution record for {record.original_operation_id} does not match operation binding"
                    )
                conflict_row = self._db.execute(
                    "SELECT original_operation_id FROM conflict_records WHERE conflict_id = ?",
                    (record.conflict_id,),
                ).fetchone()
                if not conflict_row:
                    raise KeyError(record.conflict_id)
                if conflict_row["original_operation_id"] != record.original_operation_id:
                    raise IntegrationTerminalMismatchError(
                        f"resolution record conflict {record.conflict_id} belongs to another operation"
                    )
                existing_row = self._db.execute(
                    """SELECT * FROM resolution_integration_records
                       WHERE conflict_id = ? AND attempt = ?""",
                    (record.conflict_id, record.attempt),
                ).fetchone()
                if existing_row:
                    existing = self._row_to_resolution_integration_record(existing_row)
                    if not self._resolution_record_matches(existing, record):
                        raise IntegrationTerminalMismatchError(
                            f"conflict {record.conflict_id} attempt {record.attempt} has conflicting resolution facts"
                        )
                    self._db.commit()
                    return existing
                self._db.execute(
                    """
                    INSERT INTO resolution_integration_records (
                        resolution_record_id, conflict_id, original_operation_id,
                        root_run_id, plan_task_id, integration_scope_id,
                        resolver_run_id, resolver_workspace_id, attempt, status,
                        source_branch, source_commit, target_branch,
                        target_commit_before, target_commit_after, merge_base,
                        conflict_files_json, aborted, error_code, error_message,
                        started_at, finished_at, created_at
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        record.resolution_record_id,
                        record.conflict_id,
                        record.original_operation_id,
                        record.root_run_id,
                        record.plan_task_id,
                        record.integration_scope_id,
                        record.resolver_run_id,
                        record.resolver_workspace_id,
                        record.attempt,
                        record.status,
                        record.source_branch,
                        record.source_commit,
                        record.target_branch,
                        record.target_commit_before,
                        record.target_commit_after,
                        record.merge_base,
                        json.dumps(record.conflict_files, separators=(",", ":")),
                        int(record.aborted),
                        record.error_code,
                        record.error_message,
                        record.started_at,
                        record.finished_at,
                        record.created_at,
                    ),
                )
                self._db.commit()
                return record
            except sqlite3.IntegrityError as exc:
                self._db.rollback()
                raise IntegrationTerminalMismatchError(str(exc)) from exc
            except Exception:
                self._db.rollback()
                raise

    async def get_resolution_record(
        self,
        conflict_id: str,
        attempt: int | None = None,
    ) -> ResolutionIntegrationRecord | None:
        async with self._lock:
            if attempt is None:
                row = self._db.execute(
                    """SELECT * FROM resolution_integration_records
                       WHERE conflict_id = ? ORDER BY attempt DESC LIMIT 1""",
                    (conflict_id,),
                ).fetchone()
            else:
                row = self._db.execute(
                    """SELECT * FROM resolution_integration_records
                       WHERE conflict_id = ? AND attempt = ?""",
                    (conflict_id, attempt),
                ).fetchone()
            return self._row_to_resolution_integration_record(row) if row else None

    async def get_merged_resolution_for_operation(
        self,
        operation_id: str,
    ) -> ResolutionIntegrationRecord | None:
        async with self._lock:
            row = self._db.execute(
                """SELECT * FROM resolution_integration_records
                   WHERE original_operation_id = ? AND status = 'merged'
                   ORDER BY attempt DESC, created_at DESC LIMIT 1""",
                (operation_id,),
            ).fetchone()
            return self._row_to_resolution_integration_record(row) if row else None

    async def list_resolution_integration_records_for_operation(
        self,
        operation_id: str,
    ) -> list[ResolutionIntegrationRecord]:
        async with self._lock:
            rows = self._db.execute(
                """SELECT * FROM resolution_integration_records
                   WHERE original_operation_id = ? ORDER BY attempt, created_at""",
                (operation_id,),
            ).fetchall()
            return [self._row_to_resolution_integration_record(row) for row in rows]

    async def save_resolution_attempt(self, attempt: ResolutionAttempt) -> ResolutionAttempt:
        async with self._lock:
            self._db.execute(
                """
                INSERT OR REPLACE INTO resolution_attempts (
                    resolution_attempt_id, conflict_id, original_operation_id,
                    resolver_run_id, resolver_workspace_id, attempt, status,
                    expected_target_commit, resolver_commit, error_code, error_message,
                    created_at, finished_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    attempt.resolution_attempt_id,
                    attempt.conflict_id,
                    attempt.original_operation_id,
                    attempt.resolver_run_id,
                    attempt.resolver_workspace_id,
                    attempt.attempt,
                    attempt.status,
                    attempt.expected_target_commit,
                    attempt.resolver_commit,
                    attempt.error_code,
                    attempt.error_message,
                    attempt.created_at,
                    attempt.finished_at,
                ),
            )
            self._db.commit()
            return attempt

    async def issue_capability(
        self,
        token_digest: str,
        operation_id: str,
        run_id: str,
        workspace_id: str,
        allowed_action: str,
        expires_at: str,
    ) -> None:
        async with self._lock:
            self._db.execute(
                """
                INSERT INTO integration_capabilities (
                    token_digest, integration_operation_id, run_id, workspace_id,
                    allowed_action, expires_at
                ) VALUES (?, ?, ?, ?, ?, ?)
                """,
                (token_digest, operation_id, run_id, workspace_id, allowed_action, expires_at),
            )
            self._db.commit()

    async def redeem_capability(
        self,
        token_digest: str,
        operation_id: str,
        run_id: str,
        workspace_id: str,
        action: str,
    ) -> bool:
        async with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                row = self._db.execute(
                    "SELECT * FROM integration_capabilities WHERE token_digest = ?",
                    (token_digest,),
                ).fetchone()
                if (
                    not row
                    or row["integration_operation_id"] != operation_id
                    or row["run_id"] != run_id
                    or row["workspace_id"] != workspace_id
                    or row["allowed_action"] != action
                    or row["redeemed_at"]
                    or is_expired(row["expires_at"])
                ):
                    self._db.rollback()
                    return False
                operation = self._db.execute(
                    "SELECT status FROM integration_operations WHERE integration_operation_id = ?",
                    (operation_id,),
                ).fetchone()
                if not operation or operation["status"] not in {"pending", "integrating"}:
                    self._db.rollback()
                    return False
                from src.integration.models import utc_now

                cursor = self._db.execute(
                    """UPDATE integration_capabilities SET redeemed_at = ?
                       WHERE token_digest = ? AND redeemed_at = ''""",
                    (utc_now(), token_digest),
                )
                self._db.commit()
                return cursor.rowcount == 1
            except Exception:
                self._db.rollback()
                raise

    async def authorize_terminal_capability_replay(
        self,
        token_digest: str,
        operation_id: str,
        run_id: str,
        workspace_id: str,
        action: str,
    ) -> bool:
        """Authorize a read-only replay after the operation reached a terminal state.

        A redeemed/invalidated token can never start Git again. This narrow
        path exists so a retried RPC can receive the already-persisted
        projection without weakening the single-use mutation guarantee.
        """
        async with self._lock:
            row = self._db.execute(
                "SELECT * FROM integration_capabilities WHERE token_digest = ?",
                (token_digest,),
            ).fetchone()
            if (
                not row
                or row["integration_operation_id"] != operation_id
                or row["run_id"] != run_id
                or row["workspace_id"] != workspace_id
                or row["allowed_action"] != action
                or not row["redeemed_at"]
                or is_expired(row["expires_at"])
            ):
                return False
            operation = self._db.execute(
                "SELECT status FROM integration_operations WHERE integration_operation_id = ?",
                (operation_id,),
            ).fetchone()
            return bool(
                operation
                and operation["status"]
                in {"merged", "partial", "conflict", "failed", "cancelled", "integration_state_uncertain"}
            )

    @staticmethod
    def _git_record_matches(left: GitIntegrationRecord, right: GitIntegrationRecord) -> bool:
        """Compare immutable Git facts while ignoring storage identity only."""
        fields = (
            "integration_operation_id",
            "workspace_id",
            "integration_scope_id",
            "status",
            "source_branch",
            "source_commit",
            "target_branch",
            "target_commit_before",
            "target_commit_after",
            "merge_base",
            "conflict_files",
            "aborted",
            "git_exit_code",
            "error_code",
            "error_message",
            "started_at",
            "finished_at",
        )
        return all(getattr(left, field) == getattr(right, field) for field in fields)

    @staticmethod
    def _integration_intent_matches(left: IntegrationIntent, right: IntegrationIntent) -> bool:
        fields = (
            "integration_operation_id",
            "workspace_id",
            "integration_scope_id",
            "source_branch",
            "source_commit",
            "target_branch",
            "target_commit_before",
            "merge_base",
        )
        return all(getattr(left, field) == getattr(right, field) for field in fields)

    @staticmethod
    def _resolution_record_matches(
        left: ResolutionIntegrationRecord,
        right: ResolutionIntegrationRecord,
    ) -> bool:
        fields = (
            "conflict_id",
            "original_operation_id",
            "root_run_id",
            "plan_task_id",
            "integration_scope_id",
            "resolver_run_id",
            "resolver_workspace_id",
            "attempt",
            "status",
            "source_branch",
            "source_commit",
            "target_branch",
            "target_commit_before",
            "target_commit_after",
            "merge_base",
            "conflict_files",
            "aborted",
            "error_code",
            "error_message",
        )
        return all(getattr(left, field) == getattr(right, field) for field in fields)

    @staticmethod
    def _row_to_operation(row: sqlite3.Row) -> IntegrationOperation:
        return IntegrationOperation(
            integration_operation_id=row["integration_operation_id"],
            root_run_id=row["root_run_id"],
            parent_run_id=row["parent_run_id"],
            run_id=row["run_id"],
            plan_task_id=row["plan_task_id"],
            attempt=int(row["attempt"]),
            session_id=row["session_id"],
            workspace_id=row["workspace_id"],
            workspace_handle=row["workspace_handle"],
            integration_scope_id=row["integration_scope_id"],
            status=row["status"],
            result_digest=row["result_digest"],
            error_code=row["error_code"],
            error_message=row["error_message"],
            created_at=row["created_at"],
            started_at=row["started_at"],
            finished_at=row["finished_at"],
            row_version=int(row["row_version"]),
        )

    @staticmethod
    def _row_to_integration_intent(row: sqlite3.Row) -> IntegrationIntent:
        return IntegrationIntent(
            integration_operation_id=row["integration_operation_id"],
            workspace_id=row["workspace_id"],
            integration_scope_id=row["integration_scope_id"],
            source_branch=row["source_branch"],
            source_commit=row["source_commit"],
            target_branch=row["target_branch"],
            target_commit_before=row["target_commit_before"],
            merge_base=row["merge_base"],
            created_at=row["created_at"],
        )

    @staticmethod
    def _row_to_git_record(row: sqlite3.Row) -> GitIntegrationRecord:
        return GitIntegrationRecord(
            record_id=row["record_id"],
            integration_operation_id=row["integration_operation_id"],
            workspace_id=row["workspace_id"],
            integration_scope_id=row["integration_scope_id"],
            status=row["status"],
            source_branch=row["source_branch"],
            source_commit=row["source_commit"],
            target_branch=row["target_branch"],
            target_commit_before=row["target_commit_before"],
            target_commit_after=row["target_commit_after"],
            merge_base=row["merge_base"],
            conflict_files=json.loads(row["conflict_files_json"] or "[]"),
            aborted=bool(row["aborted"]),
            git_exit_code=row["git_exit_code"],
            error_code=row["error_code"],
            error_message=row["error_message"],
            started_at=row["started_at"],
            finished_at=row["finished_at"],
            created_at=row["created_at"],
        )

    @staticmethod
    def _row_to_conflict_record(row: sqlite3.Row) -> ConflictRecord:
        return ConflictRecord(
            conflict_id=row["conflict_id"],
            root_run_id=row["root_run_id"],
            original_operation_id=row["original_operation_id"],
            plan_task_id=row["plan_task_id"],
            integration_scope_id=row["integration_scope_id"],
            workspace_id=row["workspace_id"],
            status=row["status"],
            attempt=int(row["attempt"]),
            source_branch=row["source_branch"],
            source_commit=row["source_commit"],
            target_branch=row["target_branch"],
            target_commit=row["target_commit"],
            merge_base=row["merge_base"],
            conflict_files=json.loads(row["conflict_files_json"] or "[]"),
            resolver_agent=row["resolver_agent"],
            resolver_session_id=row["resolver_session_id"],
            resolver_branch=row["resolver_branch"],
            resolver_run_id=row["resolver_run_id"],
            last_error_code=row["last_error_code"],
            last_error_message=row["last_error_message"],
            created_at=row["created_at"],
            updated_at=row["updated_at"],
            row_version=int(row["row_version"]),
        )

    @staticmethod
    def _row_to_resolution_attempt(row: sqlite3.Row) -> ResolutionAttempt:
        return ResolutionAttempt(
            resolution_attempt_id=row["resolution_attempt_id"],
            conflict_id=row["conflict_id"],
            original_operation_id=row["original_operation_id"],
            resolver_run_id=row["resolver_run_id"],
            resolver_workspace_id=row["resolver_workspace_id"],
            attempt=int(row["attempt"]),
            status=row["status"],
            expected_target_commit=row["expected_target_commit"],
            resolver_commit=row["resolver_commit"],
            error_code=row["error_code"],
            error_message=row["error_message"],
            created_at=row["created_at"],
            finished_at=row["finished_at"],
        )

    @staticmethod
    def _row_to_resolution_integration_record(row: sqlite3.Row) -> ResolutionIntegrationRecord:
        return ResolutionIntegrationRecord(
            resolution_record_id=row["resolution_record_id"],
            conflict_id=row["conflict_id"],
            original_operation_id=row["original_operation_id"],
            root_run_id=row["root_run_id"],
            plan_task_id=row["plan_task_id"],
            integration_scope_id=row["integration_scope_id"],
            resolver_run_id=row["resolver_run_id"],
            resolver_workspace_id=row["resolver_workspace_id"],
            attempt=int(row["attempt"]),
            status=row["status"],
            source_branch=row["source_branch"],
            source_commit=row["source_commit"],
            target_branch=row["target_branch"],
            target_commit_before=row["target_commit_before"],
            target_commit_after=row["target_commit_after"],
            merge_base=row["merge_base"],
            conflict_files=json.loads(row["conflict_files_json"] or "[]"),
            aborted=bool(row["aborted"]),
            error_code=row["error_code"],
            error_message=row["error_message"],
            started_at=row["started_at"],
            finished_at=row["finished_at"],
            created_at=row["created_at"],
        )

    @staticmethod
    def _row_to_conflict_action(row: sqlite3.Row) -> ConflictActionRecord:
        return ConflictActionRecord(
            action_id=row["action_id"],
            conflict_id=row["conflict_id"],
            action=row["action"],
            task_id=row["task_id"],
            session_id=row["session_id"],
            root_run_id=row["root_run_id"],
            expected_attempt=int(row["expected_attempt"]),
            idempotency_key=row["idempotency_key"],
            status=row["status"],
            result_json=row["result_json"],
            error_code=row["error_code"],
            error_message=row["error_message"],
            created_at=row["created_at"],
            finished_at=row["finished_at"],
        )
