# Orchestrator Context Compaction — 上下文压缩与 Pin 权威快照

> 状态：已实现；`/v1/pin/announcement-unpin` 暂保留为无副作用的 deprecated
> 兼容端点。本文是当前 Orchestrator 上下文组装、压缩和 Active Pin
> Snapshot 的权威设计；[17-conversation-memory.md](17-conversation-memory.md) 和
> [21-unpin-history-persistence.md](21-unpin-history-persistence.md) 仅保留为历史演进记录。

## 实现了什么

为 Orchestrator 增加基于 Token 水位的跨轮上下文压缩，并重新定义动态上下文、历史摘要和 Pin 的权限边界：

- 固定系统提示词、实时硬约束、参考上下文和对话历史分层组装，不再混入同一个 `system_prompt_append` 字符串。
- `ConversationMemoryStore` 从“保留最近 10 轮并直接丢弃旧消息”升级为“历史摘要 + 最近完整消息”的版本化存储。
- Backend pinned announcements 组成每轮完整、权威的 Active Pin Snapshot；当前 Pin 原文不进入历史摘要。
- Pin 是否有效只由当前快照决定。历史消息或摘要中提到、但不在当前快照中的 Pin 一律无效，因此不再依赖持久化的 unpin `SystemMessage`。
- Pin 快照查询失败与“成功查询但结果为空”严格区分。无法确认硬约束状态时停止本轮规划，不允许无约束降级执行。
- 文件型 `PinMemory` 定义为参考资料，不与 Backend 公告硬约束混用。
- 压缩失败不破坏原有会话记忆；新旧存储格式支持一次性迁移和安全回退。

本文只覆盖 Orchestrator 自管的 LangChain 消息历史。Claude Code、OpenCode、Codex、Pi 等 CLI 自身的上下文压缩仍由对应 CLI 会话机制负责。

## 怎么实现的

### 1. 改造前代码基线（问题背景）

在本次改造前，相关实现由以下路径组成；本节保留用于说明问题来源：

| 能力 | 当前代码 | 当前行为 |
|------|----------|----------|
| 对话记忆 | `src/orchestrator/memory/conversation_memory.py` | JSON 保存 LangChain 消息；超过 10 个 Human 轮次时直接删除旧轮次 |
| 记忆加载 | `src/adapters/orchestrator.py` | 每次 Graph 运行前加载 `conversation_memory.json` |
| Prompt 组装 | `src/orchestrator/planning/graph.py:reason_node` | 固定 System、`pin_context`、Evolution、群聊、历史、当前消息依次追加 |
| 硬 Pin 来源 | `src/api/v1/agent.py` + `src/rules/builtin.py:PinRule` | 每轮向 Backend 查询 pinned announcements，再拼入 `system_prompt_append` |
| 文件 Pin | `src/orchestrator/memory/pin_memory.py` | 提供 `_pins.yaml`、摘要和 `get_context()`；当前 Orchestrator 主流程未调用 `get_context()` |
| 取消公告 | `src/api/v1/pin.py` + Backend `AnnouncementService` | Backend 异步通知 AgentEnd，向 ConversationMemory 追加 unpin `SystemMessage` |

当前 `ConversationMemoryStore` 的裁剪入口为：

```python
_MAX_TURNS = 10

def replace_messages(self, messages: list) -> None:
    entries = messages_to_dict(messages)
    trimmed = self._trim_to_turns(entries, _MAX_TURNS)
    self._write(trimmed)
```

这不是语义压缩：第 11 轮到达时，最旧完整轮次会被永久删除。

当前 RuleEngine 还把 Safety、Scope、Pin、Soul、GroupChat、Skill 等内容都合并到 `system_prompt_append`。OrchestratorAdapter 再把整个字符串放入名为 `pin_context` 的字段，因此该字段并不只包含 Pin。

### 2. 权限分层

目标 Prompt 分为四层：

```text
┌─────────────────────────────────────────────────────┐
│ 1. System / Core                                    │
│    Orchestrator 身份、平台安全规则、Workspace Scope │
├─────────────────────────────────────────────────────┤
│ 2. System / Live Controls                           │
│    本轮完整 Active Pin Snapshot                     │
│    只有该快照可以决定 Pin 当前是否有效              │
├─────────────────────────────────────────────────────┤
│ 3. Human / Reference                                │
│    历史摘要、Evolution、群聊窗口、文件型 Pin        │
│    这些内容是资料，不提升为系统指令                 │
├─────────────────────────────────────────────────────┤
│ 4. Conversation                                     │
│    最近完整 Human/AI/Tool 消息、当前用户消息        │
│    review/replan 补充紧跟当前消息                   │
└─────────────────────────────────────────────────────┘
```

