# Orchestrator 重规划机制修复 + 合并冲突误判修复

## 变更原因

Orchestrator 在多 Agent 协作场景中存在三个互相关联的 bug：

1. **合并冲突误判**（直接原因）：subagent 成功解决冲突后，输出包含 "合并冲突" / "冲突文件" 等关键词（来自 taskctl 的冲突阶段输出），`_detect_reported_merge_conflict` 将成功解决的冲突误判为失败，导致 orchestrator 不必要地触发 replan，且 replan 后又因同样误判再次失败，形成死循环。

2. **MemorySaver 状态污染**：`build_graph()` 使用 `MemorySaver` 编译 LangGraph graph，replan 递归调用 `stream_chat` 时共用同一 `session_id`（= `thread_id`），新旧 checkpoint 通过 `_add` / `_add_one` reducer 合并，可能导致 graph 从旧 checkpoint 恢复而跳过 reason → dispatch → execute 节点。

3. **递归 replan 栈风险**：replan 通过 `self.stream_chat()` 递归调用自身，最多 3 层嵌套，每层创建独立的 graph 执行、queue、producer，栈深增加且调试困难。

另含 backend `pinned_at` 字段的 RFC3339 格式校验修复。

## 变更文件

- `agentend/src/orchestrator/execution/engine.py`：合并冲突检测三阶段逻辑（非 schema 变更）
- `agentend/src/orchestrator/planning/graph.py`：去掉 MemorySaver checkpointer（非 schema 变更）
- `agentend/src/adapters/orchestrator.py`：stream_chat 递归 → while 循环重构（非 schema 变更）
- `backend/internal/service/impl/task_service.go`：pinned_at RFC3339 校验（非 schema 变更）

## 对比结果

| 项目 | 修复前 | 修复后 |
|------|--------|--------|
| 冲突检测关键词 | "合并冲突" / "冲突文件" / "merge conflict" 任一即触发 | 仅 "冲突文件" / "conflict files" 触发，且检查后续成功信号 |
| 成功解决的冲突 | 误判为失败（输出含 "合并冲突已成功解决" 被检测） | 正确放行（三阶段：关键词 → 成功信号 → 文件列表） |
| 未解决的冲突 | 正确检测 | 正确检测（无后续成功信号 → 返回冲突文件列表） |
| 空文件列表 | 返回 `[]`（非 None），触发失败 | 返回 `None`，正确放行 |
| Graph checkpointer | `MemorySaver` 导致 replan 时新旧 state 合并 | `graph.compile()` 无 checkpointer，每次 astream 从零开始 |
| Replan 机制 | 递归 `self.stream_chat()` 调用（栈深 +1/次） | `while True` 外层循环（每轮 fresh state + queue + producer） |
| Backend pinned_at | 直接赋值字符串，无格式校验 | RFC3339 解析校验，null 时清除 |

## 跨端影响

- **AgentEnd**：`ExecutionEngine._detect_reported_merge_conflict` 检测逻辑变更；`OrchestratorAdapter.stream_chat` 控制流重构（外部接口签名不变）；`build_graph` 去掉 checkpointer（graph 节点逻辑不变）。
- **Backend**：`TaskService.PatchTask` 的 `pinned_at` 增加格式校验，接口签名不变。
- **Frontend**：无需改动。SSE 事件类型和格式均未变更。

## 契约变更

无。本次修改均为内部实现修复，不涉及 `contracts/schemas/` 中的跨端协议定义。
