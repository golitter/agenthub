# Workspace 分支清理

## 实现了什么

`cleanup()` 在移除 worktree 后同时删除对应的 git 分支（agent 分支 + task 分支），避免残留无主分支。

## 怎么实现的

### 分支模型

每个 task 的 workspace 会创建两类分支：

- **agent 分支**：`agent/{session_id}/{task_id}` — 每个 agent session 对应一个
- **task 分支**：`task/{task_id}` — 同一 task 共享的基础分支

### 修复方案

#### 1. GitOps 新增 `branch_delete()`

`src/workspace/git_ops.py` — 执行 `git branch -D <name>` 删除指定分支。

#### 2. `cleanup()` 清理 agent 分支

`src/workspace/manager.py` — worktree 移除成功后，立即删除对应的 agent 分支。

#### 3. `cleanup_by_task()` 清理 task 分支

`src/workspace/manager.py` — 所有 agent worktree 清理完毕后，对涉及的 repo_path 移除 task-base worktree 并删除 task 分支；即使已无活跃 workspace 也执行（覆盖 task 已创建但从未运行的情况）。另有 `cleanup_task_branches(task_id, repo_path)` 强制版本，额外删除该 task 残留的全部 agent 分支（对应 `/v1/workspace/task/{task_id}/cleanup-branches` 端点）。

#### 4. recovery 孤儿清理同时删除分支

`src/workspace/recovery.py` — 移除孤儿 worktree 后，也删除其对应分支（跳过主工作树与 task-base worktree）。

#### 5. shutdown 清理

`src/app/main.py` — 关闭时停止 inactive 清理任务并关闭 DB 连接，workspace 清理由 inactive cleanup loop 负责。
