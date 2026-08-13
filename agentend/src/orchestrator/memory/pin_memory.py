from __future__ import annotations

import re
from datetime import datetime, timezone
from pathlib import Path

import yaml
from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI
from langgraph.config import get_config

from src.app.config import settings
from src.persistence import atomic_write_text

_PINS_FILE = "_pins.yaml"
_SUMMARY_PROMPT = "请用 1-3 句话总结以下内容的要点，使其适合作为 AI 编排器的约束提示：\n\n{content}"


def _slugify(title: str) -> str:
    s = re.sub(r"[^\w\s-]", "", title.lower())
    return (re.sub(r"[-\s]+", "-", s).strip("-") or "untitled")[:120]


class PinMemory:
    def __init__(self, common_dir: str | Path) -> None:
        self.common_dir = Path(common_dir)
        self.common_dir.mkdir(parents=True, exist_ok=True)

    @property
    def pins_path(self) -> Path:
        return self.common_dir / _PINS_FILE

    def _load_pins(self) -> list[dict]:
        if not self.pins_path.exists():
            return []
        loaded = yaml.safe_load(self.pins_path.read_text(encoding="utf-8")) or []
        return loaded if isinstance(loaded, list) else []

    def _resolve_common_file(self, filename: str) -> Path | None:
        if (
            not filename
            or filename != Path(filename).name
            or filename in {".", ".."}
            or any(ord(char) < 32 or ord(char) == 127 for char in filename)
        ):
            return None
        candidate = (self.common_dir / filename).resolve(strict=False)
        try:
            candidate.relative_to(self.common_dir.resolve())
        except ValueError:
            return None
        return candidate

    def _save_pins(self, pins: list[dict]) -> None:
        atomic_write_text(
            self.pins_path,
            yaml.dump(pins, allow_unicode=True, default_flow_style=False),
            encoding="utf-8",
        )

    async def _generate_summary(self, content: str, config: dict | None = None) -> str:
        llm = ChatOpenAI(
            model=settings.llm.model,
            base_url=settings.llm.base_url,
            api_key=settings.llm.api_key,
        )
        prompt = _SUMMARY_PROMPT.format(content=content[:2000])
        if config is None:
            try:
                config = get_config()
            except RuntimeError:
                config = None
        response = await llm.ainvoke([HumanMessage(content=prompt)], config=config)
        return response.content.strip()

    async def pin(self, title: str, content: str, source: str = "user") -> str:
        filename = f"{_slugify(title)}.md"
        filepath = self.common_dir / filename
        atomic_write_text(filepath, content, encoding="utf-8")

        summary = await self._generate_summary(content)

        pins = self._load_pins()
        pins.append(
            {
                "filename": filename,
                "title": title,
                "source": source,
                "pinned_at": datetime.now(timezone.utc).isoformat(),
                "summary": summary,
            }
        )
        self._save_pins(pins)
        return filename

    async def pin_existing(self, filename: str, title: str = "", source: str = "user") -> bool:
        filepath = self._resolve_common_file(filename)
        if not filepath or not filepath.is_file():
            return False

        pins = self._load_pins()
        if any(p["filename"] == filename for p in pins):
            return False

        content = filepath.read_text(encoding="utf-8")
        summary = await self._generate_summary(content)

        pins.append(
            {
                "filename": filename,
                "title": title or filename,
                "source": source,
                "pinned_at": datetime.now(timezone.utc).isoformat(),
                "summary": summary,
            }
        )
        self._save_pins(pins)
        return True

    def unpin(self, filename: str) -> dict | None:
        """移除 pin 并返回被移除的条目，若未找到则返回 None。"""
        pins = self._load_pins()
        removed = next((p for p in pins if p["filename"] == filename), None)
        if not removed:
            return None
        self._save_pins([p for p in pins if p["filename"] != filename])
        return removed

    def get_context(self) -> str:
        pins = self._load_pins()
        if not pins:
            return ""
        lines = ["## 必须遵守的约束（Pin），（每一次对话这个都可能发生变化）", ""]
        for p in pins:
            lines.append(f"- **{p['title']}**: {p['summary']}")
            lines.append(f"  > 完整内容: common/{p['filename']}")
        return "\n".join(lines)

    def get_full_content(self, filename: str) -> str | None:
        filepath = self._resolve_common_file(filename)
        if not filepath or not filepath.is_file():
            return None
        return filepath.read_text(encoding="utf-8")

    def list_pins(self) -> list[dict]:
        return self._load_pins()