核心规则：

1. 动态内容不拼入静态 `build_reason_prompt()`。
2. 不把群聊、Evolution、历史摘要放入 `SystemMessage`。
3. 不把当前 Active Pin 持久化进 ConversationMemory。
4. 不允许历史摘要判断某条 Pin 是否仍然有效。
5. Tool schema 继续通过 `llm.bind_tools(tools)` 提供，不在历史摘要中重复描述。
6. 一次 Reason 的 Human / AI / Tool 完整轨迹必须进入 ConversationMemory，不能只保存最后一批工具调用。
7. 同一个 Run 的重连复用首次接纳时的 Active Pin Snapshot，不重新解释该 Run 的硬约束。

### 3. RuleEngine 输出拆分

将当前单一 `system_prompt_append` 拆成有语义的结果：

```python
class RuleResult(TypedDict):
    system_constraints: list[str]
    active_pins: list[dict]
    reference_context: list[str]
    capability_hints: list[str]
    allowed_tools: list[str] | None
    max_turns: int | None
```

规则映射：

| Rule | 目标通道 |
|------|----------|
| SafetyRule | `system_constraints` |
| ScopeRule | `system_constraints` |
| SoulRule | `system_constraints` |
| PinRule | `active_pins` |
| GroupChatRule | `reference_context` |
| SkillRule | `capability_hints` |
| TaskctlRule | `system_constraints` |

Orchestrator 不再把完整 `system_prompt_append` 重命名为 `pin_context`。非 Orchestrator CLI 若仍依赖字符串追加，可由 API 边界把结构化结果按兼容顺序重新连接，避免一次改动破坏所有 Adapter。

`capability_hints` 作为 Human/reference 内容进入 Prompt，不提升为 System。`allowed_tools` 不进入 Prompt，而是在 `llm.bind_tools()` 前过滤实际工具集合，语义固定为：

- `None`：使用 Orchestrator 默认安全工具集；
- `[]`：不绑定任何工具；
- 非空列表：默认安全工具集与 allowlist 取交集，未知工具名记录 warning 后忽略。

### 4. Active Pin Snapshot

#### 4.1 数据结构

Backend pinned announcements 是硬约束的唯一权威来源。AgentEnd 将一次成功查询规范化为：

```python
class ActivePin(TypedDict):
    pin_id: str          # announcement:{announcement.id}
    sender_id: str
    sender_name: str
    content: str
    created_at: str


class ActivePinSnapshot(TypedDict):
    task_id: str
    fetched_at: str
    complete: bool
    pins: list[ActivePin]
    snapshot_digest: str  # 规范化 pins 的 SHA-256，仅用于运行时观测
```

`complete` 只允许在 Backend 请求成功、响应结构合法且完整解析后为 `True`。实现中不接受 `complete=False` 的快照进入 LLM。

#### 4.2 空集合与失败

必须区分：

```text
HTTP 成功 + []  → 完整快照，本轮确实没有 Active Pin
超时/5xx/非法响应 → 状态未知，本轮返回错误，不调用 Orchestrator LLM
```

因此 `BackendClient.get_pinned_announcements()` 不再捕获所有异常后返回 `[]`。它应返回成功结果或抛出明确异常，由 `/v1/agent` 的同步和流式入口转换成可观察错误。

#### 4.3 SystemMessage 格式

即使当前没有 Pin，也注入完整快照声明：

```text
## Active Pin Snapshot

This is the complete and authoritative Pin set for this request.
Only Pins listed below are currently active. Any Pin mentioned in conversation
history, summaries, plans, group chat, or tool output but absent here is inactive
and must not be treated as a current constraint.

Snapshot fetched at: 2026-08-27T10:20:00+08:00
Active Pins: none
```

有 Pin 时逐项包含稳定 ID 和原文：

```text
- ID: announcement:381
  Sender: maintainer
  Constraint: 禁止修改数据库结构。
```

