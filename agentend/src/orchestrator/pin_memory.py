from __future__ import annotations

import re
from datetime import datetime, timezone
from pathlib import Path

import yaml
from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI

from src.app.config import settings

_PINS_FILE = "_pins.yaml"
_SUMMARY_PROMPT = "请用 1-3 句话总结以下内容的要点，使其适合作为 AI 编排器的约束提示：\n\n{content}"


def _slugify(title: str) -> str:
    s = re.sub(r"[^\w\s-]", "", title.lower())
    return re.sub(r"[-\s]+", "-", s).strip("-") or "untitled"


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
        return yaml.safe_load(self.pins_path.read_text(encoding="utf-8")) or []

    def _save_pins(self, pins: list[dict]) -> None:
        self.pins_path.write_text(
            yaml.dump(pins, allow_unicode=True, default_flow_style=False),
            encoding="utf-8",
        )

    async def _generate_summary(self, content: str) -> str:
        llm = ChatOpenAI(
            model=settings.llm.model,
            base_url=settings.llm.base_url,
            api_key=settings.llm.api_key,
        )
        prompt = _SUMMARY_PROMPT.format(content=content[:2000])
        response = await llm.ainvoke([HumanMessage(content=prompt)])
        return response.content.strip()

    async def pin(self, title: str, content: str, source: str = "user") -> str:
        filename = f"{_slugify(title)}.md"
        filepath = self.common_dir / filename
        filepath.write_text(content, encoding="utf-8")

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
        filepath = self.common_dir / filename
        if not filepath.exists():
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

    def unpin(self, filename: str) -> bool:
        pins = self._load_pins()
        new_pins = [p for p in pins if p["filename"] != filename]
        if len(new_pins) == len(pins):
            return False
        self._save_pins(new_pins)
        return True

    def get_context(self) -> str:
        pins = self._load_pins()
        if not pins:
            return ""
        lines = ["## 必须遵守的约束（Pin）", ""]
        for p in pins:
            lines.append(f"- **{p['title']}**: {p['summary']}")
            lines.append(f"  > 完整内容: common/{p['filename']}")
        return "\n".join(lines)

    def get_full_content(self, filename: str) -> str | None:
        filepath = self.common_dir / filename
        if not filepath.exists():
            return None
        return filepath.read_text(encoding="utf-8")

    def list_pins(self) -> list[dict]:
        return self._load_pins()
