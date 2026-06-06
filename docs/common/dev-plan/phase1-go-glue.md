# Phase 1: Go 胶水层 — SSE 透传 + 基础路由

> 目标: curl 能走通 Go → AgentEnd SSE 流，前端可以对着真实 API 开发。
> 预估: 2 天
> 状态: ✅ 已完成

## 交付标准

```bash
# 创建 session
curl -X POST http://localhost:8080/api/sessions \
  -H "Content-Type: application/json" \
  -d '{"title": "test"}'
# → {"code": 200, "data": {"session_id": "xxx", ...}}

# 运行 task 并拿到 SSE 流
curl -N http://localhost:8080/api/tasks/{taskId}/run \
  -H "Content-Type: application/json" \
  -d '{"message": "hello", "agent_type": "claude-code"}'
# → SSE: data: {"type": "text", "content": "...", ...}
```

## 实际实现

### 已完成文件

```
backend/
├── pkg/agentend_client/
│   ├── client.go              # ✅ AgentEnd HTTP 客户端（StreamAgent + HealthCheck + ValidateRepoPath + GetResources）
│   └── types.go               # ✅ 请求/响应类型定义
├── internal/
│   ├── model/
│   │   ├── session.go         # ✅ Session 模型（session_id, task_id, agent_type, agent_name, avatar_url, status, soul_md）
│   │   ├── task.go            # ✅ Task 模型（task_id, title, repo_path, status）
│   │   ├── message.go         # ✅ Message 模型（message_id, task_id, session_id, role, content, status, last_seq, agent_type, agent_name）
│   │   ├── session_agent.go   # ✅ SessionAgent 模型（多 Agent 绑定）
│   │   ├── diff_snapshot.go   # ✅ DiffSnapshot 模型（代码差异快照）
│   │   └── admin_setting.go   # ✅ AdminSetting 模型
│   ├── handler/               # （已重构为 controller + service 分层）
│   ├── controller/
│   │   ├── controller.go      # ✅ 控制器接口
│   │   └── impl/
│   │       ├── task_controller.go        # ✅ Task CRUD + Run（SSE 流式）+ 多 Agent 支持
│   │       ├── session_controller.go     # ✅ Session 状态更新（inactive）
│   │       ├── message_controller.go     # ✅ 消息列表 + 分页
│   │       ├── agent_controller.go       # ✅ Agent 类型列表
│   │       ├── agent_profile_controller.go # ✅ Agent Profile / Detail / Soul MD
│   │       ├── avatar_controller.go      # ✅ 头像上传（七牛云）
│   │       ├── stream_controller.go      # ✅ SSE 流服务（MySQL 历史 + Redis 填补 + RuntimeHub 实时）
│   │       ├── workspace_controller.go   # ✅ Workspace 代理
│   │       ├── diff_snapshot_controller.go # ✅ Diff 快照管理
│   │       ├── admin_controller.go       # ✅ Admin 认证 + 统计 + 资源监控
│   │       ├── announcement_controller.go # ✅ 公告 CRUD
│   │       └── ...
│   ├── service/
│   │   ├── service.go         # ✅ 服务接口定义
│   │   └── impl/
│   │       ├── task_service.go            # ✅ Task 业务逻辑
│   │       ├── message_service.go         # ✅ 消息业务逻辑
│   │       ├── stream_service.go          # ✅ SSE 流服务逻辑
│   │       ├── group_chat_window.go       # ✅ 群聊窗口查询
│   │       └── ...
│   ├── stream/
│   │   ├── hub.go             # ✅ RuntimeHub 内存 Pub/Sub（~10ms 延迟）
│   │   ├── writer.go          # ✅ StreamWriter（Agent 切换、Redis 双写、文本批处理）
│   │   └── ...
│   ├── middleware/
│   │   ├── auth.go            # ✅ JWT 认证
│   │   └── admin_auth.go      # ✅ Admin JWT 认证
│   └── service/
│       └── qiniu.go           # ✅ 七牛云文件上传
├── cmd/server/
│   └── main.go                # ✅ 路由注册 + DB Migration + Redis + 依赖注入
└── configs/
    └── config.yaml            # ✅ agentend / mysql / redis / qiniu 配置
```

### 超出计划的实现

| 功能 | 说明 |
|------|------|
| Redis Stream 缓冲 | SSE 消息双写：RuntimeHub 即时推送 + Redis Stream 持久化 |
| MySQL 消息持久化 | Message 模型 + 批量写入，支持历史回放 |
| RuntimeHub | 内存 Pub/Sub，~10ms 推送延迟 |
| StreamWriter | Agent 切换时自动创建子消息，支持文本批处理 |
| JWT Admin 认证 | 管理面板认证体系 |
| 七牛云头像上传 | Agent 头像存储 |
| Diff Snapshot | 代码差异快照管理 |
| Workspace 代理 | 代理 AgentEnd 文件操作、Git diff/commit/revert |
| Agent Profile | Soul MD 注入、Profile 查询 |
| Admin Dashboard | 统计、资源监控、工作区管理、服务健康 |

## 技术决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| SSE 实现 | Gin + 自定义 StreamWriter | 需要 POST body + 流式控制 |
| 消息持久化 | MySQL + Redis Stream 双写 | MySQL 历史回放 + Redis 实时填补 |
| 实时推送 | RuntimeHub（内存 Pub/Sub） | ~10ms 延迟，优于轮询 |
| 认证 | JWT | 管理面板需要认证，API 层暂不需要 |
| 文件上传 | 七牛云 | 避免本地文件管理 |
| ORM | GORM | Go 生态主流，AutoMigrate 方便 |

## 注意事项

- SSE 透传的关键: `c.Header("Content-Type", "text/event-stream")` + `c.Writer.Flush()`
- AgentEnd client 的 SSE 读取用 `bufio.Scanner` 逐行读
- Session/Task ID 用 `uuid.New().String()` 生成
- 先不接 JWT 认证（handler 不加 auth 中间件），Phase 3 再加
- 先不持久化 EventLog，直接透传
