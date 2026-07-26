# cli session_id 回写测试

## 清理环境

```bash
# 1. 停止服务后执行

# 2. 清空 logs
echo '{}' > agentend/logs/session_mappings.json
echo '{}' > agentend/logs/workspaces.json

# 3. 删除 repo worktree
cd <repo-path>
git worktree list | tail -n +2 | awk '{print $1}' | while read wt; do git worktree remove "$wt" --force; done

# 4. 删除测试分支
git branch | grep -v '^\* main$' | xargs git branch -D

# 5. 清理 worktrees 目录
rm -rf <worktrees-root>/
```

## 测试用例

### 1. opencode 首次调用

不应传 `--session`，CLI 自建 session，INIT 事件回写 mapping。

```bash
curl -s -X POST http://localhost:8001/v1/agent/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "test-001",
    "task_id": "task-001",
    "message": "说你好",
    "agent_type": "opencode",
    "repo_path": "<repo-path>"
  }' | python3 -m json.tool
```

验证 mapping 已写入（值为 `ses_xxx` 格式）：

```bash
cat agentend/logs/session_mappings.json
```

### 2. opencode resume 调用

传 `--session <ses_xxx> --fork`，CLI 复用 session。

```bash
curl -s -X POST http://localhost:8001/v1/agent/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "test-001",
    "task_id": "task-001",
    "message": "刚才我说了什么？",
    "agent_type": "opencode",
    "repo_path": "<repo-path>"
  }' | python3 -m json.tool
```

### 3. claudecode 首次调用

不应传 `--session-id`，CLI 自建 session，INIT 事件回写 mapping。

```bash
curl -s -X POST http://localhost:8001/v1/agent/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "test-002",
    "task_id": "task-002",
    "message": "say hello",
    "agent_type": "claude-code",
    "repo_path": "<repo-path>"
  }' | python3 -m json.tool
```

验证 mapping：

```bash
cat agentend/logs/session_mappings.json
```

### 4. claudecode resume 调用

传 `--resume <id>`，CLI 复用 session。

```bash
curl -s -X POST http://localhost:8001/v1/agent/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "test-002",
    "task_id": "task-002",
    "message": "what did I just say?",
    "agent_type": "claude-code",
    "repo_path": "<repo-path>"
  }' | python3 -m json.tool
```
