# inactive-cleanup 测试手册

## 前置条件

三端服务全部启动：

```bash
make run-backend    # localhost:8080
make run-agentend   # localhost:8001
make run-frontend   # localhost:5173
```

MySQL 中需有 `sessions` 表（backend AutoMigrate 自动创建）。

## 1. Backend — PATCH session status API

### 1.1 正常停用 session

先通过已有流程创建一个 task 和 session（调用 `POST /api/tasks/:taskId/run`），然后：

```bash
curl -s -X PATCH http://localhost:8080/api/sessions/<session_id> \
  -H 'Content-Type: application/json' \
  -d '{"status": "inactive"}' | python3 -m json.tool
```

预期：`{"code": 0, "data": null}`

验证 DB：

```bash
mysql -u root -p123456 agenthub -e "SELECT session_id, status FROM sessions WHERE session_id='<session_id>'"
```

status 应为 `inactive`。

### 1.2 session 不存在 → 404

```bash
curl -s -X PATCH http://localhost:8080/api/sessions/non-existent \
  -H 'Content-Type: application/json' \
  -d '{"status": "inactive"}' | python3 -m json.tool
```

预期：`{"code": 404, "msg": "session not found"}`

### 1.3 无效 status 值 → 400

```bash
curl -s -X PATCH http://localhost:8080/api/sessions/<session_id> \
  -H 'Content-Type: application/json' \
  -d '{"status": "running"}' | python3 -m json.tool
```

预期：`{"code": 400, "msg": "status must be \"inactive\""}`

### 1.4 缺少 status 字段 → 400

```bash
curl -s -X PATCH http://localhost:8080/api/sessions/<session_id> \
  -H 'Content-Type: application/json' \
  -d '{}' | python3 -m json.tool
```

预期：`{"code": 400, "msg": "status is required"}`

## 2. Agentend — inactive cleanup 后台任务

### 2.1 验证启动日志

agentend 启动后，检查日志中无 TTL 相关输出，应出现 DB 连接和 cleanup 任务启动。

### 2.2 验证 shutdown 不清理资源

```bash
make stop-agentend
```

检查：
- `agentend/logs/workspaces.json` 中 workspace 仍为 ACTIVE（未被清理）
- worktree 目录仍然存在

### 2.3 手动触发清理验证

将 `config.yaml` 的 `cleanup_interval` 临时改为较小值（如 30 秒）用于测试：

```yaml
workspace:
  cleanup_interval: 30  # 测试用，改回 7200
```

1. 通过 backend 创建 task + session + run agent
2. 确认 agentend 出现 workspace（`cat agentend/logs/workspaces.json`）
3. 调用 PATCH 停用 session
4. 等待 cleanup_interval 秒
5. 检查 agentend 日志输出：

```
Inactive cleanup: scanned X sessions, cleaned Y sessions, cleaned Z tasks
```

6. 验证 worktree 已被删除、workspace 状态变为 CLEANED

测试完毕后改回 `7200`。

### 2.4 Task 级清理验证

1. 创建一个 task，为其创建两个 session
2. 将两个 session 都停用
3. 等待 cleanup 周期
4. 验证 task 分支也被删除（`git branch` 不再包含 `task/<task_id>`）

### 2.5 混合状态不触发 task 级清理

1. 创建一个 task，为其创建两个 session
2. 只停用其中一个（另一个保持 completed）
3. 等待 cleanup 周期
4. 验证：只清理了 inactive session 的 worktree，task 分支未被删除

## 3. Frontend — 停用操作（UI 尚未实现）

> **状态**：Backend `PATCH /api/sessions/:sessionId` 已可用，但前端的「停用」按钮与会话状态 badge UI 尚未落地（见 [../common/dev-plan/TODO.md](../common/dev-plan/TODO.md) 前端 P2「Agent 断连重连 / Agent 超时状态」相关条目）。本节为后端能力可用后的前端验收预案，待 UI 实现后补全。

当前可用的人工验证路径（绕过未实现的 UI，直接调 API）：

### 前置：通过 API 创建测试数据

```bash
# 创建 task
curl -s -X POST http://localhost:8080/api/tasks \
  -H 'Content-Type: application/json' \
  -d '{"title": "test-task", "repo_path": "/path/to/repo"}' | python3 -m json.tool
# 记下返回的 task_id
```

### 页面导航

1. 访问 `http://localhost:5173` → 主页面（左侧 ConversationList 会话列表 + 右侧聊天区）
2. 左侧 ConversationList 展示所有会话，每个会话对应一个 task
3. 点击某个会话 → 右侧聊天区展示该会话的消息

### 当前临时验证

前端目前没有「停用」按钮，也没有 inactive 专属样式（灰色 badge）。如需停用某个 session，使用本文档第 1 节的 `PATCH /api/sessions/<session_id>` 接口手动停用，再按第 2 节验证 agentend 后台清理。

### UI 实现后的验收点（待补全）

- 每个 session 行右侧出现「停用」按钮，点击后按钮 loading → 列表刷新
- 停用后该 session 的 status badge 变为 inactive 样式（与正常状态区分）
- 「停用」按钮在 inactive session 上消失