硬约束不能由压缩 LLM 自动改写。AgentEnd 在新 Run 接纳前先估算 Active Pin
Snapshot；若总量超过配置预算，直接拒绝本轮请求，不创建 Run、不调用 LLM，且不能
静默截断或摘要后继续规划。Graph 入口仍会重复校验，作为内部调用和状态恢复的防线。

#### 4.4 取消语义

删除公告后，它会从下一次成功获取的完整快照中消失：

```text
上一轮 snapshot = [announcement:381]
删除公告
下一轮 snapshot = []
```

System 层已经声明“历史中存在但快照中缺席的 Pin 无效”，所以缺席本身就是取消状态。目标实现不再把取消事件作为普通 `SystemMessage` 写进对话记忆，也不需要模型侧 tombstone 或 tombstone GC。

若需要审计删除人和删除时间，应在 Backend 持久化审计事件；审计记录不参与 Orchestrator Prompt 权限判断。

一次请求在取得快照后即采用请求级快照语义。快照取得之后发生的 Pin 变化从下一次请求生效，不尝试在一次 LLM 调用中途热替换控制状态。

#### 4.5 Run 接纳、重连与请求指纹

Active Pin Snapshot 是 Run 首次接纳时确定的运行时事实。同步和流式入口必须先区分“创建新 Run”和“订阅已有 Run”，不能在每次 HTTP 重试或 SSE 重连时重新决定快照：

```text
收到请求
  │
  ├── run_id 已存在
  │     ├── 使用不含动态快照的稳定请求指纹校验调用参数
  │     └── 直接返回/订阅已有 Run，不重新查询 Backend Pin
  │
  └── run_id 不存在
        ├── 查询并完整校验 Active Pin Snapshot
        ├── 将 Snapshot 作为首次 Run runtime 持久化
        └── 原子创建并启动 Run
```

稳定请求指纹只由调用方请求、已解析 workspace 和静态规则配置构成。`_request_fingerprint()` 必须排除 `active_pins`、`fetched_at`、动态群聊/reference 窗口、Backend 重建的 live Agent 投影和查询耗时等运行时上下文，不能因为重连时 Backend 状态已变化而把同一个 `run_id` 误判为不同请求。首次接纳的 Snapshot 应写入现有 Run `runtime_json`（或等价的持久化运行时字段），runner 只读取该快照；重连只读取既有 Run journal。这样即使重连时 Backend 暂时不可达，已接纳 Run 的事件仍可正常读取。

若需要比对或观测快照，可对规范化后的 `pins` 计算 `snapshot_digest`；该摘要属于 Run runtime，不属于客户端请求指纹。新 `run_id` 必须重新查询，因此会获得当时最新的 Pin 集合。

### 5. 文件型 PinMemory

文件型 `PinMemory` 与 Backend 公告 Pin 分开定义：

| 类型 | 权限 | 内容 | 压缩策略 |
|------|------|------|----------|
| Backend announcement Pin | System 硬约束 | 当前公告原文 | 永不进入历史压缩 |
| 文件型 PinMemory | Human/reference | 摘要、文件路径 | 可作为参考上下文裁剪或按需读取 |

目标实现不得把 `PinMemory.get_context()` 的摘要并入 Active Pin Snapshot。文件摘要是 LLM 生成的有损内容，不适合作为“优先级最高”的硬约束。

如果确认文件型 Pin API 已无产品调用方，可单独废弃；该清理不阻塞上下文压缩实现。

### 6. ConversationMemory V2

#### 6.1 文件格式

`{shared_dir}/memory/conversation_memory.json` 升级为 envelope：

```json
{
  "version": 2,
  "revision": 7,
  "summary": {
    "content": "用户要求……；已完成……；仍需……",
    "compacted_message_count": 42,
    "updated_at": "2026-08-27T10:30:00+08:00"
  },
  "recent_messages": [
    {
      "type": "human",
      "data": {
        "content": "继续检查实现",
        "additional_kwargs": {},
        "response_metadata": {}
      }
    }
  ]
}
```

`recent_messages` 继续使用 LangChain `messages_to_dict()` / `messages_from_dict()`，保证 AI tool calls 与 ToolMessage 的 `tool_call_id` 不被破坏。

