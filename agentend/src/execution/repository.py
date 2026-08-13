from __future__ import annotations

import asyncio
import json
import sqlite3
from pathlib import Path

from src.execution.models import RunRecord, RunSpec, utc_now
from src.generated.agent_run import AgentRunBudget, AgentRunEventEnvelope, AgentRunState


class RunConflictError(ValueError):
    pass


class ParentRunClosedError(ValueError):
    pass


class SQLiteRunRepository:
    """Durable Run metadata and bounded event journal.

    SQLite transactions serialize idempotent creation, parent admission and
    state changes without holding an asyncio lock across process execution.
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
            CREATE TABLE IF NOT EXISTS runs (
                run_id TEXT PRIMARY KEY,
                root_run_id TEXT NOT NULL,
                parent_run_id TEXT,
                task_id TEXT NOT NULL,
                session_id TEXT NOT NULL,
                message_id TEXT,
                workspace_id TEXT NOT NULL,
                agent_type TEXT NOT NULL,
                requested_by TEXT NOT NULL,
                request_fingerprint TEXT NOT NULL DEFAULT '',
                budget_json TEXT NOT NULL,
                spec_hash TEXT NOT NULL,
                state TEXT NOT NULL,
                termination_reason TEXT,
                created_at TEXT NOT NULL,
                started_at TEXT,
                finished_at TEXT,
                last_event_seq INTEGER NOT NULL DEFAULT 0,
                admission_closed INTEGER NOT NULL DEFAULT 0,
                runtime_json TEXT NOT NULL DEFAULT '{}'
            );
            CREATE INDEX IF NOT EXISTS idx_runs_root ON runs(root_run_id);
            CREATE INDEX IF NOT EXISTS idx_runs_session_state ON runs(session_id, state);
            CREATE TABLE IF NOT EXISTS run_events (
                run_id TEXT NOT NULL,
                seq INTEGER NOT NULL,
                event_json TEXT NOT NULL,
                timestamp REAL NOT NULL,
                PRIMARY KEY(run_id, seq),
                FOREIGN KEY(run_id) REFERENCES runs(run_id) ON DELETE CASCADE
            );
            """
        )
        columns = {row["name"] for row in self._db.execute("PRAGMA table_info(runs)").fetchall()}
        if "request_fingerprint" not in columns:
            self._db.execute("ALTER TABLE runs ADD COLUMN request_fingerprint TEXT NOT NULL DEFAULT ''")
        self._db.commit()

    async def close(self) -> None:
        async with self._lock:
            self._db.close()

    async def create(self, spec: RunSpec) -> tuple[RunRecord, bool]:
        async with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                existing = self._db.execute("SELECT * FROM runs WHERE run_id = ?", (spec.run_id,)).fetchone()
                if existing:
                    if existing["spec_hash"] != spec.fingerprint():
                        raise RunConflictError("run_id already exists with a different immutable spec")
                    self._db.commit()
                    return self._row_to_record(existing), False

                if not spec.parent_run_id and spec.root_run_id != spec.run_id:
                    raise ParentRunClosedError("root run must reference itself")

                active_session = self._db.execute(
                    """SELECT run_id FROM runs
                       WHERE session_id = ? AND state NOT IN (?, ?, ?) LIMIT 1""",
                    (
                        spec.session_id,
                        AgentRunState.COMPLETED.value,
                        AgentRunState.FAILED.value,
                        AgentRunState.CANCELLED.value,
                    ),
                ).fetchone()
                if active_session:
                    raise RunConflictError("session already has an active run")

                if spec.parent_run_id:
                    parent = self._db.execute(
                        "SELECT root_run_id, state, admission_closed FROM runs WHERE run_id = ?",
                        (spec.parent_run_id,),
                    ).fetchone()
                    if not parent:
                        raise ParentRunClosedError("parent run does not exist")
                    if parent["root_run_id"] != spec.root_run_id:
                        raise ParentRunClosedError("parent run belongs to a different root")
                    parent_details = self._db.execute(
                        "SELECT task_id, budget_json FROM runs WHERE run_id = ?", (spec.parent_run_id,)
                    ).fetchone()
                    if parent_details["task_id"] != spec.task_id:
                        raise ParentRunClosedError("parent run belongs to a different task")
                    parent_budget = AgentRunBudget.model_validate_json(parent_details["budget_json"])
                    child_budget = spec.budget.model_dump()
                    parent_limits = parent_budget.model_dump()
                    expanded = [
                        name for name, value in child_budget.items() if value > parent_limits[name]
                    ]
                    if expanded:
                        raise ParentRunClosedError(
                            "child run budget exceeds parent limits: " + ", ".join(sorted(expanded))
                        )
                    child_count = self._db.execute(
                        "SELECT COUNT(*) AS count FROM runs WHERE parent_run_id = ?", (spec.parent_run_id,)
                    ).fetchone()["count"]
                    if int(child_count) >= parent_budget.max_children:
                        raise ParentRunClosedError("parent run child budget exhausted")
                    if parent["admission_closed"] or parent["state"] in {
                        AgentRunState.CANCELLING.value,
                        AgentRunState.CANCELLED.value,
                        AgentRunState.COMPLETED.value,
                        AgentRunState.FAILED.value,
                    }:
                        raise ParentRunClosedError("parent run no longer accepts children")
                    root = self._db.execute(
                        "SELECT state, admission_closed, budget_json FROM runs WHERE run_id = ?",
                        (spec.root_run_id,),
                    ).fetchone()
                    if not root or root["admission_closed"] or root["state"] in {
                        AgentRunState.CANCELLING.value,
                        AgentRunState.CANCELLED.value,
                        AgentRunState.COMPLETED.value,
                        AgentRunState.FAILED.value,
                    }:
                        raise ParentRunClosedError("root run no longer accepts children")
                    root_budget = AgentRunBudget.model_validate_json(root["budget_json"])
                    descendant_count = self._db.execute(
                        "SELECT COUNT(*) AS count FROM runs WHERE root_run_id = ? AND run_id != ?",
                        (spec.root_run_id, spec.root_run_id),
                    ).fetchone()["count"]
                    if int(descendant_count) >= root_budget.max_children:
                        raise ParentRunClosedError("root run child budget exhausted")
                    terminal = (
                        AgentRunState.COMPLETED.value,
                        AgentRunState.FAILED.value,
                        AgentRunState.CANCELLED.value,
                    )
                    active_descendants = self._db.execute(
                        """SELECT COUNT(*) AS count FROM runs
                           WHERE root_run_id = ? AND run_id != ?
                             AND state NOT IN (?, ?, ?)""",
                        (spec.root_run_id, spec.root_run_id, *terminal),
                    ).fetchone()["count"]
                    if int(active_descendants) >= root_budget.max_parallelism:
                        raise ParentRunClosedError("root run parallelism budget exhausted")

                now = utc_now()
                self._db.execute(
                    """INSERT INTO runs (
                        run_id, root_run_id, parent_run_id, task_id, session_id, message_id,
                        workspace_id, agent_type, requested_by, request_fingerprint, budget_json, spec_hash, state,
                        created_at
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                    (
                        spec.run_id,
                        spec.root_run_id,
                        spec.parent_run_id,
                        spec.task_id,
                        spec.session_id,
                        spec.message_id,
                        spec.workspace_id,
                        spec.agent_type,
                        spec.requested_by,
                        spec.request_fingerprint,
                        spec.budget.model_dump_json(),
                        spec.fingerprint(),
                        AgentRunState.QUEUED.value,
                        now,
                    ),
                )
                self._db.commit()
                row = self._db.execute("SELECT * FROM runs WHERE run_id = ?", (spec.run_id,)).fetchone()
                return self._row_to_record(row), True
            except Exception:
                self._db.rollback()
                raise

    async def get(self, run_id: str) -> RunRecord | None:
        async with self._lock:
            row = self._db.execute("SELECT * FROM runs WHERE run_id = ?", (run_id,)).fetchone()
            return self._row_to_record(row) if row else None

    async def list_active(self) -> list[RunRecord]:
        terminal = (AgentRunState.COMPLETED.value, AgentRunState.FAILED.value, AgentRunState.CANCELLED.value)
        async with self._lock:
            rows = self._db.execute(
                "SELECT * FROM runs WHERE state NOT IN (?, ?, ?) ORDER BY created_at", terminal
            ).fetchall()
            return [self._row_to_record(row) for row in rows]

    async def list_active_by_session(self, session_id: str) -> list[RunRecord]:
        terminal = (AgentRunState.COMPLETED.value, AgentRunState.FAILED.value, AgentRunState.CANCELLED.value)
        async with self._lock:
            rows = self._db.execute(
                """SELECT * FROM runs
                   WHERE session_id = ? AND state NOT IN (?, ?, ?)
                   ORDER BY created_at""",
                (session_id, *terminal),
            ).fetchall()
            return [self._row_to_record(row) for row in rows]

    async def children(self, run_id: str) -> list[RunRecord]:
        async with self._lock:
            rows = self._db.execute(
                "SELECT * FROM runs WHERE parent_run_id = ? ORDER BY created_at", (run_id,)
            ).fetchall()
            return [self._row_to_record(row) for row in rows]

    async def transition(
        self,
        run_id: str,
        expected: set[AgentRunState],
        target: AgentRunState,
        reason: str | None = None,
    ) -> bool:
        async with self._lock:
            fields = ["state = ?", "termination_reason = COALESCE(?, termination_reason)"]
            params: list[object] = [target.value, reason]
            if target == AgentRunState.RUNNING:
                fields.append("started_at = COALESCE(started_at, ?)")
                params.append(utc_now())
            if target in {AgentRunState.COMPLETED, AgentRunState.FAILED, AgentRunState.CANCELLED}:
                fields.append("finished_at = COALESCE(finished_at, ?)")
                params.append(utc_now())
                fields.append("admission_closed = 1")
            placeholders = ",".join("?" for _ in expected)
            params.extend([run_id, *(state.value for state in expected)])
            cursor = self._db.execute(
                f"UPDATE runs SET {', '.join(fields)} WHERE run_id = ? AND state IN ({placeholders})",
                params,
            )
            self._db.commit()
            return cursor.rowcount == 1

    async def close_admission(self, run_id: str) -> None:
        async with self._lock:
            self._db.execute("UPDATE runs SET admission_closed = 1 WHERE run_id = ?", (run_id,))
            self._db.commit()

    async def append_event(self, run_id: str, event: dict, timestamp: float) -> AgentRunEventEnvelope:
        async with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                row = self._db.execute(
                    "SELECT last_event_seq FROM runs WHERE run_id = ?", (run_id,)
                ).fetchone()
                if not row:
                    raise KeyError(run_id)
                seq = int(row["last_event_seq"]) + 1
                self._db.execute(
                    "INSERT INTO run_events(run_id, seq, event_json, timestamp) VALUES (?, ?, ?, ?)",
                    (run_id, seq, json.dumps(event, separators=(",", ":")), timestamp),
                )
                self._db.execute("UPDATE runs SET last_event_seq = ? WHERE run_id = ?", (seq, run_id))
                self._db.commit()
                return AgentRunEventEnvelope(run_id=run_id, seq=seq, event=event, timestamp=timestamp)
            except Exception:
                self._db.rollback()
                raise

    async def read_events(self, run_id: str, after_seq: int, limit: int = 1000) -> list[AgentRunEventEnvelope]:
        async with self._lock:
            rows = self._db.execute(
                """SELECT seq, event_json, timestamp FROM run_events
                   WHERE run_id = ? AND seq > ? ORDER BY seq LIMIT ?""",
                (run_id, max(0, after_seq), max(1, min(limit, 5000))),
            ).fetchall()
            return [
                AgentRunEventEnvelope(
                    run_id=run_id,
                    seq=int(row["seq"]),
                    event=json.loads(row["event_json"]),
                    timestamp=float(row["timestamp"]),
                )
                for row in rows
            ]

    @staticmethod
    def _row_to_record(row: sqlite3.Row) -> RunRecord:
        spec = RunSpec(
            run_id=row["run_id"],
            root_run_id=row["root_run_id"],
            parent_run_id=row["parent_run_id"],
            task_id=row["task_id"],
            session_id=row["session_id"],
            message_id=row["message_id"],
            workspace_id=row["workspace_id"],
            agent_type=row["agent_type"],
            requested_by=row["requested_by"],
            request_fingerprint=row["request_fingerprint"],
            budget=AgentRunBudget.model_validate_json(row["budget_json"]),
        )
        return RunRecord(
            spec=spec,
            state=AgentRunState(row["state"]),
            termination_reason=row["termination_reason"],
            created_at=row["created_at"],
            started_at=row["started_at"],
            finished_at=row["finished_at"],
            last_event_seq=int(row["last_event_seq"]),
            admission_closed=bool(row["admission_closed"]),
            runtime=json.loads(row["runtime_json"] or "{}"),
        )
