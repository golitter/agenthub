# taskctl merge 命令测试

测试 `taskctl merge` 子命令：agent 分支 → task 分支合并，含自动提交、冲突处理、分支回退。

## 前置条件

```bash
# 1. 构建最新 taskctl 二进制
cd agentend/src/skills/builtin/taskctl && go build -o taskctl .

# 2. 启动 agentend 服务
# 确保 localhost:8001 可访问

# 3. 确认测试仓库存在
ls /Users/yanghao/Lab/vscode/gormlab
```

## 清理环境

```bash
# 清空持久化
echo '{}' > agentend/logs/session_mappings.json
echo '{}' > agentend/logs/workspaces.json

# 清理 worktree
cd /Users/yanghao/Lab/vscode/gormlab
git worktree list | tail -n +2 | awk '{print $1}' | while read wt; do git worktree remove "$wt" --force; done
git branch | grep -v '^\* main$' | xargs git branch -D
rm -rf /Users/yanghao/Lab/vscode/worktrees/
```

---

## 测试用例

### 1. 创建 workspace

```bash
curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "/Users/yanghao/Lab/vscode/gormlab",
    "task_id": "test",
    "agent_name": "test-agent1",
    "session_id": "test-agent1",
    "agent_type": "claude-code"
  }' | python3 -m json.tool
```

记录返回的 `id` 作为 `WS_ID`。

验证分支与 taskctl 部署：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git branch
# 应看到:
#   agent/test-agent1/test
#   task/test
# * main

# taskctl 二进制已部署
ls /Users/yanghao/Lab/vscode/worktrees/test/test-agent1/.claude/skills/taskctl/taskctl
# 应存在

# merge 命令可用
/Users/yanghao/Lab/vscode/worktrees/test/test-agent1/.claude/skills/taskctl/taskctl help
# 应包含: merge  合并当前 agent 分支到 task 分支
```

### 2. Agent 写入文件

```bash
curl -s -X POST http://localhost:8001/v1/agent/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "task_id": "test",
    "session_id": "test-agent1",
    "message": "在当前目录创建一个 README.md，内容写一行：# Hello from test-agent1",
    "agent_type": "claude-code",
    "stream": false,
    "repo_path": "/Users/yanghao/Lab/vscode/gormlab",
    "config": {
      "allowed_tools": ["Read", "Write", "Edit", "Bash"]
    }
  }' | python3 -m json.tool
```

验证文件已创建：

```bash
cat /Users/yanghao/Lab/vscode/worktrees/test/test-agent1/README.md
# 应为: # Hello from test-agent1

cd /Users/yanghao/Lab/vscode/worktrees/test/test-agent1 && git status --short
# 应有未提交的 README.md
```

### 3. Agent 提交

```bash
curl -s -X POST http://localhost:8001/v1/agent/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "task_id": "test",
    "session_id": "test-agent1",
    "message": "提交当前改动",
    "agent_type": "claude-code",
    "stream": false,
    "config": {
      "allowed_tools": ["Read", "Write", "Edit", "Bash"]
    }
  }' | python3 -m json.tool
```

验证提交：

```bash
cd /Users/yanghao/Lab/vscode/worktrees/test/test-agent1
git log --oneline -1
# 应包含 Add README.md 的 commit
git branch --show-current
# 应为: agent/test-agent1/test
```

### 4. taskctl merge（成功路径）

```bash
cd /Users/yanghao/Lab/vscode/worktrees/test/test-agent1
.claude/skills/taskctl/taskctl merge
# 预期输出: merged to task/test
# exit code: 0
```

验证合并结果：

```bash
cd /Users/yanghao/Lab/vscode/worktrees/test/test-agent1

# 当前分支回到 agent 分支
git branch --show-current
# 应为: agent/test-agent1/test

# task 分支包含 README
git show task/test:README.md
# 应为: # Hello from test-agent1

# task 分支日志包含 agent 的提交
git log task/test --oneline -3
# 应包含 agent 的 commit
```

### 5. taskctl merge（有未提交改动）

```bash
# 制造未提交改动
cd /Users/yanghao/Lab/vscode/worktrees/test/test-agent1
echo "extra content" >> README.md

# merge 应自动提交后合并
.claude/skills/taskctl/taskctl merge
# 预期输出: merged to task/test
# exit code: 0

# 验证 task 分支包含新内容
git show task/test:README.md
# 应包含 extra content

# 验证自动提交记录
git log task/test --oneline -1
# 应包含: auto: merge前自动提交
```

### 6. taskctl merge（冲突路径）

创建第二个 agent 制造冲突：

```bash
# 创建第二个 agent workspace
curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "/Users/yanghao/Lab/vscode/gormlab",
    "task_id": "test",
    "agent_name": "test-agent2",
    "session_id": "test-agent2",
    "agent_type": "claude-code"
  }' | python3 -m json.tool

# 在 agent2 中修改 README 并提交
cd /Users/yanghao/Lab/vscode/worktrees/test/test-agent2
echo "# Conflict from agent2" > README.md
git add -A && git commit -m "agent2: conflict change"

# 先 merge agent2 成功
.claude/skills/taskctl/taskctl merge
# 输出: merged to task/test

# 回到 agent1，制造冲突修改
cd /Users/yanghao/Lab/vscode/worktrees/test/test-agent1
echo "# Conflict from agent1" > README.md
git add -A && git commit -m "agent1: conflict change"

# merge 应冲突失败
.claude/skills/taskctl/taskctl merge
# 预期 stderr 输出: 合并冲突: ...
# exit code: 1
```

验证冲突后状态安全：

```bash
# 当前分支仍在 agent 分支
cd /Users/yanghao/Lab/vscode/worktrees/test/test-agent1
git branch --show-current
# 应为: agent/test-agent1/test

# task 分支未被破坏（仍是 agent2 的版本）
git show task/test:README.md
# 应为: # Conflict from agent2

# agent1 的改动未丢失
git show agent/test-agent1/test:README.md
# 应为: # Conflict from agent1
```

### 7. 完整清理

```bash
# 1. 删除所有 workspace
curl -s http://localhost:8001/v1/workspace | python3 -c "
import sys, json
for ws in json.load(sys.stdin):
    print(f'deleting {ws[\"id\"]}...')
    import urllib.request
    urllib.request.urlopen(f'http://localhost:8001/v1/workspace/{ws[\"id\"]}', method='DELETE')
print('done')
"

# 2. 清空持久化记录
echo '{}' > agentend/logs/session_mappings.json
echo '{}' > agentend/logs/workspaces.json

# 3. 清理 repo 中的 worktree 残留
cd /Users/yanghao/Lab/vscode/gormlab
git worktree list | tail -n +2 | awk '{print $1}' | while read wt; do git worktree remove "$wt" --force; done

# 4. 删除除 main 外的所有分支
git branch | grep -v '^\* main$' | xargs git branch -D

# 5. main 回到测试前状态（丢弃测试提交）
git reset --hard origin/main

# 6. 清理 worktrees 目录
rm -rf /Users/yanghao/Lab/vscode/worktrees/
```

验证清理完成：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git branch
# 应仅有: * main

git worktree list
# 应仅有主 worktree

cat agentend/logs/workspaces.json
# 应为: {}

cat agentend/logs/session_mappings.json
# 应为: {}
```

---

## 已知问题

### Agent 不会自动调用 `./taskctl merge`

Agent 收到"合并"消息时，直接执行 `git merge main`，不会调用 `./taskctl merge`。需要通过 system_prompt 或 rules 引导 agent 使用 taskctl 完成合并操作。
