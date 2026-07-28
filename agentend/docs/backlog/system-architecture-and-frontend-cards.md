# Agent 系统与前端卡片 Backlog

本文保留早期设计中尚未完全产品化的方向。已实现内容分别维护在：

- `docs/design/sse-streaming-architecture.md`
- `frontend/docs/design/06-cards.md`
- `frontend/docs/design/08-block-parser.md`
- `agentend/docs/design/02-adapters.md`
- `agentend/docs/design/11-orchestrator-planning.md`

## 已收敛到当前实现的内容

| 主题 | 当前权威位置 |
|------|--------------|
| 三端 SSE 链路 | `docs/design/sse-streaming-architecture.md` |
| StreamEvent 协议 | `contracts/schemas/event-types.yaml` |
| Agent 适配器 | `agentend/docs/design/02-adapters.md` |
| Orchestrator 规划 | `agentend/docs/design/11-orchestrator-planning.md` |
| 前端卡片系统 | `frontend/docs/design/06-cards.md` |
| aka_yhy block 解析 | `frontend/docs/design/08-block-parser.md` |

## 仍可作为 backlog 的方向

| 方向 | 说明 |
|------|------|
| 更细粒度 Tool Event 生命周期 | 将 tool_call / tool_result / tool_error 的 UI 状态进一步标准化 |
| Agent Timeline | 独立展示多 Agent 执行时间线，而不只在消息流中呈现 |
| Capability 模型 | 从 agent_type 选择升级为基于能力匹配 Agent |
| Artifact 生命周期 | 对任务产物引用、清理、预览权限做更明确的生命周期管理 |
| TaskGraph | 在 Orchestrator 中引入更显式的依赖图与重试策略 |

## 维护规则

1. 已实现的细节不在 backlog 重复描述，只链接权威文档。
2. 新实现落地后，应移入 `docs/design/` 或对应端内 `docs/design/`。
3. backlog 只保留问题、方向和取舍，不维护长篇伪代码。
