# Agent Run 生命周期契约

- **变更原因**：统一 AgentEnd、Backend 和 Frontend 的执行标识、状态、取消原因与断流续接语义。
- **变更文件**：新增 `agent-run.yaml`，扩展 `agent-request.yaml`。
- **对比结果**：执行不再只由 `session_id` 标识；新增 `run_id`、父子 Run、预算、状态、终止原因和带序号事件信封。
- **跨端影响**：AgentEnd 负责 Run 生命周期与事件序号；Backend 持久化关联并提供取消/查询；Frontend 展示执行状态。
- **契约变更**：新增 AgentRunIdentity、AgentRunBudget、AgentRunState、AgentRunTerminationReason、AgentRunStatus、AgentRunEventEnvelope 与取消请求/响应。
- **内部子 Run 上下文**：`RunTaskRequest` 增加仅供 AgentEnd 认证回环使用的 `root_run_id`、`parent_run_id` 与 `budget`；浏览器公开入口会清空这些字段，避免伪造执行树身份。
