# LangSmith Trace 操作指南

## 概述

Orchestrator 已接入 LangSmith 自动 trace。开启后，每次 Orchestrator 运行的 graph 节点转换、LLM 调用（完整 messages/response）、tool calls、token usage 自动上报 LangSmith。

## 开启方式

在 `agentend/.env` 中配置：

```env
LANGSMITH_TRACING=true
LANGSMITH_API_KEY=lsv2_sk_xxxxx
LANGSMITH_PROJECT=agenthub
```

重启服务生效：`make restart-agentend`

关闭时将 `LANGSMITH_TRACING` 留空即可，无任何副作用。

## 查看 Trace

1. 打开 [smith.langchain.com](https://smith.langchain.com)
2. 进入 `agenthub` project
3. 按时间或 session_id 筛选 run

### Trace 树结构

```
Run: Orchestrator session_id=yyy
├── skill_prepare (chain)        — 系统提示词构建、技能发现
├── reason (chain)               — LLM 工具调用循环
│   ├── ChatOpenAI #1 (llm)      — 输入: [SystemMessage, HumanMessage]
│   │                              输出: AIMessage(tool_calls=[read_file])
│   ├── read_file (tool)         — 参数: {path: "src/main.py"}
│   ├── ChatOpenAI #2 (llm)      — 输入: [... + ToolMessage]
│   │                              输出: AIMessage(tool_calls=[plan_and_dispatch])
│   └── plan_and_dispatch (tool) — 参数: {overview, tasks}
├── dispatch (chain)             — 计划分发、拓扑排序
├── execute (chain)              — 子 Agent 执行
├── review (chain)               — 执行结果审查
├── evolve (chain)               — 经验学习
└── save_mem (chain)             — 记忆保存
```

点击每层可展开看完整的 prompt 和 response 文本。

## 适用场景

- **调优提示词**：查看完整的 system prompt + message history，分析 LLM 为何做出某个决策
- **调试 tool call**：查看 LLM 返回的 tool_calls 参数是否正确
- **性能分析**：查看每步耗时和 token 用量
- **回归测试**：修改 prompt 后对比前后 trace，确认行为符合预期

## 技术原理

- Orchestrator 使用 LangGraph + LangChain ChatOpenAI，LangSmith 通过环境变量自动 hook
- `reason_node` 内通过 `langgraph.config.get_config()` 获取 runnable config 并传播到 `llm_with_tools.ainvoke()`，确保 LLM 子调用挂到 graph trace 树下
- 不设 `LANGSMITH_TRACING` 时 `get_config()` 兜底返回 None，零开销

## 后续规划（Phase 5.2）

CLI Adapter（Claude Code / OpenCode / Codex）的 trace 接入，通过 `langsmith.RunTree` 手动上报 StreamEvent 生命周期。