`revision` 用于检测意外的并发覆盖。该文件仍是 task 级 Orchestrator 记忆：同一 `shared_dir` 下的 Orchestrator session 共享历史。RunSupervisor 只拒绝同一 session 的并发 active run，不足以保护不同 session 对同一文件的写入，因此所有保存都必须使用 revision CAS，而不是无条件覆盖。

#### 6.2 所有写入统一使用 CAS

Store 对外返回带 revision 的不可变快照，并提供显式提交接口：

```python
class RevisionConflict(RuntimeError):
    pass


def commit(
    self,
    *,
    expected_revision: int,
    summary: ConversationSummary,
    recent_messages: list,
) -> MemoryEnvelope:
    ...
```

提交过程必须是：持锁重新读取当前 envelope → 比较 `current.revision == expected_revision` → 匹配时写入 `revision + 1`。不匹配时抛出 `RevisionConflict`，禁止继续覆盖。原有无条件覆盖的 `replace_messages()` 不再作为 V2 保存入口。

普通对话保存与压缩提交采用不同的冲突恢复：

- 普通保存只携带本 Run 新增的 `turn_messages`。实现会在同一 workspace 锁内读取最新 envelope、合并本轮增量并提交；若调用方带来的 revision 已过期只记录冲突，不用旧的全量状态覆盖新消息。
- 压缩提交基于特定 revision 生成摘要。发生冲突时摘要结果立即作废；可以基于新版本重新压缩，或保留新版本并留待下一轮，不能把旧摘要强行合入。
- V1 → V2 迁移也必须走同一 CAS 提交路径。

进程内按 `shared_dir` 加锁用于避免同一进程内竞争，并在
`conversation_memory.json.lock` 上使用 OS 文件锁覆盖多个 AgentEnd worker；
`atomic_write_text()` 用于避免半文件。仅靠 `asyncio.Lock` 不能提供跨进程互斥。

#### 6.3 V1 迁移

读取时按 JSON 顶层类型区分：

- 顶层为 list：现有 V1 消息列表；包装为 `version=2`、空 summary、原列表作为 `recent_messages`。
- 顶层为 object 且 `version=2`：按 V2 校验。
- 数据损坏或未知版本：记录 warning，保留原文件，不覆盖；默认按 `context_memory_corruption_policy: fail` 显式失败。只有显式配置为 `empty` 时才允许本轮使用空历史。

迁移采用读时兼容、下一次成功保存时写 V2，不需要启动时批量修改所有 workspace。

### 7. Token 预算与压缩触发

新增配置：

```yaml
orchestrator:
  context_window_tokens: 65536
  context_compaction_trigger_tokens: 46000
  context_compaction_target_tokens: 36000
  context_recent_turns: 4
  context_output_reserve_tokens: 8192
  context_summary_max_tokens: 4096
  context_memory_corruption_policy: fail
  active_pin_max_tokens: 8192
```

触发判断必须计算本轮完整预算，而不是只计算历史：

```text
固定 System
+ Tool schemas
+ Active Pin Snapshot
+ Evolution / 群聊 / 文件参考
+ 既有 summary
+ recent_messages
+ 当前用户/review/replan
+ output reserve
```

当估算值超过 `context_compaction_trigger_tokens` 时：

1. 至少保留最近 `context_recent_turns` 个完整 Human 起始轮次。
2. 将更老的完整轮次与旧 summary 交给摘要器。
3. 不拆开 AI tool call 与对应 ToolMessage。
4. 不把动态上下文交给摘要器。
5. 摘要输出不得超过 `context_summary_max_tokens`，压缩后应降到 `context_compaction_target_tokens` 以下，避免下一轮立即再次压缩。
6. 摘要成功后通过 revision CAS 提交新 summary 和 recent messages。
7. 摘要失败或 CAS 冲突时保持原存储不变；如果本轮仍无法放入 `context_window_tokens`，返回明确的上下文预算错误。

待压缩的旧历史如果本身超过摘要模型的输入窗口，会按完整 Human 起始轮次分批摘要，
每批将上一个批次的 summary 作为输入；AIMessage 与对应 ToolMessage 不会跨批次拆开。
单个完整轮次如果自身就超过摘要输入窗口，则显式返回预算错误，不静默截断。

