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
ls <repo-path>
```

## 清理环境

```bash
# 清空持久化
echo '{}' > agentend/logs/session_mappings.json
echo '{}' > agentend/logs/workspaces.json

# 清理 worktree
cd <repo-path>
git worktree list | tail -n +2 | awk '{print $1}' | while read wt; do git worktree remove "$wt" --force; done
git branch | grep -v '^\* main$' | xargs git branch -D
rm -rf <worktrees-root>/
```

---

## 测试用例

### 1. 创建 Claude Code workspace

```bash
curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "<repo-path>",
    "task_id": "orch-test",
    "agent_name": "claude-code",
    "session_id": "cc-orch-test",
    "agent_type": "claude-code"
  }' | python3 -m json.tool
```

验证 worktree 创建：

```bash
ls <worktrees-root>/orch-test/cc-orch-test/
# 应存在 .claude/ 目录

ls <worktrees-root>/orch-test/shared/.agent/memory/
# 应存在 common/ 和 cc-orch-test/
```

### 2. 创建 OpenCode workspace

```bash
curl -s -X POST http://localhost:8001/v1/workspace/create \
  -H 'Content-Type: application/json' \
  -d '{
    "repo_path": "<repo-path>",
    "task_id": "orch-test",
    "agent_name": "opencode",
    "session_id": "oc-orch-test",
    "agent_type": "opencode"
  }' | python3 -m json.tool
```

验证两个 worktree 共存：

```bash
cd <repo-path>
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
        {"id": "claude-code", "type": "claude-code", "name": "Claude Code", "session_id": "cc-orch-test", "capabilities": ["代码生成", "文件编辑"]},
        {"id": "opencode", "type": "opencode", "name": "OpenCode", "session_id": "oc-orch-test", "capabilities": ["代码审查", "安全检查"]}
      ],
      "shared_dir": "<worktrees-root>/orch-test/shared/.agent"
    }
  }' | python3 -m json.tool
```

验证返回结果包含 overview 文本：

```bash
# 响应 content 字段应非空，包含规划概述
```

### 4. 验证 shared/.agent/ 产出

```bash
SHARED="<worktrees-root>/orch-test/shared/.agent"

# config.yaml — 声明式任务索引
cat $SHARED/config.yaml
# 应包含:
#   task_id: orch-test
#   overview_file: plans/overview.md
#   tasks:
#   - task_id: task-001
#     session_id: cc-orch-test
#     agent: claude-code
#     agent_type: claude-code
#     file: plans/task-001.md
#   - task_id: task-002
#     session_id: oc-orch-test
#     agent: opencode
#     agent_type: opencode
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
config = yaml.safe_load(open('<worktrees-root>/orch-test/shared/.agent/config.yaml'))
my_tasks = [t for t in config['tasks'] if t['session_id'] == 'cc-orch-test']
print(f'Claude Code 分配到 {len(my_tasks)} 个任务:')
for t in my_tasks:
    print(f'  - {t[\"task_id\"]}: {t[\"file\"]}')

# 读取任务详情
import os
base = '<worktrees-root>/orch-test/shared/.agent'
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
config = yaml.safe_load(open('<worktrees-root>/orch-test/shared/.agent/config.yaml'))
my_tasks = [t for t in config['tasks'] if t['session_id'] == 'oc-orch-test']
print(f'OpenCode 分配到 {len(my_tasks)} 个任务:')
for t in my_tasks:
    print(f'  - {t[\"task_id\"]}: {t[\"file\"]}')

import os
base = '<worktrees-root>/orch-test/shared/.agent'
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
<worktrees-root>/orch-test/cc-orch-test/.claude/skills/taskctl/taskctl summary
```

预期输出应包含：
- `=== config.yaml ===` 段，包含 task_id 和完整 tasks 列表（两个任务均可见）
- `=== plans/overview.md ===` 段，包含规划概述
- `=== plans/task-001.md ===` 段，包含 Claude Code 自己的任务详情

注意：`summary` 仅展示当前 agent 自己的 plan 文件（按 session_id 过滤），不会展示其他 agent 的 task 文件。

```bash
# OpenCode agent 同样可执行
<worktrees-root>/orch-test/oc-orch-test/.opencode/skills/taskctl/taskctl summary
# config.yaml 与 overview.md 段相同（共享同一份 shared/.agent/），
# 但 plan 文件段不同：仅展示 task-002.md（OpenCode 自己的任务）
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
cd <repo-path>
git worktree list | tail -n +2 | awk '{print $1}' | while read wt; do git worktree remove "$wt" --force; done

# 4. 删除除 main 外的所有分支
git branch | grep -v '^\* main$' | xargs git branch -D

# 5. main 回到测试前状态
git reset --hard origin/main

# 6. 清理 worktrees 目录
rm -rf <worktrees-root>/
```

验证清理完成：

```bash
cd <repo-path>
git branch
# 应仅有: * main

git worktree list
# 应仅有主 worktree

cat agentend/logs/workspaces.json
# 应为: {}
```
