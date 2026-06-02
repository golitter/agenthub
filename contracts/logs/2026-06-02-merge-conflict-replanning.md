# 2026-06-02 — Merge Conflict Replanning

## 变更原因

Orchestrator 需要在 sub-agent 合并到 task 分支失败时获得可重规划的失败信号，并在 task 分支合入 main 失败时返回结构化冲突信息，避免只暴露 `merge conflict` 字符串。

同时，`task/{task_id}` 分支由 `task-base` worktree 持有，sub-agent 不能直接 checkout task 分支。因此 `taskctl merge` 改为由 sub-agent 发起、在 `task-base` worktree 内执行实际合并。

## 变更文件

- `agentend/src/workspace/models.py`
- `agentend/src/workspace/git_ops.py`
- `agentend/src/workspace/manager.py`
- `agentend/src/api/v1/workspace.py`
- `agentend/src/orchestrator/execution/engine.py`
- `agentend/src/adapters/orchestrator.py`
- `agentend/src/orchestrator/models.py`
- `agentend/src/skills/builtin/taskctl/main.go`
- `agentend/src/skills/builtin/taskctl/taskctl`
- `backend/internal/handler/workspace.go`
- `backend/cmd/server/main.go`
- `frontend/src/lib/api.ts`

## 对比结果

- `WorkspaceManager.merge()` / `merge_task_to_main()` 从 `bool` 返回升级为结构化 `MergeResult`。
- AgentEnd merge API 返回 `success/source_branch/target_branch/conflict_files/error/aborted`。
- Backend 新增 `POST /api/workspace/task/:taskId/merge-to-main` proxy。
- Frontend 新增 `mergeTaskToMain()` API helper 和 `MergeResult` 类型。
- ExecutionEngine 不自动提交或合并，只向 sub-agent 追加 `taskctl merge` 集成要求。
- sub-agent 输出合并冲突时，ExecutionEngine 将该任务标记为 `merge_conflict`。
- Orchestrator adapter 基于真实执行失败结果触发再次规划。

## 跨端影响

- **Frontend**: 可调用 task-to-main merge API，并能展示结构化冲突文件。
- **Backend**: 增加 workspace proxy 路由，无业务结构转换。
- **AgentEnd**: merge API、taskctl、orchestrator 执行流获得冲突诊断与重规划能力。
- **Contracts**: 现有 `contracts/schemas/` 未包含 workspace merge API schema；本次不修改 YAML schema，无需 `make generate`。

## 契约变更

无 `contracts/schemas/*.yaml` 变更。

实际新增的非 schema 化响应结构：

```json
{
  "success": true,
  "source_branch": "task/{task_id}",
  "target_branch": "main",
  "conflict_files": [],
  "error": "",
  "aborted": false
}
```
