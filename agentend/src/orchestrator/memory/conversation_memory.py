"""跨对话轮次持久化 Orchestrator 的 memory_messages。

采用与 EvolutionStore 和 PinMemory 相同的基于文件的模式。
在 shared_dir 中以 JSON 格式存储序列化后的 LangChain 消息。
"""

from __future__ import annotations

import json
import logging
from pathlib import Path

from langchain_core.messages import messages_from_dict, messages_to_dict

logger = logging.getLogger(__name__)

_MAX_TURNS = 10


class ConversationMemoryStore:
    """持久化 Orchestrator 的 memory_messages（包括动态上下文消息）。

    存储位置：``{shared_dir}/memory/conversation_memory.json``

    使用 ``langchain_core.messages.messages_to_dict`` /
    ``messages_from_dict`` 对 HumanMessage、
    AIMessage（含 tool_calls）、ToolMessage 和 SystemMessage 进行无损序列化。
    """

    def __init__(self, shared_dir: str | Path) -> None:
        self.memory_dir = Path(shared_dir) / "memory"
        self.memory_dir.mkdir(parents=True, exist_ok=True)

    @property
    def memory_path(self) -> Path:
        return self.memory_dir / "conversation_memory.json"

    # ------------------------------------------------------------------
    # 公共 API
    # ------------------------------------------------------------------

    def save_messages(self, messages: list) -> None:
        """序列化 *messages*，追加到文件，并裁剪到保留上限。"""
        existing = self._load_raw()
        new_entries = messages_to_dict(messages)
        combined = existing + new_entries
        trimmed = self._trim_to_turns(combined, _MAX_TURNS)
        self._write(trimmed)

    def replace_messages(self, messages: list) -> None:
        """用恰好 *messages* 替换存储内容（替换后裁剪）。

        与 :meth:`save_messages` 读取已有条目再追加不同，
        该方法直接写入 *messages* —— 不会产生重复。
        """
        entries = messages_to_dict(messages)
        trimmed = self._trim_to_turns(entries, _MAX_TURNS)
        self._write(trimmed)

    def load_messages(self) -> list:
        """将存储的消息反序列化回 LangChain 消息对象。"""
        raw = self._load_raw()
        if not raw:
            return []
        try:
            return messages_from_dict(raw)
        except Exception:
            logger.warning("Failed to deserialize conversation memory; starting fresh", exc_info=True)
            return []

    # ------------------------------------------------------------------
    # 内部辅助函数
    # ------------------------------------------------------------------

    def _load_raw(self) -> list[dict]:
        if not self.memory_path.exists():
            return []
        try:
            data = self.memory_path.read_text(encoding="utf-8")
            return json.loads(data)
        except (json.JSONDecodeError, OSError):
            logger.warning("Failed to read conversation memory file; treating as empty", exc_info=True)
            return []

    def _write(self, entries: list[dict]) -> None:
        self.memory_path.write_text(
            json.dumps(entries, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )

    @staticmethod
    def _trim_to_turns(entries: list[dict], max_turns: int) -> list[dict]:
        """保留最后 *max_turns* 个完整轮次。

        每当出现 ``type == "human"`` 时即开始一个新轮次。我们从不
        拆分一个轮次 —— 从选定的起始索引到末尾的所有消息都会保留。
        """
        human_indices = [i for i, e in enumerate(entries) if e.get("type") == "human"]
        if len(human_indices) <= max_turns:
            return entries
        start = human_indices[-max_turns]
        return entries[start:]
