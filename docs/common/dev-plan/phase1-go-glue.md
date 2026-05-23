# Phase 1: Go 胶水层 — SSE 透传 + 基础路由

> 目标: curl 能走通 Go → AgentEnd SSE 流，前端可以对着真实 API 开发。
> 预估: 2 天

## 交付标准

```bash
# 创建 session
curl -X POST http://localhost:8080/api/sessions \
  -H "Content-Type: application/json" \
  -d '{"title": "test"}'
# → {"code": 200, "data": {"session_id": "xxx", ...}}

# 运行 task 并拿到 SSE 流
curl -N http://localhost:8080/api/sessions/{sid}/tasks/run \
  -H "Content-Type: application/json" \
  -d '{"message": "hello", "agent_type": "claude-code"}'
# → SSE: data: {"type": "text", "content": "...", ...}
```

## 要写的文件

### 1. AgentEnd HTTP Client

**文件**: `backend/pkg/agentend_client/client.go`

```
职责: 封装对 AgentEnd 的 HTTP 调用
依赖: 无 (标准库 net/http + bufio)

方法:
  - StreamAgent(req AgentRequest) (*http.Response, error)
      POST http://agentend-host:port/v1/agent/stream
      返回原始 response body 供上层逐行读 SSE

  - HealthCheck() error
      GET http://agentend-host:port/health
```

**文件**: `backend/pkg/agentend_client/types.go`

```
类型定义 (可从 contracts 生成的类型复用):
  - AgentRequest  { task_id, session_id, message, agent_type, workspace_path?, config? }
  - StreamEvent   { type, content, timestamp }
```

### 2. GORM 数据模型

**文件**: `backend/internal/model/session.go`

```go
type Session struct {
    ID        uint   `gorm:"primaryKey"`
    SessionID string `gorm:"uniqueIndex;size:36"`
    Title     string `gorm:"size:200"`
    Status    string `gorm:"size:20;default:active"` // active, archived
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

**文件**: `backend/internal/model/task.go`

```go
type Task struct {
    ID         uint   `gorm:"primaryKey"`
    TaskID     string `gorm:"uniqueIndex;size:36"`
    SessionID  string `gorm:"index;size:36"`
    AgentType  string `gorm:"size:30"` // claude-code, opencode, orchestrator
    Status     string `gorm:"size:20;default:pending"` // pending, running, completed, failed
    Message    string `gorm:"type:text"`
    Result     string `gorm:"type:text"`
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

> MVP 阶段不需要 User/Project/WorkspaceMeta/ArtifactMeta/EventLog，先跳过。

### 3. Handler 层

**文件**: `backend/internal/handler/session.go`

```
路由:
  POST   /api/sessions          创建 session
  GET    /api/sessions          列表
  GET    /api/sessions/:id      详情
  DELETE /api/sessions/:id      删除
```

**文件**: `backend/internal/handler/task.go`

```
核心路由:
  POST   /api/sessions/:sid/tasks/run   运行 agent (SSE 流式)

处理流程:
  1. 解析请求, 生成 task_id
  2. 写入 DB (task.status = running)
  3. 调 AgentEnd client.StreamAgent()
  4. 设置 SSE headers
  5. 逐行读 AgentEnd SSE body → 逐行写回前端
  6. 结束后更新 task.status = completed/failed
```

**文件**: `backend/internal/handler/agent.go`

```
路由:
  GET    /api/agents             返回可用 agent 类型列表
```

> 硬编码返回 `["claude-code", "opencode", "orchestrator"]` 即可。

### 4. 路由注册 + DB Migration

**文件**: `backend/cmd/server/main.go` (修改)

```
修改点:
  1. 注册路由组
  2. GORM AutoMigrate(Session, Task)
  3. 注入 AgentEnd client (读取配置中的 agentend 地址)
```

**文件**: `backend/configs/config.yaml` (修改)

```
新增:
  agentend:
    host: "http://localhost"
    port: 8000
```

## 文件清单

```
backend/
├── pkg/agentend_client/
│   ├── client.go              # 新增 ~80 行
│   └── types.go               # 新增 ~30 行
├── internal/
│   ├── model/
│   │   ├── session.go         # 新增 ~20 行
│   │   └── task.go            # 新增 ~25 行
│   ├── handler/
│   │   ├── session.go         # 新增 ~100 行
│   │   ├── task.go            # 新增 ~80 行
│   │   └── agent.go           # 新增 ~20 行
│   └── conf/
│       └── conf.go            # 修改: 加 agentend 配置
├── cmd/server/
│   └── main.go                # 修改: 加路由 + migration
└── configs/
    └── config.yaml            # 修改: 加 agentend 配置
```

**新增代码量: ~355 行，修改 ~50 行**

## 注意事项

- SSE 透传的关键: `c.Header("Content-Type", "text/event-stream")` + `c.Writer.Flush()`
- AgentEnd client 的 SSE 读取用 `bufio.Scanner` 逐行读
- Session/Task ID 用 `uuid.New().String()` 生成
- 先不接 JWT 认证（handler 不加 auth 中间件），Phase 3 再加
- 先不持久化 EventLog，直接透传
