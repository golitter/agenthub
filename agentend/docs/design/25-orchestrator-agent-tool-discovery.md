# Orchestrator 子 Agent 工具化按需发现实施规划

## 文档状态

- **状态**：已实施（人工端到端验收待执行）
- **范围**：AgentEnd Orchestrator 规划链路
- **目标版本**：未定
- **不涉及**：Backend 与 AgentEnd 之间的请求契约变更、子 Agent 执行协议变更、前端交互改造

## 实现了什么

将 Orchestrator Reason 阶段的子 Agent 列表从系统提示词静态注入改为 `list_available_agents()` 工具按需发现：`state["agents"]` 仍是服务端权威快照，但模型只有在需要咨询（`ask_agent`）或生成非空分派计划（`plan_and_dispatch`）时才调用发现工具，且必须在前一工具轮次完成发现。工具只投影 `id`/`name`，`_build_agents_desc()` 与 `{agents_desc}` 提示词插槽被删除，Dispatcher 取消非法 id 的静默改派，计划进入 Human Review 前增加服务端 id 校验并回灌模型自纠错。

## 怎么实现的

### 背景与既有问题

当前 Backend 在每轮 Orchestrator 请求的 `config.agents` 中携带群聊子 Agent 列表。AgentEnd 将该列表保存到 LangGraph 的 `state["agents"]`，然后由 `_build_agents_desc()` 渲染为 Markdown，并注入 Reason 系统提示词的 `## 可用 Agents` 区块。

现有链路如下：

```text
Backend config.agents
    → OrchestratorAdapter.stream_chat(..., agents=...)
    → LangGraph state["agents"]
    → skill_prepare_node
    → _build_agents_desc(state["agents"])
    → build_reason_prompt(agents_desc=...)
    → SystemMessage("## 可用 Agents ...")
```

这种方式能保证模型每轮都看到 Agent 列表，但也存在以下问题：

1. Agent 列表被无条件注入，即使本轮只是闲聊或直接问答。
2. 动态成员列表进入静态系统提示词，会降低提示词前缀的稳定性和缓存复用率。
3. Agent 发现和 Agent 使用没有明确的工具调用边界，难以通过运行时状态强制“先发现、后使用”。
4. `ask_agent` 会校验 Agent id，但 `plan_and_dispatch` 生成的任务缺少等价的前置校验。
5. `Dispatcher` 当前会将非法 `session_id` 静默改派给第一个 Agent，可能掩盖模型规划错误，甚至把任务交给非预期执行者。

## 目标

将子 Agent 列表改为通过 `list_available_agents()` 工具按需暴露，同时保持 `state["agents"]` 为服务端权威数据源。

完成后应满足：

1. 简单问答不需要加载 Agent 列表。
2. Orchestrator 在调用 `ask_agent` 或生成含任务的 `plan_and_dispatch` 前，必须先单独调用 `list_available_agents()`。
3. 模型只能使用工具返回的 `id` 作为 `ask_agent.agent` 或 `tasks[].session_id`。
4. Agent 的 `name` 只用于理解与选择，不能充当调度句柄；Agent 类型仅保留在服务端内部用于过滤。
5. Skill 名、Agent 类型、展示名称和 Orchestrator 自身都不能作为目标 Agent id。
6. 模型输出之外仍有服务端校验，提示词不是唯一安全边界。
7. 模型规划失败时，服务端兜底仍可直接读取 `state["agents"]`。

## 非目标

本阶段不实现以下能力：

- 不新增 Backend 查询群成员的 HTTP API。
- 不在一次 Reason 执行期间实时刷新群成员。
- 不修改 Backend 下发 `config.agents` 的现有结构。
- 不向模型暴露子 Agent 的真实会话 id、工作区路径或其他内部配置。
- 不将 Skills 与 Agents 合并为同一种发现工具。

`list_available_agents()` 返回的是本轮请求携带的 Agent 快照。如果后续要求运行期间实时感知进群、退群或成员配置变化，应另行设计 Backend 查询接口和异步工具执行路径。

### 目标架构

```text
Backend config.agents
    → LangGraph state["agents"]                 服务端权威快照
    → build_tools(..., agents=state["agents"])
    → list_available_agents()
    → ToolMessage({count, agents: [{id, name}]})
    → 模型选择返回结果中的 id
    → ask_agent / plan_and_dispatch
    → Graph 层再次校验 id
    → Dispatcher 最终防御性校验
```

模型是否需要 Agent 的判断仍发生在 Reason 阶段；只有进入咨询或分派路径时才触发 Agent 发现工具。

## 工具契约

### 工具名称

