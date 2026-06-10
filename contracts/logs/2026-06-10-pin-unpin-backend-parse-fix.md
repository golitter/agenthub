# 置顶/取消置顶后端解析修复 + Orchestrator 重规划重构

## 变更原因

后端 `PatchTask` 在处理前端 `PATCH /tasks/:taskId { pinned_at }` 请求时存在两个 bug：

1. **置顶无效**：前端传 ISO 字符串 `"2026-06-10T12:00:00.000Z"`，后端直接将 string 塞入 GORM `Updates` map。但 `Task.PinnedAt` 字段类型为 `*time.Time`，MySQL 无法解析带 `T`/`Z` 的 ISO 8601 格式，导致写入静默失败。
2. **取消置顶报错**：前端发 `{"pinned_at": null}`，Go 的 `*string` 反序列化 `null` 与字段不存在同为 `nil`，`PatchTask` 跳过更新导致 `updates` 为空，返回 `"no fields to update"` 错误。

同时包含 Orchestrator 端内部重构：将重规划从递归改为迭代循环、改善合并冲突检测逻辑、移除未使用的 MemorySaver。

## 变更文件

- `backend/internal/service/impl/task_service.go`：`PatchTask` 用 `time.Parse(RFC3339)` 转换 ISO 字符串为 `time.Time`；`PinnedAt == nil` 时显式设置 `updates["pinned_at"] = nil`
- `agentend/src/adapters/orchestrator.py`：重规划由递归改为 while 迭代循环，避免深层嵌套和栈溢出
- `agentend/src/orchestrator/execution/engine.py`：`_detect_reported_merge_conflict` 增加 Phase 2 已解决信号检测，减少误触发重规划
- `agentend/src/orchestrator/planning/graph.py`：移除未使用的 `MemorySaver` checkpointer

## 对比结果

| 项目 | 修复前 | 修复后 |
|------|--------|--------|
| 置顶（`pinned_at` 设为 ISO 时间） | GORM 传 string，MySQL 解析失败，数据不变 | `time.Parse(RFC3339)` 转 `time.Time`，MySQL 正确写入 |
| 取消置顶（`pinned_at` 设为 `null`） | `*string == nil`，跳过更新，返回 400 错误 | `nil` 分支显式设 `updates["pinned_at"] = nil`，正确清除 |
| Orchestrator 重规划 | 递归调用 `stream_chat`，栈深度与迭代次数成正比 | 迭代 while 循环，每次迭代启动新 graph 执行 |
| 合并冲突检测 | 仅检查关键词存在即触发重规划 | 增加已解决信号检测（"成功合并"等），已解决的冲突不再误触 |

## 跨端影响

- **Frontend**：无需改动。API 契约（请求/响应格式）不变，前端已正确发送 ISO 字符串。
- **Backend**：`PatchTask` 内部解析逻辑修复，接口签名和返回结构不变。
- **AgentEnd**：Orchestrator 内部重构，SSE 事件输出格式不变。

## 契约变更

无。`Task.PinnedAt` 是后端内部模型字段，不在 `contracts/schemas/` 契约映射表中。Orchestrator 重构为内部实现变更，不涉及跨端协议。
