# Trace 可观测性 Backlog

本文只保留未实现方向。当前已落地的 Langfuse Cloud trace 以这些文档为准：

- `agentend/docs/design/18-langfuse-trace.md`
- `agentend/docs/reference/langfuse-trace.md`
- `agentend/src/observability/`

## 背景

AgentEnd 已有 Langfuse 观测能力，用于 CLI / Orchestrator trace、隐私过滤和 callback 注入。这里记录的是后续可能引入 OpenTelemetry 的方向，不代表当前代码已经实现。

## 待评估方向

| 方向 | 价值 | 注意点 |
|------|------|--------|
| OpenTelemetry provider | 统一 HTTP、Orchestrator、Adapter、Workspace span | 需要避免和 Langfuse 重复采集 |
| FastAPI middleware | 自动记录请求耗时、状态码、trace_id | SSE 长连接要单独处理 |
| Orchestrator span tree | 观察 reason / dispatch / execute / review / save_mem 节点耗时 | 需要控制 prompt、工具输出等敏感内容 |
| async context propagation | 子任务、并发 Agent 执行保留 trace 上下文 | 需要验证 asyncio task 边界 |
| exporter 配置 | 接入 OTLP / Jaeger / Grafana Tempo | 增加部署复杂度 |

## 最小实施路径

1. 保持现有 Langfuse 实现稳定，不替换。
2. 在实验分支中添加 OTel provider 与 FastAPI middleware。
3. 只对 Orchestrator 关键节点打 span，不采集正文内容。
4. 验证 SSE、并发任务、错误路径是否能正确闭合 span。
5. 若收益明显，再沉淀为 `agentend/docs/design/` 的实施文档。

## 不做

- 不在 backlog 中维护完整代码草案。
- 不复制 `src/observability/` 已实现的 Langfuse 说明。
- 不把 prompt、用户消息、文件内容直接写入 trace attribute。
