# Workspace Git 操作测试

## 前置条件

无特殊前置条件。工具权限通过请求中的 `config.allowed_tools` 字段传递给 Rule Engine，再由适配器的 `--allowedTools` CLI 参数控制。

## 清理环境

```bash
# 1. 停止服务后执行

# 2. 清空 logs
echo '{}' > agentend/logs/session_mappings.json
echo '{}' > agentend/logs/workspaces.json

# 3. 删除 repo worktree
cd /Users/yanghao/Lab/vscode/gormlab
git worktree list | tail -n +2 | awk '{print $1}' | while read wt; do git worktree remove "$wt" --force; done

# 4. 删除测试分支
git branch | grep -v '^\* main$' | xargs git branch -D

# 5. 清理 worktrees 目录
rm -rf /Users/yanghao/Lab/vscode/worktrees/
```

---

## 测试用例

### 1. Claude Code 创建 workspace

创建 workspace 后应自动生成 task 分支、agent 分支、worktree 目录，并写入 `allowed_tools` 配置。

```bash
curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "/Users/yanghao/Lab/vscode/gormlab",
    "task_id": "task-001",
    "agent_name": "claude-code",
    "session_id": "sess-aaa",
    "agent_type": "claude-code"
  }' | python3 -m json.tool
```

验证分支结构：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git branch
# 应看到:
#   agent/sess-aaa/task-001
#   task/task-001
# * main
```

验证 worktree 目录：

```bash
ls /Users/yanghao/Lab/vscode/worktrees/task-001/sess-aaa/
# 应有完整仓库文件
```

验证 git exclude 排除了 `.claude` 目录：

```bash
cat /Users/yanghao/Lab/vscode/worktrees/task-001/sess-aaa/.git/info/exclude
# 应包含: /.claude
```

验证 workspace 状态已持久化：

```bash
cat agentend/logs/workspaces.json | python3 -m json.tool
# status 应为 "active"
```

### 2. OpenCode 创建 workspace（同 task）

同一 task 下创建第二个 agent workspace，task 分支应复用。

```bash
curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "/Users/yanghao/Lab/vscode/gormlab",
    "task_id": "task-001",
    "agent_name": "opencode",
    "session_id": "sess-bbb",
    "agent_type": "opencode"
  }' | python3 -m json.tool
```

验证分支结构：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git branch
# 应看到:
#   agent/sess-aaa/task-001
#   agent/sess-bbb/task-001
#   task/task-001
# * main
```

验证 git exclude 排除了 `.opencode` 目录：

```bash
cat /Users/yanghao/Lab/vscode/worktrees/task-001/sess-bbb/.git/info/exclude
# 应包含: /.opencode
```

### 3. Agent Commit

在 agent 分支上提交变更。

```bash
# 记录上一步返回的 workspace_id（Claude Code 那个）
WS_CLAUDE="<workspace_id>"

curl -s -X POST "http://localhost:8001/v1/workspace/${WS_CLAUDE}/commit" \
  -H 'Content-Type: application/json' \
  -d '{"message": "feat: test commit"}' | python3 -m json.tool
```

验证提交：

```bash
cd /Users/yanghao/Lab/vscode/worktrees/task-001/sess-aaa
git log --oneline -1
# 应看到: feat: test commit
```

无变更时 commit 应返回 `false`：

```bash
curl -s -X POST "http://localhost:8001/v1/workspace/${WS_CLAUDE}/commit" \
  -H 'Content-Type: application/json' \
  -d '{"message": "should be skipped"}' | python3 -m json.tool
# {"success": false}
```

### 4. Agent → Task 分支 Merge

将 agent 分支合并到 task 分支，workspace 状态应保持 ACTIVE。

```bash
curl -s -X POST "http://localhost:8001/v1/workspace/${WS_CLAUDE}/merge" \
  -H 'Content-Type: application/json' \
  -d '{"target_branch": "task/task-001"}' | python3 -m json.tool
# {"success": true}
```

验证 task 分支包含提交：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git log task/task-001 --oneline -3
# 应包含 agent 的提交
```

验证 workspace 状态未变：

```bash
cat agentend/logs/workspaces.json | python3 -m json.tool
# status 仍为 "active"
```

### 5. Agent → Task Merge 冲突

在两个 agent 分支上修改同一文件制造冲突，验证 merge abort 行为。

```bash
# 在 sess-aaa 的 worktree 中修改文件
echo "change from aaa" > /Users/yanghao/Lab/vscode/worktrees/task-001/sess-aaa/conflict.txt
cd /Users/yanghao/Lab/vscode/worktrees/task-001/sess-aaa && git add -A && git commit -m "aaa change"

# 在 sess-bbb 的 worktree 中修改同一文件
echo "change from bbb" > /Users/yanghao/Lab/vscode/worktrees/task-001/sess-bbb/conflict.txt
cd /Users/yanghao/Lab/vscode/worktrees/task-001/sess-bbb && git add -A && git commit -m "bbb change"

