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

## 后续（Phase 5.2）

CLI Adapter Execution Trace — 手动 RunTree 上报 StreamEvent 生命周期。注意事项：
- 不承诺完整 LLM prompt，只 trace 外部 agent 的 StreamEvent 生命周期
- text 做聚合，不要每个 chunk 一个 child run
- pending_tool_runs 用局部变量，避免并发问题
