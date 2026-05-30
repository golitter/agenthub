# Ask Agent Card Events

- **变更原因**：Phase 5.1 需要在 Orchestrator 会话中展示向子 Agent 提问的卡片生命周期，同时子 Agent 在自己的 session 中保持正常流式回答。
- **变更文件**：`contracts/schemas/event-types.yaml`
- **对比结果**：新增 `ask_card_start` 与 `ask_card_done` 两个 SSE 事件类型。
- **跨端影响**：AgentEnd 可发送 Ask Card 生命周期事件；Frontend 需要解析并渲染 `ask_agent` block；Backend 仅使用生成枚举，无需新增业务接口。
- **契约变更**：
  - `ask_card_start`：包含 `question_id`、`target_agent`、`target_session_id`、`question`。
  - `ask_card_done`：包含 `question_id`、`target_agent`、`target_session_id`、`summary`、`status`。