如果固定 System、Tool schemas、Active Pin Snapshot、当前输入和输出预留本身已经超过窗口，即使没有可压缩历史也必须直接报错。`context_recent_turns` 是正常保留下限；若保留这些完整轮次后仍超过窗口，不允许拆开工具协议或静默截断，而是返回预算错误。

Token 估算器可以先使用保守的字符近似，但必须给 Tool schema、中文文本和输出预留留出安全边际。若模型供应商提供匹配 tokenizer，应通过独立接口替换估算实现，避免把 tokenizer 细节散落到 Graph。

### 8. 摘要生成

摘要输入由服务端构造：

```text
已有历史摘要（可能为空）
待压缩的 Human / AI / Tool 完整消息
已经完成的 plan review 决定和 replan 失败事实
```

摘要器不得接收：

- 当前 Active Pin Snapshot；
- Safety、Scope、Soul、Skill 等 System 控制文本；
- Evolution 和当前群聊窗口；
- 当前尚未完成的用户输入。

review/replan 只有在已经成为会话事实并写入 `turn_messages` 后，才允许在后续压缩中进入摘要；当前仍在等待用户确认的草案或尚未发生的推断不得提前写入摘要。

摘要提示必须要求保留：

- 用户长期目标与明确偏好；
- 已确认的设计决定及原因；
- 已完成工作和可验证结果；
- 未完成事项、阻塞和错误；
- 继续工作必需的文件路径、符号名和 ID。

同时必须要求：

- 不把历史 Pin 或旧 System 指令描述成当前有效规则；
- 可以用过去时记录旧 Pin 造成的历史结果；
- 不复制大段工具输出、日志或代码；
- 不杜撰未出现在输入中的状态；
- 将摘要视为历史资料，而不是新的系统指令。

摘要结果在主 LLM Prompt 中使用带边界标记的 `HumanMessage`：

```text
[Historical conversation summary — reference only]
<summary>
...
</summary>
[/Historical conversation summary]
```

### 9. Prompt 组装

`reason_node` 不再直接逐个读取松散字符串，改由纯函数构建消息：

```python
def build_reason_messages(
    *,
    system_prompt: str,
    system_constraints: list[str],
    active_pin_snapshot: ActivePinSnapshot,
    history_summary: str,
    reference_contexts: list[str],
    capability_hints: list[str],
    recent_messages: list,
    current_messages: list,
) -> list:
    ...
```

输出顺序固定为：

```python
messages = [
    SystemMessage(core_system_prompt),
    SystemMessage(render_system_constraints(...)),
    SystemMessage(render_active_pin_snapshot(...)),
    HumanMessage(render_history_summary(...)),       # 可选
    HumanMessage(render_reference_contexts(...)),    # 可选
    HumanMessage(render_capability_hints(...)),      # 可选
    *recent_messages,
    *current_messages,
]
```

`recent_messages` 只包含请求开始前已经提交的历史；`current_messages` 是本 Run 尚未提交的完整 turn transcript，第一项必须是当前 `HumanMessage`，后续可包含本 Run 已产生的 review/replan Human、AI 和 Tool 消息。这样重新进入 `reason_node` 时可以继续使用本 Run 轨迹，又不会重复追加原始用户消息。

若消息提供方要求 SystemMessage 连续，前三项保持连续；历史摘要和动态参考全部位于其后。测试必须断言消息顺序和 role，防止后续 Rule 再次把群聊或 capability hint 提升为 System。

`allowed_tools` 不属于消息构建参数。`reason_node` 在调用 builder 前先执行：

```python
tools = build_tools(...)
tools = filter_allowed_tools(tools, allowed_tools)
llm_with_tools = llm.bind_tools(tools)
```

### 10. 完整 Reason Transcript 与当前轮事实

一次 Run 的记忆增量由独立的 `turn_messages` 收集。初始只追加一次原始用户消息；每次模型响应和每个工具结果都按实际调用顺序追加：

```text
Human(当前用户请求)
AI(list_available_agents tool call)
Tool(list_available_agents result)
AI(ask_agent tool call)
Tool(ask_agent result)
AI(plan_and_dispatch tool call)
Tool(plan_generated)
```

不能只在 `reason_node` 返回时保存最后一个 AIMessage 和最后一批 ToolMessage。任何发送给 Reason LLM、且属于本轮对话事实的 AI/Tool 协议消息都必须进入 `turn_messages`，以便后续轮次和摘要器看到完整决策依据。

