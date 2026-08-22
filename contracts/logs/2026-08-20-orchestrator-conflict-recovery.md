# Orchestrator 冲突恢复契约变更

## 变更原因

并行 Agent 的 Git 合入冲突需要由结构化事实驱动恢复流程，不能再通过 Agent 文本猜测，也不能在
Resolver 尚未结束时关闭根流。

## 变更文件

- `contracts/schemas/event-types.yaml`
- `contracts/schemas/session-state.yaml`
- `contracts/schemas/integration-result.yaml`

## 对比结果

- 新增 integration/resolution 生命周期事件和 `orchestrator_paused` 事件。
- 会话状态新增 `resolving`、`awaiting_resolution` 及其合法转换。
- 新增 `IntegrationResult`，约束 taskctl 原子结果文件的 Run、分支、提交、冲突和错误字段。

## 跨端影响

- AgentEnd 产生结构化集成与 Resolver 进度事件。
- Backend 持久化并转发这些事件，暂停态不应合成为根任务 DONE。
- Frontend 将事件投影为任务的集成/冲突恢复状态。

## 契约变更

新增事件：`integration_started`、`integration_completed`、`integration_conflict`、
`resolution_started`、`resolution_progress`、`resolution_completed`、`resolution_failed`、
`orchestrator_paused`。

`integration-result.yaml` 的 `IntegrationStatus` 固定为 `merged`、`conflict`、`failed`；结果文件路径为
`shared/.agent/integration-results/<run_id>.json`，三端生成代码由 `make generate` 更新。
