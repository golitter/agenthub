# Orchestrator 架构设计

## 概述

Orchestrator 是一个基于 LangGraph 的多 Agent 编排器。它接收用户需求，决定是直接回答还是协调多个 Agent 完成任务，通过 LLM tool-calling 循环实现规划和分发。

## 核心架构

```
用户消息
  │
  ▼
┌──────────────────────────────────────────────────────────┐
│                    LangGraph State Machine                │
│                                                          │
│  skill_prepare → reason → (human_review) → dispatch →   │
│  execute → review → evolve → save_mem                    │
│                                                          │
│  GraphState: message, plan, dispatch_results,            │
│             execution_waves, task_results, ...            │
└──────────────────────────────────────────────────────────┘
  │
  ▼
子 Agent 执行（并行/串行）
```

### 关键文件

| 文件 | 职责 |
|------|------|
| `src/orchestrator/planning/graph.py` | LangGraph 定义：节点、边、条件路由 |
| `src/orchestrator/planning/prompts.py` | 系统提示词模板 + 动态构建 |
| `src/orchestrator/planning/tools.py` | LLM 工具定义（read_file, plan_and_dispatch 等） |
| `src/orchestrator/execution/dispatcher.py` | 计划 → DispatchResult 转换 + 拓扑排序 |
| `src/orchestrator/execution/engine.py` | 子 Agent 并发执行引擎 |
| `src/rules/builtin.py` | PinRule：从 Backend 读取 pinned announcements 注入约束 |
| `src/orchestrator/memory/evolution.py` | 编排经验学习 |
| `src/orchestrator/memory/conversation_memory.py` | 对话记忆持久化（ConversationMemoryStore） |
| `src/adapters/orchestrator.py` | OrchestratorAdapter：graph 生命周期管理 |

---

## Graph 节点详解

### 1. skill_prepare — 技能发现与提示词构建

```
输入: user message, agents, shared_dir, task_base_path
输出: system_prompt, pin_context, evolution_context (写入 state)
```

**L1 技能发现**：扫描 `shared/.orchestrator/skills/` 目录下的 SKILL.md frontmatter，提取 name + description 元数据，写入系统提示词。L2（SKILL.md 正文）和 L3（资源文件）由 LLM 在 reason 阶段通过 `load_skill_detail` 工具按需加载。

**系统提示词构建**（`build_reason_prompt`）：仅包含身份 + 规则 + 工具，不再包含动态上下文。

**动态上下文计算**：Pin 约束由 `PinRule`（读取 Backend 的 pinned announcements）通过 `system_prompt_append` 传入 `state["pin_context"]`；编排经验在 `skill_prepare_node` 中计算，存入 `state["evolution_context"]`。两者由 `reason_node` 以消息列表方式注入。

```
┌─────────────────────────────────────┐
│  系统提示词 (SystemMessage)          │
│  基础身份 + 规则 + 工具 + 技能       │
│  {agents_desc}    可用 Agent 列表    │
│  {soul_section}   SOUL.md 身份定义   │
│  {skills_section} L1 技能元数据      │
│  {tools_section}  工具说明           │
│  {workspace_section} 工作区目录说明  │
│  （无绝对路径，通过 workspace_type  │
│   参数区分 shared / taskbase）      │
│  规则（判断逻辑、Agent/Skill 区别等）│
├─────────────────────────────────────┤
│  动态上下文消息（每轮重建，不持久化）│
│  SystemMessage: Pin 约束             │
│  SystemMessage: 编排经验             │
│  HumanMessage: 重规划反馈（仅重规划）│
│  HumanMessage: 群聊上下文            │
├─────────────────────────────────────┤
│  memory_messages（持久化历史）       │
│  仅含对话链：Human/AI/Tool 消息      │
├─────────────────────────────────────┤
│  HumanMessage: 当前用户消息          │
│  HumanMessage: 审查反馈（仅审查时）  │
└─────────────────────────────────────┘
```

### 2. reason — LLM 工具调用循环（核心节点）

```
输入: system_prompt, pin_context, evolution_context, orchestrator_context, replan_reason, memory_messages, message
输出: output_type="text"|"plan", text|plan, memory_messages
```

**流程**：

```
构建 messages =
  [SystemMessage(system_prompt)]           ← 身份 + 规则 + 工具
  + [SystemMessage(pin_context)]           ← Pin 约束（全局）
  + [SystemMessage(evolution_context)]     ← 编排经验（全局）
  + [HumanMessage(replan_reason)]          ← 重规划反馈（仅重规划时）
  + [HumanMessage(orchestrator_context)]   ← 群聊上下文
  + memory_messages                        ← 持久化的历史（含之前轮次上下文+对话）
  + [HumanMessage(message)]               ← 当前用户消息
  + [HumanMessage(review_message)]        ← 审查反馈（仅审查时）
     │
     ▼
┌─ 循环（最多 10 次）──────────────────────┐
│                                          │
│  llm_with_tools.ainvoke(messages)        │
│       │                                  │
│       ├── 无 tool_calls?                 │
│       │   └── 返回 text 输出             │
│       │                                  │
│       ├── 有 ask_agent?                  │
│       │   └── 执行 ask_agent（同步等待）  │
│       │       append ToolMessage         │
│       │       continue 循环              │
│       │                                  │
│       ├── 有 plan_and_dispatch?          │
│       │   └── 构造 PlanOutput            │
│       │       返回 plan 输出             │
│       │                                  │
│       └── 其他 tool_calls?               │
│           └── 执行工具（read_file 等）    │
│               append ToolMessage         │
│               continue 循环              │
│                                          │
└──────────────────────────────────────────┘
```

