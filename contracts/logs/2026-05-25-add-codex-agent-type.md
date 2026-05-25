## 变更原因

新增 Codex CLI（OpenAI）作为第四个 Agent 后端，需要扩展 AgentType 枚举以支持 `codex` 类型。

## 变更文件

- `contracts/schemas/agent-request.yaml`

## 对比结果

AgentType 枚举新增 `codex` 值：
```yaml
# Before
enum:
  - claude-code
  - opencode
  - orchestrator

# After
enum:
  - claude-code
  - opencode
  - orchestrator
  - codex
```

## 跨端影响

- **AgentEnd**: `src/generated/request.py` AgentType 枚举新增 `CODEX` 值
- **Frontend**: `src/generated/request.ts` AgentType 类型新增 `"codex"`
- **Backend**: `internal/generated/request.go` AgentType 枚举新增 `codex`
- 前端 Agent 选择器需支持 `codex` 类型

## 契约变更

枚举值新增，向后兼容（不影响现有 `claude-code`、`opencode`、`orchestrator` 值）。
