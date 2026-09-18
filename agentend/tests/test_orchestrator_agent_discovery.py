import asyncio
import json
import sys
from pathlib import Path

import pytest
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import src.orchestrator.planning.graph as graph_module
from src.orchestrator.agent_utils import dispatchable_agent_ids, project_available_agents
from src.orchestrator.execution.dispatcher import Dispatcher
from src.orchestrator.memory.context_compactor import CompactionResult
from src.orchestrator.memory.conversation_memory import ConversationMemoryStore, ConversationSummary, RevisionConflict
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
        "turn_messages": [
            HumanMessage(
                content=message,
                additional_kwargs={"memory_kind": "user_request"},
            )
        ],
        "system_constraints": [],
        "active_pin_snapshot": {
            "task_id": "task-test",
            "fetched_at": "2026-08-28T00:00:00+08:00",
            "complete": True,
            "pins": [],
            "snapshot_digest": "empty",
        },
        "reference_contexts": [],
        "capability_hints": [],
        "allowed_tools": None,
        "evolution_context": "",
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


def test_turn_message_reducer_starts_a_fresh_run_at_user_request() -> None:
    previous = [
        HumanMessage(
            content="previous request",
            additional_kwargs={"memory_kind": "user_request"},
        ),
        AIMessage(content="previous answer"),
    ]
    current = [
        HumanMessage(
            content="current request",
            additional_kwargs={"memory_kind": "user_request"},
        )
    ]

    merged = graph_module._merge_turn_messages(previous, current)

    assert merged == current


def test_checkpoint_safe_summary_dict_is_restored_for_reason_helpers() -> None:
    summary = ConversationSummary(content="historical", compacted_message_count=3)

    restored = graph_module._summary_from_state(summary.to_dict())

    assert restored == summary


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


class _TextBoundLLM:
    async def ainvoke(self, messages, config=None):
        return AIMessage(content="直接回答", tool_calls=[])


class _TextLLM:
    def bind_tools(self, tools):
        return _TextBoundLLM()


def _patch_llm(monkeypatch, responses: list[AIMessage], calls: list[list]) -> None:
    monkeypatch.setattr(graph_module, "ChatOpenAI", lambda **_: _FakeLLM(responses, calls))
    monkeypatch.setattr(graph_module.settings.orchestrator, "reason_max_iterations", 6)


@pytest.mark.asyncio
async def test_compiled_graph_astream_crosses_async_boundaries(tmp_path: Path, monkeypatch) -> None:
    """The production async stream must not stall after a sync helper node."""
    monkeypatch.setattr(graph_module, "ChatOpenAI", lambda **_: _TextLLM())

    async def collect_updates() -> list[dict]:
        updates = []
        graph = graph_module.build_graph()
        async for update in graph.astream(
            _state(tmp_path, message="你好"),
            config={"configurable": {"thread_id": "async-boundary-test"}},
            stream_mode="updates",
        ):
            updates.append(update)
        return updates

    updates = await asyncio.wait_for(collect_updates(), timeout=5)

    assert [next(iter(update)) for update in updates] == [
        "skill_prepare",
        "compact_context",
        "reason",
        "save_mem",
    ]


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


def test_plan_tool_schema_requires_structured_dependency_fields(tmp_path: Path) -> None:
    tool = _tool_by_name(build_tools(str(tmp_path), agents=AGENTS), "plan_and_dispatch")
    schema = tool.args_schema.model_json_schema()
    task_schema = schema["$defs"]["PlanTaskInput"]

    assert {"depends_on", "requires_integrated_dependencies"} <= set(task_schema["required"])
    assert "开始前" in task_schema["properties"]["depends_on"]["description"]


def test_plan_dependency_validation_rejects_text_only_prerequisites() -> None:
    plan = PlanOutput(
        overview="A、B 完成后执行 C",
        tasks=[
            TaskDef(task_id="task-001", session_id="worker", title="A", content="实现 A"),
            TaskDef(task_id="task-002", session_id="reviewer", title="B", content="实现 B"),
            TaskDef(
                task_id="task-003",
                session_id="worker",
                title="集成",
                content="前置条件：任务 A 与任务 B 都必须已完成，再执行集成。",
            ),
        ],
    )

    error = graph_module._plan_dependency_error(plan, "C 必须在 depends_on 中声明 A、B")

    assert error is not None
    assert "task-003" in error
    assert "depends_on is empty" in error


