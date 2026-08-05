from __future__ import annotations

import json
import logging
from collections.abc import AsyncIterator
from urllib.parse import urlsplit, urlunsplit

import httpx

logger = logging.getLogger(__name__)

_SSE_READ_TIMEOUT = 600.0


def _normalize_loopback_url(base_url: str) -> str:
    parts = urlsplit(base_url.rstrip("/"))
    if parts.hostname != "localhost":
        return base_url.rstrip("/")

    netloc = "127.0.0.1"
    if parts.port:
        netloc = f"{netloc}:{parts.port}"
    return urlunsplit((parts.scheme, netloc, parts.path.rstrip("/"), parts.query, parts.fragment))


class BackendClient:
    """用于调用后端 API（RunTask、SSE 流）的 HTTP 客户端。"""

    def __init__(self, base_url: str, timeout: float = 30.0) -> None:
        self._base_url = _normalize_loopback_url(base_url)
        # run_task 用短超时；stream_result 使用独立的 stream client
        self._client = httpx.AsyncClient(timeout=timeout, trust_env=False)

    async def close(self) -> None:
        await self._client.aclose()

    async def run_task(
        self,
        task_id: str,
        session_id: str,
        message: str,
        agent_type: str,
        cwd: str = "",
        skip_user_message: bool = True,
    ) -> str:
        """POST /api/tasks/:taskId/run → 返回 message_id。"""
        resp = await self._client.post(
            f"{self._base_url}/api/tasks/{task_id}/run",
            json={
                "message": message,
                "session_id": session_id,
                "agent_type": agent_type,
                "cwd": cwd,
                "skip_user_message": skip_user_message,
            },
        )
        resp.raise_for_status()
        body = resp.json()
        data = body.get("data", body)
        message_id = data.get("message_id")
        if not message_id:
            raise ValueError(f"RunTask response missing message_id: {body}")
        logger.info(
            "BackendClient.run_task: task=%s session=%s agent=%s → message_id=%s",
            task_id,
            session_id,
            agent_type,
            message_id,
        )
        return message_id

    async def stream_result(
        self,
        task_id: str,
        message_id: str,
        session_id: str,
    ) -> AsyncIterator[dict]:
        """订阅 SSE GET /api/tasks/:taskId/stream 并产出解析后的事件。

        使用独立的 httpx 客户端（带长读取超时）以处理长连接 SSE，
        并通过 aiter_text() 配合手动分行来实现健壮的 SSE 解析。
        """
        url = f"{self._base_url}/api/tasks/{task_id}/stream"
        params = {"message_id": message_id, "session_id": session_id}
        logger.info("BackendClient.stream_result: connecting %s params=%s", url, params)

        # 为 SSE 使用带长读取超时的独立客户端
        sse_client = httpx.AsyncClient(
            timeout=httpx.Timeout(connect=10.0, read=_SSE_READ_TIMEOUT, write=10.0, pool=10.0),
            trust_env=False,
        )
        try:
            async with sse_client.stream("GET", url, params=params) as resp:
                logger.info("BackendClient.stream_result: status=%d", resp.status_code)
                resp.raise_for_status()

                buf = ""
                async for text_chunk in resp.aiter_text():
                    buf += text_chunk
                    while "\n" in buf:
                        line, buf = buf.split("\n", 1)
                        line = line.strip()
                        if not line or not line.startswith("data: "):
                            continue
                        payload = line[6:]  # 去掉 "data: " 前缀
                        try:
                            event = json.loads(payload)
                        except json.JSONDecodeError:
                            logger.warning("BackendClient.stream_result: JSON parse error: %s", payload[:120])
                            continue
                        logger.debug(
                            "BackendClient.stream_result: event type=%s",
                            event.get("type"),
                        )
                        yield event

                # 排空剩余缓冲区（最后一个事件可能不以 \n 结尾）
                if buf.strip():
                    line = buf.strip()
                    if line.startswith("data: "):
                        payload = line[6:]
                        try:
                            event = json.loads(payload)
                            logger.debug("BackendClient.stream_result: drained final event type=%s", event.get("type"))
                            yield event
                        except json.JSONDecodeError:
                            pass
        finally:
            await sse_client.aclose()

    async def get_pinned_announcements(self, task_id: str) -> list[dict]:
        """GET /api/tasks/:taskId/announcements?pinned=true

        返回置顶公告作为 PinRule 的约束。
        优雅降级：出错时返回 []。
        """
        try:
            resp = await self._client.get(
                f"{self._base_url}/api/tasks/{task_id}/announcements",
                params={"pinned": "true"},
            )
            resp.raise_for_status()
            body = resp.json()
            data = body.get("data", [])
            return data if isinstance(data, list) else []
        except Exception:
            logger.warning(
                "BackendClient.get_pinned_announcements: failed task=%s",
                task_id,
                exc_info=True,
            )
            return []

    async def get_agent_window_messages(self, task_id: str, session_id: str) -> list[dict]:
        """GET /api/tasks/:taskId/messages/window?session_id=xxx

        返回该会话的群聊窗口消息。
        出错时返回空列表（优雅降级）。
        """
        try:
            resp = await self._client.get(
                f"{self._base_url}/api/tasks/{task_id}/messages/window",
                params={"session_id": session_id},
            )
            resp.raise_for_status()
            body = resp.json()
            data = body.get("data", [])
            return data if isinstance(data, list) else []
        except Exception:
            logger.warning(
                "BackendClient.get_agent_window_messages: failed task=%s session=%s",
                task_id,
                session_id,
                exc_info=True,
            )
            return []

    async def report_builtin_skills(self, skills: list[dict]) -> None:
        """POST /api/internal/builtin-skills — 向 Backend 上报内置技能。

        使用指数退避重试（3 次尝试：2s/4s/8s）。
        只记录错误但不抛出 —— 不能阻塞启动流程。
        """
        import asyncio

        delays = [2.0, 4.0, 8.0]
        for attempt, delay in enumerate(delays, 1):
            try:
                resp = await self._client.post(
                    f"{self._base_url}/api/internal/builtin-skills",
                    json=skills,
                )
                resp.raise_for_status()
                logger.info("BackendClient.report_builtin_skills: reported %d skills", len(skills))
                return
            except Exception:
                logger.warning(
                    "BackendClient.report_builtin_skills: attempt %d/%d failed",
                    attempt,
                    len(delays),
                    exc_info=True,
                )
                if attempt < len(delays):
                    await asyncio.sleep(delay)
        logger.error(
            "BackendClient.report_builtin_skills: all %d attempts failed, giving up",
            len(delays),
        )
