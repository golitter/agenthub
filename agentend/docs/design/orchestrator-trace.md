# Phase 5.1：Orchestrator LangSmith Trace 接入

## Context

调优 Orchestrator 提示词时，需要看到每次 LLM 调用的**完整输入/输出**（system prompt、message history、tool calls、token 用量）。目前没有任何 trace 机制，没有 trace 基本就是盲调 prompt。

**目标**：接入 LangSmith，自动 trace Orchestrator 的全部 LLM 调用、工具执行、Graph 节点转换。最小闭环，不自研 tracer。

**不在本 phase 做的事**：CLI Adapter（Claude Code / OpenCode / Codex）的手动 RunTree trace，放入 Phase 5.2。

---

## 方案

项目已安装 `langsmith==0.8.5`，LangChain/LangGraph 内置支持，大部分 tracing 依赖环境变量自动开启。

### 环境变量

`.env` 中加 3 行，全局生效：

```env
LANGSMITH_TRACING=true
LANGSMITH_API_KEY=lsv2_sk_xxxxx
LANGSMITH_PROJECT=agenthub
```

> `LANGCHAIN_TRACING_V2=true` 旧写法仍可用，官方推荐新版 `LANGSMITH_TRACING=true`。

开启后，LangGraph 的 graph nodes、ChatOpenAI 调用、tool calls 自动上报 LangSmith，无需代码改动。

### 唯一代码改动

**`agentend/src/orchestrator/planning/graph.py`** — reason_node（~line 539）

```python
from langchain_core.runnables import get_config

# 原来：response = await llm_with_tools.ainvoke(messages)
# 改为：
try:
    llm_config = get_config()
except RuntimeError:
    llm_config = None
response = await llm_with_tools.ainvoke(messages, config=llm_config)
```

**原因**：graph 节点内部如果手动调用 LLM，会脱离 LangGraph 当前 runnable config，导致 LLM 子调用没有正确挂到 graph trace 树下面。显式传播 `config` 后，graph 级 + LLM 级 + tool 级全部完整挂到同一个 trace 下。

---

## LangSmith 能看到什么

```
Run: Orchestrator session_id=yyy
├── skill_prepare (chain)        — 0.8s
├── reason (chain)               — 12.3s
│   ├── ChatOpenAI #1 (llm)      — input: [SystemMessage, HumanMessage]
│   │                              output: AIMessage(tool_calls=[read_file])
│   ├── read_file (tool)         — args: {path: "src/main.py"}
│   ├── ChatOpenAI #2 (llm)      — input: [... + ToolMessage]
│   │                              output: AIMessage(tool_calls=[plan_and_dispatch])
│   └── plan_and_dispatch (tool) — args: {overview, tasks}
├── dispatch (chain)             — 0.3s
├── execute (chain)              — 45.2s
├── evolve (chain)               — 1.2s
└── save_mem (chain)             — 0.1s
```

每层点击可展开看完整的 prompt 和 response 文本。

---

## 关键文件

| 文件 | 操作 |
|------|------|
| `agentend/src/orchestrator/planning/graph.py` | **改 1 处** — reason_node 里 `get_config()` 传播到 LLM |
| `agentend/.env` | **加 3 行** — LANGSMITH_TRACING / LANGSMITH_API_KEY / LANGSMITH_PROJECT |

---

## 验证方式