def test_plan_dependency_validation_accepts_explicit_dag() -> None:
    plan = PlanOutput(
        overview="A、B 并行，之后执行 C",
        tasks=[
            TaskDef(task_id="task-001", session_id="worker", title="A", content="实现 A"),
            TaskDef(task_id="task-002", session_id="reviewer", title="B", content="实现 B"),
            TaskDef(
                task_id="task-003",
                session_id="worker",
                title="集成",
                content="前置条件：任务 A 与任务 B 都必须已完成，再执行集成。",
                depends_on=["task-001", "task-002"],
            ),
        ],
    )

    assert graph_module._plan_dependency_error(plan) is None


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
    assert [message.type for message in result["turn_messages"]] == [
        "ai",
        "tool",
        "tool",
        "ai",
        "tool",
    ]
    assert {message.tool_call_id for message in result["turn_messages"] if isinstance(message, ToolMessage)} == {
        "discover-1",
        "plan-1",
        "plan-2",
    }


@pytest.mark.asyncio
async def test_reason_answers_invalid_tool_calls_to_keep_message_sequence_valid(
    tmp_path: Path, monkeypatch
) -> None:
    """DeepSeek 400 回归：langchain_openai 会把 invalid_tool_calls 原样序列化回
    下一次请求的 tool_calls；若不为每个 id 补 ToolMessage 应答，消息序非法。"""
    calls: list[list] = []
    responses = [
        AIMessage(
            content="我先说明一下思路",
            tool_calls=[],
            invalid_tool_calls=[
                {
                    "name": "plan_and_dispatch",
                    "args": '{"overview": ',
                    "id": "broken-1",
                    "error": "Expecting value: line 1 column 13 (char 12)",
                }
            ],
        ),
        AIMessage(
            content="",
            tool_calls=[{"name": "list_available_agents", "args": {}, "id": "discover-1"}],
        ),
        AIMessage(content="", tool_calls=[_plan_call("plan-3")]),
    ]
    _patch_llm(monkeypatch, responses, calls)
    state = _state(tmp_path)
    state["review_message"] = "请分派执行"  # 触发 forced-retry 分支（生产事故路径）

    result = await graph_module.reason_node(state)

    assert result["output_type"] == "plan"
    second_round = calls[1]
    answered = {
        message.tool_call_id for message in second_round if isinstance(message, ToolMessage)
    }
    assert "broken-1" in answered
    # 模拟 DeepSeek 服务端校验：assistant 的每个 tool_call_id 必须被紧邻的
    # tool 消息应答（tool 消息之后才允许出现其它角色）。
    for index, message in enumerate(second_round):
        if not isinstance(message, AIMessage):
            continue
        ids = {str(tc.get("id")) for tc in message.tool_calls}
        ids |= {str(tc.get("id")) for tc in message.invalid_tool_calls if tc.get("id")}
        immediate: set[str] = set()
        for follow in second_round[index + 1 :]:
            if not isinstance(follow, ToolMessage):
                break
            immediate.add(follow.tool_call_id)
        assert ids <= immediate, f"tool calls {ids - immediate} 未被紧邻应答"
    # turn_messages 会进入下一轮 reason 的拼接，同样必须保持应答完整
    turn_ids = {
        message.tool_call_id for message in result["turn_messages"] if isinstance(message, ToolMessage)
    }
    assert {"broken-1", "discover-1", "plan-3"} <= turn_ids


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


class _ExplodingBoundLLM:
    def __init__(self) -> None:
        self.calls = 0

    async def ainvoke(self, messages, config=None):
        self.calls += 1
        if self.calls == 1:
            return AIMessage(
                content="",
                tool_calls=[
                    {"name": "list_available_agents", "args": {}, "id": "discover-before-error"}
                ],
            )
        raise RuntimeError("provider disconnected")


class _ExplodingLLM:
    def __init__(self) -> None:
        self.bound = _ExplodingBoundLLM()

    def bind_tools(self, tools):
        return self.bound


@pytest.mark.asyncio
async def test_reason_returns_partial_transcript_when_provider_fails(tmp_path: Path, monkeypatch) -> None:
    exploding = _ExplodingLLM()
    monkeypatch.setattr(graph_module, "ChatOpenAI", lambda **_: exploding)

    result = await graph_module.reason_node(_state(tmp_path))

    assert result["output_type"] == "error"
    assert [message.type for message in result["turn_messages"]] == ["ai", "tool"]
    assert result["turn_messages"][1].tool_call_id == "discover-before-error"