**LangSmith trace 接入点**：通过 `get_config()` 获取 LangGraph runnable config，传入 `ainvoke(messages, config=llm_config)`，使每次 LLM 调用自动上报 LangSmith。

### 3. human_review — 人工审查（可选）

```
输入: plan
输出: review_decision ("approve"|"discuss"|"reject"), review_message
```

- 阻塞等待外部审查结果（通过 `asyncio.Event`）
- 超时自动批准（默认 120s）
- 审查结果决定路由：
  - `approve` → dispatch
  - `discuss`/`reject` → 回到 reason（带反馈）

### 4. dispatch — 计划分发

```
输入: plan
输出: dispatch_results, execution_waves
```

- `Dispatcher` 将 `PlanOutput.tasks` 转换为 `DispatchResult` 列表
- 每个 task 映射到对应 agent（通过 session_id）
- `topological_sort` 按 `depends_on` 关系排序为执行波次：
  - 同一波次内的任务可并行执行
  - 波次之间串行执行
- 将计划写入 `shared/.agent/plans/` 目录

### 5. execute — 子 Agent 执行

```
输入: dispatch_results, execution_waves
输出: task_results
```

> 注意：graph 中的 `execute` 节点是占位符（`_execute_placeholder`），实际执行由 `OrchestratorAdapter.stream_chat` 中的 `_handle_execute` 完成。

**执行引擎**（`ExecutionEngine`）：
- 按波次依次执行
- 同波次内并发（`asyncio.create_task`）
- 每个子任务通过 `BackendClient` 调用对应 Agent 的 API
- 支持超时控制（默认 300s/任务）
- 自动为每个子 Agent 创建独立的 git worktree

### 6. review — 执行结果审查

```
输入: task_results, iteration, max_iterations
输出: needs_replan, replan_reason
```

- 分析每个子任务的成功/失败
- 如果有失败且未超过最大迭代次数 → 设置 `needs_replan=true`
- 构造重规划原因描述
- 路由：`needs_replan` → 回到 `skill_prepare`；否则 → `evolve`

### 7. evolve — 经验学习

```
输入: message, plan, task_results
输出: (无 state 变更，写文件)
```

- 将本轮编排经验记录到 `EvolutionStore`（`evolution.yaml`）
- 记录：用户需求摘要、规划摘要、执行结果、agent 表现
- 保留最近 20 条经验
- 下一轮 `skill_prepare` 时加载到提示词中

### 8. save_mem — 记忆保存

```
输入: memory_messages, shared_dir
输出: (无 state 变更，写文件)
```

- 将 `memory_messages`（仅对话链：Human/AI/Tool 消息）持久化到 `ConversationMemoryStore`
- 存储路径：`{shared_dir}/memory/conversation_memory.json`
- 使用 LangChain `messages_to_dict` 序列化
- 保留最近 10 轮（按 HumanMessage 起点计算轮次）
- 下一轮 `stream_chat` 时通过 `ConversationMemoryStore.load_messages()` 恢复

---

## 条件路由

```
reason ──→ route_by_output_type ──┬── "text"  → save_mem → END
                                  ├── "plan"  → human_review
                                  └── "error" → END

human_review ──→ route_by_review_decision ──┬── "approve" → dispatch
                                             └── "discuss"/"reject" → reason

execute ──→ review ──→ route_by_review ──┬── needs_replan → skill_prepare
                                          └── done → evolve → save_mem → END
```

---

## GraphState 字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `message` | `str` | 用户输入 |
| `agents` | `list[dict]` | 可用 Agent 列表 |
| `task_id` | `str` | 当前任务 ID |
| `shared_dir` | `str` | 共享目录路径 |
| `allowed_read_dirs` | `list[str]` | 允许读取的目录 |
| `output_type` | `str` | 输出类型："text" / "plan" / "error" |
| `text` | `str` | 文本响应 |
| `plan` | `PlanOutput \| None` | 生成的计划 |
| `dispatch_results` | `list[DispatchResult]` | 分发结果 |
| `execution_waves` | `list[list[DispatchResult]]` | 执行波次 |
| `task_results` | `Annotated[list, _add]` | 任务执行结果（累加） |
| `iteration` | `Annotated[int, _add_one]` | 当前迭代次数 |
| `max_iterations` | `int` | 最大重规划次数（默认 3） |
| `memory_messages` | `Annotated[list, _add]` | 对话历史（累加，持久化到 conversation_memory.json） |
| `system_prompt` | `str` | skill_prepare 构建的系统提示词（仅身份+规则+工具） |
| `pin_context` | `str` | Pin 约束文本（PinRule → system_prompt_append → adapter 传入） |
| `evolution_context` | `str` | 编排经验文本（skill_prepare 计算） |
| `orchestrator_context` | `str` | 跨轮群聊上下文 |
| `review_decision` | `str` | 审查决定 |
| `review_message` | `str` | 审查反馈 |
| `needs_replan` | `bool` | 是否需要重规划 |
| `replan_reason` | `str` | 重规划原因 |

