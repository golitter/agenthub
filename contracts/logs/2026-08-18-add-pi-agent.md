## 变更原因

新增 Pi CLI Adapter，使 AgentHub 支持 Pi 0.82.1 的 JSONL 流式事件、会话恢复和 Skills。

## 变更文件

- `contracts/schemas/agent-request.yaml`

## 对比结果

AgentType 枚举新增 `pi` 值：

```yaml
enum:
  - claude-code
  - opencode
  - orchestrator
  - codex
  - pi
```

## 跨端影响

- **AgentEnd**：生成 `AgentType.PI`，注册 `PiAdapter`，并支持 `.pi/skills/`。
- **Backend**：生成 `AgentTypePi`，允许创建 Pi 会话和导入外部 Skill。
- **Frontend**：生成 `"pi"` 类型，支持 Pi 单聊、群聊、颜色和 Agent 资料页。

## 契约变更

新增枚举值向后兼容，不改变现有 AgentType 值和请求字段。
