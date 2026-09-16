# Tool Call 生命周期关联

## 变更原因

Coding Agent 评测需要准确关联同名并发工具调用，不能再依靠工具名称猜测 Tool Result 的归属。

## 变更文件

- `contracts/schemas/event-types.yaml`

## 对比结果

事件枚举与 `StreamEvent` 外形不变；补充 `content` 的协议语义。

## 跨端影响

- AgentEnd Adapter 优先保留 Provider ID，无 ID 时按 Run 单调生成。
- Backend 与 Frontend 继续按开放 `content` 透传，兼容旧事件。
- Langfuse Tool Span 改按 `tool_call_id` 关联。

## 契约变更

`tool_call` 和 `tool_result` 事件必须携带相同的 `tool_call_id`、工具名、状态和时间信息；流结束时未完成调用以 `incomplete` 结果事件闭合。