review 和 replan 也必须规范化为只追加一次的 Human/reference 消息：

```python
HumanMessage(
    content="[Plan review feedback]\n...",
    additional_kwargs={"memory_kind": "plan_review"},
)

HumanMessage(
    content="[Execution/replan facts]\n...",
    additional_kwargs={"memory_kind": "replan_failure"},
)
```

推荐的 `memory_kind` 至少包括 `user_request`、`plan_review` 和 `replan_failure`。当前 Active Pin、Evolution、群聊窗口和 capability hints 仍是每轮重建的动态参考，不追加到 `turn_messages`。Graph 完成时只把本 Run 的 `turn_messages` 作为增量提交一次；同一 Run 内重新进入 Reason 时复用该列表，不再次追加原始用户消息。

LangGraph 的 checkpoint 线程也按 Run 隔离：API 启动时使用 `run_id` 作为
`thread_id`（直接调用 Adapter 且未提供 Run ID 时生成一次性调用标识）。此外，
`turn_messages` reducer 将 `memory_kind=user_request` 视为新的 Run 边界，即使调用方
误复用旧的 checkpoint 线程，也不会把上一 Run 的未提交轨迹拼接到当前 Prompt。
`memory_summary` 在 checkpoint 中使用普通字典表示，节点入口再还原为
`ConversationSummary`，避免依赖 LangGraph 对自定义 dataclass 的反序列化注册。

### 11. 群聊上下文去重

当前群聊存在两个入口：

1. Backend 请求中的 `group_chat_messages` 经 `GroupChatRule` 进入 `system_prompt_append`。
2. `OrchestratorAdapter` 再调用 `get_agent_window_messages()` 构造 `orchestrator_context`。

目标实现只保留一个权威入口。推荐复用 Backend 已随请求提供的 `group_chat_messages`，由 RuleEngine 输出到 `reference_context`；Adapter 不再二次查询同一窗口。

群聊窗口是实时参考资料，不写入 ConversationMemory，也不参与历史摘要。群聊中真正影响当前任务的结论，应通过当前对话或任务产物进入可持久化历史。

### 12. 保存事务

压缩采用“先生成、后提交”：

```text
读取 V2 + revision=N
    │
    ├── 在内存中选择 compactable/recent
    ├── 调用摘要 LLM
    ├── 校验摘要非空、大小和结构
    ▼
持锁重新检查 revision=N
    │
    ├── 不一致：放弃结果并重试或留待下轮
    └── 一致：atomic_write V2 revision=N+1
```

不得先删除旧消息再调用摘要模型。`atomic_write_text()` 只保证单次替换不会产生半文件，不提供读取—修改—写入的并发隔离，因此 revision 检查和按 workspace 锁仍然必要。

普通保存同样必须执行 CAS：读取 `revision=N` 后，只提交当前 Run 的 `turn_messages` 增量；若 revision 已改变，重新加载最新版本并重新合并增量。压缩 CAS 冲突则丢弃过期摘要，不能套用普通保存的直接合并策略。

### 13. 旧 unpin 路径迁移

上线顺序：

1. 先实现完整 Active Pin Snapshot 和 fail-closed 查询。
2. Prompt 加入“只有当前快照中的 Pin 有效”的控制声明。
3. 压缩器识别现有历史中的 `[Pin 约束已取消]` / `[公告约束已取消]` SystemMessage：迁移时不将其复制为当前 System 指令，可把必要历史结果合并进摘要。
4. 停止 `/v1/pin/remove` 向 ConversationMemory 写取消消息。
5. 移除 Backend `notifyUnpin()` 和 `/v1/pin/announcement-unpin`；若兼容期仍接收旧通知，端点只记录 deprecated 日志，不修改会话历史。
6. 观察一个发布周期后删除兼容端点及对应客户端结构。

在第 1、2 步完成前不能先删除旧 unpin 通知，否则当前模型没有足够强的权威快照声明来覆盖历史内容。

### 14. 配置与可观测性

至少记录以下指标或结构化日志：

