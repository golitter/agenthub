import json
import sys
from pathlib import Path

import pytest
from langchain_core.messages import AIMessage, ToolMessage

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import src.orchestrator.planning.graph as graph_module
from src.orchestrator.agent_utils import dispatchable_agent_ids, project_available_agents
from src.orchestrator.execution.dispatcher import Dispatcher
from src.orchestrator.models import PlanOutput, TaskDef
from src.orchestrator.planning.prompts import build_reason_prompt
from src.orchestrator.planning.tools import build_tools


AGENTS = [
    {
        "id": "worker",
        "name": "执行者",
        "type": "claude-code",
        "session_id": "worker-session",
        "workspace_path": "/private/worktree",
    },
    {
        "id": "reviewer",
        "type": "opencode",
        "session_id": "reviewer-session",
    },
    {
        "id": "orchestrator",
        "name": "编排器",
        "type": "orchestrator",
        "session_id": "orchestrator-session",
    },
    {"id": "   ", "name": "空 id", "type": "claude-code"},
]


def _tool_by_name(tools: list, name: str):
    return next(tool for tool in tools if tool.name == name)


def _state(tmp_path: Path, message: str = "请实现一个功能") -> dict:
    return {
        "message": message,
        "agents": AGENTS,
        "task_id": "task-test",
        "shared_dir": str(tmp_path),
        "allowed_read_dirs": [str(tmp_path)],
        "task_base_path": "",
        "system_prompt": "静态测试提示词",
        "memory_messages": [],
        "pin_context": "",
        "evolution_context": "",
        "orchestrator_context": "",
        "review_message": "",
        "review_decision": "",
        "replan_reason": "",
        "orchestrator": {},
    }


def _plan_call(call_id: str, session_id: str = "worker") -> dict:
    return {
        "name": "plan_and_dispatch",
        "id": call_id,
        "args": {
            "overview": "实现并验证",
            "tasks": [
                {
                    "task_id": "task-001",
                    "session_id": session_id,
                    "title": "实现",
                    "content": "完成实现并运行测试",
                }
            ],
        },
    }


class _FakeBoundLLM:
    def __init__(self, responses: list[AIMessage], calls: list[list]) -> None:
        self.responses = responses
        self.calls = calls

    async def ainvoke(self, messages, config=None):
        self.calls.append(list(messages))
        return self.responses.pop(0)


class _FakeLLM:
    def __init__(self, responses: list[AIMessage], calls: list[list]) -> None:
        self.bound = _FakeBoundLLM(responses, calls)

    def bind_tools(self, tools):
        assert "list_available_agents" in {tool.name for tool in tools}
        return self.bound


def _patch_llm(monkeypatch, responses: list[AIMessage], calls: list[list]) -> None:
    monkeypatch.setattr(graph_module, "ChatOpenAI", lambda **_: _FakeLLM(responses, calls))
    monkeypatch.setattr(graph_module.settings.orchestrator, "reason_max_iterations", 6)


def test_available_agent_projection_is_allowlisted_and_filters_internal_entries() -> None:
    projected = project_available_agents(AGENTS)

    assert projected == [
        {"id": "worker", "name": "执行者"},
        {"id": "reviewer", "name": "reviewer"},
    ]
    assert dispatchable_agent_ids(AGENTS) == {"worker", "reviewer"}
    assert "session_id" not in json.dumps(projected)
    assert "workspace_path" not in json.dumps(projected)


def test_list_available_agents_uses_request_snapshot_and_returns_empty_for_no_agents(tmp_path: Path) -> None:
    source_agents = [dict(agent) for agent in AGENTS]
    tools = build_tools(str(tmp_path), agents=source_agents)
    source_agent = source_agents[0]
    source_agent["name"] = "mutated after build"

    result = json.loads(_tool_by_name(tools, "list_available_agents").invoke({}))

    assert result == {
        "count": 2,
        "agents": [
            {"id": "worker", "name": "执行者"},
            {"id": "reviewer", "name": "reviewer"},
        ],
    }
    empty = json.loads(
        _tool_by_name(build_tools(str(tmp_path), agents=[]), "list_available_agents").invoke({})
    )
    assert empty == {"count": 0, "agents": []}


def test_list_available_agents_normalizes_agent_type_alias(tmp_path: Path) -> None:
    tools = build_tools(
        str(tmp_path),
        agents=[
            {"id": "codex-worker", "name": "Codex", "agent_type": "codex"},
            {"id": "orchestrator", "name": "编排器", "agent_type": "orchestrator"},
        ],
    )

    result = json.loads(_tool_by_name(tools, "list_available_agents").invoke({}))

    assert result == {
        "count": 1,
        "agents": [{"id": "codex-worker", "name": "Codex"}],
    }
    assert all("type" not in agent and "agent_type" not in agent for agent in result["agents"])


def test_reason_prompt_has_no_dynamic_agent_snapshot(tmp_path: Path) -> None:
    prompt = build_reason_prompt(str(tmp_path))

    assert "{agents_desc}" not in prompt
    assert "list_available_agents()" in prompt
    assert "可用 Agents\n" not in prompt


