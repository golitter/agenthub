# Orchestrator 规划测试

测试 Orchestrator 接收用户需求 → LLM 拆解任务 → 写入 shared/.agent/ 目录，验证各 Agent 可读到分配给自己的任务。

## 前置条件

```bash
# 1. 确保 .env 配置了 DS_API_KEY
cat agentend/.env
# 应包含: DS_MODEL, DS_BASE_URL, DS_API_KEY

# 2. 启动 agentend 服务（从 agentend/ 目录）
cd agentend && uv run python -m src.app.main

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

### 1. 创建 Claude Code workspace

```bash
curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "/Users/yanghao/Lab/vscode/gormlab",
    "task_id": "orch-test",
    "agent_name": "claude-code",
    "session_id": "cc-orch-test",
    "agent_type": "claude-code"
  }' | python3 -m json.tool
```

验证 worktree 创建：

```bash
ls /Users/yanghao/Lab/vscode/worktrees/orch-test/cc-orch-test/
# 应存在 .claude/ 目录

ls /Users/yanghao/Lab/vscode/worktrees/orch-test/shared/.agent/memory/
# 应存在 common/ 和 cc-orch-test/
```

### 2. 创建 OpenCode workspace

```bash
curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "/Users/yanghao/Lab/vscode/gormlab",
    "task_id": "orch-test",
    "agent_name": "opencode",
    "session_id": "oc-orch-test",
    "agent_type": "opencode"
  }' | python3 -m json.tool
```

验证两个 worktree 共存：

```bash
cd /Users/yanghao/Lab/vscode/gormlab
git branch
# 应看到:
#   agent/cc-orch-test/orch-test
#   agent/oc-orch-test/orch-test
#   task/orch-test
# * main
```

### 3. Orchestrator 规划

```bash
curl -s -X POST http://localhost:8001/v1/agent/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "task_id": "orch-test",
    "session_id": "orch-planner",
    "message": "用 Claude Code 写一个登录页面，用 OpenCode 审查代码质量",
    "agent_type": "orchestrator",
    "config": {
      "agents": [
        {"id": "claude-code", "name": "Claude Code", "capabilities": ["代码生成", "文件编辑"]},
        {"id": "opencode", "name": "OpenCode", "capabilities": ["代码审查", "安全检查"]}
      ],
      "shared_dir": "/Users/yanghao/Lab/vscode/worktrees/orch-test/shared/.agent"
    }
  }' | python3 -m json.tool
```

验证返回结果包含 overview 文本：

```bash
# 响应 content 字段应非空，包含规划概述
```

### 4. 验证 shared/.agent/ 产出

```bash
SHARED="/Users/yanghao/Lab/vscode/worktrees/orch-test/shared/.agent"

# config.yaml — 声明式任务索引
cat $SHARED/config.yaml
# 应包含:
#   task_id: orch-test
#   tasks:
#   - task_id: task-001
#     session_id: claude-code
#     file: plans/task-001.md
#   - task_id: task-002
#     session_id: opencode
#     file: plans/task-002.md

# plans/ — 整体规划 + 各任务文件（taskctl summary 可读）
ls $SHARED/plans/
# 应为: overview.md  task-001.md  task-002.md

cat $SHARED/plans/overview.md
# 应包含关于"登录页面"+"代码审查"的规划描述
```

### 5. 验证 Claude Code agent 可读取分配的任务

```bash
# 模拟 claude-code agent 从 config.yaml 中找到自己的任务
python3 -c "
import yaml
config = yaml.safe_load(open('/Users/yanghao/Lab/vscode/worktrees/orch-test/shared/.agent/config.yaml'))
my_tasks = [t for t in config['tasks'] if t['session_id'] == 'claude-code']
print(f'Claude Code 分配到 {len(my_tasks)} 个任务:')
for t in my_tasks:
    print(f'  - {t[\"task_id\"]}: {t[\"file\"]}')

# 读取任务详情
import os
base = '/Users/yanghao/Lab/vscode/worktrees/orch-test/shared/.agent'
for t in my_tasks:
    path = os.path.join(base, t['file'])
    print(f'\n=== {path} ===')
    print(open(path).read()[:200])
"
```

预期输出应包含：
- Claude Code 分配到 1 个任务
- task_id 为 task-001
- 任务内容关于"写登录页面"

### 6. 验证 OpenCode agent 可读取分配的任务

```bash
python3 -c "
import yaml
config = yaml.safe_load(open('/Users/yanghao/Lab/vscode/worktrees/orch-test/shared/.agent/config.yaml'))
my_tasks = [t for t in config['tasks'] if t['session_id'] == 'opencode']
print(f'OpenCode 分配到 {len(my_tasks)} 个任务:')
for t in my_tasks:
    print(f'  - {t[\"task_id\"]}: {t[\"file\"]}')

import os
base = '/Users/yanghao/Lab/vscode/worktrees/orch-test/shared/.agent'
for t in my_tasks:
    path = os.path.join(base, t['file'])
    print(f'\n=== {path} ===')
    print(open(path).read()[:200])
"
```

预期输出应包含：
- OpenCode 分配到 1 个任务
- task_id 为 task-002
- 任务内容关于"审查代码质量"

### 7. 验证 taskctl summary 可读

```bash
# Claude Code agent 在其 worktree 中执行 taskctl
/Users/yanghao/Lab/vscode/worktrees/orch-test/cc-orch-test/.claude/skills/taskctl/taskctl summary
```

预期输出应包含：
- `=== config.yaml ===` 段，包含 task_id 和 tasks 列表
- `=== plans/overview.md ===` 段，包含规划概述
- `=== plans/task-001.md ===` 段，包含 Claude Code 的任务详情
- `=== plans/task-002.md ===` 段，包含 OpenCode 的任务详情

```bash
# OpenCode agent 同样可执行
/Users/yanghao/Lab/vscode/worktrees/orch-test/oc-orch-test/.opencode/skills/taskctl/taskctl summary
# 输出应完全相同（共享同一份 shared/.agent/）
```

### 8. 完整清理

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

# 5. main 回到测试前状态
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
```
