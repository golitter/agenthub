# contracts/ — 三端共享契约

本目录是 Frontend / Backend / AgentEnd 三端共享协议的**单一来源 (Single Source of Truth)**。

## 目录结构

```
contracts/
├── schemas/          # YAML 格式的 JSON Schema 契约定义
│   ├── event-types.yaml
│   ├── agent-request.yaml
│   ├── agent-response.yaml
│   ├── session-state.yaml
│   ├── message.yaml
│   └── validate-repo-path.yaml
├── logs/             # 契约变更审计记录
└── AGENTS.md         # 本文件
```

## 规则

1. **契约优先**：所有跨端协议的类型定义必须以 `schemas/` 中的 YAML 文件为准。
2. **不要手改生成文件**：各端 `generated/` 目录下的文件由 `make generate` 自动生成，手动修改会在下次生成时被覆盖。
3. **变更必须记录**：修改 `schemas/` 后，必须在 `logs/` 中写入变更记录。

## make generate

从 `contracts/schemas/*.yaml` 生成三端类型文件：

| Schema | Python | TypeScript | Go |
|--------|--------|------------|-----|
| event-types.yaml | `agentend/src/generated/events.py` | `frontend/src/generated/events.ts` | `backend/internal/generated/events.go` |
| agent-request.yaml | `agentend/src/generated/request.py` | `frontend/src/generated/request.ts` | `backend/internal/generated/request.go` |
| agent-response.yaml | `agentend/src/generated/response.py` | `frontend/src/generated/response.ts` | `backend/internal/generated/response.go` |
| session-state.yaml | `agentend/src/generated/session.py` | `frontend/src/generated/session.ts` | `backend/internal/generated/session.go` |

## 变更日志格式

在 `contracts/logs/` 中创建文件，命名格式：`YYYY-MM-DD-<kebab-case-description>.md`

内容应包含：

- **变更原因**：为什么需要修改契约
- **变更文件**：修改了哪些 schema 文件
- **对比结果**：与原契约的差异
- **跨端影响**：对 Frontend/Backend/AgentEnd 的影响
- **契约变更**：具体的枚举值/字段变更
