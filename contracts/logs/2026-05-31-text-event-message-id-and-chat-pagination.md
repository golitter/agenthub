# TEXT 事件补充 message_id 与聊天历史冷加载修复

## 变更原因

Orchestrator 调用子 Agent 时，一次用户请求会产生多段输出：Orchestrator 发起提问、子 Agent 原始回答、Orchestrator 总结。前端仅依赖 `agent` / `agent_type` 判断消息边界时，同一 Agent 的不同持久化 Message 可能被合并，刷新或 SSE 重放后容易出现消息顺序错乱、用户消息被历史分页截断后不可见、以及当前会话默认停留在较老消息位置的问题。

因此，本次变更要求 TEXT SSE 事件在可用时携带当前持久化 Message 的 `message_id`，让前端可以按 `message_id` 识别消息边界。同时前端历史加载保持冷加载，只拉取最新一页，默认滚动到最新位置；继续通过 `before` 分页向上加载更老消息。

## 变更文件

本次变更 **未修改** `contracts/schemas/*.yaml`。

`contracts/schemas/event-types.yaml` 中 `StreamEvent.content` 已定义为开放对象：

- `type: object`
- `additionalProperties: true`

因此 TEXT 事件新增 `message_id` 字段在现有 schema 下合法，不需要重新生成三端契约类型。

## 对比结果

### 变更前

TEXT 事件可能只携带：

```json
{
  "type": "text",
  "content": {
    "text": "...",
    "agent": "执行者",
    "agent_type": "claude-code"
  }
}
```

前端只能按 `agent` / `agent_type` 切换消息，无法区分同一 Agent 连续产生的不同 Message。

### 变更后

TEXT 事件在后端已知当前 Message 时携带：

```json
{
  "type": "text",
  "content": {
    "text": "...",
    "agent": "执行者",
    "agent_type": "claude-code",
    "message_id": "..."
  }
}
```

前端按 `message_id` 变化拆分消息；无 `message_id` 时仍兼容旧逻辑，继续按 `agent` / `agent_type` 处理。

## 跨端影响

- **AgentEnd**: `ask_agent` 转发子 Agent TEXT 时透传子 Agent 的 `message_id`，便于 Backend 和 Frontend 保持原始消息边界。
- **Backend**: `StreamWriter` 发布、Redis 重放、completed / failed 回放时携带当前 Message 的 `message_id`；ListMessages 仍按 `session_id` 过滤并支持 `before` 分页。
- **Frontend**: TEXT 事件处理读取 `content.message_id`；历史初始加载只请求最新一页，默认滚动到最新位置；上滑触顶时继续通过 `before=firstMsg.dbId` 冷加载更老消息。
- **Contracts**: 无 schema 变更；记录 TEXT 事件 content 扩展字段 `message_id` 的约定。

## 契约变更

TEXT 事件 content 新增可选字段：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `message_id` | `string` | 否 | 当前 TEXT chunk 所属持久化 Message 的 ID，用于前端识别消息边界和 SSE 重放归属。 |

兼容性：

- 旧客户端忽略未知字段，不受影响。
- 新客户端在字段缺失时回退到 `agent` / `agent_type` 边界判断。
