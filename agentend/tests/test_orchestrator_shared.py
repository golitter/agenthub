import sys
from pathlib import Path

import pytest
import yaml

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.api.v1.agent import _orchestrator_kwargs, _resolve_workspace
from src.orchestrator.models import DispatchResult, PlanOutput, TaskDef
from src.orchestrator.planning.graph import _write_shared_plan
from src.orchestrator.planning.tools import build_tools
from src.orchestrator.reporting.aggregator import _AGGREGATE_PROMPT, _current_time_context
from src.schemas.request import AgentRequest, AgentType


def _tool_by_name(tools: list, name: str):
    for tool in tools:
        if tool.name == name:
            return tool
    raise AssertionError(f"tool not found: {name}")


def test_write_shared_plan_uses_real_session_ids(tmp_path: Path):
    shared_dir = tmp_path / "worktrees" / "task-1" / "shared" / ".agent"
    plan = PlanOutput(
        overview="Build then review.",
        tasks=[
            TaskDef(task_id="task-001", session_id="frontend", title="Build", content="Create the page."),
            TaskDef(task_id="task-002", session_id="reviewer", title="Review", content="Review the page."),
        ],
    )
    dispatches = [
        DispatchResult(
            task_id="task-001",
            agent="frontend",
            agent_type="claude-code",
            real_session_id="sess-frontend",
            mention="@frontend",
            content="Create the page.",
        ),
        DispatchResult(
            task_id="task-002",
            agent="reviewer",
            agent_type="opencode",
            real_session_id="sess-reviewer",
            mention="@reviewer",
            content="Review the page.",
        ),
    ]

    _write_shared_plan(str(shared_dir), "task-1", plan, dispatches)

    config = yaml.safe_load((shared_dir / "config.yaml").read_text())
    assert config["task_id"] == "task-1"
    assert config["tasks"][0]["session_id"] == "sess-frontend"
    assert config["tasks"][0]["agent"] == "frontend"
    assert (shared_dir / "plans" / "overview.md").read_text() == "Build then review."
    assert "Create the page." in (shared_dir / "plans" / "task-001.md").read_text()


def test_orchestrator_tools_resolve_relative_paths_under_shared(tmp_path: Path):
    shared_dir = tmp_path / "shared" / ".agent"
    shared_dir.mkdir(parents=True)
    (shared_dir / "notes.md").write_text("hello shared", encoding="utf-8")

    tools = build_tools(str(shared_dir))
    read_file = _tool_by_name(tools, "read_file")
    list_dir = _tool_by_name(tools, "list_dir")

    assert "hello shared" in read_file.invoke({"path": "notes.md"})
    assert "notes.md" in list_dir.invoke({"path": "."})


def test_orchestrator_current_time_tool_is_available(tmp_path: Path):
    tools = build_tools(str(tmp_path))
    current_time = _tool_by_name(tools, "current_time")

    output = current_time.invoke({})

    assert "当前日期:" in output
    assert "当前时间:" in output
    assert "UTC offset:" in output


def test_aggregate_prompt_includes_current_time_context():
    current_time = _current_time_context()
    prompt = _AGGREGATE_PROMPT.format(
        current_time=current_time,
        overview="overview",
        results="results",
    )

    assert "## 当前时间" in prompt
    assert current_time in prompt
    assert "不要输出占位符" in prompt


@pytest.mark.asyncio
async def test_orchestrator_does_not_auto_create_code_workspace():
    class WorkspaceManagerStub:
        async def is_git_repo(self, path: str) -> bool:
            raise AssertionError("orchestrator must not create a code workspace")

    request = AgentRequest(
        task_id="task-1",
        session_id="orch",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
        repo_path="/tmp/repo",
    )

    assert await _resolve_workspace(request, WorkspaceManagerStub()) == ""


def test_orchestrator_kwargs_rejects_shared_dir_outside_task(tmp_path: Path):
    repo = tmp_path / "repo"
    repo.mkdir()
    request = AgentRequest(
        task_id="task-1",
        session_id="orch",
        message="hello",
        agent_type=AgentType.ORCHESTRATOR,
        repo_path=str(repo),
        config={"shared_dir": str(tmp_path / "elsewhere" / ".agent")},
    )

    try:
        _orchestrator_kwargs(request)
    except Exception as exc:
        assert getattr(exc, "status_code", None) == 400
    else:
        raise AssertionError("expected shared_dir validation failure")
