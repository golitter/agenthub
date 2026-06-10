from __future__ import annotations

import asyncio
import logging
import time
from collections.abc import AsyncIterator

from src.clients.backend_client import BackendClient
from src.generated.events import EventType
from src.orchestrator.models import DispatchResult, TaskResult
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
    ) -> None:
        self._backend_client = backend_client
        self._workspace_mgr = workspace_mgr
        self._repo_path = repo_path
        self._task_id = task_id
        self._shared_dir = shared_dir
        self._cwd = cwd

    async def execute(
        self,
        dispatches: list[DispatchResult],
        timeout_per_task: float = 300.0,
    ) -> AsyncIterator[tuple[StreamEvent, TaskResult | None]]:
        if len(dispatches) <= 1:
            for dispatch in dispatches:
                async for item in self._execute_task(dispatch, timeout_per_task):
                    yield item
            return

        queue: asyncio.Queue[tuple[StreamEvent, TaskResult | None]] = asyncio.Queue()

        async def _run(dispatch: DispatchResult) -> None:
            async for item in self._execute_task(dispatch, timeout_per_task):
                await queue.put(item)

        tasks = [asyncio.create_task(_run(d)) for d in dispatches]

        async def _drain() -> None:
            await asyncio.gather(*tasks)
            await queue.put(None)  # sentinel

        drain_task = asyncio.create_task(_drain())

        while True:
            item = await queue.get()
            if item is None:
                break
            yield item

        await drain_task

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
                "ExecutionEngine: created worktree for agent=%s session=%s path=%s",
                dispatch.agent,
                real_session_id,
                ws.worktree_path,
            )
            return ws.worktree_path
        except Exception:
            logger.exception(
                "ExecutionEngine: failed to create worktree for agent=%s session=%s",
                dispatch.agent,
                real_session_id,
            )
            return self._cwd or dispatch.workspace_path

    async def _execute_task(
        self,
        dispatch: DispatchResult,
        timeout: float,
    ) -> AsyncIterator[tuple[StreamEvent, TaskResult | None]]:
        task_id = dispatch.task_id
        agent_name = dispatch.agent
        agent_type = dispatch.agent_type or agent_name
        start = time.monotonic()

        yield (
            StreamEvent.create(
                EventType.RUNTIME_EXECUTING,
                task_id=task_id,
                agent=agent_name,
                title=dispatch.content[:80],
                status="running",
            ),
            None,
        )

        session_id = dispatch.real_session_id or f"orch-{task_id}"
        success = False
        collected: list[str] = []
        error_type = ""
        error_message = ""
        message_id = ""
        conflict_files: list[str] = []
        try:
            agent_cwd = await self._ensure_worktree(dispatch)
            agent_message = self._build_agent_message(dispatch)

            # Unified HTTP path — Backend queries window and injects group_chat_messages
            logger.info(
                "ExecutionEngine: HTTP path agent=%s type=%s task=%s session=%s cwd=%s",
                agent_name,
                agent_type,
                task_id,
                session_id,
                agent_cwd,
            )
            message_id = await asyncio.wait_for(
                self._backend_client.run_task(
                    task_id=self._task_id,
                    session_id=session_id,
                    message=agent_message,
                    agent_type=agent_type,
                    cwd=agent_cwd,
                ),
                timeout=30.0,
            )

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
                                EventType.RUNTIME_TEXT,
                                task_id=task_id,
                                agent=agent_name,
                                text=text,
                            ),
                            None,
                        )
                elif event_type == "done":
                    success = True
                    break
                elif event_type == "error":
                    content = event.get("content", {})
                    msg = content.get("message", "unknown error") if isinstance(content, dict) else str(content)
                    error_type = "error"
                    error_message = str(msg)
                    break
            else:
                success = True

            logger.info(
                "ExecutionEngine: completed agent=%s task=%s collected=%d chars success=%s",
                agent_name,
                task_id,
                len("".join(collected)),
                success,
            )
            if success:
                merge_conflict_files = self._detect_reported_merge_conflict("".join(collected))
                if merge_conflict_files is not None:
                    success = False
                    error_type = "merge_conflict"
                    conflict_files = merge_conflict_files
                    error_message = "Sub-agent reported merge conflict"

        except asyncio.TimeoutError:
            msg = f"Task {task_id} exceeded {timeout}s"
            logger.warning("ExecutionEngine: %s", msg)
            error_type = "timeout"
            error_message = msg
        except Exception as exc:
            msg = f"Task {task_id} agent={agent_name} failed: {exc}"
            logger.error("ExecutionEngine: %s", msg, exc_info=True)
            error_type = "error"
            error_message = msg

        duration = time.monotonic() - start
        result = TaskResult(
            task_id=task_id,
            agent=agent_name,
            success=success,
            content="".join(collected),
            message_id=message_id,
            duration=round(duration, 2),
            error_type=error_type,
            error_message=error_message,
            conflict_files=conflict_files,
        )

        yield (
            StreamEvent.create(
                EventType.RUNTIME_COMPLETED,
                task_id=task_id,
                agent=agent_name,
                success=success,
                duration=result.duration,
                status="completed" if success else "failed",
                error_type=error_type or None,
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
        # Phase 1: quick reject — no conflict-related keywords at all
        if "冲突文件" not in text and "conflict files" not in lowered:
            return None

        # Phase 2: check whether the conflict was ultimately resolved.
        # A subagent may encounter "冲突文件" during its work but then resolve
        # it and report success (e.g. taskctl merge fails → manual fix → taskctl
        # merge succeeds).  If a success signal appears AFTER the last conflict
        # mention, the conflict is considered resolved.
        last_conflict_pos = max(text.rfind("冲突文件"), lowered.rfind("conflict files"))
        after_conflict = text[last_conflict_pos:]
        resolution_signals = ["成功合并", "成功同步", "合并成功", "成功完成", "已成功"]
        if any(sig in after_conflict for sig in resolution_signals):
            return None

        # Phase 3: extract the file list from the "冲突文件" section
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