```text
list_available_agents()
```

工具不接收参数，避免模型通过筛选条件绕过完整列表发现，也避免把 Backend 查询语义误引入本地快照工具。

### 返回格式

返回结构化 JSON 字符串：

```json
{
  "count": 2,
  "agents": [
    {
      "id": "executor",
      "name": "执行者"
    },
    {
      "id": "reviewer",
      "name": "审查员"
    }
  ]
}
```

字段语义：

| 字段 | 用途 | 是否可作为句柄 |
|------|------|----------------|
| `id` | 群成员唯一标识，用于咨询和分派 | 是 |
| `name` | 展示名称，帮助模型理解角色 | 否 |

工具必须过滤：

- 缺失或空白 `id` 的条目；
- 服务端内部类型为 `orchestrator` 的条目。

工具不得返回：

- Backend 真实 `session_id`；
- `workspace_path`；
- CLI 配置路径或进程环境；
- 未明确允许的扩展配置字段。

当没有可用子 Agent 时返回：

```json
{"count": 0, "agents": []}
```

空列表不是工具错误，Reason 节点应根据用户请求给出“当前没有可分派 Agent”的明确回复。

## Reason 调用协议

### 基本规则

- 直接文本回答：无需调用 `list_available_agents()`。
- 调用 `ask_agent`：必须先在前一个工具轮次成功调用 `list_available_agents()`。
- 生成非空任务计划：必须先在前一个工具轮次成功调用 `list_available_agents()`。
- 仅执行 `tasks=[]` 的合入 main 请求：不依赖 Agent，可不调用发现工具。
- 重规划进入新的 `reason_node` 执行时，应重新发现 Agent，避免沿用模型记忆中的旧列表。

### 禁止同轮发现并使用

模型可能在同一个 AIMessage 中同时发出：

```text
list_available_agents()
plan_and_dispatch(...)
```

此时模型在生成计划时尚未看到发现结果，因此不能接受该计划。Reason 节点必须：

1. 执行 `list_available_agents()`；
2. 对同批次中的 `ask_agent` 或非空 `plan_and_dispatch` 返回工具错误；
3. 将所有 ToolMessage 回灌模型；
4. 进入下一轮推理，让模型基于发现结果重新咨询或规划。

建议错误文本：

```text
Error: list_available_agents must complete in a previous tool round before using ask_agent or plan_and_dispatch.
```

这项约束必须由代码实现，不能只依赖系统提示词。

### Reason 局部状态

在单次 `reason_node` 中维护局部变量：

```python
agents_discovered = False
```

只有成功执行发现工具并将结果加入消息链后，后续 LLM 轮次才可将其设为可用。不要把该状态写入持久化会话记忆，也不要跨 Reason 调用复用。

## 实施改动

### 1. `planning/tools.py`

修改 `build_tools()`：

- 新增 `agents: list[dict] | None = None` 参数；
- 在闭包中保存本轮 Agent 快照；
- 新增 `list_available_agents()`；
- 对返回字段做显式白名单投影；
- 将工具加入 `build_tools()` 返回列表。

工具使用传入的不可变快照语义，不读取全局变量，也不主动访问 Backend。

### 2. `planning/graph.py`

删除提示词渲染路径：

- 删除 `_build_agents_desc()`；
- `skill_prepare_node()` 不再计算 `agents_desc`；
- 调用 `build_reason_prompt()` 时不再传入该参数。

修改工具构建：

- `reason_node()` 调用 `build_tools()` 时传入 `state.get("agents", [])`。

修改工具循环：

- 增加 `agents_discovered` 局部状态；
- 识别 `list_available_agents` 工具调用；
- 拒绝同轮发现并咨询/分派；
- 未发现时拒绝 `ask_agent` 和非空任务计划；
- 保证每个 tool call 都产生对应的 ToolMessage，保持 LangChain/OpenAI 工具消息协议完整。

修改计划接收逻辑：

- 生成 `PlanOutput` 前，校验所有 `tasks[].session_id`；
- 合法集合只包含非 Orchestrator 且 id 非空的 Agent；
- 存在非法 id 时，不进入 Human Review，而是把错误作为 `plan_and_dispatch` 的 ToolMessage 回灌模型并继续推理；
- 错误信息包含非法 id 和合法 id 列表，帮助模型自我修正。

### 3. `planning/prompts.py`

删除：

- `REASON_PROMPT` 中动态的 `## 可用 Agents` 和 `{agents_desc}`；
- `build_reason_prompt(agents_desc=...)` 参数；
- 依赖 Markdown 加粗列表的表述和固定 Agent id 正确示例。

新增静态规则：