# 先 merge aaa 成功
WS_OPENCODE="<opencode_workspace_id>"

curl -s -X POST "http://localhost:8001/v1/workspace/${WS_CLAUDE}/merge" \
  -H 'Content-Type: application/json' \
  -d '{"target_branch": "task/task-001"}' | python3 -m json.tool
# {"success": true}

# 再 merge bbb 应冲突失败
curl -s -X POST "http://localhost:8001/v1/workspace/${WS_OPENCODE}/merge" \
  -H 'Content-Type: application/json' \
  -d '{"target_branch": "task/task-001"}' | python3 -m json.tool
# {"success": false, "error": "merge conflict"}
```

验证 task 分支未被破坏（aaa 的变更保留，bbb 的被 abort）：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git show task/task-001:conflict.txt
# 应为: change from aaa
```

### 6. Task → Main Merge

将 task 分支合并到 main。

```bash
curl -s -X POST "http://localhost:8001/v1/workspace/task/task-001/merge-to-main" \
  -H 'Content-Type: application/json' \
  -d '{"repo_path": "/Users/yanghao/Lab/vscode/gormlab"}' | python3 -m json.tool
# {"success": true}
```

验证 main 分支包含变更：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git log main --oneline -5
# 应包含 agent 提交
```

### 7. 清理单个 Workspace

删除 agent worktree 和分支，状态变为 CLEANED。

```bash
curl -s -X DELETE "http://localhost:8001/v1/workspace/${WS_CLAUDE}" | python3 -m json.tool
# {"success": true}
```

验证 worktree 已删除：

```bash
ls /Users/yanghao/Lab/vscode/worktrees/task-001/sess-aaa/
# 目录应不存在
```

验证 agent 分支已删除：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git branch | grep sess-aaa
# 应无结果
```

验证 workspace 状态：

```bash
cat agentend/logs/workspaces.json | python3 -m json.tool
# 对应 workspace status 应为 "cleaned"
```

### 8. 批量清理 Task

清理 task 下剩余 workspace 时，task 分支也应一并删除。

```bash
curl -s -X DELETE "http://localhost:8001/v1/workspace/${WS_OPENCODE}" | python3 -m json.tool
```

验证 task 分支已删除：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git branch | grep task-001
# 应无结果
```

### 9. 列出所有 Workspace

```bash
curl -s http://localhost:8001/v1/workspace | python3 -m json.tool
# 应返回所有 workspace 列表，包含 id/task_id/session_id/status 等字段
```

---

## 并发测试

### 10. 同 task 并发创建

同一 task_id 下并发创建两个 workspace，应串行执行，不会出现 task 分支创建竞态。

```bash
# 并发发起
curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "/Users/yanghao/Lab/vscode/gormlab",
    "task_id": "task-concurrent",
    "agent_name": "agent-1",
    "session_id": "sess-c1",
    "agent_type": "claude-code"
  }' &

curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "/Users/yanghao/Lab/vscode/gormlab",
    "task_id": "task-concurrent",
    "agent_name": "agent-2",
    "session_id": "sess-c2",
    "agent_type": "opencode"
  }' &

wait
```

验证：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git branch | grep task-concurrent
# 应有且仅有:
#   agent/sess-c1/task-concurrent
#   agent/sess-c2/task-concurrent
#   task/task-concurrent
```

### 11. 同 task 并发 Merge

同一 task 下并发 merge 两个 agent 分支到 task branch，应串行执行。

```bash
# 先清理并发测试环境
curl -s -X DELETE "http://localhost:8001/v1/workspace/<sess-c1-id>"
curl -s -X DELETE "http://localhost:8001/v1/workspace/<sess-c2-id>"
```

---

## TTL 测试

### 12. Inactive session 自动清理

workspace 清理由 DB inactive session 查询驱动。当 sessions 表中 session 状态变为 `inactive` 时，下一个清理周期会自动清理对应的 workspace。

前置条件：MySQL 中 `sessions` 表包含该 session 且 `status = 'inactive'`。

```bash
# 1. 创建 workspace
curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "/Users/yanghao/Lab/vscode/gormlab",
    "task_id": "task-inactive",
    "agent_name": "test",
    "session_id": "sess-inactive",
    "agent_type": "claude-code"
  }' | python3 -m json.tool

# 2. 在 DB 中将该 session 标记为 inactive
# UPDATE sessions SET status = 'inactive' WHERE session_id = 'sess-inactive';

# 3. 等待清理周期（cleanup_interval 秒，来自 config.yaml）
# 服务日志应输出: Inactive cleanup: scanned 1 sessions, cleaned 1 sessions, cleaned 0 tasks

# 4. 验证已清理
cat agentend/logs/workspaces.json | python3 -m json.tool
# 对应 workspace status 应为 "cleaned"
```