| 指标 | 用途 |
|------|------|
| `context.estimated_tokens_before` | 触发前估算量 |
| `context.estimated_tokens_after` | 压缩后估算量 |
| `context.compacted_message_count` | 本次压缩消息数 |
| `context.summary_tokens` | 摘要大小 |
| `context.compaction_duration_ms` | 摘要耗时 |
| `context.compaction_failed` | 压缩失败计数 |
| `context.revision_conflict` | Memory CAS 冲突次数 |
| `context.turn_message_count` | 本 Run 提交的完整轨迹消息数 |
| `pin.snapshot_count` | 本轮 Active Pin 数量 |
| `pin.snapshot_fetch_failed` | Pin 权威状态不可用次数 |
| `pin.snapshot_tokens` | 硬 Pin 占用 Token |
| `pin.snapshot_reused` | 已有 Run 重连复用快照次数 |

日志不能输出完整 Pin、用户消息、Tool output 或摘要正文，只记录计数、长度、task/session/run 标识和错误类型。

### 15. 代码改动范围

AgentEnd：

| 文件 | 目标改动 |
|------|----------|
| `src/orchestrator/memory/conversation_memory.py` | V2 envelope、V1 兼容读取、所有写入的 revision CAS、原子提交与冲突类型 |
| `src/orchestrator/memory/context_compactor.py` | 新增 Token 预算、轮次选择、摘要生成与结果校验 |
| `src/orchestrator/planning/context_builder.py` | 新增 Prompt 分层纯函数和 capability hints reference 渲染 |
| `src/orchestrator/planning/graph.py` | 加载 V2、压缩触发、完整 `turn_messages`、review/replan 事实、工具过滤和增量 CAS 保存 |
| `src/adapters/orchestrator.py` | 传入结构化 RuleResult；移除重复群聊查询；分离已提交历史和当前 Run transcript |
| `src/clients/backend_client.py` | Pin 查询改为成功或抛错，不再失败返回 `[]` |
| `src/rules/engine.py` | 输出结构化上下文通道 |
| `src/rules/builtin.py` | PinRule/GroupChatRule 输出到各自通道 |
| `src/api/v1/agent.py` | 新 Run 构造完整 ActivePinSnapshot 并 fail-closed；已有 Run 重连跳过 Pin 查询；稳定请求指纹排除动态快照 |
| `src/execution/models.py`、`src/execution/repository.py` | 首次接纳时持久化 Run 级 Snapshot runtime，并为已有 Run 提供复用途径 |
| `src/api/v1/pin.py` | 兼容期后移除取消事件历史写入和公告取消通知端点 |
| `src/app/config.py`、`config.example.yaml` | 新增上下文窗口、触发/目标阈值、摘要上限、损坏策略、保留轮次和 Pin 预算配置 |

Backend：

| 文件 | 目标改动 |
|------|----------|
| `internal/service/impl/announcement_service.go` | 分阶段移除 fire-and-forget `notifyUnpin()` |
| `pkg/agentend_client/client.go` | 分阶段移除 `AnnouncementUnpinRequest` 和通知方法 |

本方案不要求修改三端公共 contracts。Active Pin Snapshot 是 AgentEnd 内部领域模型；Backend 内部公告接口现有 JSON 已包含公告 ID、发送人、内容和创建时间。若未来将 Snapshot 暴露给 Frontend 或其他服务，再按契约优先原则新增 schema。

### 16. 测试

#### 16.1 单元测试

- V1 list 文件可以读取，并在下一次成功保存后迁移为 V2。
- V2 summary 和 recent messages 可无损往返，AI tool calls 与 ToolMessage 关联不丢失。
- 一次 Reason 包含多轮工具调用时，所有 AIMessage/ToolMessage 都进入 `turn_messages`，每个 `tool_call_id` 均完整配对。
- review/replan 事实只追加一次，重新进入 Reason 不重复原始用户消息。
- 普通保存发生 revision 冲突时重新加载并合并本轮增量，不丢失任一提交者的消息。
- 压缩提交发生 revision 冲突时丢弃过期摘要，不覆盖较新版本。
- V1 → V2 迁移发生 revision 冲突时不执行无条件覆盖。
- Token 未达到阈值时不调用摘要器。
- 达到阈值时只压缩旧完整轮次，最近轮次保持原样。
- 摘要器失败、超时、返回空内容或超过 summary 上限时不覆盖原文件。
- Prompt 顺序严格为 Core System → System Constraints → Active Pin Snapshot → History/Reference/Capability → Recent → Current。
- 历史摘要、Evolution、群聊和 capability hints 使用 Human/reference role。
- `allowed_tools=None`、空列表和非空 allowlist 分别产生约定的工具集合，未知工具不会被绑定。
- Active Pin 不出现在摘要器输入中。
- 成功空快照注入 `Active Pins: none`。
- Pin 查询失败时不调用 Reason LLM。
- 已存在 Run 的重连不重新查询 Pin，并复用首次持久化的 Snapshot。
- `fetched_at` 变化不会改变稳定请求指纹或导致同一 `run_id` 返回 409。
- 历史中出现、当前快照缺席的 Pin 不被渲染为有效约束。
- Active Pin 超过预算时显式失败，不截断正文。
- 旧 unpin SystemMessage 在迁移压缩后不再以 System role 保留。

