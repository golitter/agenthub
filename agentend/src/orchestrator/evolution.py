from __future__ import annotations

from datetime import datetime, timezone
from pathlib import Path

import yaml

_MAX_ENTRIES = 20


class EvolutionStore:
    def __init__(self, shared_dir: str | Path) -> None:
        self.shared_dir = Path(shared_dir)
        self.shared_dir.mkdir(parents=True, exist_ok=True)

    @property
    def evolution_path(self) -> Path:
        return self.shared_dir / "evolution.yaml"

    def _load(self) -> list[dict]:
        if not self.evolution_path.exists():
            return []
        return yaml.safe_load(self.evolution_path.read_text(encoding="utf-8")) or []

    def _save(self, entries: list[dict]) -> None:
        self.evolution_path.write_text(
            yaml.dump(entries, allow_unicode=True, default_flow_style=False),
            encoding="utf-8",
        )

    def record(
        self,
        message: str,
        plan_summary: str,
        results_summary: str,
        success: bool,
        agent_performance: list[dict] | None = None,
    ) -> None:
        entries = self._load()
        entries.append(
            {
                "timestamp": datetime.now(timezone.utc).isoformat(),
                "message_summary": message[:200],
                "plan_summary": plan_summary,
                "results_summary": results_summary,
                "success": success,
                "agent_performance": agent_performance or [],
            }
        )
        if len(entries) > _MAX_ENTRIES:
            entries = entries[-_MAX_ENTRIES:]
        self._save(entries)

    def get_recent_experience(self, n: int = 5) -> str:
        entries = self._load()
        if not entries:
            return ""
        recent = entries[-n:]
        lines = ["## 最近编排经验", ""]
        for e in reversed(recent):
            indicator = "✅" if e.get("success") else "❌"
            lines.append(f"{indicator} [{e['timestamp'][:10]}] {e.get('message_summary', '')}")
            lines.append(f"   规划: {e.get('plan_summary', '')[:80]}")
            if e.get("results_summary"):
                lines.append(f"   结果: {e['results_summary'][:80]}")
            lines.append("")
        return "\n".join(lines)