- Agent 列表通过 `list_available_agents()` 获取；
- 咨询或分派前必须先单独调用发现工具；
- 只有返回对象的 `id` 是合法句柄；
- `name`、Agent 类型、Skill 名和 Orchestrator 自身均不是合法句柄；
- 工具返回空列表时不得虚构 Agent；
- `tools_section` 增加工具签名和用途。

提示词仍保留 Agents 与 Skills 的概念区分，但不再包含本轮动态成员数据。

### 4. `execution/dispatcher.py`

取消非法 Agent id 的静默改派：

```python
if task.session_id not in valid_ids:
    raise ValueError(f"Unknown agent id: {task.session_id}")
```

Graph 层负责向模型提供可恢复的校验反馈；Dispatcher 是最终防御边界，遇到非法计划应明确失败，而不是改变计划语义。

如现有调用方不适合直接处理 `ValueError`，可引入领域异常 `InvalidDispatchAgentError`，由 `dispatch_node()` 捕获并转换为结构化错误状态。实施时应优先确认 LangGraph 节点异常的现有统一处理方式。

### 5. fallback 兜底

`_fallback_plan_from_text()` 是模型未正确调用工具时的服务端兜底，可继续直接读取 `state["agents"]`，不要求经过发现工具。

需要修正空 Agent 行为：

- `_default_dispatch_agent_id()` 返回 `str | None`；
- 优先选择 id 非空且类型不是 Orchestrator 的条目；
- 没有可分派 Agent 时不得返回虚构的 `"agent"`；
- Reason 节点应返回明确错误文本，而不是生成不可执行计划。

## 计划校验规则

建议提取统一辅助函数，供计划接收、fallback 和 Dispatcher 测试复用语义：

```python
def _dispatchable_agent_ids(agents: list[dict]) -> set[str]:
    return {
        str(agent.get("id", "")).strip()
        for agent in agents
        if str(agent.get("id", "")).strip()
        and str(agent.get("type", "")).strip() != "orchestrator"
    }
```

校验要求：

1. 每个非空任务的 `session_id` 必须精确匹配合法 id；
2. 不自动执行大小写转换、别名解析或类型名映射；
3. `tasks=[]` 合法，用于无需子 Agent 的合入 main 请求；
4. 同一 Agent 可以承接多个任务；
5. 发现结果为空时，任何非空任务计划均无效。

## 错误处理

| 场景 | 行为 |
|------|------|
| Agent 列表为空 | 工具正常返回空数组；需要分派时输出无可用 Agent 提示 |
| 未发现就 `ask_agent` | 返回 ToolMessage 错误，继续 Reason 循环 |
| 未发现就提交非空计划 | 返回 ToolMessage 错误，继续 Reason 循环 |
| 同轮发现并使用 | 执行发现，拒绝同轮使用，下一轮重试 |
| 使用 Agent 类型或 Skill 名 | 返回非法 id 错误和合法 id 列表 |
| Agent 缺少真实 `session_id` | 延续 `_handle_ask_agent_call()` 的明确错误；分派执行前也应失败 |
| Reason 达到最大轮数 | 沿用超时处理；只有存在可分派 Agent 时才允许 fallback |
| 非法计划到达 Dispatcher | 明确抛错，不静默改派 |

## 安全与隐私

- Agent 列表继续由 Backend 请求决定，工具无权扩大可见范围。
- 工具只返回公开的 `id` 和 `name`，内部 Agent 类型、真实 session 和文件路径不进入 LLM 上下文。
- `ask_agent` 和计划接收必须继续基于 `state["agents"]` 做服务端校验，不能信任模型记忆或工具参数。
- 不使用名称模糊匹配，避免同名 Agent 或提示注入导致错误路由。
- 工具返回使用 JSON 序列化，不拼接可执行提示词片段。

## 测试计划

### 工具单元测试

- 返回值仅包含 `count`、`id`、`name`；
- 过滤 Orchestrator 条目；
- 过滤空 id；
- 缺失 name 时回退到 id；
- 空输入返回空数组；
- 不泄露 `session_id`、`workspace_path` 等内部字段。

### Reason 节点测试

- 简单问答不强制调用发现工具；
- 未发现时调用 `ask_agent` 被拒绝；
- 未发现时提交非空计划被拒绝；
- 同一轮同时发现和规划时，计划被拒绝；
- 下一轮基于发现结果使用合法 id 时成功；
- 类型名、展示名、Skill 名和 Orchestrator id 被拒绝；
- `tasks=[]` 的合入 main 计划无需发现即可通过；
- 重规划时要求重新发现；
- 每个 tool call 均有对应 ToolMessage；
- 发现工具结果不会被错误地当作持久化权威状态。