class _RecordingCompactor:
    def __init__(self) -> None:
        self.calls: list[dict] = []

    async def compact(self, **kwargs) -> CompactionResult:
        self.calls.append(kwargs)
        messages = list(kwargs["messages"])
        return CompactionResult(
            summary=ConversationSummary(content=f"fresh-summary-{len(self.calls)}"),
            recent_messages=tuple(messages[-1:]),
            compacted_message_count=max(0, len(messages) - 1),
        )


@pytest.mark.asyncio
async def test_compaction_discards_stale_result_and_retries_from_latest_revision(tmp_path: Path, monkeypatch) -> None:
    store = ConversationMemoryStore(tmp_path)
    store.commit(
        expected_revision=0,
        summary=ConversationSummary(content="concurrent-summary"),
        recent_messages=[HumanMessage(content="concurrent-message")],
    )
    state = _state(tmp_path, message="继续")
    state["memory_messages"] = [HumanMessage(content=f"old-{index}") for index in range(6)]
    state["memory_revision"] = 0

    compactor = _RecordingCompactor()
    monkeypatch.setattr(graph_module, "ContextCompactor", lambda: compactor)
    monkeypatch.setattr(graph_module.settings.orchestrator, "context_compaction_trigger_tokens", 1)

    original_commit = ConversationMemoryStore.commit
    commit_calls = 0

    def conflict_once(self, **kwargs):
        nonlocal commit_calls
        commit_calls += 1
        if commit_calls == 1:
            raise RevisionConflict("simulated concurrent writer")
        return original_commit(self, **kwargs)

    monkeypatch.setattr(ConversationMemoryStore, "commit", conflict_once)

    result = await graph_module.compact_context_node(state)

    assert len(compactor.calls) == 2
    assert compactor.calls[1]["summary"].content == "concurrent-summary"
    assert compactor.calls[1]["messages"][0].content == "concurrent-message"
    assert result["memory_summary"].content == "fresh-summary-2"
    assert result["memory_revision"] == 2


@pytest.mark.asyncio
async def test_compaction_commit_failure_returns_error_without_using_stale_result(
    tmp_path: Path, monkeypatch
) -> None:
    state = _state(tmp_path, message="继续")
    state["memory_messages"] = [HumanMessage(content=f"old-{index}") for index in range(6)]
    state["memory_revision"] = 0

    class _Compactor:
        async def compact(self, **_kwargs) -> CompactionResult:
            return CompactionResult(
                summary=ConversationSummary(content="should-not-be-used"),
                recent_messages=(HumanMessage(content="recent"),),
                compacted_message_count=5,
            )

    monkeypatch.setattr(graph_module, "ContextCompactor", lambda: _Compactor())
    monkeypatch.setattr(graph_module.settings.orchestrator, "context_compaction_trigger_tokens", 1)

    def fail_commit(self, **_kwargs):
        raise OSError("disk full")

    monkeypatch.setattr(ConversationMemoryStore, "commit", fail_commit)

    result = await graph_module.compact_context_node(state)

    assert result["output_type"] == "error"
    assert "原有会话记忆未修改" in result["text"]


@pytest.mark.asyncio
async def test_compaction_rejects_invalid_pin_snapshot_without_calling_tools_or_llm(
    tmp_path: Path, monkeypatch
) -> None:
    state = _state(tmp_path)
    state["active_pin_snapshot"]["complete"] = False

    class _UnexpectedCompactor:
        async def compact(self, **kwargs):
            raise AssertionError("invalid snapshots must fail before compaction")

    monkeypatch.setattr(graph_module, "ContextCompactor", lambda: _UnexpectedCompactor())

    result = await graph_module.compact_context_node(state)

    assert result["output_type"] == "error"
    assert "Snapshot 无效" in result["text"]


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
    responses = [
        AIMessage(content="我会分派", tool_calls=[]),
        AIMessage(content="仍然无法调用", tool_calls=[]),
    ]
    _patch_llm(monkeypatch, responses, calls)
    state = _state(tmp_path, message="请实现 README 功能")
    state["agents"] = []

    result = await graph_module.reason_node(state)

    assert result["plan"] is None
    assert "没有可分派 Agent" in result["text"]
    assert graph_module._default_dispatch_agent_id([]) is None
    assert graph_module._fallback_plan_from_text(state, "") is None
