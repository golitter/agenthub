# Orchestrator 任务身份解耦契约变更

## 变更原因

计划子任务 ID（如 `task-001`）与 Git 集成范围 ID（根任务 UUID）曾共用
`task_id`，导致 `IntegrationResult` 被 Engine 错误拒收。15 号实施规格要求以
`IntegrationOperation` 绑定计划任务、Run、Workspace 和 Git 集成范围，并支持 V1/V2
双读迁移。

## 变更文件

- `contracts/schemas/agent-request.yaml`
- `contracts/schemas/agent-routing.yaml`
- `contracts/schemas/agent-run.yaml`
- `contracts/schemas/integration-result.yaml`

## 契约变更

- 增加 `plan_task_id`、`integration_operation_id`、`workspace_handle` 和
  `integration_attempt` 等内部 child-run 字段。
- `IntegrationResult.task_id` 保留为 V1 兼容字段，语义固定为
  `integration_scope_id`，不再与计划任务 ID 比较。
- V2 增加 operation、计划任务、scope、workspace 和幂等摘要字段。
- Run 身份投影增加计划任务与集成 operation 关联字段。
- Agent Run 增加 `awaiting_resolution` 非终态；Orchestrator 自动恢复耗尽后持久化暂停，重启不会把它误收敛为完成。

## 跨端影响

- AgentEnd 负责创建/校验 IntegrationOperation 并双读 V1/V2 结果。
- Backend 只负责校验和透传内部 child-run 字段，不解析 Git scope。
- Frontend 继续把 `task_id` 作为业务/逻辑任务兼容字段，普通 SSE 不展示 Git 谱系。
- 生成文件必须通过 `make generate` 更新，禁止手工修改。