### Dispatcher 与 fallback 测试

- 合法 id 正常转换为 `DispatchResult`；
- 非法 id 明确失败且不改派；
- fallback 选择第一个非 Orchestrator Agent；
- 空列表时 fallback 不生成虚构 Agent；
- Agent 缺少真实 `session_id` 时执行路径明确失败。

### 回归测试

- `ask_agent` 的 SSE 卡片事件保持不变；
- Human Review 流程保持不变；
- 多轮咨询后仍能生成计划；
- Skill L1/L2/L3 渐进式加载不受影响；
- Pin、Evolution、群聊上下文和 Conversation Memory 注入顺序不变；
- 现有 Orchestrator 规划、执行、重规划和合入 main 测试通过。

## 实施顺序

### 阶段 1：工具与纯函数

- [x] 为 `build_tools()` 增加 Agent 快照参数；
- [x] 实现 `list_available_agents()`；
- [x] 提取合法 Agent id 计算函数；
- [x] 添加工具返回和过滤测试。

### 阶段 2：Reason 协议

- [x] 在 `reason_node()` 传入 Agent 快照；
- [x] 增加“前一工具轮次已发现”状态；
- [x] 拒绝同轮发现并使用；
- [x] 增加计划 Agent id 校验与模型自纠错；
- [x] 添加工具轮次和计划校验测试。

### 阶段 3：提示词迁移

- [x] 删除 `_build_agents_desc()`；
- [x] 删除 `{agents_desc}` 模板参数；
- [x] 加入工具发现规则；
- [x] 更新工具清单和 Agents/Skills 区分说明；
- [x] 检查系统提示词中不再出现动态 Agent 列表。

### 阶段 4：最终防御与兜底

- [x] Dispatcher 取消静默改派；
- [x] 修正空 Agent fallback；
- [x] 明确缺失真实 session 的分派错误；
- [x] 添加 Dispatcher、fallback 和空列表测试。

### 阶段 5：文档与验收

- [x] 更新 `11-orchestrator-planning.md` 的工具清单和 Reason 数据流；
- [x] 更新 `16-system-prompt-tuning.md`，记录 Agent 列表从静态注入迁移为按需工具；
- [x] 更新 `reference/orchestrator-architecture.md`；
- [x] 运行 AgentEnd 单元测试和语法检查；
- [ ] 使用至少两个不同类型的子 Agent 完成一次人工端到端规划验收。

## 验收标准

满足以下条件后可视为完成：

1. Reason 系统提示词中不再包含本轮动态 Agent 列表。
2. `list_available_agents()` 只暴露允许字段并排除 Orchestrator。
3. 未完成上一轮发现时，`ask_agent` 和非空任务计划无法通过。
4. 同一轮“发现 + 使用”无法绕过协议。
5. 非法 Agent id 在 Human Review 前被拒绝并允许模型自行修正。
6. Dispatcher 不再静默改变目标 Agent。
7. 空 Agent 列表不会生成虚构任务或虚构 Agent。
8. 原有咨询、规划审查、执行、重规划、记忆和 Skill 加载链路回归通过。

## 风险与权衡

### 额外模型往返

需要咨询或分派时会增加至少一次工具调用往返，带来少量延迟。换来的收益是动态列表按需加载、静态提示词更稳定，以及可观察、可校验的 Agent 发现边界。

### Reason 最大轮数

发现工具会占用一次 Reason 迭代。实施前需要确认 `reason_max_iterations` 能覆盖典型链路：

```text
发现 Agent → 可选 ask_agent → 生成计划
```

若当前上限过低，应基于测试结果调整，而不是取消“上一轮发现”约束。

### 对历史记忆的信任

Conversation Memory 可能包含旧的 Agent 工具结果。实现必须只使用本次 `reason_node` 的 `agents_discovered` 状态作为许可条件，不能因历史 ToolMessage 中出现过列表就跳过本轮发现。

### 缓存收益范围

当群成员很少时，节省的 token 有限；主要价值是协议清晰度、运行时校验和动态系统提示词剥离，不应只以 token 数衡量。

## 后续可选增强

本阶段完成后，可独立评估：

- 为 Agent 增加经过契约定义的 L1 能力摘要；
- 支持按能力过滤，但仍返回精确 id；
- 通过 Backend API 实现运行期间实时成员刷新；
- 为发现、非法 id、自纠错次数增加 Langfuse 指标；
- 把通用工具执行与 ToolMessage 包装逻辑提取为统一执行器，减少 Reason 分支重复代码。
