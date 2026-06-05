# Orchestrator 群聊消息 group_id 编排归属修复

## 变更原因

Orchestrator 群聊在实时流式阶段会交替发送管理者事件、ask-card 和子 Agent 正文。原有模型只能靠 `agent_type` 切气泡，导致：

- ask-card 先落到管理者消息，再被子 Agent 正文拆成新气泡
- 子 Agent 回复在主会话历史中缺少稳定归属
- 群聊历史会混入子 session 原始消息，与主会话编排流重复

本次通过 `group_id` 为同一轮编排建立稳定关联，让主会话中的子 Agent 消息成为 ask-card 与正文的统一承载体。

## 契约变更

### `contracts/schemas/message.yaml`

新增字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `group_id` | `string \| null` | 编排组标识，同一轮 orchestrator 编排消息共享该 ID |

## 跨端行为

- **AgentEnd**
  - `orchestrator` 执行阶段为同一轮 wave/汇总事件注入 `group_id`
  - ask-card、子 Agent 完整 TEXT、汇总 TEXT 均携带相同 `group_id`

- **Backend**
  - `Message` 模型新增 `group_id`
  - `StreamWriter` 对带 `group_id` 的子 Agent TEXT 在主会话创建或复用独立消息
  - ask-card 直接持久化到对应子 Agent 主会话消息中，后续正文继续写入同一消息
  - group 模式历史只展示主会话编排流，避免与子 session 原始消息重复

- **Frontend**
  - live store 在 grouped ask-card 场景下保留 ask-card block，等待子 Agent 正文接管同一气泡
  - 历史消息读取与实时消息都保留 `group_id`
  - group chat 视图仅展示主会话编排流

## 兼容性

- 未识别 `group_id` 的旧客户端仍可展示文本，但不会得到新的归属语义
- 新客户端对旧历史兼容，旧消息缺少 `group_id` 时仍按既有逻辑渲染
