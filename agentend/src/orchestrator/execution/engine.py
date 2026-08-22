from __future__ import annotations

import asyncio
import hashlib
import json
import logging
import time
import uuid
from collections.abc import AsyncIterator
from pathlib import Path
from pydantic import ValidationError

from src.clients.backend_client import BackendClient
from src.generated.events import EventType
from src.app.config import settings
from src.integration.errors import (
    ERROR_INTEGRATION_RESULT_INVALID,
    ERROR_OPERATION_BINDING_MISMATCH,
    ERROR_OPERATION_NOT_FOUND,
    IntegrationError,
    sanitize_error_text,
)
from src.integration.models import ConflictRecord as IntegrationConflictRecord
from src.integration.models import ResolutionAttempt, ResolutionIntegrationRecord, utc_now
from src.integration.service import IntegrationService
from src.orchestrator.models import (
    ConflictRecord,
    DispatchResult,
    IntegrationResult,
    IntegrationResultV1,
    IntegrationResultV2,
    TaskResult,
)
from src.persistence import atomic_write_text
from src.schemas.events import StreamEvent
from src.schemas.request import AgentType
from src.workspace.manager import WorkspaceManager

logger = logging.getLogger(__name__)


class ExecutionEngine:
    def __init__(
        self,
        backend_client: BackendClient,
        workspace_mgr: WorkspaceManager | None = None,
        repo_path: str = "",
        task_id: str = "",
        shared_dir: str = "",
        cwd: str = "",
        root_run_id: str = "",
        parent_run_id: str = "",
        current_run_id: str = "",
        budget: dict | None = None,
        agents: list[dict] | None = None,
        integration_service: IntegrationService | None = None,
    ) -> None:
        self._backend_client = backend_client
        self._workspace_mgr = workspace_mgr
        self._repo_path = repo_path
        self._task_id = task_id
        self._shared_dir = shared_dir
        self._cwd = cwd
        self._root_run_id = root_run_id
        self._parent_run_id = parent_run_id
        self._current_run_id = current_run_id
        self._budget = budget
        self._agents = agents or []
        self._integration_service = integration_service
        self._invalid_integration_results: set[str] = set()
        self._invalid_integration_errors: dict[str, str] = {}

    async def execute(
        self,
        dispatches: list[DispatchResult],
        timeout_per_task: float = 300.0,
    ) -> AsyncIterator[tuple[StreamEvent, TaskResult | None]]:
        if len(dispatches) <= 1:
            for dispatch in dispatches:
                async for item in self._execute_with_retries(dispatch, timeout_per_task):
                    yield item
            return

        queue: asyncio.Queue[tuple[StreamEvent, TaskResult | None] | BaseException | None] = asyncio.Queue()

        async def _run(dispatch: DispatchResult) -> None:
            try:
                async for item in self._execute_with_retries(dispatch, timeout_per_task):
                    await queue.put(item)
            except asyncio.CancelledError:
                raise
            except BaseException as exc:
                await queue.put(exc)

        tasks = [asyncio.create_task(_run(d)) for d in dispatches]

        async def _drain() -> None:
            try:
                await asyncio.gather(*tasks, return_exceptions=True)
            finally:
                await queue.put(None)  # 哨兵值

        drain_task = asyncio.create_task(_drain())

        try:
            while True:
                item = await queue.get()
                if item is None:
                    break
                if isinstance(item, BaseException):
                    raise item
                yield item
            await drain_task
        finally:
            for task in tasks:
                if not task.done():
                    task.cancel()
            if not drain_task.done():
                drain_task.cancel()
            await asyncio.gather(*tasks, drain_task, return_exceptions=True)

    async def _execute_with_retries(
        self,
        dispatch: DispatchResult,
        timeout: float,
    ) -> AsyncIterator[tuple[StreamEvent, TaskResult | None]]:
        """Retry only execution/integration failures that are safe to repeat.

        Git conflicts are deliberately excluded: their committed source branch
        is already a recovery input and must go through Resolver instead of
        rerunning the original task.
        """
        max_attempts = max(1, settings.orchestrator.execution_retry_max_attempts)
        for offset in range(max_attempts):
            attempt_dispatch = dispatch.model_copy(update={"attempt": dispatch.attempt + offset})
            final_result: TaskResult | None = None
            try:
                async for item in self._execute_task(attempt_dispatch, timeout):
                    if item[1] is not None:
                        final_result = item[1]
                    yield item
            except asyncio.CancelledError:
                raise
            except Exception:
                if offset + 1 >= max_attempts:
                    raise
                logger.warning(
                    "ExecutionEngine: retrying task=%s after attempt=%d/%d raised",
                    dispatch.task_id,
                    offset + 1,
                    max_attempts,
                    exc_info=True,
                )
                continue

            if final_result is None or offset + 1 >= max_attempts:
                return
            if not self._should_retry_result(final_result):
                return
            logger.warning(
                "ExecutionEngine: retrying task=%s after attempt=%d/%d error_code=%s",
                dispatch.task_id,
                offset + 1,
                max_attempts,
                final_result.error_code,
            )

    @staticmethod
    def _should_retry_result(result: TaskResult) -> bool:
        if result.execution_status in {"failed", "timeout"}:
            return True
        if result.execution_status != "completed" or result.integration_status != "failed":
            return False
        return result.error_code in {
            "integration_missing",
            "commit_failed",
            "source_missing",
        }

    def _child_budget(self, timeout: float) -> dict:
        budget = dict(self._budget or {})
        wall_time = max(1, int(timeout))
        parent_wall_time = budget.get("wall_time_seconds")
        if isinstance(parent_wall_time, (int, float)) and parent_wall_time > 0:
            wall_time = min(wall_time, int(parent_wall_time))
        budget["wall_time_seconds"] = wall_time
        return budget

    async def _validate_registered_git_lineage(
        self,
        result: IntegrationResult,
        dispatch: DispatchResult,
    ) -> None:
        """Bind legacy file facts to the registered workspace before use.

        V1 has no operation ID, so branch names are the remaining claimable
        Git identity. They must come from the active WorkspaceManager record;
        a deterministic string assembled from task/session values is not
        sufficient authority on its own.
        """
        if self._workspace_mgr is None:
            if self._integration_service:
                raise ValueError("workspace_missing: registered workspace manager is unavailable")
            return
        workspace = None
        if self._integration_service and dispatch.integration_operation_id:
            operation = await self._integration_service.repository.get(dispatch.integration_operation_id)
            if operation is not None:
                workspace = self._workspace_mgr.get(operation.workspace_id)
        if workspace is None and hasattr(self._workspace_mgr, "get_by_task_and_session"):
            workspace = self._workspace_mgr.get_by_task_and_session(self._task_id, dispatch.real_session_id)
        if workspace is None:
            workspace = self._workspace_mgr.get_by_session(dispatch.real_session_id)
        if workspace is None:
            raise ValueError("workspace_missing: registered workspace is unavailable")
        workspace_status = getattr(getattr(workspace, "status", None), "value", getattr(workspace, "status", ""))
        if workspace_status != "active" or workspace.task_id != self._task_id:
            raise ValueError("operation_binding_mismatch: workspace scope is not active")
        if workspace.session_id != dispatch.real_session_id:
            raise ValueError("operation_binding_mismatch: workspace session mismatch")
        expected_source = workspace.branch_name
        expected_target = f"task/{workspace.task_id}"
        if result.source_branch != expected_source:
            raise ValueError("operation_binding_mismatch: source_branch mismatch")
        if result.target_branch != expected_target:
            raise ValueError("operation_binding_mismatch: target_branch mismatch")
        if result.status == "merged" and any(
            not value
            for value in (
                result.source_commit,
                result.target_commit,
                result.target_commit_after,
                result.merge_base,
            )
        ):
            raise ValueError("integration_result_invalid: merged result is missing Git facts")
        probe = None
        if hasattr(self._workspace_mgr, "probe_integration"):
            intent = None
            if self._integration_service and dispatch.integration_operation_id:
                intent = await self._integration_service.repository.get_integration_intent(
                    dispatch.integration_operation_id
                )
                operation = await self._integration_service.repository.get(dispatch.integration_operation_id)
            else:
                operation = None
            if self._integration_service and operation is not None:
                probe = await self._integration_service._probe_workspace(
                    self._workspace_mgr,
                    operation,
                    intent,
                    expected_source_commit=result.source_commit,
                    expected_target_commit_before=result.target_commit,
                    expected_merge_base=result.merge_base,
                )
            else:
                probe = await self._workspace_mgr.probe_integration(
                    workspace.id,
                    expected_source_commit=result.source_commit,
                    expected_target_commit_before=result.target_commit,
                    expected_merge_base=result.merge_base,
                )
            if probe.source_branch and probe.source_branch != expected_source:
                raise ValueError("operation_binding_mismatch: Git source branch mismatch")
            if probe.target_branch and probe.target_branch != expected_target:
                raise ValueError("operation_binding_mismatch: Git target branch mismatch")
            if result.status == "merged":
                if not probe.success:
                    raise ValueError("integration_result_invalid: Git probe does not prove merged state")
                if result.source_commit != probe.source_commit:
                    raise ValueError("operation_binding_mismatch: source commit is not authoritative")
                if result.target_commit != probe.target_commit:
                    raise ValueError("operation_binding_mismatch: target commit before is not authoritative")
                if result.target_commit_after != probe.target_commit_after:
                    raise ValueError("operation_binding_mismatch: target commit after is not authoritative")
                if result.merge_base != probe.merge_base:
                    raise ValueError("operation_binding_mismatch: merge base is not authoritative")
            elif result.status == "conflict" and result.aborted and result.target_commit:
                if probe.target_commit != result.target_commit:
                    raise ValueError("operation_binding_mismatch: aborted conflict moved target commit")
        if result.source_commit:
            current_source = await self._workspace_mgr.current_commit(workspace.id)
            if current_source and current_source != result.source_commit:
                raise ValueError("operation_binding_mismatch: source commit moved")

    @staticmethod
    def _projection_identity(dispatch: DispatchResult) -> dict[str, str]:
        """Identity carried by normal runtime SSE without Git path details."""
        return {
            "plan_task_id": dispatch.plan_task_id or dispatch.task_id,
            "integration_operation_id": dispatch.integration_operation_id,
        }

    @staticmethod
    def _conflict_projection_id(dispatch: DispatchResult, result: TaskResult | IntegrationResult) -> str:
        if dispatch.integration_operation_id:
            return IntegrationService.conflict_id_for(
                dispatch.integration_operation_id,
                result.source_commit,
                result.target_commit,
                result.conflict_files,
            )
        return hashlib.sha256(
            "|".join(
                (
                    result.source_commit,
                    result.target_commit,
                    ",".join(sorted(result.conflict_files)),
                )
            ).encode("utf-8")
        ).hexdigest()[:20]

    async def _ensure_worktree(self, dispatch: DispatchResult) -> str:
        if not self._workspace_mgr or not self._repo_path:
            return self._cwd or dispatch.workspace_path

        real_session_id = dispatch.real_session_id
        if not real_session_id:
            logger.warning("ExecutionEngine: no real_session_id for task=%s, fallback to shared cwd", dispatch.task_id)
            return self._cwd or dispatch.workspace_path

        try:
            agent_type_str = dispatch.agent_type or dispatch.agent
            try:
                agent_type = AgentType(agent_type_str)
            except ValueError:
                agent_type = AgentType.CLAUDE_CODE

            ws = await self._workspace_mgr.create(
                repo_path=self._repo_path,
                task_id=self._task_id,
                agent_name=dispatch.agent,
                session_id=real_session_id,
                agent_type=agent_type,
            )
            logger.info(
                "ExecutionEngine: created worktree for agent=%s session=%s",
                dispatch.agent,
                real_session_id,
            )
            return ws.worktree_path
        except Exception:
            logger.exception(
                "ExecutionEngine: failed to create worktree for agent=%s session=%s",
                dispatch.agent,
                real_session_id,
            )
            return self._cwd or dispatch.workspace_path

    async def _read_integration_result(
        self,
        run_id: str,
        dispatch: DispatchResult,
        timeout: float = 3.0,
    ) -> IntegrationResult | None:
        """Read one atomically-written result and reject stale/mismatched facts."""
        if not self._shared_dir or not run_id:
            return None

        self._invalid_integration_results.discard(run_id)
        result_path = Path(self._shared_dir) / "integration-results" / f"{run_id}.json"
        deadline = asyncio.get_running_loop().time() + max(0.1, timeout)
        while asyncio.get_running_loop().time() < deadline:
            result_version: int | str = "unknown"
            try:
                if result_path.stat().st_size > 256 * 1024:
                    raise ValueError("integration_result_invalid")
                raw = json.loads(result_path.read_text(encoding="utf-8"))
                if not isinstance(raw, dict) or "version" not in raw:
                    raise ValueError("integration_result_invalid")
                version = raw["version"]
                if isinstance(version, bool) or not isinstance(version, int):
                    raise ValueError("integration_result_invalid")
                result_version = version
                if version == 1:
                    legacy = IntegrationResultV1.model_validate(raw)
                    result = IntegrationResult.model_validate(legacy.model_dump())
                elif version == 2:
                    current = IntegrationResultV2.model_validate(raw)
                    result = IntegrationResult.model_validate(current.model_dump())
                else:
                    raise ValueError("integration_version_unsupported")
                expected_target = f"task/{self._task_id}"
                if result.run_id != run_id:
                    raise ValueError("operation_binding_mismatch: run_id mismatch")
                # V1 task_id is the Git integration scope extracted from the
                # worktree path. It is never the planner's task-001/task-002.
                if result.version == 1 and result.task_id != self._task_id:
                    raise ValueError("operation_binding_mismatch: integration_scope_id mismatch")
                if result.session_id != dispatch.real_session_id:
                    raise ValueError("operation_binding_mismatch: session_id mismatch")
                if result.attempt != dispatch.attempt:
                    raise ValueError("operation_stale_attempt: attempt mismatch")
                operation = None
                if self._integration_service:
                    operation = await self._integration_service.validate_result(
                        result,
                        expected_run_id=run_id,
                        expected_root_run_id=self._root_run_id,
                        expected_parent_run_id=self._current_run_id or self._parent_run_id or self._root_run_id,
                        expected_plan_task_id=dispatch.plan_task_id or dispatch.task_id,
                        expected_session_id=dispatch.real_session_id,
                        expected_attempt=dispatch.attempt,
                        expected_integration_scope_id=self._task_id,
                        # The dispatch carries an opaque workspace_handle. The
                        # authoritative workspace_id is already bound to the
                        # persisted operation and must not be compared with the
                        # handle as if they were the same identity.
                        expected_workspace_id="",
                        expected_operation_id=dispatch.integration_operation_id,
                    )
                expected_source = f"agent/{dispatch.real_session_id}/{self._task_id}"
                if result.source_branch and result.source_branch != expected_source:
                    raise ValueError("operation_binding_mismatch: source_branch mismatch")
                if result.root_run_id and self._root_run_id and result.root_run_id != self._root_run_id:
                    raise ValueError("operation_binding_mismatch: root_run_id mismatch")
                expected_parent_run_id = self._current_run_id or self._parent_run_id
                if result.parent_run_id and expected_parent_run_id and result.parent_run_id != expected_parent_run_id:
                    raise ValueError("operation_binding_mismatch: parent_run_id mismatch")
                if result.target_branch and result.target_branch != expected_target:
                    raise ValueError("operation_binding_mismatch: target_branch mismatch")
                if result.version in {1, 2}:
                    await self._validate_registered_git_lineage(result, dispatch)
                if any(
                    not isinstance(name, str)
                    or not name
                    or len(name) > 1024
                    or Path(name).is_absolute()
                    or "\x00" in name
                    or ".." in Path(name).parts
                    for name in result.conflict_files
                ):
                    raise ValueError("integration_result_invalid: invalid conflict file path")
                if operation and self._integration_service:
                    # Claim the operation only after every identity and
                    # workspace-independent field has passed validation.
                    await self._integration_service.begin_operation(
                        operation.integration_operation_id
                    )
                    await self._integration_service.finalize_result(result, operation)
                return result
            except FileNotFoundError:
                await asyncio.sleep(0.05)
            except IntegrationError as exc:
                if self._integration_service:
                    self._integration_service.record_result_rejected(exc.code, result_version)
                self._invalid_integration_results.add(run_id)
                self._invalid_integration_errors[run_id] = exc.code
                logger.warning(
                    "ExecutionEngine: invalid integration result run=%s plan_task=%s attempt=%s code=%s: %s",
                    run_id,
                    dispatch.task_id,
                    dispatch.attempt,
                    exc.code,
                    exc,
                )
                return None
            except ValidationError as exc:
                if self._integration_service:
                    self._integration_service.record_result_rejected(
                        ERROR_INTEGRATION_RESULT_INVALID, result_version
                    )
                self._invalid_integration_results.add(run_id)
                self._invalid_integration_errors[run_id] = ERROR_INTEGRATION_RESULT_INVALID
                logger.warning(
                    "ExecutionEngine: invalid integration schema run=%s plan_task=%s attempt=%s: %s",
                    run_id,
                    dispatch.task_id,
                    dispatch.attempt,
                    exc,
                )
                return None
            except (OSError, json.JSONDecodeError, ValueError) as exc:
                if self._integration_service:
                    self._integration_service.record_result_rejected(
                        str(exc).split(":", 1)[0], result_version
                    )
                self._invalid_integration_results.add(run_id)
                self._invalid_integration_errors[run_id] = str(exc).split(":", 1)[0]
                logger.warning(
                    "ExecutionEngine: invalid integration result run=%s plan_task=%s attempt=%s code=%s: %s",
                    run_id,
                    dispatch.task_id,
                    dispatch.attempt,
                    self._invalid_integration_errors[run_id],
                    exc,
                )
                return None
        return None

    @staticmethod
    def _integration_error(result: IntegrationResult) -> tuple[str, str]:
        error_code = sanitize_error_text(
            result.error_code or ("merge_conflict" if result.status == "conflict" else "integration_failed"),
            limit=128,
        )
        error_message = sanitize_error_text(result.error_message or error_code)
        return error_code, error_message

    def _resolver_candidates(self, dispatch: DispatchResult) -> list[dict]:
        candidates: list[dict] = []
        seen: set[str] = set()
        for cfg in self._agents:
            if not isinstance(cfg, dict):
                continue
            agent_id = str(cfg.get("id") or cfg.get("name") or "").strip()
            session_id = str(cfg.get("session_id") or "").strip()
            agent_type = str(cfg.get("type") or cfg.get("agent_type") or agent_id).strip()
            if not agent_id or not session_id or agent_type == "orchestrator":
                continue
            key = session_id + "\x00" + agent_type
            if key not in seen:
                candidates.append(
                    {
                        "id": agent_id,
                        "session_id": session_id,
                        "agent_type": agent_type,
                    }
                )
                seen.add(key)

        # The conflicted Agent gets first right of repair even when the agent
        # snapshot omitted a display id.
        original = {
            "id": dispatch.agent,
            "session_id": dispatch.real_session_id,
            "agent_type": dispatch.agent_type or dispatch.agent,
        }
        if original["session_id"] and original["agent_type"] != "orchestrator":
            candidates = [original] + [
                candidate
                for candidate in candidates
                if candidate["session_id"] != original["session_id"]
            ]
        return candidates

    def _resolver_prompt(
        self,
        dispatch: DispatchResult,
        result: TaskResult,
        conflict_id: str,
        context: str,
    ) -> str:
        return (
            "你是 Orchestrator 的专用 Git 冲突 Resolver。\n\n"
            f"原始用户目标/子任务：{dispatch.content}\n"
            f"冲突 ID：{conflict_id}\n"
            f"冲突文件：{', '.join(result.conflict_files) or '(Git 未提供文件名)'}\n\n"
            "必须保留 source 与 target 两侧的业务意图，禁止采用 ours/theirs 静默丢弃一方，"
            "禁止修改 task/main 分支，禁止修改与冲突无关的文件。请在当前 resolver worktree 内解决冲突，"
            "运行可用的轻量验证，并提交修复结果；不要执行 taskctl merge。\n\n"
            "以下是 base/ours/theirs 事实：\n"
            f"{context[:24000]}"
        )

    @staticmethod
    def _contains_binary_conflict(worktree_path: str, conflict_files: list[str]) -> bool:
        root = Path(worktree_path).resolve()
        for relative_name in conflict_files:
            candidate = (root / relative_name).resolve()
            try:
                candidate.relative_to(root)
            except ValueError:
                return True
            try:
                if b"\x00" in candidate.read_bytes()[:8192]:
                    return True
            except OSError:
                # An unreadable conflict file is unsafe to hand to an LLM.
                return True
        return False

    @staticmethod
    def _high_risk_conflict_reason(conflict_files: list[str]) -> str:
        """Classify files whose merge semantics must be reviewed by a user.

        A Resolver can safely handle ordinary source/text conflicts, but a
        blind LLM decision in dependency locks, migration history, deployment
        policy, or credential/configuration files can change runtime or
        security behavior without a reliable local oracle.  This classifier is
        intentionally conservative and only runs after Git has reported an
        actual conflict; it never blocks an ordinary successful merge.
        """
        lock_names = {
            "cargo.lock",
            "composer.lock",
            "go.sum",
            "package-lock.json",
            "packages.lock.json",
            "pipfile.lock",
            "poetry.lock",
            "pnpm-lock.yaml",
            "yarn.lock",
        }
        high_risk_tokens = (
            "/.github/workflows/",
            "/.gitlab/",
            "/docker/",
            "/helm/",
            "/k8s/",
            "/kubernetes/",
            "/migrations/",
            "/db/migrate/",
            "/security/",
            "/policies/",
            "/permissions/",
            "/terraform/",
        )
        high_risk_names = {
            "dockerfile",
            ".env",
            ".env.local",
            ".env.production",
            "authorization.json",
            "permissions.json",
            "security.json",
        }
        normalized = [name.replace("\\", "/").lower().strip("/") for name in conflict_files]
        for name in normalized:
            basename = name.rsplit("/", 1)[-1]
            if basename in lock_names:
                return "dependency lockfile conflict"
            if basename in high_risk_names or any(token in f"/{name}" for token in high_risk_tokens):
                return "security, deployment, migration or production configuration conflict"
            if basename.endswith((".pem", ".key", ".p12", ".pfx")):
                return "credential or signing-key conflict"
            if "migration" in basename or basename.startswith("v") and basename.endswith(".sql"):
                return "database migration conflict"
        return ""

    @staticmethod
    def _contains_conflict_markers(worktree_path: str, conflict_files: list[str]) -> bool:
        root = Path(worktree_path).resolve()
        markers = (b"<<<<<<<", b"=======", b">>>>>>>")
        for relative_name in conflict_files:
            candidate = (root / relative_name).resolve()
            try:
                candidate.relative_to(root)
                data = candidate.read_bytes()
            except OSError:
                return True
            except ValueError:
                return True
            if any(marker in data for marker in markers):
                return True
        return False

    async def _write_conflict_record(
        self,
        conflict_id: str,
        dispatch: DispatchResult,
        result: TaskResult,
        status: str,
        attempt: int,
        resolver_agent: str = "",
        resolver_run_id: str = "",
        resolver_branch: str = "",
        resolver_session_id: str = "",
        error_code: str = "",
        error_message: str = "",
    ) -> None:
        operation = None
        if self._integration_service and dispatch.integration_operation_id:
            operation = await self._integration_service.repository.get(dispatch.integration_operation_id)
        root_run_id = operation.root_run_id if operation else self._root_run_id
        original_operation_id = (
            operation.integration_operation_id if operation else dispatch.integration_operation_id
        )
        plan_task_id = operation.plan_task_id if operation else dispatch.plan_task_id or dispatch.task_id
        integration_scope_id = operation.integration_scope_id if operation else dispatch.integration_scope_id or self._task_id
        workspace_id = operation.workspace_id if operation else dispatch.workspace_handle
        safe_error_code = sanitize_error_text(error_code, limit=128)
        safe_error_message = sanitize_error_text(error_message)
        payload = ConflictRecord(
            conflict_id=conflict_id,
            root_run_id=root_run_id,
            task_id=self._task_id,
            failed_task_id=dispatch.plan_task_id or dispatch.task_id,
            original_operation_id=original_operation_id,
            plan_task_id=plan_task_id,
            integration_scope_id=integration_scope_id,
            workspace_id=workspace_id,
            failed_agent=dispatch.agent,
            attempt=attempt,
            source_branch=result.source_branch,
            source_commit=result.source_commit,
            target_branch=result.target_branch,
            target_commit=result.target_commit,
            merge_base=result.merge_base,
            conflict_files=result.conflict_files,
            resolver_agent=resolver_agent,
            resolver_session_id=resolver_session_id,
            resolver_branch=resolver_branch,
            resolver_run_id=resolver_run_id,
            status=status,
            last_error_code=safe_error_code,
            last_error_message=safe_error_message,
        ).model_dump()
        if self._shared_dir:
            path = Path(self._shared_dir) / "conflicts" / f"{conflict_id}.json"
            try:
                atomic_write_text(path, json.dumps(payload, ensure_ascii=False, indent=2))
            except OSError:
                logger.exception("ExecutionEngine: failed to persist conflict record %s", conflict_id)
        if self._integration_service and dispatch.integration_operation_id:
            try:
                await self._integration_service.save_conflict_record(
                    IntegrationConflictRecord(
                        conflict_id=conflict_id,
                        root_run_id=root_run_id,
                        original_operation_id=original_operation_id,
                        plan_task_id=plan_task_id,
                        integration_scope_id=integration_scope_id,
                        workspace_id=workspace_id,
                        status=status,
                        attempt=attempt,
                        source_branch=result.source_branch,
                        source_commit=result.source_commit,
                        target_branch=result.target_branch,
                        target_commit=result.target_commit,
                        merge_base=result.merge_base,
                        conflict_files=list(result.conflict_files),
                        resolver_agent=resolver_agent,
                        resolver_session_id=resolver_session_id,
                        resolver_branch=resolver_branch,
                        resolver_run_id=resolver_run_id,
                        last_error_code=safe_error_code,
                        last_error_message=safe_error_message,
                    )
                )
            except Exception:
                logger.warning(
                    "ExecutionEngine: failed to persist durable conflict record conflict=%s",
                    conflict_id,
                    exc_info=True,
                )

    async def _write_resolution_attempt(
        self,
        conflict_id: str,
        dispatch: DispatchResult,
        result: TaskResult,
        attempt: int,
        status: str,
        *,
        resolver_run_id: str = "",
        resolver_workspace_id: str = "",
        resolver_commit: str = "",
        error_code: str = "",
        error_message: str = "",
    ) -> None:
        if not self._integration_service or not dispatch.integration_operation_id:
            return
        try:
            await self._integration_service.save_resolution_attempt(
                ResolutionAttempt(
                    resolution_attempt_id=f"{conflict_id}:{attempt}",
                    conflict_id=conflict_id,
                    original_operation_id=dispatch.integration_operation_id,
                    resolver_run_id=resolver_run_id,
                    resolver_workspace_id=resolver_workspace_id,
                    attempt=attempt,
                    status=status,
                    expected_target_commit=result.target_commit,
                    resolver_commit=resolver_commit,
                    error_code=error_code,
                    error_message=error_message,
                    created_at=utc_now(),
                    finished_at=utc_now() if status in {"completed", "failed", "awaiting_user", "cancelled"} else "",
                )
            )
        except Exception:
            logger.warning(
                "ExecutionEngine: failed to persist resolution attempt conflict=%s attempt=%d",
                conflict_id,
                attempt,
                exc_info=True,
            )

    async def _write_resolution_integration_record(
        self,
        conflict_id: str,
        dispatch: DispatchResult,
        result: TaskResult,
        attempt: int,
        resolver_run_id: str,
        resolver_workspace_id: str,
        merge_result: object | None,
        status: str,
        *,
        error_code: str = "",
        error_message: str = "",
    ) -> bool:
        """Persist resolver Git facts separately from the original operation."""
        if not self._integration_service or not dispatch.integration_operation_id or not resolver_workspace_id:
            return True
        operation = await self._integration_service.repository.get(dispatch.integration_operation_id)
        if operation is None:
            return False
        source_branch = getattr(merge_result, "source_branch", "") if merge_result else ""
        source_commit = getattr(merge_result, "source_commit", "") if merge_result else ""
        target_branch = getattr(merge_result, "target_branch", "") if merge_result else result.target_branch
        target_before = getattr(merge_result, "target_commit", "") if merge_result else result.target_commit
        target_after = getattr(merge_result, "target_commit_after", "") if merge_result else ""
        merge_base = getattr(merge_result, "merge_base", "") if merge_result else result.merge_base
        aborted = bool(getattr(merge_result, "aborted", False)) if merge_result else False
        record = ResolutionIntegrationRecord(
            resolution_record_id=f"{conflict_id}:{attempt}",
            conflict_id=conflict_id,
            original_operation_id=operation.integration_operation_id,
            root_run_id=operation.root_run_id,
            plan_task_id=operation.plan_task_id,
            integration_scope_id=operation.integration_scope_id,
            resolver_run_id=resolver_run_id,
            resolver_workspace_id=resolver_workspace_id,
            attempt=attempt,
            status=status,
            source_branch=source_branch,
            source_commit=source_commit,
            target_branch=target_branch,
            target_commit_before=target_before,
            target_commit_after=target_after,
            merge_base=merge_base,
            conflict_files=list(result.conflict_files),
            aborted=aborted,
            error_code=sanitize_error_text(error_code, limit=128),
            error_message=sanitize_error_text(error_message),
            started_at=utc_now(),
            finished_at=utc_now(),
        )
        try:
            await self._integration_service.save_resolution_integration_record(record)
        except Exception:
            logger.warning(
                "ExecutionEngine: failed to persist resolution Git record conflict=%s attempt=%d",
                conflict_id,
                attempt,
                exc_info=True,
            )
            return False
        return True

    async def _attempt_conflict_recovery(
        self,
        dispatch: DispatchResult,
        result: TaskResult,
        *,
        start_attempt: int = 0,
        force_manual: bool = False,
    ) -> tuple[TaskResult, list[StreamEvent]]:
        """Resolve a completed-but-unintegrated Agent artifact in isolation."""
        events: list[StreamEvent] = []
        operation = None
        if self._integration_service and dispatch.integration_operation_id:
            operation = await self._integration_service.repository.get(dispatch.integration_operation_id)
            if operation is not None:
                resolved_conflict = await self._integration_service.get_resolved_conflict(
                    operation.integration_operation_id
                )
                if resolved_conflict is not None:
                    # The original operation remains a durable ``conflict``
                    # fact. A completed resolver is a separate resolution
                    # fact, so replay must project success without starting a
                    # second resolver attempt.
                    result.integration_status = "merged"
                    result.success = True
                    result.resolved_from_conflict = True
                    result.error_type = ""
                    result.error_code = ""
                    result.error_message = ""
                    return result, events
        if not result.source_branch:
            result.source_branch = f"agent/{dispatch.real_session_id}/{self._task_id}"
        if not result.target_branch:
            result.target_branch = f"task/{self._task_id}"

        if dispatch.integration_operation_id:
            conflict_id = IntegrationService.conflict_id_for(
                dispatch.integration_operation_id,
                result.source_commit,
                result.target_commit,
                result.conflict_files,
            )
        else:
            conflict_id = hashlib.sha256(
                "|".join(
                    [
                        result.source_commit,
                        result.target_commit,
                        ",".join(sorted(result.conflict_files)),
                    ]
                ).encode()
            ).hexdigest()[:20]

        if not settings.orchestrator.conflict_resolver_enabled and not force_manual:
            result.integration_status = "awaiting_user"
            result.error_code = "conflict_resolver_disabled"
            result.error_message = "automatic conflict recovery is disabled"
            result.success = False
            await self._write_conflict_record(
                conflict_id,
                dispatch,
                result,
                "awaiting_user",
                start_attempt,
                error_code=result.error_code,
                error_message=result.error_message,
            )
            return result, events
        if not self._workspace_mgr or not self._repo_path or not self._backend_client:
            result.integration_status = "awaiting_user"
            result.error_code = "resolver_unavailable"
            result.error_message = "resolver requires workspace, repository and Backend client"
            result.success = False
            await self._write_conflict_record(
                conflict_id,
                dispatch,
                result,
                "awaiting_user",
                start_attempt,
                error_code=result.error_code,
                error_message=result.error_message,
            )
            return result, events

        candidates = self._resolver_candidates(dispatch)
        max_attempts = max(1, settings.orchestrator.conflict_resolver_max_attempts)
        if not candidates:
            result.integration_status = "awaiting_user"
            result.error_code = "resolver_agent_unavailable"
            result.error_message = "no eligible resolver Agent is available"
            result.success = False
            await self._write_conflict_record(
                conflict_id,
                dispatch,
                result,
                "awaiting_user",
                start_attempt,
                error_code=result.error_code,
                error_message=result.error_message,
            )
            return result, events

        # The first automatic pass creates the initial durable conflict row.
        # A user retry or startup continuation keeps that row and advances the
        # attempt monotonically; writing a synthetic ``detected/0`` here would
        # regress the persisted state machine.
        existing_conflict = (
            await self._integration_service.get_conflict_record(conflict_id)
            if self._integration_service
            else None
        )
        if existing_conflict is None:
            await self._write_conflict_record(conflict_id, dispatch, result, "detected", start_attempt)
        failed_signatures: set[tuple[str, str, tuple[str, ...], str]] = set()
        last_attempt = start_attempt
        for offset in range(max_attempts):
            attempt = start_attempt + offset
            candidate = candidates[min(offset, len(candidates) - 1)]
            resolver_session_id = candidate["session_id"]
            resolver_agent = candidate["id"]
            signature = (
                result.source_commit,
                result.target_commit,
                tuple(sorted(result.conflict_files)),
                resolver_agent,
            )
            if signature in failed_signatures:
                logger.warning(
                    "ExecutionEngine: skip duplicate resolver attempt conflict=%s agent=%s",
                    conflict_id,
                    resolver_agent,
                )
                continue
            last_attempt = attempt
            resolver_run_id = str(uuid.uuid4())
            resolver_workspace = None
            preparation = None
            merge_result = None
            try:
                events.append(
                    StreamEvent.create(
                        EventType.RESOLUTION_STARTED,
                        **self._projection_identity(dispatch),
                        task_id=dispatch.plan_task_id or dispatch.task_id,
                        conflict_id=conflict_id,
                        attempt=attempt,
                        resolver_agent=resolver_agent,
                        status="resolving",
                    )
                )
                resolver_type = candidate["agent_type"]
                try:
                    resolver_agent_type = AgentType(resolver_type)
                except ValueError:
                    resolver_agent_type = AgentType.CLAUDE_CODE
                resolver_workspace, preparation = await self._workspace_mgr.create_resolver(
                    repo_path=self._repo_path,
                    task_id=self._task_id,
                    conflict_id=conflict_id,
                    attempt=attempt,
                    source_branch=result.source_branch,
                    resolver_session_id=resolver_session_id,
                    agent_type=resolver_agent_type,
                    agent_name=resolver_agent,
                )
                # Legacy text fallback may not carry Git facts. The isolated
                # preparation is the trusted place to fill them before the
                # ConflictRecord and resolver prompt are persisted.
                result.source_commit = preparation.source_commit or result.source_commit
                result.target_commit = preparation.target_commit or result.target_commit
                result.merge_base = preparation.merge_base or result.merge_base
                result.conflict_files = preparation.conflict_files or result.conflict_files
                await self._write_conflict_record(
                    conflict_id,
                    dispatch,
                    result,
                    "preparing",
                    attempt,
                    resolver_agent=resolver_agent,
                    resolver_branch=resolver_workspace.branch_name,
                    resolver_run_id=resolver_run_id,
                    resolver_session_id=resolver_session_id,
                )
                await self._write_resolution_attempt(
                    conflict_id,
                    dispatch,
                    result,
                    attempt,
                    "preparing",
                    resolver_run_id=resolver_run_id,
                    resolver_workspace_id=resolver_workspace.id,
                )

                manual_reason = ""
                if not preparation.success and not settings.orchestrator.conflict_auto_resolve_text:
                    manual_reason = "automatic text conflict recovery is disabled"
                    result.error_code = "text_auto_resolve_disabled"
                elif (
                    not preparation.success
                    and not settings.orchestrator.conflict_auto_resolve_binary
                    and self._contains_binary_conflict(
                        resolver_workspace.worktree_path,
                        preparation.conflict_files or result.conflict_files,
                    )
                ):
                    manual_reason = "binary or unreadable conflict requires manual resolution"
                    result.error_code = "binary_conflict"
                elif not preparation.success:
                    high_risk_reason = self._high_risk_conflict_reason(
                        preparation.conflict_files or result.conflict_files
                    )
                    if high_risk_reason:
                        manual_reason = f"{high_risk_reason} requires manual resolution"
                        result.error_code = "high_risk_conflict"

                if manual_reason:
                    result.integration_status = "awaiting_user"
                    result.success = False
                    result.error_type = "merge_conflict"
                    result.error_message = manual_reason
                    events.append(
                        StreamEvent.create(
                            EventType.RESOLUTION_FAILED,
                            **self._projection_identity(dispatch),
                            task_id=dispatch.plan_task_id or dispatch.task_id,
                            conflict_id=conflict_id,
                            attempt=attempt,
                            resolver_agent=resolver_agent,
                            status="awaiting_user",
                            retrying=False,
                            error_code=result.error_code,
                            error_message=manual_reason,
                        )
                    )
                    await self._write_conflict_record(
                        conflict_id,
                        dispatch,
                        result,
                        "awaiting_user",
                        attempt,
                        resolver_agent=resolver_agent,
                        resolver_branch=resolver_workspace.branch_name,
                        resolver_run_id=resolver_run_id,
                        resolver_session_id=resolver_session_id,
                        error_code=result.error_code,
                        error_message=manual_reason,
                    )
                    await self._write_resolution_attempt(
                        conflict_id,
                        dispatch,
                        result,
                        attempt,
                        "awaiting_user",
                        resolver_run_id=resolver_run_id,
                        resolver_workspace_id=resolver_workspace.id,
                        error_code=result.error_code,
                        error_message=manual_reason,
                    )
                    return result, events

                if preparation.success:
                    context = "Git 自动合并无剩余冲突，直接进入验证。"
                else:
                    context = await self._workspace_mgr.resolver_conflict_context(
                        resolver_workspace.id,
                        preparation.conflict_files or result.conflict_files,
                    )
                    if preparation.error_code != "merge_conflict":
                        raise RuntimeError(preparation.error or preparation.error_code)

                if not preparation.success:
                    await self._write_conflict_record(
                        conflict_id,
                        dispatch,
                        result,
                        "resolving",
                        attempt,
                        resolver_agent=resolver_agent,
                        resolver_branch=resolver_workspace.branch_name,
                        resolver_run_id=resolver_run_id,
                        resolver_session_id=resolver_session_id,
                    )
                    await self._write_resolution_attempt(
                        conflict_id,
                        dispatch,
                        result,
                        attempt,
                        "resolving",
                        resolver_run_id=resolver_run_id,
                        resolver_workspace_id=resolver_workspace.id,
                    )
                    events.append(
                        StreamEvent.create(
                            EventType.RESOLUTION_PROGRESS,
                            **self._projection_identity(dispatch),
                            task_id=dispatch.plan_task_id or dispatch.task_id,
                            conflict_id=conflict_id,
                            attempt=attempt,
                            status="agent_running",
                            conflict_files=result.conflict_files,
                        )
                    )
                    resolver_run_kwargs = {
                        "task_id": self._task_id,
                        "session_id": resolver_session_id,
                        "message": self._resolver_prompt(dispatch, result, conflict_id, context),
                        "agent_type": resolver_type,
                        "cwd": resolver_workspace.worktree_path,
                        "root_run_id": operation.root_run_id if operation else self._root_run_id,
                        "parent_run_id": (
                            operation.parent_run_id
                            if operation
                            else self._current_run_id or result.run_id or self._parent_run_id
                        ),
                        "current_run_id": operation.parent_run_id if operation else self._current_run_id,
                        "plan_task_id": dispatch.plan_task_id or dispatch.task_id,
                        "workspace_id": resolver_workspace.id,
                        "workspace_handle": resolver_workspace.id,
                        "budget": self._child_budget(settings.orchestrator.conflict_resolver_timeout),
                        "run_id": resolver_run_id,
                    }
                    if attempt:
                        resolver_run_kwargs["integration_attempt"] = attempt
                    child = await asyncio.wait_for(
                        self._backend_client.run_task(**resolver_run_kwargs),
                        timeout=30.0,
                    )
                    resolver_output: list[str] = []
                    resolver_failed = ""
                    async for event in self._backend_client.stream_result(
                        task_id=self._task_id,
                        message_id=child.message_id,
                        session_id=resolver_session_id,
                    ):
                        event_type = event.get("type", "")
                        content = event.get("content") or {}
                        if event_type == "text":
                            resolver_output.append(str(content.get("text", "")))
                        elif event_type == "error":
                            resolver_failed = str(content.get("message") or content.get("error") or "resolver failed")
                            break
                        elif event_type == "done":
                            break
                    if resolver_failed:
                        raise RuntimeError(resolver_failed)
                    result.content = (result.content + "\n\n" + "".join(resolver_output)).strip()

                unmerged = await self._workspace_mgr.resolver_unmerged_files(resolver_workspace.id)
                await self._write_conflict_record(
                    conflict_id,
                    dispatch,
                    result,
                    "verifying",
                    attempt,
                    resolver_agent=resolver_agent,
                    resolver_branch=resolver_workspace.branch_name,
                    resolver_run_id=resolver_run_id,
                    resolver_session_id=resolver_session_id,
                )
                await self._write_resolution_attempt(
                    conflict_id,
                    dispatch,
                    result,
                    attempt,
                    "verifying",
                    resolver_run_id=resolver_run_id,
                    resolver_workspace_id=resolver_workspace.id,
                )
                if unmerged:
                    raise RuntimeError("resolver left unmerged files: " + ", ".join(unmerged))
                if self._contains_conflict_markers(
                    resolver_workspace.worktree_path,
                    preparation.conflict_files or result.conflict_files,
                ):
                    raise RuntimeError("resolver left conflict markers in resolved files")
                await self._workspace_mgr.resolver_commit(
                    resolver_workspace.id,
                    f"fix: resolve {conflict_id} ({result.source_commit[:8]} + {result.target_commit[:8]})",
                )
                if self._integration_service:
                    async with self._integration_service.integration_scope_lock(self._task_id):
                        merge_result = await self._workspace_mgr.merge_resolver(
                            resolver_workspace.id,
                            preparation.target_commit,
                        )
                else:
                    merge_result = await self._workspace_mgr.merge_resolver(
                        resolver_workspace.id,
                        preparation.target_commit,
                    )
                if not merge_result.success:
                    if merge_result.error_code == "target_moved":
                        raise RuntimeError("target_moved: " + merge_result.error)
                    raise RuntimeError(merge_result.error or "resolver branch integration failed")

                result.integration_status = "merged"
                result.success = True
                result.resolved_from_conflict = True
                result.error_type = ""
                result.error_code = ""
                result.error_message = ""
                events.append(
                    StreamEvent.create(
                        EventType.RESOLUTION_COMPLETED,
                        **self._projection_identity(dispatch),
                        task_id=dispatch.plan_task_id or dispatch.task_id,
                        conflict_id=conflict_id,
                        attempt=attempt,
                        resolver_agent=resolver_agent,
                        status="completed",
                    )
                )
                resolution_record_saved = await self._write_resolution_integration_record(
                    conflict_id,
                    dispatch,
                    result,
                    attempt,
                    resolver_run_id,
                    resolver_workspace.id,
                    merge_result,
                    "merged",
                )
                if not resolution_record_saved:
                    raise RuntimeError("resolution integration record could not be persisted")
                await self._write_resolution_attempt(
                    conflict_id,
                    dispatch,
                    result,
                    attempt,
                    "completed",
                    resolver_run_id=resolver_run_id,
                    resolver_workspace_id=resolver_workspace.id,
                    resolver_commit=await self._workspace_mgr.current_commit(resolver_workspace.id),
                )
                await self._write_conflict_record(
                    conflict_id,
                    dispatch,
                    result,
                    "resolved",
                    attempt,
                    resolver_agent=resolver_agent,
                    resolver_branch=resolver_workspace.branch_name,
                    resolver_run_id=resolver_run_id,
                    resolver_session_id=resolver_session_id,
                )
                await self._workspace_mgr.cleanup(resolver_workspace.id)
                return result, events
            except asyncio.CancelledError:
                # Parent Run cancellation must not leave an active resolver
                # child/worktree behind. Preserve the conflict facts and the
                # resolver branch status for audit, then propagate cancellation
                # so RunSupervisor can converge the child Run state.
                cancel_message = "resolver cancelled with its parent run"
                await self._write_conflict_record(
                    conflict_id,
                    dispatch,
                    result,
                    "cancelled",
                    last_attempt if last_attempt >= attempt else attempt,
                    resolver_agent=resolver_agent if resolver_workspace else "",
                    resolver_branch=resolver_workspace.branch_name if resolver_workspace else "",
                    resolver_run_id=resolver_run_id if resolver_workspace else "",
                    resolver_session_id=resolver_session_id if resolver_workspace else "",
                    error_code="operation_cancelled",
                    error_message=cancel_message,
                )
                await self._write_resolution_attempt(
                    conflict_id,
                    dispatch,
                    result,
                    last_attempt if last_attempt >= attempt else attempt,
                    "cancelled",
                    resolver_run_id=resolver_run_id if resolver_workspace else "",
                    resolver_workspace_id=resolver_workspace.id if resolver_workspace else "",
                    error_code="operation_cancelled",
                    error_message=cancel_message,
                )
                if resolver_workspace is not None:
                    try:
                        await self._workspace_mgr.cleanup(resolver_workspace.id)
                    except Exception:
                        logger.warning(
                            "ExecutionEngine: failed to clean cancelled resolver workspace conflict=%s",
                            conflict_id,
                            exc_info=True,
                        )
                raise
            except Exception as exc:
                message = str(exc) or exc.__class__.__name__
                will_retry = offset + 1 < max_attempts
                result.error_type = "resolution_failed"
                result.error_code = "resolver_failed"
                result.error_message = message
                events.append(
                    StreamEvent.create(
                        EventType.RESOLUTION_FAILED,
                        **self._projection_identity(dispatch),
                        task_id=dispatch.plan_task_id or dispatch.task_id,
                        conflict_id=conflict_id,
                        attempt=attempt,
                        resolver_agent=resolver_agent,
                        status="retrying" if will_retry else "awaiting_user",
                        retrying=will_retry,
                        error_code=result.error_code,
                        error_message=message,
                    )
                )
                await self._write_conflict_record(
                    conflict_id,
                    dispatch,
                    result,
                    "retryable" if will_retry else "awaiting_user",
                    attempt,
                    resolver_agent=resolver_agent,
                    resolver_branch=resolver_workspace.branch_name if resolver_workspace else "",
                    resolver_run_id=resolver_run_id,
                    resolver_session_id=resolver_session_id,
                    error_code=result.error_code,
                    error_message=message,
                )
                await self._write_resolution_attempt(
                    conflict_id,
                    dispatch,
                    result,
                    attempt,
                    "failed" if not will_retry else "retryable",
                    resolver_run_id=resolver_run_id,
                    resolver_workspace_id=resolver_workspace.id if resolver_workspace else "",
                    error_code=result.error_code,
                    error_message=message,
                )
                if merge_result is not None and resolver_workspace is not None:
                    await self._write_resolution_integration_record(
                        conflict_id,
                        dispatch,
                        result,
                        attempt,
                        resolver_run_id,
                        resolver_workspace.id,
                        merge_result,
                        "failed",
                        error_code=result.error_code,
                        error_message=message,
                    )
                failed_signatures.add(signature)
                failed_signatures.add(
                    (
                        result.source_commit,
                        result.target_commit,
                        tuple(sorted(result.conflict_files)),
                        resolver_agent,
                    )
                )

        result.integration_status = "awaiting_user"
        result.success = False
        result.error_type = "merge_conflict"
        result.error_code = "conflict_resolver_exhausted"
        result.error_message = (
            f"automatic conflict recovery exhausted after {max_attempts} attempt(s); "
            "manual resolution is required"
        )
        await self._write_conflict_record(
            conflict_id,
            dispatch,
            result,
            "awaiting_user",
            last_attempt,
            resolver_agent=resolver_agent if candidates else "",
            resolver_session_id=resolver_session_id if candidates else "",
            resolver_branch=resolver_workspace.branch_name if resolver_workspace else "",
            resolver_run_id=resolver_run_id if candidates else "",
            error_code=result.error_code,
            error_message=result.error_message,
        )
        return result, events

    async def _execute_task(
        self,
        dispatch: DispatchResult,
        timeout: float,
    ) -> AsyncIterator[tuple[StreamEvent, TaskResult | None]]:
        task_id = dispatch.task_id
        agent_name = dispatch.agent
        agent_type = dispatch.agent_type or agent_name
        start = time.monotonic()
        session_id = dispatch.real_session_id or f"orch-{task_id}"
        child_run_id = str(uuid.uuid4())
        success = False
        execution_status = "failed"
        integration_status = "pending"
        collected: list[str] = []
        error_type = ""
        error_code = ""
        error_message = ""
        message_id = ""
        run_id = ""
        operation = None
        workspace_id = ""
        integration_capability = ""
        conflict_files: list[str] = []
        source_branch = ""
        source_commit = ""
        target_branch = ""
        target_commit = ""
        merge_base = ""
        integration_events: list[StreamEvent] = []
        resolver_events: list[StreamEvent] = []
        try:
            agent_cwd = await self._ensure_worktree(dispatch)
            agent_message = self._build_agent_message(dispatch)

            # 统一的 HTTP 路径 —— 由 Backend 查询窗口并注入 group_chat_messages
            logger.info(
                "ExecutionEngine: HTTP path agent=%s type=%s task=%s session=%s",
                agent_name,
                agent_type,
                task_id,
                session_id,
            )
            # Register the immutable integration operation before dispatching
            # the child Run.  The child receives only opaque identity values;
            # scope/branch/path facts remain control-plane state.
            operation_root_run_id = self._root_run_id or child_run_id
            operation_parent_run_id = self._current_run_id or self._parent_run_id or operation_root_run_id
            if self._workspace_mgr:
                workspace = self._workspace_mgr.get_by_session_and_path(session_id, agent_cwd)
                if workspace:
                    workspace_id = workspace.id
            if self._integration_service:
                # An opaque handle is a transport reference, never a substitute
                # for the authoritative WorkspaceManager record ID.
                if not workspace_id:
                    raise IntegrationError("workspace_missing", "child workspace is not registered")
                operation_workspace_handle = dispatch.workspace_handle or workspace_id
            else:
                workspace_id = workspace_id or dispatch.workspace_handle
                operation_workspace_handle = dispatch.workspace_handle or workspace_id
            if self._integration_service:
                plan_task_id = dispatch.plan_task_id or dispatch.task_id
                existing_operation = None
                if dispatch.integration_operation_id:
                    existing_operation = await self._integration_service.repository.get(
                        dispatch.integration_operation_id
                    )
                    if existing_operation is None:
                        raise IntegrationError(ERROR_OPERATION_NOT_FOUND)
                else:
                    # A restarted/replayed planner may not carry the opaque
                    # operation ID in memory.  The durable root/plan/attempt
                    # binding is the safe replay key; it lets us reuse the
                    # original child Run instead of creating a second one.
                    existing_operation = await self._integration_service.repository.get_by_binding(
                        operation_root_run_id,
                        plan_task_id,
                        dispatch.attempt,
                    )

                if existing_operation is not None:
                    expected_binding = {
                        "root_run_id": operation_root_run_id,
                        "parent_run_id": operation_parent_run_id,
                        "plan_task_id": plan_task_id,
                        "session_id": session_id,
                        "workspace_id": workspace_id,
                        "integration_scope_id": self._task_id,
                    }
                    for field, expected in expected_binding.items():
                        if getattr(existing_operation, field) != expected:
                            raise IntegrationError(
                                ERROR_OPERATION_BINDING_MISMATCH,
                                f"replayed operation {field} does not match",
                            )
                    if (
                        dispatch.workspace_handle
                        and existing_operation.workspace_handle != dispatch.workspace_handle
                    ):
                        raise IntegrationError(
                            ERROR_OPERATION_BINDING_MISMATCH,
                            "replayed operation workspace_handle does not match",
                        )
                    operation = existing_operation
                    workspace_id = operation.workspace_id
                    operation_workspace_handle = operation.workspace_handle
                else:
                    operation = await self._integration_service.create_operation(
                        plan_task_id=plan_task_id,
                        run_id=child_run_id,
                        root_run_id=operation_root_run_id,
                        parent_run_id=operation_parent_run_id,
                        attempt=dispatch.attempt,
                        session_id=session_id,
                        workspace_id=workspace_id,
                        workspace_handle=operation_workspace_handle,
                        integration_scope_id=self._task_id,
                    )
                child_run_id = operation.run_id
                if not operation.terminal:
                    integration_capability = await self._integration_service.issue_capability(
                        operation.integration_operation_id
                    )
                dispatch = dispatch.model_copy(
                    update={
                        "plan_task_id": dispatch.plan_task_id or dispatch.task_id,
                        "integration_operation_id": operation.integration_operation_id,
                        "workspace_handle": operation.workspace_handle,
                        "integration_scope_id": self._task_id,
                    }
                )

            # Registering the operation happens before the first ordinary SSE
            # event, so a replay uses the durable child run identity too.
            yield (
                StreamEvent.create(
                    EventType.RUNTIME_EXECUTING,
                    **self._projection_identity(dispatch),
                    task_id=dispatch.plan_task_id or dispatch.task_id,
                    agent=agent_name,
                    run_id=child_run_id,
                    attempt=dispatch.attempt,
                    title=dispatch.content[:80],
                    status="running",
                ),
                None,
            )

            if self._integration_service and operation and operation.terminal:
                # A durable terminal operation is authoritative.  Do not
                # dispatch the original Agent again after a restart or a
                # duplicate planner callback; only replay its projection (and
                # let the Resolver handle an existing conflict).
                if operation.terminal:
                    terminal_integration = await self._integration_service.result_for_operation(
                        operation.integration_operation_id
                    )
                    if terminal_integration is None:
                        terminal_integration = IntegrationResult(
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
                            error_code=operation.error_code or "operation_terminal_mismatch",
                            error_message=operation.error_message or "terminal operation has no Git record",
                        )
                    replayed = TaskResult(
                        task_id=dispatch.plan_task_id or dispatch.task_id,
                        root_task_id=self._task_id,
                        agent=agent_name,
                        attempt=dispatch.attempt,
                        execution_status="completed",
                        integration_status=(
                            "merged"
                            if terminal_integration.status == "merged"
                            else "conflict"
                            if terminal_integration.status == "conflict"
                            else "partial"
                            if terminal_integration.status == "partial"
                            else "failed"
                        ),
                        success=terminal_integration.status == "merged",
                        content="",
                        run_id=operation.run_id,
                        plan_task_id=dispatch.plan_task_id or dispatch.task_id,
                        integration_operation_id=operation.integration_operation_id,
                        integration_scope_id=operation.integration_scope_id,
                        workspace_id=operation.workspace_id,
                        duration=round(time.monotonic() - start, 2),
                        error_type=(
                            "merge_conflict"
                            if terminal_integration.status == "conflict"
                            else ""
                            if terminal_integration.status == "partial"
                            else "integration_failed"
                            if terminal_integration.status != "merged"
                            else ""
                        ),
                        error_code=terminal_integration.error_code,
                        error_message=terminal_integration.error_message,
                        conflict_files=list(terminal_integration.conflict_files),
                        source_branch=terminal_integration.source_branch,
                        source_commit=terminal_integration.source_commit,
                        target_branch=terminal_integration.target_branch,
                        target_commit=terminal_integration.target_commit,
                        merge_base=terminal_integration.merge_base,
                    )
                    if replayed.integration_status == "conflict":
                        replayed, resolver_events = await self._attempt_conflict_recovery(dispatch, replayed)
                    if replayed.integration_status == "conflict":
                        integration_event = StreamEvent.create(
                            EventType.INTEGRATION_CONFLICT,
                            **self._projection_identity(dispatch),
                            task_id=dispatch.plan_task_id or dispatch.task_id,
                            agent=agent_name,
                            run_id=operation.run_id,
                            attempt=dispatch.attempt,
                            status="conflict",
                            conflict_id=self._conflict_projection_id(dispatch, replayed),
                            conflict_files=replayed.conflict_files,
                            error_code=replayed.error_code,
                            error_message=replayed.error_message,
                        )
                    else:
                        integration_event = StreamEvent.create(
                            EventType.INTEGRATION_COMPLETED,
                            **self._projection_identity(dispatch),
                            task_id=dispatch.plan_task_id or dispatch.task_id,
                            agent=agent_name,
                            run_id=operation.run_id,
                            attempt=dispatch.attempt,
                            status=replayed.integration_status,
                            success=replayed.success,
                            error_code=replayed.error_code or None,
                            error_message=replayed.error_message or None,
                        )
                    yield integration_event, None
                    for resolver_event in resolver_events:
                        yield resolver_event, None
                    yield (
                        StreamEvent.create(
                            EventType.RUNTIME_COMPLETED,
                            **self._projection_identity(dispatch),
                            task_id=dispatch.plan_task_id or dispatch.task_id,
                            agent=agent_name,
                            run_id=replayed.run_id,
                            attempt=dispatch.attempt,
                            success=replayed.success,
                            duration=replayed.duration,
                            status=replayed.integration_status,
                            error_type=replayed.error_type or None,
                            error_code=replayed.error_code or None,
                            error_message=replayed.error_message or None,
                            conflict_files=replayed.conflict_files or None,
                        ),
                        replayed,
                    )
                    return

            child_run_kwargs = {
                "task_id": self._task_id,
                "session_id": session_id,
                "message": agent_message,
                "agent_type": agent_type,
                "cwd": agent_cwd,
                "root_run_id": operation.root_run_id if operation else operation_root_run_id,
                "parent_run_id": operation.parent_run_id if operation else operation_parent_run_id,
                "budget": self._child_budget(timeout),
                "run_id": child_run_id,
                "workspace_id": workspace_id,
                "current_run_id": operation.parent_run_id if operation else operation_parent_run_id,
                "plan_task_id": dispatch.plan_task_id or dispatch.task_id,
                "integration_operation_id": (
                    dispatch.integration_operation_id
                    if (
                        settings.orchestrator.integration_result_v2_write_enabled
                        or settings.orchestrator.integration_service_execute_enabled
                    )
                    else ""
                ),
                "workspace_handle": dispatch.workspace_handle,
                "integration_attempt": dispatch.attempt,
                "integration_capability": integration_capability,
            }
            child_run = await asyncio.wait_for(
                self._backend_client.run_task(**child_run_kwargs),
                timeout=30.0,
            )
            message_id = child_run.message_id
            run_id = child_run.run_id
            if operation and run_id != operation.run_id:
                raise IntegrationError(
                    "operation_binding_mismatch",
                    "Backend returned a different run_id than the pre-registered operation",
                )

            saw_terminal_done = False
            async for event in self._backend_client.stream_result(
                task_id=self._task_id,
                message_id=message_id,
                session_id=session_id,
            ):
                event_type = event.get("type", "")
                if event_type == "text":
                    content = event.get("content", {})
                    text = content.get("text", "") if isinstance(content, dict) else str(content)
                    if text:
                        collected.append(text)
                        yield (
                            StreamEvent.create(
                                EventType.TEXT,
                                **self._projection_identity(dispatch),
                                task_id=dispatch.plan_task_id or dispatch.task_id,
                                agent=agent_name,
                                agent_type=agent_type,
                                session_id=session_id,
                                message_id=message_id,
                                run_id=run_id,
                                attempt=dispatch.attempt,
                                text=text,
                            ),
                            None,
                        )
                elif event_type == "done":
                    saw_terminal_done = True
                    break
                elif event_type == "error":
                    content = event.get("content", {})
                    if isinstance(content, dict):
                        msg = content.get("message") or content.get("error") or "unknown error"
                    else:
                        msg = str(content)
                    error_type = "error"
                    error_message = sanitize_error_text(msg)
                    break
            else:
                saw_terminal_done = True

            # A taskctl conflict/failed result intentionally exits non-zero.
            # Prefer the durable operation (or the V1 result file) before
            # treating that process exit as an Agent execution failure.
            recovered_integration: IntegrationResult | None = None
            if error_message and operation and self._integration_service:
                if (
                    settings.orchestrator.integration_service_execute_enabled
                    and dispatch.integration_operation_id
                ):
                    recovered_integration = await self._integration_service.result_for_operation(
                        dispatch.integration_operation_id
                    )
                else:
                    recovered_integration = await self._read_integration_result(run_id, dispatch)
                if recovered_integration is not None:
                    error_message = ""
                    error_type = ""
                    saw_terminal_done = True

            if not error_message and saw_terminal_done:
                execution_status = "completed"
                integration_events.append(
                    StreamEvent.create(
                        EventType.INTEGRATION_STARTED,
                        **self._projection_identity(dispatch),
                        task_id=dispatch.plan_task_id or dispatch.task_id,
                        agent=agent_name,
                        run_id=run_id,
                        attempt=dispatch.attempt,
                        status="integrating",
                    )
                )
                if recovered_integration is not None:
                    integration = recovered_integration
                elif (
                    settings.orchestrator.integration_service_execute_enabled
                    and self._integration_service
                    and dispatch.integration_operation_id
                ):
                    integration = await self._integration_service.result_for_operation(
                        dispatch.integration_operation_id
                    )
                else:
                    integration = await self._read_integration_result(run_id, dispatch)
                if integration is None:
                    invalid_result = run_id in self._invalid_integration_results
                    self._invalid_integration_results.discard(run_id)
                    if invalid_result:
                        integration_status = "failed"
                        error_type = "integration_failed"
                        error_code = self._invalid_integration_errors.pop(run_id, "integration_result_invalid")
                        error_message = "Agent produced an invalid or mismatched IntegrationResult"
                        integration_events.append(
                            StreamEvent.create(
                                EventType.INTEGRATION_COMPLETED,
                                **self._projection_identity(dispatch),
                                task_id=dispatch.plan_task_id or dispatch.task_id,
                                agent=agent_name,
                                run_id=run_id,
                                status="failed",
                                attempt=dispatch.attempt,
                                success=False,
                                error_code=error_code,
                                error_message=error_message,
                            )
                        )
                    elif operation:
                        # Once an operation exists, natural-language conflict
                        # text is not an authority. Missing structured facts
                        # must close the operation as integration_missing.
                        integration_status = "failed"
                        error_type = "integration_failed"
                        error_code = "integration_missing"
                        error_message = "Agent completed without a matching taskctl IntegrationResult"
                        integration_events.append(
                            StreamEvent.create(
                                EventType.INTEGRATION_COMPLETED,
                                **self._projection_identity(dispatch),
                                task_id=dispatch.plan_task_id or dispatch.task_id,
                                agent=agent_name,
                                run_id=run_id,
                                status="failed",
                                success=False,
                                attempt=dispatch.attempt,
                                error_code=error_code,
                                error_message=error_message,
                            )
                        )
                    else:
                        # 兼容旧 Agent/旧 taskctl：自然语言只允许作为兜底，不能伪造 Git 谱系事实。
                        legacy_conflict_files = self._detect_reported_merge_conflict("".join(collected))
                        if legacy_conflict_files is not None:
                            integration_status = "conflict"
                            error_type = "merge_conflict"
                            error_code = "merge_conflict"
                            error_message = "Sub-agent reported merge conflict (legacy fallback)"
                            conflict_files = legacy_conflict_files
                            integration_events.append(
                                StreamEvent.create(
                                    EventType.INTEGRATION_CONFLICT,
                                    **self._projection_identity(dispatch),
                                    task_id=dispatch.plan_task_id or dispatch.task_id,
                                    agent=agent_name,
                                    run_id=run_id,
                                    attempt=dispatch.attempt,
                                    status="conflict",
                                    conflict_id=self._conflict_projection_id(dispatch, integration),
                                    conflict_files=conflict_files,
                                    error_code=error_code,
                                    error_message=error_message,
                                )
                            )
                        else:
                            integration_status = "failed"
                            error_type = "integration_missing"
                            error_code = "integration_missing"
                            error_message = "Agent completed without a matching taskctl IntegrationResult"
                            integration_events.append(
                                StreamEvent.create(
                                    EventType.INTEGRATION_COMPLETED,
                                    **self._projection_identity(dispatch),
                                    task_id=dispatch.plan_task_id or dispatch.task_id,
                                    agent=agent_name,
                                    run_id=run_id,
                                    attempt=dispatch.attempt,
                                    status="failed",
                                    success=False,
                                    error_code=error_code,
                                    error_message=error_message,
                                )
                            )
                else:
                    source_branch = integration.source_branch
                    source_commit = integration.source_commit
                    target_branch = integration.target_branch
                    target_commit = integration.target_commit
                    merge_base = integration.merge_base
                    conflict_files = list(integration.conflict_files)
                    error_code, error_message = self._integration_error(integration)
                    if integration.status == "merged":
                        integration_status = "merged"
                        success = True
                        error_code = ""
                        error_message = ""
                        integration_events.append(
                            StreamEvent.create(
                                EventType.INTEGRATION_COMPLETED,
                                **self._projection_identity(dispatch),
                                task_id=dispatch.plan_task_id or dispatch.task_id,
                                agent=agent_name,
                                run_id=run_id,
                                attempt=dispatch.attempt,
                                status="merged",
                                success=True,
                            )
                        )
                    elif integration.status == "partial":
                        integration_status = "partial"
                        error_type = ""
                        integration_events.append(
                            StreamEvent.create(
                                EventType.INTEGRATION_COMPLETED,
                                **self._projection_identity(dispatch),
                                task_id=dispatch.plan_task_id or dispatch.task_id,
                                agent=agent_name,
                                run_id=run_id,
                                attempt=dispatch.attempt,
                                status="partial",
                                success=False,
                                conflict_files=conflict_files or None,
                                error_code=error_code or None,
                                error_message=error_message or None,
                            )
                        )
                    elif integration.status == "conflict":
                        integration_status = "conflict"
                        error_type = "merge_conflict"
                        integration_events.append(
                            StreamEvent.create(
                                EventType.INTEGRATION_CONFLICT,
                                **self._projection_identity(dispatch),
                                task_id=dispatch.plan_task_id or dispatch.task_id,
                                agent=agent_name,
                                run_id=run_id,
                                attempt=dispatch.attempt,
                                status="conflict",
                                conflict_id=self._conflict_projection_id(dispatch, integration),
                                conflict_files=conflict_files,
                                error_code=error_code,
                                error_message=error_message,
                            )
                        )
                    else:
                        integration_status = "failed"
                        error_type = "integration_failed"
                        integration_events.append(
                            StreamEvent.create(
                                EventType.INTEGRATION_COMPLETED,
                                **self._projection_identity(dispatch),
                                task_id=dispatch.plan_task_id or dispatch.task_id,
                                agent=agent_name,
                                run_id=run_id,
                                attempt=dispatch.attempt,
                                status="failed",
                                success=False,
                                conflict_files=conflict_files,
                                error_code=error_code,
                                error_message=error_message,
                            )
                        )

            logger.info(
                "ExecutionEngine: completed agent=%s task=%s collected=%d chars success=%s",
                agent_name,
                task_id,
                len("".join(collected)),
                execution_status == "completed" and integration_status == "merged",
            )

        except asyncio.TimeoutError:
            msg = f"Task {task_id} exceeded {timeout}s"
            logger.warning("ExecutionEngine: %s", msg)
            error_type = "timeout"
            error_code = "timeout"
            error_message = sanitize_error_text(msg)
            execution_status = "timeout"
        except Exception as exc:
            msg = f"Task {task_id} agent={agent_name} failed: {exc}"
            logger.error("ExecutionEngine: %s", msg, exc_info=True)
            error_type = "error"
            error_code = "execution_failed"
            error_message = sanitize_error_text(msg)
            execution_status = "failed"

        if (
            operation
            and self._integration_service
            and (execution_status != "completed" or integration_status == "failed")
        ):
            try:
                await self._integration_service.fail_operation(
                    operation.integration_operation_id,
                    error_code or "execution_failed",
                    error_message or "child Run failed before integration",
                )
            except Exception:
                logger.warning(
                    "ExecutionEngine: failed to finalize operation=%s after child failure",
                    operation.integration_operation_id,
                    exc_info=True,
                )

        success = execution_status == "completed" and integration_status in {"merged", "not_required"}
        if execution_status != "completed":
            integration_status = "pending"

        duration = time.monotonic() - start
        result = TaskResult(
            task_id=dispatch.plan_task_id or dispatch.task_id,
            root_task_id=self._task_id,
            agent=agent_name,
            attempt=dispatch.attempt if hasattr(dispatch, "attempt") else 0,
            execution_status=execution_status,
            integration_status=integration_status,
            success=success,
            content="".join(collected),
            message_id=message_id,
            run_id=run_id,
            plan_task_id=dispatch.plan_task_id or dispatch.task_id,
            integration_operation_id=dispatch.integration_operation_id,
            integration_scope_id=dispatch.integration_scope_id or self._task_id,
            workspace_id=workspace_id or dispatch.workspace_handle,
            duration=round(duration, 2),
            error_type=error_type,
            error_code=error_code,
            error_message=error_message,
            conflict_files=conflict_files,
            source_branch=source_branch,
            source_commit=source_commit,
            target_branch=target_branch,
            target_commit=target_commit,
            merge_base=merge_base,
            legacy_conflict_detection=(error_code == "merge_conflict" and not bool(run_id and source_branch)),
        )

        if result.execution_status == "completed" and result.integration_status == "conflict":
            result, resolver_events = await self._attempt_conflict_recovery(dispatch, result)

        # Recovery mutates the authoritative result. Re-derive the compatibility
        # locals before publishing the terminal runtime event so a successful
        # resolver cannot be displayed as a failed task.
        success = result.success is True
        integration_status = result.integration_status
        error_type = result.error_type
        error_code = result.error_code
        error_message = result.error_message
        conflict_files = result.conflict_files

        for integration_event in integration_events:
            yield integration_event, None
        for resolver_event in resolver_events:
            yield resolver_event, None

        yield (
            StreamEvent.create(
                EventType.RUNTIME_COMPLETED,
                **self._projection_identity(dispatch),
                task_id=dispatch.plan_task_id or dispatch.task_id,
                agent=agent_name,
                agent_type=agent_type,
                session_id=session_id,
                message_id=message_id or None,
                run_id=run_id or child_run_id,
                attempt=dispatch.attempt,
                success=success,
                duration=result.duration,
                status=("completed" if success else integration_status if execution_status == "completed" else "failed"),
                error_type=error_type or None,
                error_code=error_code or None,
                error_message=error_message or None,
                conflict_files=conflict_files or None,
            ),
            result,
        )

    def _build_agent_message(self, dispatch: DispatchResult) -> str:
        return (
            dispatch.content.rstrip()
            + "\n\n## 集成要求\n"
            + "完成任务并验证后，由你在自己的 workspace 中执行合并到 task 分支：使用 `taskctl merge`。"
            + "如果合并冲突或命令失败，请停止后续改动，并在回复中报告失败原因和冲突文件。"
        )

    def _detect_reported_merge_conflict(self, text: str) -> list[str] | None:
        lowered = text.lower()
        # 阶段 1：快速排除 —— 完全没有冲突相关关键词
        if "冲突文件" not in text and "conflict files" not in lowered:
            return None

        # 阶段 2：检查冲突是否最终被解决。
        # 子 Agent 在工作过程中可能会遇到"冲突文件"，但随后将其解决并上报成功
        # （例如 taskctl merge 失败 → 手动修复 → taskctl merge 成功）。
        # 如果在最后一次冲突提及之后出现了成功信号，则认为冲突已解决。
        last_conflict_pos = max(text.rfind("冲突文件"), lowered.rfind("conflict files"))
        after_conflict = text[last_conflict_pos:]
        resolution_signals = ["成功合并", "成功同步", "合并成功", "成功完成", "已成功"]
        if any(sig in after_conflict for sig in resolution_signals):
            return None

        # 阶段 3：从"冲突文件"区块中提取文件列表
        files: list[str] = []
        collect = False
        for raw_line in text.splitlines():
            line = raw_line.strip()
            if not line:
                if collect:
                    break
                continue
            if "冲突文件" in line or "conflict files" in line.lower():
                collect = True
                continue
            if collect:
                files.append(line.lstrip("- ").strip())
        return files if files else None