---

## LLM 工具清单

| 工具名 | 用途 | 参数 |
|--------|------|------|
| `read_file` | 读取文件（带行号） | `path`, `start_line`, `line_count`, `workspace_type` |
| `write_file` | 写入文件到共享目录 | `path`, `content` |
| `list_dir` | 列出目录内容 | `path`, `workspace_type` |
| `run_skill` | 执行已注册的 skill 命令 | `skill`, `command`, `args` |
| `load_resource` | 加载 skill 的参考资源 | `skill_name`, `resource_path` |
| `load_skill_detail` | 按需加载 skill L2（正文）/ L3（资源文件） | `skill_name`, `level`, `resource_path` |
| `ask_agent` | 向指定 Agent 提问 | `agent`, `question` |
| `plan_and_dispatch` | 编排多 Agent 任务 | `overview`, `tasks`, `merge_to_main` |
| `current_time` | 获取当前时间 | (无) |

---

## 记忆系统

### Conversation Memory（对话记忆）

- 持久化对话链（HumanMessage + AIMessage + ToolMessage）跨轮历史
- 存储：`{shared_dir}/memory/conversation_memory.json`
- 保留最近 10 轮（按 HumanMessage 起点裁剪）
- `save_mem_node` 写入，`stream_chat` 加载
- 动态上下文（Pin、Evolution、replan、群聊上下文）不持久化，每轮重新构建注入

### Pin Memory（硬约束）

- 置顶的 Announcement（公告）即为 Pin 约束，所有 Agent 必须遵守
- 持久化：Backend MySQL `announcements` 表（`pinned=true`）
- 获取方式：`agent_execute` / `agent_stream` 预获取 `backend_client.get_pinned_announcements(task_id)`
- 注入方式：`PinRule`（priority=9）→ `system_prompt_append` → OrchestratorAdapter 写入 `state["pin_context"]` → `reason_node` 以 `SystemMessage` 注入消息列表
- 对非 Orchestrator Agent：`system_prompt_append` 通过 CLI 参数传递

### Evolution Store（经验学习）

- 每轮编排完成后记录经验（成功/失败、agent 表现）
- 存储：`shared/evolution.yaml`
- 保留最近 20 条
- 注入方式：`skill_prepare_node` 计算后存入 `state["evolution_context"]`，`reason_node` 以 `SystemMessage` 注入消息列表

---

## 分支模型

```
main                              ← 仓库主分支
  └── task/{task_id}              ← 任务基础分支（所有 agent 共享）
        ├── agent/{session_id_1}/{task_id}  ← Agent A 的独立分支
        └── agent/{session_id_2}/{task_id}  ← Agent B 的独立分支
```

- Orchestrator 只读 `task/{task_id}` 的 task-base worktree（用于了解代码结构）
- 子 Agent 各自创建独立的 `agent/{sid}/{tid}` 分支 worktree
- 子 Agent 完成后 merge 回 `task/{task_id}`
- Orchestrator 决定是否将 `task/{task_id}` 合入 `main`

---

## Adapter 层（生命周期管理）

`OrchestratorAdapter` 负责 graph 外围的生命周期：

1. **初始化**：创建共享目录、provision skills、写入 SOUL.md
2. **运行 graph**：`graph.astream(initial_state, config, stream_mode="updates")`
3. **处理节点输出**：将 graph updates 转换为 `StreamEvent` 发送给前端
4. **执行子 Agent**：`execute` 节点由 adapter 的 `_handle_execute` 接管
5. **重规划**：执行失败时递归调用 `stream_chat`，携带重规划上下文
6. **合并到 main**：用户批准后执行 `task/{id}` → `main` 的合并

---

## 重规划机制

当子 Agent 执行失败时：

```
execute → review (检测到失败)
  → needs_replan=true, iteration < max_iterations
  → 回到 skill_prepare（带 replan_reason）
  → 重新走 reason → dispatch → execute
  → 最多重规划 3 次
```

重规划消息会追加原始用户需求 + 失败原因，要求 Orchestrator 保留已完成工作的意图，只修复失败部分。

---

## 与其他 Adapter 的区别

| 维度 | Orchestrator | Claude Code / OpenCode / Codex |
|------|---|---|
| LLM 调用 | 内部 ChatOpenAI | 外部 CLI 子进程 |
| 工具系统 | LangChain tools | CLI 内置 |
| 工作区 | 只读 task-base worktree | 独立读写 worktree |
| 输出 | Graph 节点流转 | stdout JSON 流解析 |
| Trace | LangSmith 自动 trace | 需手动 RunTree（Phase 5.2） |
