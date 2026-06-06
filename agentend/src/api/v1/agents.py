from pathlib import Path

from fastapi import APIRouter

from src.app.agent_config import get_agent_config_path

router = APIRouter(prefix="/v1/agents", tags=["agents"])

# Agent 展示信息映射
_AGENT_META = {
    "claude-code": {"name": "Claude Code", "description": "Anthropic Claude Code CLI"},
    "opencode": {"name": "OpenCode", "description": "OpenCode CLI"},
    "codex": {"name": "Codex", "description": "OpenAI Codex CLI"},
    "orchestrator": {"name": "Orchestrator", "description": "Task Orchestrator"},
}


@router.get("/configs")
async def get_agent_configs() -> list[dict]:
    """读取各 Agent CLI 的系统级配置文件内容，由后端 admin 接口调用。"""
    results: list[dict] = []
    for agent_type, meta in _AGENT_META.items():
        config_path = get_agent_config_path(agent_type)
        content = ""
        if config_path:
            p = Path(config_path)
            if p.is_file():
                content = p.read_text(encoding="utf-8")
            else:
                content = "配置文件不存在或无法读取"

        results.append(
            {
                "type": agent_type,
                "name": meta["name"],
                "description": meta["description"],
                "configPath": config_path or "",
                "configContent": content,
            }
        )
    return results
