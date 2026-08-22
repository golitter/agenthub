# Orchestrator 子 Agent 独立 IM 消息语义

## 变更原因

Orchestrator 转发的子 Agent 文本此前使用 `runtime_text`，前端只能把它显示在运行卡片内，
无法形成群成员各自的聊天消息，也无法正确承载并行 Agent 输出。

## 变更文件

- `contracts/schemas/event-types.yaml`

## 对比结果

- `text` 的语义明确为可见聊天文本；同时携带 `group_id + message_id` 时表示群成员独立 IM 消息。
- `runtime_text` 明确为运行卡片增量日志和旧流兼容，不再承载子 Agent 自然语言回复。
- 事件枚举值和 `StreamEvent` JSON 结构均未新增或删除，现有客户端保持线协议兼容。

## 跨端影响

- AgentEnd：子 Agent 输出改发 `text`，并附带 Agent、session、message、run 和 attempt 身份。
- Backend：把源 `message_id` 映射为群聊镜像 Message，并向前端发布镜像 ID。
- Frontend：按 `message_id` 并发 upsert 群聊消息；`runtime_*` 继续投影为状态卡片。

## 契约变更

本次是现有事件类型的语义收紧，没有新增字段约束。`content` 仍允许附加字段；群聊 IM 投影
约定使用 `group_id`、`message_id`、`agent`、`agent_type` 和 `text`。