def test_dispatcher_rejects_unknown_and_orchestrator_ids_without_mutating_plan() -> None:
    plan = PlanOutput(
        overview="test",
        tasks=[TaskDef(task_id="task-001", session_id="render", title="", content="")],
    )

    with pytest.raises(ValueError, match="Unknown agent id: render"):
        Dispatcher(AGENTS).dispatch(plan)
    assert plan.tasks[0].session_id == "render"

    orchestrator_plan = plan.model_copy(
        update={"tasks": [TaskDef(task_id="task-001", session_id="orchestrator", title="", content="")]}
    )
    with pytest.raises(ValueError, match="Unknown agent id: orchestrator"):
        Dispatcher(AGENTS).dispatch(orchestrator_plan)


def test_dispatcher_rejects_missing_real_session() -> None:
    plan = PlanOutput(
        overview="test",
        tasks=[TaskDef(task_id="task-001", session_id="worker", title="", content="")],
    )

    with pytest.raises(ValueError, match="has no session_id"):
        Dispatcher([{"id": "worker", "type": "claude-code"}]).dispatch(plan)


def test_dispatcher_preserves_dependencies_and_rejects_invalid_topology() -> None:
    plan = PlanOutput(
        overview="dependency test",
        tasks=[
            TaskDef(task_id="task-a", session_id="worker", title="A", content=""),
            TaskDef(
                task_id="task-b",
                session_id="reviewer",
                title="B",
                content="",
                depends_on=["task-a"],
                requires_integrated_dependencies=True,
            ),
        ],
    )
    dispatches = Dispatcher(AGENTS).dispatch(plan)
    assert dispatches[1].depends_on == ["task-a"]
    assert dispatches[1].requires_integrated_dependencies is True

    from src.orchestrator.execution.dispatcher import topological_sort

    assert [[item.task_id for item in wave] for wave in topological_sort(dispatches)] == [["task-a"], ["task-b"]]
    dispatches[0].depends_on = ["missing"]
    with pytest.raises(ValueError, match="unknown task"):
        topological_sort(dispatches)

    dispatches[0].depends_on = ["task-b"]
    dispatches[1].depends_on = ["task-a"]
    with pytest.raises(ValueError, match="cycle"):
        topological_sort(dispatches)


@pytest.mark.asyncio
async def test_reason_rejects_discovery_and_plan_in_same_tool_round(tmp_path: Path, monkeypatch) -> None:
    calls: list[list] = []
    responses = [
        AIMessage(
            content="",
            tool_calls=[
                {"name": "list_available_agents", "args": {}, "id": "discover-1"},
                _plan_call("plan-1"),
            ],
        ),
        AIMessage(content="", tool_calls=[_plan_call("plan-2")]),
    ]
    _patch_llm(monkeypatch, responses, calls)

    result = await graph_module.reason_node(_state(tmp_path))

    assert result["output_type"] == "plan"
    assert result["plan"].tasks[0].session_id == "worker"
    first_round_tools = [message for message in calls[1] if isinstance(message, ToolMessage)]
    assert {message.tool_call_id for message in first_round_tools} == {"discover-1", "plan-1"}
    discovery_message = next(message for message in first_round_tools if message.tool_call_id == "discover-1")
    assert json.loads(discovery_message.content)["agents"][0]["id"] == "worker"
    assert any("previous tool round" in message.content for message in first_round_tools)


@pytest.mark.asyncio
async def test_reason_rejects_ask_before_discovery_and_allows_next_round_after_discovery(
    tmp_path: Path, monkeypatch
) -> None:
    calls: list[list] = []
    responses = [
        AIMessage(
            content="",
            tool_calls=[{"name": "ask_agent", "args": {"agent": "worker", "question": "意见？"}, "id": "ask-1"}],
        ),
        AIMessage(content="直接回答", tool_calls=[]),
    ]
    _patch_llm(monkeypatch, responses, calls)

    result = await graph_module.reason_node(_state(tmp_path, message="请告诉我意见"))

    assert result["output_type"] == "text"
    first_round_tools = [message for message in calls[1] if isinstance(message, ToolMessage)]
    assert len(first_round_tools) == 1
    assert "previous tool round" in first_round_tools[0].content


@pytest.mark.asyncio
async def test_reason_validates_plan_ids_before_human_review(tmp_path: Path, monkeypatch) -> None:
    calls: list[list] = []
    responses = [
        AIMessage(
            content="",
            tool_calls=[{"name": "list_available_agents", "args": {}, "id": "discover-1"}],
        ),
        AIMessage(content="", tool_calls=[_plan_call("plan-1", session_id="claude-code")]),
        AIMessage(content="", tool_calls=[_plan_call("plan-2", session_id="worker")]),
    ]
    _patch_llm(monkeypatch, responses, calls)

    result = await graph_module.reason_node(_state(tmp_path))

    assert result["output_type"] == "plan"
    assert result["plan"].tasks[0].session_id == "worker"
    invalid_round_tools = [message for message in calls[2] if isinstance(message, ToolMessage)]
    assert any("claude-code" in message.content and "worker" in message.content for message in invalid_round_tools)


@pytest.mark.asyncio
async def test_empty_agents_do_not_create_fallback_handle(tmp_path: Path, monkeypatch) -> None:
    calls: list[list] = []
    responses = [AIMessage(content="我会分派", tool_calls=[]), AIMessage(content="仍然无法调用", tool_calls=[])]
    _patch_llm(monkeypatch, responses, calls)
    state = _state(tmp_path, message="请实现 README 功能")
    state["agents"] = []

    result = await graph_module.reason_node(state)

    assert result["plan"] is None
    assert "没有可分派 Agent" in result["text"]
    assert graph_module._default_dispatch_agent_id([]) is None
    assert graph_module._fallback_plan_from_text(state, "") is None