#### 16.2 集成测试

```text
场景 A：压缩后保持当前 Pin
创建 Pin A → 产生长对话 → 触发压缩 → 下一轮 Prompt 仍包含 Pin A 原文

场景 B：取消后不复活
创建 Pin A → 模型历史提及 A → 删除 A → 下一轮完整快照为空
→ System 声明历史 Pin 无效 → 摘要中不得把 A 表述为当前约束

场景 C：Pin 查询失败
Backend 不可达 → AgentEnd 返回可观察错误 → Reason LLM 调用次数为 0

场景 D：空 Pin 快照
Backend 正常返回 [] → Orchestrator 正常运行，并注入明确的空快照声明

场景 E：群聊去重
请求携带 group_chat_messages → 最终 Prompt 只出现一次群聊窗口，且 role 为 Human/reference

场景 F：压缩事务失败
摘要生成成功后 revision 被改变 → 不覆盖较新的 ConversationMemory

场景 G：普通保存并发冲突
两个 session 读取 revision=N → 分别提交不同 turn_messages
→ 后提交者重新加载并合并 → 最终历史同时包含两轮消息

场景 H：Run 重连复用 Pin Snapshot
首次请求取得 Pin A 并创建 Run → Backend Pin 改为 B 或暂时不可达
→ 使用同一 run_id 重连 → 不查询 Pin、不返回 409、继续读取原 Run journal

场景 I：新 Run 获取最新 Pin
Run 1 使用 Pin A → Pin 更新为 B → 使用新 run_id 发起 Run 2
→ Run 2 Snapshot 只包含 B

场景 J：完整工具轨迹
Reason 依次调用发现、询问和规划工具 → 保存并重新加载 Memory
→ 全部 AI/Tool 消息顺序和 tool_call_id 保持不变
```

### 17. 实施顺序与完成标准

推荐拆成五个可独立验收的阶段：

1. **Prompt 权限修正**：RuleResult 分层、capability/allowed_tools 消费、群聊去重、Active Pin Snapshot、查询 fail-closed。
2. **Run 快照幂等**：稳定请求指纹、首次 Snapshot runtime 持久化、已有 Run 重连复用。
3. **Memory V2 与完整轨迹**：版本化 envelope、V1 兼容、全路径 revision CAS、完整 Reason transcript、review/replan 事实。
4. **Context Compactor**：Token 预算、摘要器、最近完整轮次保留、可观测性。
5. **旧链路清理**：停止持久化 unpin SystemMessage，移除异步通知和兼容端点。

完成标准：

- 任何一次 Reason 调用都能从消息列表中明确区分固定 System、当前硬约束、参考上下文、历史摘要和当前输入。
- 一次 Reason 内所有 AI tool call 与 ToolMessage 都会按顺序保存，review/replan 事实可进入后续摘要且不会重复当前用户消息。
- 压缩前后当前 Active Pin 内容逐字一致。
- 同一 Run 的重连复用首次 Snapshot；动态 `fetched_at` 或后续 Pin 变化不会破坏 Run 幂等。
- 删除 Pin 后无需取消历史消息即可在下一次成功快照中失效。
- Backend Pin 状态未知时不执行规划。
- 长对话不再因为固定 10 轮裁剪而无摘要丢失早期决定。
- 普通保存、迁移和压缩发生并发冲突或失败时均不会丢失较新消息，也不会损坏现有 `conversation_memory.json`。
- capability hints 保持 reference 权限，`allowed_tools` 实际约束绑定给 Orchestrator LLM 的工具集合。