1. 在 `.env` 中配置 LangSmith 环境变量
2. `make run-agentend` 启动服务
3. 发起一个 orchestrator 任务
4. 打开 [smith.langchain.com](https://smith.langchain.com)，在 `agenthub` project 下查看 trace
5. 确认：能看到 graph nodes、每轮 ChatOpenAI 的完整 messages/response、tool calls、token usage

---

## Phase 5.2：CLI Adapter Execution Trace

### Context

Phase 5.1 已通过 LangGraph 自动 trace 覆盖了 Orchestrator 的所有 LLM 调用。但 CLI Adapter（Claude Code / OpenCode / Codex）是通过子进程与外部 CLI 交互的，LangSmith 无法自动感知其内部行为。需要在 `_execute_stream`（所有 Adapter 事件的统一出口）手动构建 RunTree，上报 StreamEvent 生命周期。

**不承诺**完整 LLM prompt（CLI 不暴露），只 trace 外部 agent 的 StreamEvent 生命周期。

### 方案

新增 `src/adapters/trace.py`，提供 `trace_stream_events()` async generator wrapper。在 `_execute_stream` 中用该 wrapper 包裹 `adapter.stream_chat()`，不改任何 Adapter 代码。

#### 事件 → RunTree 映射

```
StreamEvent lifecycle          →  RunTree 操作
───────────────────────────────────────────────────────
流开始                          →  创建 root RunTree (chain)
TEXT chunk × N                  →  聚合到 text_parts[]（不创建 child）
TOOL_CALL                       →  ① flush 聚合文本 → 1 个 text child run
                                   ② 创建 tool:{name} child run (pending)
TOOL_RESULT                     →  LIFO 匹配 pending tool run → end + patch
DONE                            →  flush 剩余文本
ERROR                           →  flush + 标记 root.error
流结束 (finally)                →  清理残留 pending → root.end() + root.patch()
```

#### 关键设计点

| 约束 | 实现 |
|------|------|
| **不承诺完整 LLM prompt** | 只 trace StreamEvent，不碰 CLI 内部 prompt |
| **text 做聚合** | `text_parts: list[str]` 累积，按 TOOL_CALL / DONE / ERROR 边界 flush 成一个 child run |
| **pending_tool_runs 用局部变量** | `pending_tool_runs` 定义在 `trace_stream_events` 函数体内，每次调用独立，无共享状态 |

#### Orchestrator 排除

Orchestrator Adapter 的事件流走 `_handle_execute`（内部用 LangGraph 执行），Phase 5.1 已通过 `get_config()` 自动 trace。`trace_stream_events` 仅作用于 CLI Adapter。

### 关键文件

| 文件 | 操作 |
|------|------|
| `agentend/src/adapters/trace.py` | **新建** — `trace_stream_events()` async generator wrapper |
| `agentend/src/api/v1/agent.py` | **改 1 处** — `_execute_stream` 用 wrapper 包裹 `adapter.stream_chat()` |

### LangSmith 能看到什么

```
Run: claude-code session_id=abc123
│   inputs:
│     message: "帮我分析这个文件"
│     session_id: "abc123"
│     system_prompt_append: "You are operating in a managed environment...输出技能..."
│     allowed_tools: ["read_file", "write_file", "render"]
│     max_turns: 30
│     cwd: "/Users/yanghao/worktrees/.../..."
│     is_resume: false
│   outputs:
│     status: "completed"
│     output: "我来帮你分析这个文件...让我先看一下...这个文件的问题是..."
│
├── init (chain)                 — Adapter 启动
│     outputs: {"cli_session_id": "sess_xyz", "agent_type": "claude-code"}
├── text (llm)                  — 聚合所有 TEXT chunk
│     outputs: {"text": "我来帮你分析这个文件...让我先看一下..."}
├── tool:read_file (tool)       — TOOL_CALL → TOOL_RESULT
│     inputs:  {"args": {"path": "src/main.py"}}
│     outputs: {"result": "import os..."}
├── text (llm)                  — 第二段文本（工具执行后）
│     outputs: {"text": "这个文件的问题是..."}
├── done (chain)                 — Adapter 完成
│     outputs: {"usage": {"input_tokens": 1200, "output_tokens": 800}}
└── (root end, status=completed)
```

- **root inputs** 包含 message + 全部 stream_kwargs（rule 注入的 system_prompt_append、allowed_tools、max_turns、cwd 等）
- **root outputs** 包含 status + 聚合后的完整响应文本
- **init** child run 展示 adapter 类型、CLI session ID
- **text** child run 展示聚合后的完整文本（不是每个 chunk 一个 run）
- **tool:{name}** child run 展示完整的 args 和 result
- **done** child run 展示 token usage

#### 环境变量控制

Phase 5.1 和 Phase 5.2 统一由 `LANGSMITH_API_KEY` 控制（与 LangChain SDK 行为一致）：

```env
# 两个都要配置才能启用追踪功能
LANGSMITH_TRACING=true
LANGSMITH_API_KEY=lsv2_sk_xxxxx
LANGSMITH_PROJECT=agenthub
```

> 删掉或清空 `LANGSMITH_API_KEY` 即可彻底关闭追踪，不依赖 `LANGSMITH_TRACING` 值。

### 验证方式

1. 确保 `.env` 中 `LANGSMITH_API_KEY` 已配置
2. `make run-agentend` 启动服务
3. 发起一个 claude-code / opencode / codex 类型的任务
4. 打开 [smith.langchain.com](https://smith.langchain.com)，在 `agenthub` project 下查看 trace
5. 确认：能看到 root inputs 包含 rule 上下文、聚合文本、每个 TOOL_CALL/TOOL_RESULT 对、root outputs 包含完整响应
